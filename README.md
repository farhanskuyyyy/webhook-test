# Hookdesk

Webhook inspector self-hosted: Go + Fiber v2 + SQLite (pure Go, tanpa CGO). HTML/CSS/JS di-embed ke binary, tanpa npm atau CDN.

## Jalankan

```sh
go run .
```

Buka http://localhost:8080. Halaman membuat kode acak 128-bit lewat API lalu mengganti URL menjadi `/#!/view/<code>`. Simpan URL ini untuk membuka inspector yang sama. Membuka `/` lagi membuat endpoint baru.

```sh
curl -X POST http://localhost:8080/<code> \
  -H 'Content-Type: application/json' \
  -d '{"event":"payment.success"}'
```

`/<code>` dan `/<code>/path/tambahan` menerima semua HTTP method, termasuk GET, DELETE, dan OPTIONS. Respons sukses `200 {"received":true}` diberikan setelah SQLite menyimpan data. Kode yang belum dibuat memberikan 404.

Inspector otomatis memeriksa request baru setiap 2 detik, menampilkan 100 terbaru dan tombol untuk memuat halaman sebelumnya. Bisa melihat body (JSON diformat otomatis), headers, query berulang, waktu, IP peer, serta download body asli termasuk payload biner.

## Deploy Docker

```sh
docker compose up -d --build
```

Akses port 8080 atau arahkan reverse proxy HTTPS/domain ke port tersebut. SQLite tersimpan di named volume `webhook-data`; restart/rebuild tidak menghapus data. Gunakan satu instance aplikasi dengan volume lokal, jangan berbagi database lewat network filesystem. HTML memakai origin browser sehingga URL webhook mengikuti domain HTTPS tanpa konfigurasi URL tambahan.

## Konfigurasi

| Environment | Default | Fungsi |
| --- | --- | --- |
| `PORT` | `8080` | Port server |
| `LISTEN_ADDR` | `:<PORT>` | Override alamat bind |
| `DB_PATH` | `data/webhooks.db` | Lokasi SQLite |
| `MAX_BODY_BYTES` | `10485760` | Maksimum payload per request (10 MiB), bisa dinaikkan |

Tidak ada kuota endpoint, jumlah request, atau expiry. Kapasitas tetap mengikuti disk, memori, dan throughput SQLite. Limit payload mencegah satu request menghabiskan memori; limit reverse proxy juga perlu disesuaikan jika menerima payload besar.

Kode endpoint berfungsi sebagai akses: siapa pun yang memiliki kode dapat mengirim, melihat, dan menghapus request. Tidak ada akun/login. Jangan gunakan untuk data rahasia pada server publik tanpa pembatasan akses inspector di reverse proxy. IP yang dicatat adalah peer langsung (IP proxy jika memakai proxy); header forwarded tetap dapat dilihat sebagai data request.

Backup SQLite memakai SQLite backup API/CLI `.backup`, atau hentikan aplikasi sebelum menyalin file database. Jangan menyalin hanya file `.db` ketika aplikasi aktif dalam mode WAL.

## API

| Method | Path | Hasil |
| --- | --- | --- |
| POST | `/api/bins` | Buat kode baru |
| GET | `/api/bins/:code/requests?before=<id>` | Daftar 100 request, terbaru dahulu |
| GET | `/api/bins/:code/requests/:id` | Detail dengan body teks dan `body_base64` untuk byte asli |
| DELETE | `/api/bins/:code/requests` | Hapus seluruh request endpoint |
| GET | `/healthz` | Health check database |

```sh
go test ./...
go vet ./...
go build .
```

Desain mengikuti [anti-slop](https://github.com/miqdadbadjuber/anti-slop) (during) dan [UI UX Pro Max](https://github.com/nextlevelbuilder/ui-ux-pro-max-skill): inspector sederhana, fokus ke data nyata, layout responsif, dan payload dirender sebagai teks supaya HTML callback tidak dieksekusi. Fiber dipilih karena berbasis fasthttp dan fokus performa; klaim framework paling cepat memerlukan benchmark workload sendiri.
