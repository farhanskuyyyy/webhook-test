# Hookdesk design

Direction: alat debugging webhook dengan HTML sederhana, mengikuti brief pengguna. Fokus utama URL endpoint; komposisi master/detail untuk membandingkan callback tanpa pindah halaman. Hijau menghubungkan endpoint aktif dan request terpilih. Permukaan terang dan pemisah tipis menjaga payload tetap mudah dibaca. Monospace hanya untuk URL, method, dan payload; system sans untuk kontrol agar tanpa unduhan font.

ENERGY 1 / RHYTHM 2 / MOTION 1. Identitas berupa aksen hijau pada URL dan baris request aktif. Ritme mengikuti pekerjaan: endpoint, status, daftar, detail. Tidak ada animasi dekoratif. Contoh curl dapat dijalankan dan berasal dari URL endpoint aktif.

Preview detail diperbarui mengikuti referensi pengguna: ringkasan request dan headers berdampingan di desktop, kemudian query dan body dalam bagian terpisah. Tabel membedakan nama field dan nilai tanpa memaksa pengguna berganti tab. Di layar sempit ringkasan dan header ditumpuk. Pilihan formatted/raw/hex melayani JSON, teks, dan byte biner. Metadata yang tidak ada pada request lama diberi label jujur.

Lokasi GeoIP ditempatkan langsung di ringkasan setelah IP pengirim, dengan label perkiraan. IP peer dipisahkan agar proxy tidak tersembunyi. Status private/tidak tersedia/download dijelaskan sebagai teks. Tautan atribusi data DB-IP berada di footer; tidak menambahkan peta atau gambar eksternal.

CMS memakai workspace admin yang netral dan padat: navigasi pekerjaan di samping pada desktop, metrik total yang nyata, dan tabel endpoint dengan pencarian serta pagination. Aksi hapus ditampilkan di baris dan menyebut endpoint serta dampak callback melalui dialog konfirmasi. Halaman login dipisahkan dari dashboard; kredensial dari environment menghindari akun default. Styling ditulis dengan utility Tailwind lokal dan stylesheet hasil compile di-embed agar aplikasi tidak memerlukan CDN ketika berjalan.

UI UX Pro Max: pencarian design-system dijalankan untuk developer webhook inspector dashboard, diulang untuk operations dashboard. Rekomendasi layout landing tidak cocok untuk inspector; fallback ke panduan aksesibilitas/interaction bawaan dan komposisi master/detail. Arahan minimal functional, developer monospace, dan aksen endpoint hijau sesuai hasil pertama. Tidak menggunakan rekomendasi hero/marketing.
