#!/usr/bin/env bash
# =============================================================================
# MusicSeer Enhanced — Proxmox LXC provisioner
#
# Run this ON THE PROXMOX HOST (as root). It creates an unprivileged Debian 13
# LXC, pins DNS (Tailscale-on-the-host can poison container DNS otherwise —
# see your CLAUDE.md build notes), installs the musicseer binary and a systemd
# service, and starts it.
#
# Usage (defaults shown, tuned for Jonathan's 12U homelab):
#   bash proxmox-create-lxc.sh \
#       --ctid 112 \
#       --ip 10.0.10.253/24 \
#       --gateway 10.0.10.1 \
#       --dns 10.0.10.1 \
#       --bridge vmbr0 \
#       --storage local-lvm \
#       --binary-url https://github.com/Enthusiastbttf/musicseer-go/releases/latest/download/musicseer-linux-amd64
#
# A local binary can be used instead of a URL:  --binary /root/musicseer
# =============================================================================
set -euo pipefail

CTID=112
IP="10.0.10.253/24"
GW="10.0.10.1"
DNS="10.0.10.1"
BRIDGE="vmbr0"
STORAGE="local-lvm"
HOSTNAME="musicseer"
DISK_GB=4
MEMORY_MB=512
CORES=2
BINARY=""
BINARY_URL=""
TEMPLATE_STORAGE="local"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --ctid) CTID="$2"; shift 2 ;;
    --ip) IP="$2"; shift 2 ;;
    --gateway) GW="$2"; shift 2 ;;
    --dns) DNS="$2"; shift 2 ;;
    --bridge) BRIDGE="$2"; shift 2 ;;
    --storage) STORAGE="$2"; shift 2 ;;
    --hostname) HOSTNAME="$2"; shift 2 ;;
    --binary) BINARY="$2"; shift 2 ;;
    --binary-url) BINARY_URL="$2"; shift 2 ;;
    --memory) MEMORY_MB="$2"; shift 2 ;;
    *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
done

if [[ -z "$BINARY" && -z "$BINARY_URL" ]]; then
  echo "ERROR: provide --binary /path/to/musicseer or --binary-url https://..." >&2
  exit 2
fi
if pct status "$CTID" &>/dev/null; then
  echo "ERROR: CT $CTID already exists — pick another --ctid" >&2
  exit 2
fi

echo "==> Finding Debian 13 template…"
pveam update >/dev/null
TEMPLATE=$(pveam available --section system | awk '/debian-13-standard/ {print $2}' | sort | tail -1)
if [[ -z "$TEMPLATE" ]]; then
  echo "ERROR: no debian-13-standard template available via pveam" >&2
  exit 1
fi
if ! pveam list "$TEMPLATE_STORAGE" | grep -q "$TEMPLATE"; then
  echo "==> Downloading template $TEMPLATE…"
  pveam download "$TEMPLATE_STORAGE" "$TEMPLATE"
fi

echo "==> Creating CT $CTID ($HOSTNAME) at $IP…"
pct create "$CTID" "$TEMPLATE_STORAGE:vztmpl/$TEMPLATE" \
  --hostname "$HOSTNAME" \
  --unprivileged 1 \
  --features nesting=1 \
  --cores "$CORES" \
  --memory "$MEMORY_MB" \
  --swap 256 \
  --rootfs "$STORAGE:$DISK_GB" \
  --net0 "name=eth0,bridge=$BRIDGE,ip=$IP,gw=$GW" \
  --nameserver "$DNS" \
  --onboot 1 \
  --start 1

echo "==> Waiting for network…"
sleep 5
for i in $(seq 1 12); do
  pct exec "$CTID" -- ping -c1 -W2 "$GW" &>/dev/null && break
  sleep 2
done

echo "==> Installing base packages…"
pct exec "$CTID" -- bash -c "apt-get update -qq && apt-get install -y -qq ca-certificates curl >/dev/null"

echo "==> Installing musicseer binary…"
if [[ -n "$BINARY" ]]; then
  pct push "$CTID" "$BINARY" /opt/musicseer.bin
else
  pct exec "$CTID" -- bash -c "curl -fsSL '$BINARY_URL' -o /opt/musicseer.bin"
fi

pct exec "$CTID" -- bash -c '
set -e
useradd --system --home /var/lib/musicseer --shell /usr/sbin/nologin musicseer 2>/dev/null || true
mkdir -p /opt/musicseer /var/lib/musicseer /etc/musicseer
mv /opt/musicseer.bin /opt/musicseer/musicseer
chmod 755 /opt/musicseer/musicseer
chown -R musicseer:musicseer /var/lib/musicseer

if [[ ! -f /etc/musicseer/musicseer.env ]]; then
cat > /etc/musicseer/musicseer.env <<EOF
# Required for discovery (trending / recommendations):
LASTFM_API_KEY=
# Contact email used in the MusicBrainz user-agent (their API policy):
MUSICBRAINZ_CONTACT=admin@example.com
EOF
chmod 640 /etc/musicseer/musicseer.env
fi

cat > /etc/systemd/system/musicseer.service <<"EOF"
[Unit]
Description=MusicSeer Enhanced — music discovery and requests for Lidarr
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=musicseer
Group=musicseer
ExecStart=/opt/musicseer/musicseer
WorkingDirectory=/opt/musicseer
Environment=MUSICSEER_DATA_DIR=/var/lib/musicseer
Environment=MUSICSEER_PORT=8688
EnvironmentFile=-/etc/musicseer/musicseer.env
Restart=on-failure
RestartSec=5
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/musicseer
PrivateTmp=true

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable --now musicseer
'

IP_ONLY="${IP%%/*}"
echo
echo "=============================================================="
echo " MusicSeer Enhanced is up:  http://$IP_ONLY:8688"
echo
echo " Next steps:"
echo "   1. Add your Last.fm API key:"
echo "        pct exec $CTID -- nano /etc/musicseer/musicseer.env"
echo "        pct exec $CTID -- systemctl restart musicseer"
echo "   2. Open the web UI and create your admin account."
echo "   3. (Migrating?) see docs/MIGRATION.md"
echo
echo " DNS is pinned to $DNS (survives host Tailscale DNS drift)."
echo "=============================================================="
