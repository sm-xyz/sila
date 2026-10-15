#!/bin/bash
# ==============================================================================
# SILA (Social Integration & Linked Accounts) - Automated Installer for VPS 2
# Kompatibel penuh dengan sudoer non-root, auto-install Go 1.22+, dan Caddy Proxy
# ==============================================================================
set -e

# Otomatis eskalasi ke sudo jika dijalankan oleh non-root sudoer user
if [ "$EUID" -ne 0 ]; then
  echo "[INFO] Skrip membutuhkan hak akses root. Melanjutkan dengan sudo..."
  exec sudo -E bash "$0" "$@"
fi

# Pastikan /usr/local/go/bin dan /usr/bin ada dalam PATH
export PATH="/usr/local/go/bin:/usr/bin:$PATH"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

echo -e "${BLUE}================================================================${NC}"
echo -e "${GREEN}   SILA (Social Integration & Linked Accounts) - Installer      ${NC}"
echo -e "${BLUE}================================================================${NC}"

INSTALL_DIR="/opt/sila"
DATA_DIR="$INSTALL_DIR/data"
PUBLIC_DIR="$INSTALL_DIR/public"
BIN_TARGET="$INSTALL_DIR/sila-daemon"
SERVICE_FILE="/etc/systemd/system/sila.service"

SRC_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SRC_DIR"

echo -e "${YELLOW}[1/6] Mempersiapkan direktori sistem di $INSTALL_DIR...${NC}"
mkdir -p "$INSTALL_DIR"
mkdir -p "$DATA_DIR"
mkdir -p "$PUBLIC_DIR"
chmod 755 "$INSTALL_DIR"
chmod 755 "$DATA_DIR"
chmod 755 "$PUBLIC_DIR"

# Salin template env jika belum ada
if [ ! -f "$INSTALL_DIR/sila.env" ]; then
  if [ -f "$SRC_DIR/sila.env.example" ]; then
    cp "$SRC_DIR/sila.env.example" "$INSTALL_DIR/sila.env"
    chmod 600 "$INSTALL_DIR/sila.env"
    echo -e "${GREEN}[+] File template konfigurasi $INSTALL_DIR/sila.env berhasil dibuat.${NC}"
  fi
fi

# Salin aset statis HTML (privacy & deletion compliance)
if [ -d "$SRC_DIR/public" ]; then
  cp -r "$SRC_DIR/public/"* "$PUBLIC_DIR/"
  echo -e "${GREEN}[+] Halaman publik kepatuhan Meta & Google tersalin ke $PUBLIC_DIR.${NC}"
fi

echo -e "${YELLOW}[2/6] Memeriksa compiler Golang...${NC}"
NEED_GO_INSTALL=false
if ! command -v go &> /dev/null; then
    NEED_GO_INSTALL=true
else
    GO_VER=$(go version | awk '{print $3}' | sed 's/go//')
    GO_MAJOR=$(echo "$GO_VER" | cut -d. -f1)
    GO_MINOR=$(echo "$GO_VER" | cut -d. -f2)
    if [ "$GO_MAJOR" -lt 1 ] || [ "$GO_MINOR" -lt 22 ]; then
        echo -e "${BLUE}[INFO] Versi Go terpasang ($GO_VER) terlalu lama. Memperbarui ke Go terbaru...${NC}"
        NEED_GO_INSTALL=true
    fi
fi

if [ "$NEED_GO_INSTALL" = true ]; then
    echo -e "${BLUE}[INFO] Mengunduh dan menginstall Golang 1.22.5 resmi...${NC}"
    ARCH=$(uname -m)
    GO_ARCH="amd64"
    if [ "$ARCH" = "aarch64" ] || [ "$ARCH" = "arm64" ]; then
        GO_ARCH="arm64"
    fi
    GO_TAR="go1.22.5.linux-${GO_ARCH}.tar.gz"
    wget -q --show-progress "https://go.dev/dl/${GO_TAR}" -O "/tmp/${GO_TAR}"
    rm -rf /usr/local/go
    tar -C /usr/local -xzf "/tmp/${GO_TAR}"
    rm -f "/tmp/${GO_TAR}"
    ln -sf /usr/local/go/bin/go /usr/bin/go
    ln -sf /usr/local/go/bin/gofmt /usr/bin/gofmt
    echo -e "${GREEN}[OK] Golang $(go version) berhasil dipasang.${NC}"
