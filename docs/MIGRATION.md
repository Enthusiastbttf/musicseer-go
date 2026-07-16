# Migration plan — original MusicSeer → MusicSeer 2

A **non-destructive, run-both-side-by-side** cutover. The old app keeps
running untouched until you decide it's done; its database is only ever read.
Rollback at any point = "keep using the old one."

This plan is written for the reference homelab (Proxmox VE 9, old MusicSeerr
in **CT 109** at `10.0.10.251:8688`, Lidarr in CT 108 at `10.0.10.250:8686`,
NPM on CT 106) — adjust IDs/IPs to taste.

## Phase 0 — Safety net (5 min)

On the Proxmox host, snapshot/backup the old container:

```bash
vzdump 109 --mode snapshot --storage local --compress zstd
```

Nothing below modifies CT 109, but a backup makes it a non-event either way.

## Phase 1 — Stand up MusicSeer 2 alongside (10 min)

Pick **one** (both are fully supported):

**Option A — new native LXC (recommended; matches your *arr pattern):**

```bash
# on the Proxmox host
bash deploy/proxmox-create-lxc.sh \
  --ctid 112 --hostname musicseer \
  --ip 10.0.10.253/24 --gateway 10.0.10.1 --dns 10.0.10.1 \
  --binary-url https://github.com/Enthusiastbttf/musicseer-go/releases/latest/download/musicseer-linux-amd64
```

DNS is pinned to `10.0.10.1` by the script — this avoids the Tailscale
MagicDNS inheritance problem you've hit twice on other CTs.

**Option B — Docker container (e.g. in the CT 106 stack):**

```bash
# on CT 106
mkdir -p /opt/musicseer && cd /opt/musicseer
# copy docker-compose.yml + .env (set LASTFM_API_KEY), then:
docker compose up -d       # serves on 8688
```

Then in either case: add the Last.fm API key
(`/etc/musicseer/musicseer.env` + `systemctl restart musicseer` for the LXC),
open `http://10.0.10.253:8688`, and create your admin account.

> Create the admin with the **same username** you use in the old app — the
> importer then treats them as the same person and attaches your request
> history to it.

## Phase 2 — Import your data (5 min)

You need the old Postgres credentials — they're in the old app's `.env`
(`POSTGRES_USER` / `POSTGRES_PASSWORD` / `POSTGRES_DB`, defaults
`musicseer` / `changeme` / `musicseer`). On CT 109 the compose file publishes
Postgres on port 5432.

From the new instance:

```bash
# LXC: 
pct exec 112 -- /opt/musicseer/musicseer migrate \
  'postgres://musicseer:PASSWORD@10.0.10.251:5432/musicseer?sslmode=disable'

# Docker:
docker compose exec musicseer musicseer migrate \
  'postgres://musicseer:PASSWORD@10.0.10.251:5432/musicseer?sslmode=disable'
```

What it does — and all it does:

- **reads** users, server instances and requests from Postgres (SELECTs only);
- imports users **with their existing bcrypt password hashes** — everyone
  keeps their password; Navidrome-auth users keep working too;
- imports Navidrome/Lidarr instance definitions, **encrypting** the API keys
  that the old app stored in plaintext;
- imports request history (statuses mapped, `completed` → `sent`);
- skips anything that already exists — **safe to re-run any time**.

If the old Postgres port isn't reachable, run the same command on CT 109
itself against `127.0.0.1:5432` with a copy of the binary.

## Phase 3 — Validate (take your time)

Old and new are now running simultaneously on different IPs. On the new one:

1. Log in with your **old** password. Have a family member do the same.
2. Admin → Instances → **Test connection** on Navidrome and Lidarr, and set
   the Lidarr **quality profile / metadata profile / root folder** from the
   dropdowns (the old app hardcoded IDs; these must be set once).
3. Admin → Status → **Sync library**, confirm artist count looks right.
4. Wait for “Similar to Your Library” to populate (first build takes a
   minute or two of background work).
5. Request a test artist and watch it appear in Lidarr; delete it in Lidarr
   afterwards if it was just a test.

The old app is untouched throughout. If anything is off, you've lost nothing.

## Phase 4 — Cut over (5 min)

1. In **NPM** (CT 106): point your `musicseer.home` proxy host at
   `10.0.10.253:8688` (or create it — your CLAUDE.md notes it was never set
   up for the old one).
2. Tell the family. Old bookmarks to `10.0.10.251:8688` keep working until
   Phase 5, so there's no flag day.

## Phase 5 — Retire the old app (whenever you're comfortable)

```bash
pct stop 109                  # keep it stopped-but-present for a while
# …two quiet weeks later…
vzdump 109 --mode stop --storage local --compress zstd   # final keepsake
pct destroy 109
```

You already took a vzdump in Phase 0, so even after destroy you can restore.

## Ongoing

- **Backups:** the new app's entire state is `/var/lib/musicseer/`
  (`musicseer.db` + `secret.key`) — include CT 112 in your normal vzdump
  schedule and you're covered.
- **Updates:** replace the binary, restart the service (or
  `docker compose pull && up -d`). The schema migrates itself.
- **DNS check habit** (from your build notes): after creating CT 112, verify
  `pct exec 112 -- grep nameserver /etc/resolv.conf` shows `10.0.10.1` — the
  install script pins it, but it costs two seconds to confirm.
