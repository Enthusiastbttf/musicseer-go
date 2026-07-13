# MusicSeer 2

> Music discovery and request management for **Navidrome** + **Lidarr** — rebuilt from the ground up as a single fast binary.

MusicSeer 2 is a complete rewrite of [MusicSeer](https://github.com/tkharbeche/musicseer). Same idea — an Overseerr-style request front end for music — but instead of four containers (Next.js + NestJS + Postgres + your patience), it is **one ~9 MB Go binary with the web UI and database built in**. It idles at ~30 MB of RAM and serves every page from a local SQLite cache in single-digit milliseconds.

## Features

- 🔥 **Trending Now** — global Last.fm charts, refreshed in the background
- 🎯 **Similar to Your Library** — personalized recommendations scored on popularity, similarity, genre diversity and freshness
- 💎 **Hidden Gems** — artists your library says you'll love, under 500K listeners
- 🔎 **Search** — find any artist and request them in one click
- 📝 **Request workflow** — pending → approved → sent to Lidarr (auto-approve per user), with retry on failure
- 🔐 **Navidrome login** — family members sign in with the Navidrome credentials they already have
- ⚙️ **Admin panel** — users, instances (with connection tests and Lidarr profile dropdowns), job status, manual syncs
- 📦 **Zero-dependency deploy** — one binary, one data directory; Docker image and Proxmox LXC installer included

## Architecture in one paragraph

Interactive requests **never** call an external API (the only exception: the search box makes one Last.fm query). Background jobs sync the Last.fm charts, your Navidrome library and per-user recommendations into SQLite on schedules; artist images are resolved by a rate-limited worker (Deezer → TheAudioDB fallback). Page loads are pure local reads. See [docs/COMPARISON.md](docs/COMPARISON.md) for why this matters and receipts from the old codebase.

## Quick start (Docker)

```bash
cp .env.example .env    # add your Last.fm API key
docker compose up -d
# open http://localhost:8688 and create your admin account
```

## Quick start (binary)

```bash
LASTFM_API_KEY=xxxx ./musicseer
# data lands in ./data by default; see configuration below
```

## Quick start (Proxmox LXC)

On the Proxmox host:

```bash
bash deploy/proxmox-create-lxc.sh \
  --ctid 112 --ip 10.0.10.253/24 --gateway 10.0.10.1 --dns 10.0.10.1 \
  --binary-url https://github.com/Enthusiastbttf/musicseer-go/releases/latest/download/musicseer-linux-amd64
```

The script creates an unprivileged Debian 13 container with pinned DNS, installs a hardened systemd service, and starts it on port 8688.

## First-run checklist

1. Open the web UI → create the **admin account** (setup closes itself afterwards — there is no open registration endpoint).
2. **Admin → Instances** → add Navidrome (base URL + a Navidrome username/password; tick *login source* if family members should log in with Navidrome credentials).
3. **Admin → Instances** → add Lidarr (base URL + API key), then pick the **quality profile, metadata profile and root folder** from the dropdowns.
4. Add your **Last.fm API key** to the environment ([get one free](https://www.last.fm/api/account/create)) and restart.
5. **Admin → Status** → *Sync library* once; recommendations build automatically after every sync.

## Migrating from the original MusicSeer

Fully non-destructive — the old Postgres is only ever read:

```bash
./musicseer migrate 'postgres://musicseer:PASS@10.0.10.251:5432/musicseer?sslmode=disable'
```

Users (bcrypt password hashes carry over — everyone keeps their password), server instances and request history are imported; API keys that the old app stored in plaintext are encrypted at rest on import. The command is idempotent — run it as often as you like. Full cutover plan: [docs/MIGRATION.md](docs/MIGRATION.md).

## Configuration

Everything is environment variables (a `.env` file in the working directory is also read):

| Variable | Default | Purpose |
|---|---|---|
| `MUSICSEER_PORT` | `8688` | HTTP port |
| `MUSICSEER_DATA_DIR` | `./data` | SQLite DB + encryption key |
| `LASTFM_API_KEY` | — | required for discovery |
| `MUSICBRAINZ_CONTACT` | `admin@example.com` | contact in MusicBrainz user-agent (their policy) |
| `MUSICSEER_TRENDING_INTERVAL` | `6h` | trending refresh |
| `MUSICSEER_LIBRARY_INTERVAL` | `12h` | library sync + recommendation rebuild |
| `MUSICSEER_RECS_TTL` | `12h` | staleness threshold before background refresh |
| `MUSICSEER_SESSION_TTL` | `720h` | login session lifetime |
| `MUSICSEER_LOG_LEVEL` | `info` | `debug`/`info`/`warn`/`error` |

## Building from source

```bash
# frontend (embedded into the binary)
cd web/app && npm ci && npm run build && cd ../..
# binary
CGO_ENABLED=1 go build -o musicseer ./cmd/musicseer
```

Tagged pushes build a static release binary and a Docker image via GitHub Actions.

## Backup

Stop nothing, copy two files: `musicseer.db` and `secret.key` from the data directory. That's the whole state.

## License

MIT