fi

echo -e "${YELLOW}[3/6] Mengunduh dependensi dan mengompilasi SILA Daemon...${NC}"
# Pastikan modul SQLite modernc terpasang
go get modernc.org/sqlite@v1.34.4
go mod tidy

# Hentikan daemon aktif jika ada untuk menghindari error text file busy
systemctl stop sila 2>/dev/null || true

CGO_ENABLED=0 go build -ldflags="-s -w" -o "$BIN_TARGET" ./cmd/daemon/main.go
chmod 755 "$BIN_TARGET"
echo -e "${GREEN}[OK] Binary berhasil dikompilasi di $BIN_TARGET.${NC}"

echo -e "${YELLOW}[4/6] Memasang systemd service...${NC}"
if [ -f "$SRC_DIR/sila.service" ]; then
  cp "$SRC_DIR/sila.service" "$SERVICE_FILE"
else
  cat << 'EOF' > "$SERVICE_FILE"
[Unit]
Description=SILA Social Integration & Linked Accounts Daemon
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/opt/sila
ExecStart=/opt/sila/sila-daemon
Restart=always
RestartSec=5s
EnvironmentFile=/opt/sila/sila.env
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF
fi

systemctl daemon-reload
systemctl enable sila
systemctl restart sila
echo -e "${GREEN}[OK] SILA daemon aktif via systemd!${NC}"

echo -e "${YELLOW}[5/6] Memeriksa instalasi Caddy Reverse Proxy...${NC}"
if ! command -v caddy &> /dev/null; then
    echo -e "${BLUE}[INFO] Caddy belum terpasang. Menginstall Caddy resmi dari repo debian/ubuntu...${NC}"
    apt-get update -qq
    apt-get install -y -qq debian-keyring debian-archive-keyring apt-transport-https curl
    curl -1sLF 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg --yes 2>/dev/null || true
    curl -1sLF 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | tee /etc/apt/sources.list.d/caddy-stable.list >/dev/null
    apt-get update -qq
    apt-get install -y -qq caddy
    echo -e "${GREEN}[OK] Caddy web server berhasil diinstal.${NC}"
else
    echo -e "${GREEN}[OK] Caddy web server sudah terpasang.${NC}"
fi

# Template Caddyfile jika /etc/caddy/Caddyfile belum dikonfigurasi untuk SILA
if [ -f /etc/caddy/Caddyfile ] && ! grep -q "127.0.0.1:8092" /etc/caddy/Caddyfile; then
    echo -e "${BLUE}[INFO] Menyiapkan contoh blok konfigurasi Caddy di /opt/sila/Caddyfile.sample...${NC}"
    cat << 'EOF' > /opt/sila/Caddyfile.sample
# ==============================================================================
# Tambahkan blok ini ke /etc/caddy/Caddyfile lalu restart: systemctl restart caddy
# ==============================================================================
sila.domainanda.com {
    reverse_proxy 127.0.0.1:8092 {
        header_up Host {host}
        header_up X-Real-IP {remote_host}
        header_up X-Forwarded-For {remote_host}
        header_up X-Forwarded-Proto {scheme}
    }

    request_body {
        max_size 500MB
    }

    log {
        output file /var/log/caddy/sila_access.log {
            roll_size 20mb
            roll_keep 3
        }
    }
}
EOF
fi

echo -e "${YELLOW}[6/6] Menampilkan status service SILA...${NC}"
systemctl status sila --no-pager -l || true

echo -e "${BLUE}================================================================${NC}"
echo -e "${GREEN}   INSTALASI SILA SELESAI DENGAN SUKSES!                        ${NC}"
echo -e "${BLUE}================================================================${NC}"
echo -e "Langkah lanjutan:"
echo -e "1. Sesuaikan kredensial Meta & Google di: ${YELLOW}nano $INSTALL_DIR/sila.env${NC}"
echo -e "2. Terapkan domain di Caddy: ${YELLOW}nano /etc/caddy/Caddyfile${NC} (contoh: /opt/sila/Caddyfile.sample)"
echo -e "3. Validasi & restart Caddy: ${YELLOW}caddy validate --config /etc/caddy/Caddyfile && systemctl restart caddy${NC}"
echo -e "4. Restart SILA: ${YELLOW}systemctl restart sila${NC}"
echo -e "${BLUE}================================================================${NC}"
