package main

import (
	"crypto/rand"
	"database/sql"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"
	_ "modernc.org/sqlite"
)

//go:embed web/index.html
var page []byte

var codePattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func openDB(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000; PRAGMA foreign_keys=ON;
 CREATE TABLE IF NOT EXISTS bins (code TEXT PRIMARY KEY, created_at TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS requests (id INTEGER PRIMARY KEY AUTOINCREMENT, code TEXT NOT NULL REFERENCES bins(code), method TEXT NOT NULL, path TEXT NOT NULL, query TEXT NOT NULL, headers TEXT NOT NULL, body BLOB NOT NULL, remote TEXT NOT NULL, created_at TEXT NOT NULL);
 CREATE INDEX IF NOT EXISTS requests_code_id ON requests(code, id DESC);`)
	if err != nil {
		db.Close()
		return nil, err
	}
	// Older databases keep their requests; metadata is added in place.
	rows, err := db.Query("PRAGMA table_info(requests)")
	if err != nil {
		db.Close()
		return nil, err
	}
	hasMetadata := false
	for rows.Next() {
		var cid, notNull, pk int
		var name, kind string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &pk); err != nil {
			rows.Close()
			db.Close()
			return nil, err
		}
		if name == "metadata" {
			hasMetadata = true
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		db.Close()
		return nil, err
	}
	if !hasMetadata {
		if _, err := db.Exec("ALTER TABLE requests ADD COLUMN metadata TEXT NOT NULL DEFAULT '{}'"); err != nil {
			db.Close()
			return nil, err
		}
	}
	return db, nil
}

func newApp(db *sql.DB) *fiber.App {
	return newAppWithGeo(db, &geoLocator{})
}

func newAppWithGeo(db *sql.DB, geo *geoLocator) *fiber.App {
	proxies := trustedProxies()
	limit, err := strconv.Atoi(env("MAX_BODY_BYTES", "10485760"))
	if err != nil || limit < 1 {
		panic("MAX_BODY_BYTES must be positive")
	}
	app := fiber.New(fiber.Config{BodyLimit: limit, DisableStartupMessage: true, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, ErrorHandler: func(c *fiber.Ctx, err error) error {
		status := 500
		var fe *fiber.Error
		message := "Internal server error"
		if errors.As(err, &fe) {
			status = fe.Code
			message = fe.Message
		} else {
			log.Printf("request error: %v", err)
		}
		return c.Status(status).JSON(fiber.Map{"error": message})
	}})
	app.Use(recover.New())
	app.Use(func(c *fiber.Ctx) error {
		c.Set("X-Content-Type-Options", "nosniff")
		c.Set("Cache-Control", "no-store")
		return c.Next()
	})
	exists := func(c *fiber.Ctx) error {
		code := c.Params("code")
		if !codePattern.MatchString(code) {
			return fiber.ErrNotFound
		}
		var found string
		err := db.QueryRow("SELECT code FROM bins WHERE code=?", code).Scan(&found)
		if errors.Is(err, sql.ErrNoRows) {
			return fiber.ErrNotFound
		}
		if err != nil {
			return err
		}
		return c.Next()
	}
	app.Get("/healthz", func(c *fiber.Ctx) error {
		if err := db.Ping(); err != nil {
			return err
		}
		return c.JSON(fiber.Map{"status": "ok"})
	})
	app.Get("/", func(c *fiber.Ctx) error {
		c.Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'")
		return c.Type("html").Send(page)
	})
	app.Post("/api/bins", func(c *fiber.Ctx) error {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			return err
		}
		code := hex.EncodeToString(b)
		if _, err := db.Exec("INSERT INTO bins VALUES (?,?)", code, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
		return c.Status(201).JSON(fiber.Map{"code": code})
	})
	app.Get("/api/bins/:code/requests", exists, func(c *fiber.Ctx) error {
		before, _ := strconv.ParseInt(c.Query("before"), 10, 64)
		if before <= 0 {
			before = 9223372036854775807
		}
		rows, err := db.Query("SELECT id, method, path, length(body), created_at FROM requests WHERE code=? AND id<? ORDER BY id DESC LIMIT 100", c.Params("code"), before)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []fiber.Map{}
		for rows.Next() {
			var id, size int64
			var method, path, at string
			if err := rows.Scan(&id, &method, &path, &size, &at); err != nil {
				return err
			}
			items = append(items, fiber.Map{"id": id, "method": method, "path": path, "size": size, "created_at": at})
		}
		if err := rows.Err(); err != nil {
			return err
		}
		return c.JSON(items)
	})
	app.Get("/api/bins/:code/requests/:id", exists, func(c *fiber.Ctx) error {
		var id int64
		var method, path, query, headers, remote, at, metadata string
		var body []byte
		err := db.QueryRow("SELECT id,method,path,query,headers,body,remote,created_at,metadata FROM requests WHERE code=? AND id=?", c.Params("code"), c.Params("id")).Scan(&id, &method, &path, &query, &headers, &body, &remote, &at, &metadata)
		if errors.Is(err, sql.ErrNoRows) {
			return fiber.ErrNotFound
		}
		if err != nil {
			return err
		}
		var meta map[string]string
		if err := json.Unmarshal([]byte(metadata), &meta); err != nil {
			return err
		}
		ip := meta["client_ip"]
		if ip == "" {
			ip = remote
		}
		return c.JSON(fiber.Map{"id": id, "method": method, "path": path, "query": query, "headers": json.RawMessage(headers), "metadata": json.RawMessage(metadata), "location": geo.lookup(ip), "body": string(body), "body_base64": base64.StdEncoding.EncodeToString(body), "size": len(body), "remote": remote, "created_at": at})
	})
	app.Delete("/api/bins/:code/requests", exists, func(c *fiber.Ctx) error {
		if _, err := db.Exec("DELETE FROM requests WHERE code=?", c.Params("code")); err != nil {
			return err
		}
		return c.SendStatus(204)
	})
	capture := func(c *fiber.Ctx) error {
		headers := map[string][]string{}
		c.Request().Header.VisitAll(func(k, v []byte) { key := string(k); headers[key] = append(headers[key], string(v)) })
		h, err := json.Marshal(headers)
		if err != nil {
			return err
		}
		body := c.Body()
		if body == nil {
			body = []byte{}
		}
		metadata, err := json.Marshal(fiber.Map{"host": string(c.Request().Host()), "scheme": c.Protocol(), "request_uri": string(c.Request().RequestURI()), "http_version": string(c.Request().Header.Protocol()), "remote_addr": c.Context().RemoteAddr().String(), "client_ip": clientIP(c, proxies)})
		if err != nil {
			return err
		}
		_, err = db.Exec("INSERT INTO requests(code,method,path,query,headers,body,remote,created_at,metadata) VALUES (?,?,?,?,?,?,?,?,?)", c.Params("code"), c.Method(), c.Path(), string(c.Request().URI().QueryString()), string(h), body, c.IP(), time.Now().UTC().Format(time.RFC3339Nano), string(metadata))
		if err != nil {
			return err
		}
		return c.JSON(fiber.Map{"received": true})
	}
	app.All("/:code", exists, capture)
	app.All("/:code/*", exists, capture)
	return app
}

func main() {
	db, err := openDB(env("DB_PATH", "data/webhooks.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	geo := startGeo(env("GEOIP_DB_PATH", filepath.Join(filepath.Dir(env("DB_PATH", "data/webhooks.db")), "dbip-city-lite.mmdb")), env("GEOIP_AUTO_DOWNLOAD", "true") == "true")
	app := newAppWithGeo(db, geo)
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stop
		if err := app.ShutdownWithTimeout(10 * time.Second); err != nil {
			log.Print(err)
		}
	}()
	addr := env("LISTEN_ADDR", ":"+env("PORT", "8080"))
	if !strings.Contains(addr, ":") {
		log.Fatal("LISTEN_ADDR must include a port")
	}
	log.Printf("Webhook inspector listening on %s", addr)
	if err := app.Listen(addr); err != nil {
		log.Print(err)
	}
}
