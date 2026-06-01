# bot-po-akrab

Bot pre-order otomatis untuk khfy-store: memonitor stok produk, auto-beli saat
tersedia, notifikasi Telegram, dan web dashboard. Semua trafik ke API khfy
dilewatkan melalui **rotating proxy (nautica-client)** untuk bypass rate-limit.

```
preorder-bot → http_proxy 127.0.0.1:62080 (nautica) → khfy-store API
            └→ web dashboard :8090
            └→ Telegram notif
```

## Ports

| Komponen | Port | Catatan |
|----------|------|---------|
| Web dashboard / API | `:8090` | `listen_addr` di config |
| Proxy (dikonsumsi) | `127.0.0.1:62080` | nautica-client, harus jalan dulu |

## Dependensi

- **nautica-client** harus sudah berjalan di `127.0.0.1:62080` (rotating proxy).
- Go 1.25+ untuk build. SQLite memakai `modernc.org/sqlite` (pure Go, **tanpa CGO**).

## Setup

```bash
# 1. build (statis, tanpa CGO)
CGO_ENABLED=0 go build -o preorder-bot .

# 2. siapkan config (JANGAN commit config.json — berisi secret)
cp config.example.json config.json
# edit config.json: isi khfy_api_key, telegram_token, telegram_chat_id

# 3. jalankan
./preorder-bot -config config.json
```

## Config

| Field | Default | Keterangan |
|-------|---------|------------|
| `khfy_api_key` | — | API key khfy-store |
| `telegram_token` | — | token bot dari BotFather (kosongkan = nonaktif) |
| `telegram_chat_id` | — | chat id tujuan notif |
| `proxy_addr` | `http://127.0.0.1:62080` | proxy nautica; kosongkan = direct |
| `poll_interval_sec` | `5` | interval cek stok (detik) |
| `listen_addr` | `:8090` | alamat web dashboard |
| `db_path` | `preorder.db` | path SQLite |
| `max_retries` | `6` | retry pembelian |

## Deploy via PM2

```bash
CGO_ENABLED=0 go build -o preorder-bot .
cp config.example.json config.json   # lalu isi secret-nya
mkdir -p logs
pm2 start ecosystem.config.js
pm2 save
pm2 logs preorder-bot
```

## API endpoints

| Method | Path | Fungsi |
|--------|------|--------|
| GET | `/api/products` | daftar produk + status stok |
| GET/POST | `/api/preorders` | list / buat pre-order |
| DELETE | `/api/preorders/{id}` | hapus pre-order |
| GET | `/api/logs` | log transaksi |
| GET | `/api/stats` | statistik |
| GET | `/api/saldo` | saldo reseller |
| GET | `/api/events` | server-sent events (realtime) |
| POST | `/webhook/khfy` | webhook callback khfy |
| GET | `/` | web dashboard (static) |

Dashboard: buka `http://127.0.0.1:8090` (atau via SSH tunnel kalau di VPS).
