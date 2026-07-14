# MusicSeer 2

> Music discovery and request management for **Navidrome** + **Lidarr** — rebuilt from the ground up as a single fast binary.

MusicSeer 2 is an Overseerr-style request front end for music, built for people running the classic *arr pipeline (Lidarr + Prowlarr + a torrent client). It needs **no API keys at all** out of the box — discovery runs on the open ListenBrainz + MusicBrainz APIs, and upgrades itself to Last.fm's richer similarity data if you provide a key. It is **one ~9 MB Go binary with the web UI and database built in**, idles at ~30 MB of RAM, and serves every page from a local SQLite cache in single-digit milliseconds. It began as a ground-up rewrite of [tkharbeche/musicseer](https://github.com/tkharbeche/musicseer) (see docs/COMPARISON.md) and works with or without a Navidrome server.

## Features

- 🔥 **Trending Now** — global charts (Last.fm or ListenBrainz), refreshed in the background
- 🎯 **Similar to Your Library** — personalized recommendations scored on popularity, similarity, genre diversity and freshness
- 💎 **Hidden Gems** — artists your library says you'll love, under 500K listeners
- 🔎 **Search** — find any artist and request them in one click
- 📝 **Request workflow** — pending → approved → sent to Lidarr (auto-approve per user), with retry on failure
- 🔐 **Flexible library + login** — Lidarr's artist list doubles as the library source for recommendations (no Navidrome required); add Navidrome for richer signals (stars/ratings) and Navidrome-credential logins
- ⚙️ **Admin panel** — users, instances (with connection tests and Lidarr profile dropdowns), job status, manual syncs
- 📦 **Zero-dependency deploy** — one binary, one data directory; Docker image and Proxmox LXC installer included

## Architecture in one paragraph

Interactive requests **never** call an external API (the only exception: the search box makes one Last.fm query). Background jobs sync the Last.fm charts, your Navidrome library and per-user recommendations into SQLite on schedules; artist images are resolved by a rate-limited worker (Deezer → TheAudioDB fallback). Page loads are pure local reads. See [docs/COMPARISON.md](docs/COMPARISON.md) for why this matters and receipts from the old codebase.

## Quick start (Docker)

```bash
cp .env.example .env    # optionally add a Last.fm API key
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
2. **Admin → Instances** → add Lidarr (base URL + API key), then pick the **quality profile, metadata profile and root folder** from the dropdowns. Lidarr also serves as the library source for recommendations.
3. *(Optional)* **Admin → Instances** → add Navidrome (base URL + username/password; tick *login source* if family members should log in with Navidrome credentials).
4. *(Optional)* Add a **Last.fm API key** to the environment ([free](https://www.last.fm/api/account/create)) for Last.fm-based discovery; without one, the keyless ListenBrainz/MusicBrainz backend is used automatically.
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
| `LASTFM_API_KEY` | — | optional: switches discovery from ListenBrainz (keyless) to Last.fm |
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
