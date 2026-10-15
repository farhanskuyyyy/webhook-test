# Hookdesk agent notes

Hookdesk is a self-hosted webhook inspector. It is a small Go service with Fiber v2, SQLite via `modernc.org/sqlite` (pure Go, no CGO), and a single embedded HTML/CSS/JavaScript inspector page. Docker Compose is the documented deployment. Keep the project straightforward to build and run without Node or a frontend CDN.

## Start here

- `README.md`: run, API, environment variables, Docker deployment, SQLite operations, DB-IP GeoIP and reverse proxy setup.
- `main.go`: Fiber routes, SQLite schema/migration, request capture, shutdown.
- `geoip.go`: local MMDB lookup/download and trusted reverse-proxy IP resolution.
- `main_test.go`, `geoip_test.go`: API lifecycle, SQLite migration, encoding, GeoIP, and proxy-chain tests.
- `web/index.html`: the embedded inspector UI. It is embedded by `//go:embed`; rebuilding the Go binary or container includes HTML changes.
- `admin.go`, `web/admin.html`: admin session, login protection, endpoint monitoring API and CMS page.
- `web/admin.src.css`, `web/admin.css`, `package.json`: Tailwind source, checked-in compiled stylesheet and the local CSS build command (`npm ci && npm run build:css`).
- `compose.yaml`, `Dockerfile`: persistent `/data` volume and deploy configuration.
- `DESIGN.md`: interface direction and rationale.

## Development

Use the Go version from `go.mod` (`go 1.22.1`). Typical local commands:

```sh
go run .
go test ./...
go vet ./...
go build .
```

The default server is `http://localhost:8080`; SQLite defaults to `data/webhooks.db`. Do not commit local SQLite/MMDB files, `.env`, or built binaries.

## Behavior and data

- `POST /api/bins` creates a random 128-bit endpoint code. Opening `/` creates a new inspector URL at `/#!/view/<code>`.
- `/<code>` and `/<code>/*` capture every HTTP method, the raw body bytes, repeated query parameters, request headers, peer address, timestamps, HTTP metadata, and the resolved client IP. A successful capture returns HTTP 200 after SQLite commits the row.
- A callback code is also the read/delete capability; there is no account layer. Preserve escaping of request data in HTML (`textContent` or another safe text sink).
- SQLite uses WAL and one connection. Keep schema migration additive and preserve old request rows. Old rows can lack the newer metadata; show unavailable fields honestly.
- Request count has no application quota. `MAX_BODY_BYTES` defaults to 10 MiB to bound per-request memory. Do not add automatic expiry unless the user asks.
- GeoIP uses the local DB-IP City Lite MMDB, downloaded over HTTPS in the background if missing. It must never block callback capture. The database file stays beside SQLite on the persistent volume. DB-IP Lite requires CC BY 4.0 attribution; retain the inspector footer link and README attribution.
- GeoIP is approximate and returns city/region/country only. Do not label it as an exact person/device location or claim ISP data. Private addresses and missing records must not receive invented locations.
- Only honor `X-Forwarded-For` from addresses configured in `TRUSTED_PROXIES`. The default is empty by design. Resolve a proxy chain from the nearest hop outward and stop at the first untrusted hop; never default to trusting every address.
- CMS credentials come from `ADMIN_USERNAME` and `ADMIN_PASSWORD`; password must be at least 16 characters. There is intentionally no default admin credential. If they are unset, keep webhook inspection working and show the CMS setup message.
- Admin sessions use a signed, HttpOnly, SameSite Strict cookie with a 12-hour expiry. Protect state-changing admin routes with the same-origin check and require confirmation before endpoint deletion. Deleting an endpoint must atomically delete all its requests and the bin.
- Keep endpoint and request totals scoped to the admin API. Public callback inspector data remains capability-protected by its random 128-bit endpoint code.

## UI changes

Keep the inspector focused on endpoint details, request metadata, headers, query and payload. Preserve responsive behavior, visible keyboard focus, useful empty/error states, and the formatted/raw/hex views. User-supplied callback data is untrusted. Avoid external assets and scripts; the page should continue to work offline from the embedded origin.

## Deployment and handoff

The supported container workflow is:

```sh
docker compose up -d --build
```

Keep SQLite and MMDB in `/data`. For a reverse proxy, document the actual `TRUSTED_PROXIES` requirement rather than guessing a deployment network. Docker Compose restarts the app but does not publish changes automatically; after a code change, commit/push and rebuild the image on the deployment host. Never claim the public site changed unless deployment was actually performed and verified.

For public HTTPS deployments, set `ADMIN_COOKIE_SECURE=true` and configure a strong `ADMIN_USERNAME`/`ADMIN_PASSWORD` in the deployment environment. Never put a production password in committed compose files or source.

When handing work off, report changed files, tests/builds actually run, material limits, and the commit/push status. Do not say commands passed if they were not run.
