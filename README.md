# SILA (Social Integration & Linked Accounts) Daemon

Microservice independen berbasis Golang dan SQLite WAL Engine untuk integrasi media sosial mandiri 100% tanpa pihak ketiga (Zero Repliz):
- **Meta Graph API v21.0**: Facebook Pages, Instagram Business, Threads.
- **Google YouTube Data API v3**: YouTube Videos & YouTube Shorts.
- **Omni CRM Messaging**: Direct Message & Comment Ingestion via Meta Webhook, pengiriman pesan balasan langsung, serta WebSocket stream real-time.
- **Worker Queue**: Otomasi antrean postingan dengan limitasi konkurensi (Max 2 Concurrency) agar aman dan hemat memori pada VPS.

## Struktur Direktori
```
/opt/sila/
├── cmd/daemon/main.go
├── config/config.go
├── internal/
│   ├── database/ (db.go, repository.go)
│   ├── gateway/  (middleware.go, caddy_stub.go)
│   ├── meta/     (client.go, oauth.go, publisher.go, webhook.go)
│   ├── youtube/  (client.go, oauth.go, uploader.go)
│   ├── crm/      (hub.go, service.go)
│   └── scheduler/(queue.go, worker.go)
├── public/       (privacy.html, deletion.html)
├── data/sila.db
├── sila.env
└── sila.service
```

## Quick Start di VPS 2
```bash
cd /opt/sila
cp sila.env.example sila.env
nano sila.env # Isi META_APP_ID, META_APP_SECRET, GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET
chmod +x install.sh
./install.sh
```
