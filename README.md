# Hookdesk

Webhook inspector self-hosted: Go + Fiber v2 + SQLite (pure Go, tanpa CGO). Halaman HTML/CSS/JS di-embed ke binary; aset CMS memakai Tailwind CSS yang di-compile lokal. Tidak ada CDN saat aplikasi berjalan.

## Jalankan

### Docker (disarankan)

Pastikan Docker Desktop sudah terbuka dan Docker Compose aktif. Buat file konfigurasi admin dari contoh, lalu ganti password contoh dengan password rahasia minimal 16 karakter:

```sh
cp .env.example .env
```

Edit `.env`, kemudian build dan jalankan:

```sh
docker compose up -d --build
docker compose ps
```

Buka inspector di http://localhost:8080 dan CMS admin di http://localhost:8080/admin. Log bisa dilihat dengan `docker compose logs -f --tail=100 hookdesk`. Hentikan aplikasi dengan `docker compose down`; database tetap tersimpan di volume Docker `webhook-data`.

Kalau GNU Make tersedia, jalankan `make help` untuk daftar shortcut: `make up`, `make logs`, `make status`, dan `make down`. `make down` mempertahankan volume data.

### Go lokal

Perintah Go di bawah menjalankan aplikasi langsung tanpa Docker:

```sh
go run .
```

Buka http://localhost:8080. Halaman membuat kode acak 128-bit lewat API lalu mengganti URL menjadi `/#!/view/<code>`. Simpan URL ini untuk membuka inspector yang sama. Membuka `/` lagi membuat endpoint baru.

CMS admin tersedia di http://localhost:8080/admin. Atur `ADMIN_USERNAME` dan `ADMIN_PASSWORD` (minimal 16 karakter) di environment atau file `.env` sebelum login. Contoh: `ADMIN_USERNAME=admin` dan password acak minimal 16 karakter. Di deployment HTTPS, set `ADMIN_COOKIE_SECURE=true` agar cookie admin hanya dikirim lewat HTTPS. Endpoint inspector tetap dapat dipakai tanpa login; CMS dan API pengelolaan hanya menerima sesi admin.

Tailwind hanya diperlukan ketika mengubah styling CMS: jalankan `npm ci` lalu `npm run build:css`. File hasil compile `web/admin.css` ikut di-embed saat Go dibuild. Dockerfile melakukan langkah Tailwind ini otomatis di build stage.

```sh
curl -X POST http://localhost:8080/<code> \
  -H 'Content-Type: application/json' \
  -d '{"event":"payment.success"}'
```

`/<code>` dan `/<code>/path/tambahan` menerima semua HTTP method, termasuk GET, DELETE, dan OPTIONS. Respons sukses `200 {"received":true}` diberikan setelah SQLite menyimpan data. Kode yang belum dibuat memberikan 404.

Inspector otomatis memeriksa request baru setiap 2 detik, menampilkan 100 terbaru dan tombol untuk memuat halaman sebelumnya. Preview menampilkan URL lengkap, request ID, host, IP/alamat peer, versi HTTP, waktu lokal dan UTC, ukuran body, Content-Type, serta User-Agent. Header dan query berulang ditampilkan dalam tabel, termasuk query mentah. Body bisa dilihat sebagai formatted JSON, raw text, atau hex untuk payload biner; tersedia salin body, salin detail JSON, dan download byte asli.

Database versi awal otomatis dimigrasikan tanpa menghapus request. Metadata tambahan hanya tersedia untuk callback baru; request lama menampilkan informasi yang memang sudah direkam. Versi HTTP adalah koneksi yang diterima aplikasi (bisa HTTP/1.1 dari reverse proxy walau pengirim memakai HTTP/2). Scheme mengikuti informasi request/proxy yang diterima, dan bukan verifikasi koneksi TLS pengirim. Durasi proses tidak ditampilkan karena belum direkam.

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
| `TRUSTED_PROXIES` | kosong | Daftar IP/CIDR reverse proxy dipisahkan koma, untuk membaca IP pengirim dari X-Forwarded-For |
| `GEOIP_DB_PATH` | `dbip-city-lite.mmdb` di direktori SQLite | Lokasi database kota GeoIP lokal |
| `GEOIP_AUTO_DOWNLOAD` | `true` | Unduh DB-IP City Lite jika database belum tersedia; `false` untuk mengelola MMDB sendiri |
| `ADMIN_USERNAME` | kosong | Username CMS admin; wajib diisi untuk login |
| `ADMIN_PASSWORD` | kosong | Password CMS admin minimal 16 karakter; wajib diisi untuk login |
| `ADMIN_COOKIE_SECURE` | `false` | Set `true` di deployment HTTPS |

Tidak ada kuota endpoint, jumlah request, atau expiry. Kapasitas tetap mengikuti disk, memori, dan throughput SQLite. Limit payload mencegah satu request menghabiskan memori; limit reverse proxy juga perlu disesuaikan jika menerima payload besar.

Kode endpoint berfungsi sebagai akses inspector: siapa pun yang memiliki kode dapat mengirim, melihat, dan menghapus callback endpoint itu. CMS admin terpisah memakai akun yang diatur lewat environment. Lindungi halaman inspector dengan reverse proxy jika data request bersifat privat. IP peer tetap dicatat terpisah dari IP pengirim; header forwarded tetap dapat dilihat sebagai data request.

