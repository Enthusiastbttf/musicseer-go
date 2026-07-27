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
#       --version v2.14.0
#
# The release is pinned by tag, not "latest", so re-running this script months
# apart provisions the same thing twice. `--version latest` opts back into
# tracking the newest release.
#
# Downloads are SHA-256-verified against the checksums.txt published alongside
# the release, so a hijacked release asset does not silently become a binary
# running as the service account. TLS alone was the only trust anchor before.
#
# A local binary can be used instead:  --binary /root/musicseer
# For a hand-rolled URL, supply the digest yourself: --binary-url URL --sha256 SUM
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

# Bumped with each release. Pinning by default means this script is
# reproducible; pass --version latest if you want the newest instead.
VERSION="v2.14.0"
REPO="Enthusiastbttf/musicseer-go"
ASSET="musicseer-linux-amd64"
SHA256=""
SKIP_CHECKSUM=0

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
    --version) VERSION="$2"; shift 2 ;;
    --sha256) SHA256="$2"; shift 2 ;;
    --insecure-skip-checksum) SKIP_CHECKSUM=1; shift ;;
    --memory) MEMORY_MB="$2"; shift 2 ;;
    *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
done

# Resolve the download and its digest. An explicit --binary-url is honoured but
# then the digest has to come from somewhere: either --sha256, or the release's
# checksums.txt when the URL is a normal release asset. Refusing to install an
# unverified binary is the whole point, so there is no silent fallback.
CHECKSUM_URL=""
if [[ -z "$BINARY" ]]; then
  if [[ -z "$BINARY_URL" ]]; then
    if [[ "$VERSION" == "latest" ]]; then
      BINARY_URL="https://github.com/$REPO/releases/latest/download/$ASSET"
      CHECKSUM_URL="https://github.com/$REPO/releases/latest/download/checksums.txt"
    else
      BINARY_URL="https://github.com/$REPO/releases/download/$VERSION/$ASSET"
      CHECKSUM_URL="https://github.com/$REPO/releases/download/$VERSION/checksums.txt"
    fi
  elif [[ -z "$SHA256" && "$BINARY_URL" == */releases/download/*/"$ASSET" ]]; then
    CHECKSUM_URL="${BINARY_URL%/$ASSET}/checksums.txt"
  fi

  if [[ -z "$SHA256" && -z "$CHECKSUM_URL" && $SKIP_CHECKSUM -eq 0 ]]; then
    echo "ERROR: cannot verify '$BINARY_URL' — pass --sha256 <digest>, or" >&2
    echo "       --insecure-skip-checksum if you really mean to trust TLS alone." >&2
    exit 2
  fi
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
  echo "    from $BINARY_URL"
  pct exec "$CTID" -- bash -c "curl -fsSL '$BINARY_URL' -o /opt/musicseer.bin"

  if [[ -z "$SHA256" && -n "$CHECKSUM_URL" ]]; then
    echo "==> Fetching published checksum…"
    # checksums.txt lines are "<sha256>  <filename>".
    SHA256=$(pct exec "$CTID" -- bash -c \
      "curl -fsSL '$CHECKSUM_URL' | awk '\$2 == \"$ASSET\" || \$2 == \"*$ASSET\" {print \$1}'")
    SHA256=$(tr -d '[:space:]' <<<"$SHA256")
    if [[ -z "$SHA256" ]]; then
      echo "ERROR: no entry for $ASSET in $CHECKSUM_URL" >&2
      exit 1
    fi
  fi

  if [[ -n "$SHA256" ]]; then
    echo "==> Verifying SHA-256…"
    # sha256sum -c fails the script (set -e) on a mismatch, before the binary
    # is ever moved into place or made executable.
    pct exec "$CTID" -- bash -c \
      "cd /opt && echo '$SHA256  musicseer.bin' | sha256sum -c -" \
      || { echo "ERROR: checksum mismatch — refusing to install" >&2; \
           pct exec "$CTID" -- rm -f /opt/musicseer.bin; exit 1; }
  else
    echo "!!! WARNING: installing an unverified binary (--insecure-skip-checksum)" >&2
  fi
fi

pct exec "$CTID" -- bash -c '
set -e
useradd --system --home /var/lib/musicseer --shell /usr/sbin/nologin musicseer 2>/dev/null || true
mkdir -p /opt/musicseer /var/lib/musicseer /etc/musicseer
mv /opt/musicseer.bin /opt/musicseer/musicseer
chmod 755 /opt/musicseer/musicseer
# Own the binary dir so the in-app updater can replace the binary and re-exec.
chown -R musicseer:musicseer /var/lib/musicseer /opt/musicseer

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

# Hardening.
#
# This block is the single source of truth: deploy/proxmox-create-lxc.sh writes
# a byte-identical unit, and TestLXCUnitMatchesReference fails the build if the
# two drift apart. Edit here, then copy into the heredoc in that script.
#
# NOTE: this text is embedded in a single-quoted shell string in that script,
# so it must stay free of apostrophes.
#
# Inside an unprivileged LXC systemd silently skips the directives it cannot
# apply rather than refusing to start, so these are safe in a container — but
# check `systemctl status musicseer` after a fresh provision anyway.
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
# /opt/musicseer is writable so the in-app updater can replace the binary and
# re-exec. It must also be owned by the musicseer user (chown -R). Downloads are
# SHA-256-verified against the release before the swap. Drop /opt/musicseer here
# (and from the chown) if you prefer CLI-only upgrades.
ReadWritePaths=/var/lib/musicseer /opt/musicseer
PrivateTmp=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictSUIDSGID=true
RestrictNamespaces=true
RestrictRealtime=true
LockPersonality=true
# A Go HTTP service needs nothing outside the normal service set. Not
# MemoryDenyWriteExecute — the Go runtime maps its own executable pages.
SystemCallFilter=@system-service
SystemCallErrorNumber=EPERM

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
