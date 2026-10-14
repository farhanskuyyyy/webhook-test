package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/gofiber/fiber/v2"
	"io"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
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
	}
	json.Unmarshal(call(t, app, "GET", detailPath, nil, 200), &detail)
	if detail.BodyBase64 != base64.StdEncoding.EncodeToString(payload) || detail.Query != "a=1&a=2" || detail.Headers["X-Test"][0] != "callback" {
		t.Fatalf("capture mismatch: %+v", detail)
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
