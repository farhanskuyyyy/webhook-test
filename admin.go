package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
)

const adminCookie = "hookdesk_admin"
const adminSessionDuration = 12 * time.Hour

type adminAuth struct {
	username, password string
	secure             bool
	mu                 sync.Mutex
	attempts           map[string][]time.Time
}

func (a *adminAuth) configured() bool { return len(a.username) > 0 && len(a.password) >= 16 }

func (a *adminAuth) sessionToken(expires int64) string {
	message := strconv.FormatInt(expires, 10)
	mac := hmac.New(sha256.New, adminSessionKey)
	mac.Write([]byte(message))
	return message + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (a *adminAuth) valid(c *fiber.Ctx) bool {
	cookie := c.Cookies(adminCookie)
	parts := strings.Split(cookie, ".")
	if len(parts) != 2 {
		return false
	}
	expires, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || time.Now().Unix() > expires {
		return false
	}
	expected := strings.Split(a.sessionToken(expires), ".")[1]
	return hmac.Equal([]byte(parts[1]), []byte(expected))
}

func (a *adminAuth) require(c *fiber.Ctx) error {
	if !a.configured() {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Admin login belum dikonfigurasi. Set ADMIN_USERNAME dan ADMIN_PASSWORD minimal 16 karakter."})
	}
	if !a.valid(c) {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Silakan login sebagai admin."})
	}
	return c.Next()
}

func (a *adminAuth) allowAttempt(ip string, now time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	cutoff := now.Add(-15 * time.Minute)
	for key, entries := range a.attempts {
		kept := entries[:0]
		for _, at := range entries {
			if at.After(cutoff) {
				kept = append(kept, at)
			}
		}
		if len(kept) == 0 {
			delete(a.attempts, key)
		} else {
			a.attempts[key] = kept
		}
	}
	if len(a.attempts) >= 10000 && a.attempts[ip] == nil {
		return false
	}
	entries := a.attempts[ip]
	if len(entries) >= 8 {
		return false
	}
	a.attempts[ip] = append(entries, now)
	return true
}

func (a *adminAuth) login(c *fiber.Ctx) error {
	if !a.configured() {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Set ADMIN_USERNAME and ADMIN_PASSWORD minimal 16 karakter sebelum mengaktifkan admin."})
	}
	if err := a.checkOrigin(c); err != nil {
		return err
	}
	ip := c.Context().RemoteIP().String()
	if !a.allowAttempt(ip, time.Now()) {
		return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{"error": "Terlalu banyak percobaan login. Tunggu 15 menit."})
	}
	var credentials struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal(c.Body(), &credentials); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Format login tidak valid."})
	}
	userOK := hmac.Equal([]byte(credentials.Username), []byte(a.username))
	passOK := hmac.Equal([]byte(credentials.Password), []byte(a.password))
	if !userOK || !passOK {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Username atau password salah."})
	}
	a.mu.Lock()
	delete(a.attempts, ip)
	a.mu.Unlock()
	expires := time.Now().Add(adminSessionDuration)
	c.Cookie(&fiber.Cookie{Name: adminCookie, Value: a.sessionToken(expires.Unix()), Path: "/", HTTPOnly: true, Secure: a.secure, SameSite: "Strict", Expires: expires, MaxAge: int(adminSessionDuration.Seconds())})
	c.Set("Cache-Control", "no-store")
	return c.JSON(fiber.Map{"ok": true, "username": a.username})
}

func (a *adminAuth) sameOrigin(c *fiber.Ctx) error {
	if err := a.checkOrigin(c); err != nil {
		return err
	}
	return c.Next()
}

func (a *adminAuth) checkOrigin(c *fiber.Ctx) error {
	origin := c.Get("Origin")
	if origin == "" {
		return fiber.ErrForbidden
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" || !strings.EqualFold(parsed.Host, string(c.Request().Host())) || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fiber.ErrForbidden
	}
	return nil
}

func (a *adminAuth) logout(c *fiber.Ctx) error {
	c.Cookie(&fiber.Cookie{Name: adminCookie, Value: "", Path: "/", HTTPOnly: true, Secure: a.secure, SameSite: "Strict", Expires: time.Unix(1, 0), MaxAge: -1})
	return c.SendStatus(fiber.StatusNoContent)
}

func (a *adminAuth) summary(db *sql.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var endpoints, requests int64
		if err := db.QueryRow("SELECT COUNT(*) FROM bins").Scan(&endpoints); err != nil {
			return err
		}
		if err := db.QueryRow("SELECT COUNT(*) FROM requests").Scan(&requests); err != nil {
			return err
		}
		return c.JSON(fiber.Map{"endpoints": endpoints, "requests": requests})
	}
}

func (a *adminAuth) bins(db *sql.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		page, _ := strconv.Atoi(c.Query("page", "1"))
		if page < 1 {
			page = 1
		}
		if page > 1000000 {
			page = 1000000
		}
		search := strings.TrimSpace(c.Query("q"))
		if len(search) > 128 {
			search = search[:128]
		}
		var total int64
		if err := db.QueryRow("SELECT COUNT(*) FROM bins WHERE ?='' OR instr(code,?)>0", search, search).Scan(&total); err != nil {
			return err
		}
		rows, err := db.Query(`SELECT b.code,b.created_at,COUNT(r.id),COALESCE(MAX(r.created_at),''),COALESCE((SELECT r2.method||' '||r2.path FROM requests r2 WHERE r2.code=b.code ORDER BY r2.id DESC LIMIT 1),'')
		FROM bins b LEFT JOIN requests r ON r.code=b.code WHERE ?='' OR instr(b.code,?)>0 GROUP BY b.code ORDER BY COALESCE(MAX(r.created_at),b.created_at) DESC LIMIT 50 OFFSET ?`, search, search, (page-1)*50)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []fiber.Map{}
		for rows.Next() {
			var code, created, lastSeen, lastRequest string
			var count int64
			if err := rows.Scan(&code, &created, &count, &lastSeen, &lastRequest); err != nil {
				return err
			}
			items = append(items, fiber.Map{"code": code, "created_at": created, "request_count": count, "last_seen": lastSeen, "last_request": lastRequest})
		}
		if err := rows.Err(); err != nil {
			return err
		}
		pages := (total + 49) / 50
		return c.JSON(fiber.Map{"items": items, "total": total, "page": page, "pages": pages})
	}
}

func (a *adminAuth) deleteBin(db *sql.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		code := c.Params("code")
		if !codePattern.MatchString(code) {
			return fiber.ErrNotFound
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		result, err := tx.Exec("DELETE FROM requests WHERE code=?", code)
		if err != nil {
			return err
		}
		requests, _ := result.RowsAffected()
		result, err = tx.Exec("DELETE FROM bins WHERE code=?", code)
		if err != nil {
			return err
		}
		endpoints, _ := result.RowsAffected()
		if endpoints == 0 {
			return fiber.ErrNotFound
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		return c.JSON(fiber.Map{"deleted": true, "endpoint": code, "requests_deleted": requests})
	}
}