## Lokasi IP pengirim

Preview menampilkan negara, region/provinsi, dan kota perkiraan dengan database lokal [DB-IP City Lite](https://db-ip.com/db/download/ip-to-city-lite). Tidak ada API lookup per request, kuota lookup, atau pengiriman IP callback ke pihak luar. Pertama kali server berjalan, aplikasi mengunduh database MMDB bulanan lewat HTTPS di background (sekitar 121 MB setelah ekstraksi untuk rilis Oktober 2026). Callback tetap diterima selama download atau jika download gagal; preview memberi status yang sesuai. Jika gagal, periksa koneksi internet/log server lalu restart aplikasi untuk mencoba lagi. IP lokal/private tidak diberi lokasi palsu.

Database disimpan di volume data dan digunakan kembali setelah restart. Untuk memperbarui data bulanan, hentikan aplikasi, hapus/ganti **hanya file MMDB**, lalu jalankan kembali. Jangan hapus database SQLite/volume. Bisa juga menyediakan MMDB DB-IP City Lite sendiri di `GEOIP_DB_PATH` dan mematikan auto-download. Lisensi data [CC BY 4.0](https://creativecommons.org/licenses/by/4.0/); atribusi DB-IP ditampilkan pada inspector.

GeoIP menunjukkan lokasi jaringan, bukan lokasi pasti orang/perangkat. VPN/proxy bisa menunjukkan lokasi exit node. Database City Lite tidak menyediakan ISP; tidak ada geolokasi untuk IP yang tidak ditemukan. Callback lama memakai IP peer yang dulu direkam, karena IP asli tidak bisa direkonstruksi secara tepercaya dari header lama.

### Reverse proxy

Jika aplikasi berada di belakang Nginx/Caddy/Traefik/Cloudflare, atur `TRUSTED_PROXIES` ke **IP/CIDR proxy yang benar-benar terhubung ke aplikasi**. Default kosong sengaja mengabaikan X-Forwarded-For untuk mencegah pengirim memalsukan IP.

Contoh (ganti dengan IP proxy di jaringan deploy lo):

```sh
TRUSTED_PROXIES=172.20.0.2 docker compose up -d --build
```

Atau tulis `TRUSTED_PROXIES=172.20.0.2` dalam file `.env` di server. Jika ada beberapa hop, daftarkan seluruh proxy tepercaya dipisahkan koma. Proxy harus menimpa/menambahkan header X-Forwarded-For dengan IP koneksi yang sebenarnya; aplikasi membaca rantai dari kanan dan berhenti di hop pertama yang tidak dipercaya. Jangan memakai `0.0.0.0/0` atau `::/0`. Jika Cloudflare berada di depan proxy lain, konfigurasi trust IP Cloudflare di proxy depan dan sanitasi header sebelum diteruskan; jangan menganggap semua header dari internet terpercaya.

Backup SQLite memakai SQLite backup API/CLI `.backup`, atau hentikan aplikasi sebelum menyalin file database. Jangan menyalin hanya file `.db` ketika aplikasi aktif dalam mode WAL.

## API

| Method | Path | Hasil |
| --- | --- | --- |
| POST | `/api/bins` | Buat kode baru |
| GET | `/api/bins/:code/requests?before=<id>` | Daftar 100 request, terbaru dahulu |
| GET | `/api/bins/:code/requests/:id` | Detail dengan body teks dan `body_base64` untuk byte asli |
| DELETE | `/api/bins/:code/requests` | Hapus seluruh request endpoint |
| GET | `/healthz` | Health check database |

CMS ada di `/admin`, dengan login sebelum data admin dapat dibaca. Tabel memberi jumlah callback, waktu dan request terakhir, pencarian kode, pagination, link inspector, serta penghapusan endpoint dengan seluruh callback di dalam satu transaksi.

API CMS memerlukan cookie sesi admin dari login:

| Method | Path | Hasil |
| --- | --- | --- |
| POST | `/api/admin/login` | Login JSON `{"username":"...","password":"..."}` |
| POST | `/api/admin/logout` | Akhiri sesi admin |
| GET | `/api/admin/summary` | Total endpoint dan callback |
| GET | `/api/admin/bins?page=1&q=<kode>` | Daftar endpoint dengan pagination 50 baris |
| DELETE | `/api/admin/bins/:code` | Hapus endpoint beserta seluruh callback secara permanen |

CMS mengonfirmasi penghapusan dan menampilkan jumlah callback yang ikut terhapus. Login dibatasi 8 percobaan per IP per 15 menit. Cookie sesi HttpOnly, SameSite Strict, dan berlaku 12 jam; rahasia tanda tangan dibuat ulang saat server restart, jadi admin perlu login lagi.

```sh
go test ./...
go vet ./...
go build .
```

Desain mengikuti [anti-slop](https://github.com/miqdadbadjuber/anti-slop) (during) dan [UI UX Pro Max](https://github.com/nextlevelbuilder/ui-ux-pro-max-skill): inspector sederhana, fokus ke data nyata, layout responsif, dan payload dirender sebagai teks supaya HTML callback tidak dieksekusi. Fiber dipilih karena berbasis fasthttp dan fokus performa; klaim framework paling cepat memerlukan benchmark workload sendiri.
