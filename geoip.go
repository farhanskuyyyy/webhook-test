package main

import (
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/oschwald/maxminddb-golang"
)

type geoLocator struct {
	mu     sync.RWMutex
	reader *maxminddb.Reader
	status string
}

type geoRecord struct {
	Country struct {
		Code  string            `maxminddb:"iso_code"`
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"country"`
	City struct {
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"city"`
	Subdivisions []struct {
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"subdivisions"`
}

func (g *geoLocator) lookup(raw string) fiber.Map {
	ip := net.ParseIP(raw)
	if ip == nil {
		return fiber.Map{"status": "unknown", "message": "IP pengirim belum direkam dengan valid."}
	}
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || net.ParseIP("100.64.0.0").Equal(ip.Mask(net.CIDRMask(10, 32))) {
		return fiber.Map{"status": "private", "message": "IP lokal/private; lokasi publik tidak tersedia."}
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	if g.reader == nil {
		message := "Database GeoIP belum tersedia."
		if g.status == "loading" {
			message = "Database GeoIP sedang diunduh. Buka ulang request setelah selesai."
		}
		return fiber.Map{"status": envStatus(g.status), "message": message}
	}
	var record geoRecord
	if err := g.reader.Lookup(ip, &record); err != nil {
		return fiber.Map{"status": "unavailable", "message": "Lookup GeoIP gagal."}
	}
	if record.Country.Code == "" && record.Country.Names["en"] == "" {
		return fiber.Map{"status": "not_found", "message": "Lokasi IP tidak ditemukan di database."}
	}
	region := ""
	if len(record.Subdivisions) > 0 {
		region = record.Subdivisions[0].Names["en"]
	}
	return fiber.Map{"status": "ok", "country": record.Country.Names["en"], "country_code": record.Country.Code, "region": region, "city": record.City.Names["en"], "source": "DB-IP Lite", "database_date": time.Unix(int64(g.reader.Metadata.BuildEpoch), 0).UTC().Format("2006-01-02")}
}

func envStatus(status string) string {
	if status == "loading" {
		return status
	}
	return "unavailable"
}

func startGeo(path string, autoDownload bool) *geoLocator {
	g := &geoLocator{status: "unavailable"}
	if reader, err := maxminddb.Open(path); err == nil {
		g.reader = reader
		g.status = "ready"
		return g
	}
	if !autoDownload {
		return g
	}
	g.status = "loading"
	go func() {
		reader, err := downloadGeo(path, time.Now().UTC())
		g.mu.Lock()
		defer g.mu.Unlock()
		if err != nil {
			g.status = "unavailable"
			log.Printf("GeoIP unavailable: %v (webhooks remain active)", err)
			return
		}
		g.reader = reader
		g.status = "ready"
		log.Print("GeoIP database ready")
	}()
	return g
}

func downloadGeo(path string, month time.Time) (*maxminddb.Reader, error) {
	month = time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.UTC)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	// A new month's database may not be published yet; fall back one release.
	var lastErr error
	for i := 0; i < 2; i++ {
		release := month.AddDate(0, -i, 0).Format("2006-01")
		url := "https://download.db-ip.com/free/dbip-city-lite-" + release + ".mmdb.gz"
		reader, err := downloadGeoURL(path, url)
		if err == nil {
			return reader, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func downloadGeoURL(path, url string) (*maxminddb.Reader, error) {
	client := &http.Client{Timeout: 5 * time.Minute}
	response, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GeoIP download HTTP %d", response.StatusCode)
	}
	gz, err := gzip.NewReader(io.LimitReader(response.Body, 256<<20))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	temp, err := os.CreateTemp(filepath.Dir(path), ".geoip-*.mmdb")
	if err != nil {
		return nil, err
	}
	defer os.Remove(temp.Name())
	n, copyErr := io.Copy(temp, io.LimitReader(gz, (512<<20)+1))
	closeErr := temp.Close()
	if copyErr != nil {
		return nil, copyErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if n > 512<<20 {
		return nil, fmt.Errorf("GeoIP database too large")
	}
	reader, err := maxminddb.Open(temp.Name())
	if err != nil {
		return nil, err
	}
	reader.Close()
	if err := os.Rename(temp.Name(), path); err != nil {
		return nil, err
	}
	return maxminddb.Open(path)
}

func trustedProxies() []string {
	var proxies []string
	for _, value := range strings.Split(os.Getenv("TRUSTED_PROXIES"), ",") {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if net.ParseIP(value) == nil {
			if _, _, err := net.ParseCIDR(value); err != nil {
				panic("TRUSTED_PROXIES must contain IP addresses or CIDRs")
			}
		}
		proxies = append(proxies, value)
	}
	return proxies
}

// Walk from the socket peer toward the client and stop at the first untrusted hop.
func clientIP(c *fiber.Ctx, proxies []string) string {
	return resolveClientIP(c.Context().RemoteIP().String(), c.Get("X-Forwarded-For"), proxies)
}

func resolveClientIP(peer, forwarded string, proxies []string) string {
	trusted := func(raw string) bool {
		ip := net.ParseIP(raw)
		if ip == nil {
			return false
		}
		for _, proxy := range proxies {
			if networkIP := net.ParseIP(proxy); networkIP != nil {
				if ip.Equal(networkIP) {
					return true
				}
			} else if _, network, err := net.ParseCIDR(proxy); err == nil && network.Contains(ip) {
				return true
			}
		}
		return false
	}
	if !trusted(peer) {
		return peer
	}
	hops := strings.Split(forwarded, ",")
	for i := len(hops) - 1; i >= 0; i-- {
		candidate := strings.TrimSpace(hops[i])
		ip := net.ParseIP(candidate)
		if ip == nil {
			return peer
		}
		peer = ip.String()
		if !trusted(peer) {
			return peer
		}
	}
	return peer
}
