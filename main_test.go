package main

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/gofiber/fiber/v2"
	"io"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func call(t *testing.T, app *fiber.App, method, path string, body []byte, want int) []byte {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("X-Test", "callback")
	res, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != want {
		t.Fatalf("%s %s: status %d want %d: %s", method, path, res.StatusCode, want, data)
	}
	return data
}
func TestCallbackLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := openDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { db.Close() }()
	app := newApp(db)
	var bin struct{ Code string }
	json.Unmarshal(call(t, app, "POST", "/api/bins", nil, 201), &bin)
	if !codePattern.MatchString(bin.Code) {
		t.Fatal("invalid generated code")
	}
	payload := []byte{0, 1, 255, 10, 60, 62}
	call(t, app, "POST", "/"+bin.Code+"/payment?a=1&a=2", payload, 200)
	var list []struct{ ID int64 }
	json.Unmarshal(call(t, app, "GET", "/api/bins/"+bin.Code+"/requests", nil, 200), &list)
	if len(list) != 1 {
		t.Fatal("request not captured")
	}
	detailPath := fmt.Sprintf("/api/bins/%s/requests/%d", bin.Code, list[0].ID)
	var detail struct {
		BodyBase64 string `json:"body_base64"`
		Query      string
		Headers    map[string][]string
		Metadata   map[string]string
	}
	json.Unmarshal(call(t, app, "GET", detailPath, nil, 200), &detail)
	if detail.BodyBase64 != base64.StdEncoding.EncodeToString(payload) || detail.Query != "a=1&a=2" || detail.Headers["X-Test"][0] != "callback" {
		t.Fatalf("capture mismatch: %+v", detail)
	}
	if detail.Metadata["request_uri"] != "/"+bin.Code+"/payment?a=1&a=2" || detail.Metadata["http_version"] != "HTTP/1.1" || detail.Metadata["host"] != "example.com" {
		t.Fatalf("metadata mismatch: %+v", detail.Metadata)
	}
	db.Close()
	db, err = openDB(path)
	if err != nil {
		t.Fatal(err)
	}
	app = newApp(db)
	call(t, app, "GET", detailPath, nil, 200)
	var other struct{ Code string }
	json.Unmarshal(call(t, app, "POST", "/api/bins", nil, 201), &other)
	call(t, app, "GET", fmt.Sprintf("/api/bins/%s/requests/%d", other.Code, list[0].ID), nil, 404)
	for _, method := range []string{"GET", "PUT", "PATCH", "DELETE", "OPTIONS", "HEAD"} {
		call(t, app, method, "/"+bin.Code, nil, 200)
	}
	call(t, app, "GET", "/api/bins/"+bin.Code+"/requests?before=1", nil, 200)
	call(t, app, "DELETE", "/api/bins/"+bin.Code+"/requests", nil, 204)
	call(t, app, "GET", detailPath, nil, 404)
	call(t, app, "POST", "/01234567890123456789012345678901", nil, 404)
}

func TestLegacyDatabaseMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE bins (code TEXT PRIMARY KEY, created_at TEXT NOT NULL);
CREATE TABLE requests (id INTEGER PRIMARY KEY AUTOINCREMENT, code TEXT NOT NULL REFERENCES bins(code), method TEXT NOT NULL, path TEXT NOT NULL, query TEXT NOT NULL, headers TEXT NOT NULL, body BLOB NOT NULL, remote TEXT NOT NULL, created_at TEXT NOT NULL);
INSERT INTO bins VALUES ('01234567890123456789012345678901','2026-01-01T00:00:00Z');
INSERT INTO requests(code,method,path,query,headers,body,remote,created_at) VALUES ('01234567890123456789012345678901','POST','/legacy','','{}',X'616263','127.0.0.1','2026-01-01T00:00:00Z');`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		db, err = openDB(path)
		if err != nil {
			t.Fatal(err)
		}
		data := call(t, newApp(db), "GET", "/api/bins/01234567890123456789012345678901/requests/1", nil, 200)
		var result struct {
			Body     string
			Metadata map[string]string
		}
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatal(err)
		}
		if result.Body != "abc" || len(result.Metadata) != 0 {
			t.Fatalf("legacy data changed: %s", data)
		}
		db.Close()
	}
}
func TestPayloadLimitAndHTML(t *testing.T) {
	t.Setenv("MAX_BODY_BYTES", "4")
	db, err := openDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	app := newApp(db)
	call(t, app, "GET", "/", nil, 200)
	var bin struct{ Code string }
	json.Unmarshal(call(t, app, "POST", "/api/bins", nil, 201), &bin)
	_, err = app.Test(httptest.NewRequest("POST", "/"+bin.Code, strings.NewReader("12345")), -1)
	if err == nil || !strings.Contains(err.Error(), "body size exceeds") {
		t.Fatalf("expected oversized payload rejection, got %v", err)
	}
}

func TestHTMLTextEncoding(t *testing.T) {
	if !utf8.Valid(page) {
		t.Fatal("HTML must be valid UTF-8")
	}
	for _, broken := range []string{"\u00c2\u00b7", "\u00e2\u20ac", "\u00ef\u00bb\u00bf", "\ufffd"} {
		if bytes.Contains(page, []byte(broken)) {
			t.Fatalf("HTML contains broken encoding: %q", broken)
		}
	}
}
