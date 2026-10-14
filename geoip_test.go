package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/maxmind/mmdbwriter"
	"github.com/maxmind/mmdbwriter/mmdbtype"
)

func geoFixture(t *testing.T) []byte {
	t.Helper()
	tree, err := mmdbwriter.New(mmdbwriter.Options{DatabaseType: "DBIP-City-Lite", IPVersion: 6, RecordSize: 24})
	if err != nil {
		t.Fatal(err)
	}
	_, network, _ := net.ParseCIDR("8.8.8.0/24")
	record := mmdbtype.Map{
		"country":      mmdbtype.Map{"iso_code": mmdbtype.String("ID"), "names": mmdbtype.Map{"en": mmdbtype.String("Indonesia")}},
		"city":         mmdbtype.Map{"names": mmdbtype.Map{"en": mmdbtype.String("Test City")}},
		"subdivisions": mmdbtype.Slice{mmdbtype.Map{"names": mmdbtype.Map{"en": mmdbtype.String("Test Region")}}},
	}
	if err := tree.Insert(network, record); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if _, err := tree.WriteTo(&output); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestGeoLocalLookupAndDownload(t *testing.T) {
	data := geoFixture(t)
	path := filepath.Join(t.TempDir(), "city.mmdb")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	locator := startGeo(path, false)
	if locator.reader == nil {
		t.Fatal("failed to open fixture")
	}
	defer locator.reader.Close()
	location := locator.lookup("8.8.8.8")
	if location["country"] != "Indonesia" || location["city"] != "Test City" || location["region"] != "Test Region" {
		t.Fatalf("unexpected location: %+v", location)
	}
	if locator.lookup("1.1.1.1")["status"] != "not_found" {
		t.Fatal("unknown IP should not invent a location")
	}
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "192.168.0.1", "100.64.0.1", "::1", "fc00::1", "169.254.0.1"} {
		if locator.lookup(ip)["status"] != "private" {
			t.Fatalf("private IP lookup: %s", ip)
		}
	}
	if (&geoLocator{}).lookup("8.8.8.8")["status"] != "unavailable" {
		t.Fatal("missing database should degrade gracefully")
	}
	if (&geoLocator{status: "loading"}).lookup("8.8.8.8")["status"] != "loading" {
		t.Fatal("download state must be visible")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { gz := gzip.NewWriter(w); gz.Write(data); gz.Close() }))
	defer server.Close()
	downloaded, err := downloadGeoURL(filepath.Join(t.TempDir(), "download.mmdb"), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	downloaded.Close()
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("not a gzip")) }))
	defer broken.Close()
	if _, err := downloadGeoURL(filepath.Join(t.TempDir(), "bad.mmdb"), broken.URL); err == nil {
		t.Fatal("corrupt download accepted")
	}
}

func TestTrustedProxyChain(t *testing.T) {
	cases := []struct {
		peer, forwarded string
		proxies         []string
		want            string
	}{
		{"8.8.8.8", "1.1.1.1", nil, "8.8.8.8"},
		{"8.8.8.8", "1.1.1.1", []string{"10.0.0.0/8"}, "8.8.8.8"},
		{"10.0.0.2", "8.8.8.8", []string{"10.0.0.0/8"}, "8.8.8.8"},
		{"10.0.0.2", "1.1.1.1, 8.8.8.8, 10.0.0.3", []string{"10.0.0.0/8"}, "8.8.8.8"},
		{"10.0.0.2", "garbage", []string{"10.0.0.0/8"}, "10.0.0.2"},
		{"10.0.0.2", "", []string{"10.0.0.0/8"}, "10.0.0.2"},
		{"fd00::1", "2001:4860:4860::8888", []string{"fd00::/8"}, "2001:4860:4860::8888"},
	}
	for _, tc := range cases {
		if got := resolveClientIP(tc.peer, tc.forwarded, tc.proxies); got != tc.want {
			t.Fatalf("%+v got %s", tc, got)
		}
	}
}

func TestGeoDetailAPI(t *testing.T) {
	t.Setenv("TRUSTED_PROXIES", "0.0.0.0/0")
	path := filepath.Join(t.TempDir(), "city.mmdb")
	if err := os.WriteFile(path, geoFixture(t), 0600); err != nil {
		t.Fatal(err)
	}
	locator := startGeo(path, false)
	defer locator.reader.Close()
	db, err := openDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	app := newAppWithGeo(db, locator)
	var bin struct{ Code string }
	json.Unmarshal(call(t, app, "POST", "/api/bins", nil, 201), &bin)
	req := httptest.NewRequest("POST", "/"+bin.Code, nil)
	req.Header.Set("X-Forwarded-For", "8.8.8.8")
	response, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	var detail struct {
		Location map[string]string
		Metadata map[string]string
	}
	if err := json.Unmarshal(call(t, app, "GET", "/api/bins/"+bin.Code+"/requests/1", nil, 200), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Location["city"] != "Test City" || detail.Metadata["client_ip"] != "8.8.8.8" {
		t.Fatalf("wrong GeoIP detail: %+v", detail)
	}
}
