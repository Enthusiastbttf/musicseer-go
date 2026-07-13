# Why MusicSeer 2 is faster — an audit of the original codebase

This is the analysis that drove the rewrite. Every claim cites the original
source (`tkharbeche/musicseer`, commit `aa3ecfe`).

## TL;DR

| | Original | MusicSeer 2 |
|---|---|---|
| Processes | 4 containers (Next.js, NestJS, Postgres, + Node dev servers) | 1 binary |
| Runtime footprint | ~1–1.5 GB RAM | ~30–60 MB RAM |
| Home page load | seconds-to-minutes (external API calls in the request path) | single-digit ms (SQLite reads only) |
| External calls per page load | up to **hundreds** | **0** |
| Database | PostgreSQL server | embedded SQLite (WAL) |
| Frontend delivery | Next.js dev server (`target: development` in compose) | 62 KB gzipped static SPA, embedded, immutable-cached |
| Auth | JWT in `localStorage`, **open `/auth/register`** | HttpOnly cookie sessions, first-run-only setup, login throttling |
| Secrets at rest | API keys in **plaintext** | AES-256-GCM encrypted |
| Deploy | docker-compose only | Docker, or a single binary in an LXC/systemd |

## The specific problems, with receipts

### 1. External HTTP calls inside the page-load path (the big one)

`recommendation.service.ts#getRecommendations` runs **per request**:

- For each of 10 seed artists, a similarity lookup — DB-cached, but a cold or
  expired entry triggers a **sequential** Last.fm call (`similarity.service.ts`).
- For **every candidate** (10 seeds × 20 similar ≈ up to 200 artists):
  - an individual `findOne` on `artists_cache` (N+1),
  - if missing → `imageResolver.resolveArtistImage()` → **a Deezer HTTP call,
    then possibly an AudioDB HTTP call** — inline, before the response returns,
  - if the cached image looks bad → *another* resolve round-trip.

A single "Similar to you" render could fire hundreds of third-party HTTP
requests. Deezer's public limit is 50 requests/5s — after the first few dozen
candidates the service is throttled and every remaining call waits or fails.
This is why the app feels "slow and unusable": it re-earns its data on every
page view.

**MusicSeer 2:** recommendations are computed by a background job after each
library sync (or asynchronously on first login), stored as one JSON row per
user, and served with a single `SELECT`. Stale data is served instantly while
a refresh runs behind the scenes. Image resolution lives in a dedicated worker
with per-provider rate limits (Deezer 5/s, AudioDB 1/s, MusicBrainz 0.9/s).

### 2. "Hidden Gems" multiplies the damage ×5

`getHiddenGems()` literally calls `getRecommendations(userId, limit * 5)` —
all of the above, five times the volume, for a list that renders below the
fold. **MusicSeer 2** derives gems from the same candidate set in the same
background pass; serving them is another single-row read.

### 3. N+1 queries on trending

`trending.service.ts#getTrendingArtists` does up to **two** `findOne` queries
per trending artist (one by MBID, one by name fallback) — ~100–200 queries per
home page — and fires background image "catch-up" fetches on every view.
**MusicSeer 2** reads the chart and enriches it with **one** batched
`WHERE name IN (...)` query.

### 4. The trending sync ignores MusicBrainz's rate rules

`updateArtistCache` calls MusicBrainz for every charted artist with **no rate
limiting** (`musicbrainz.service.ts` has none). MusicBrainz enforces 1 req/s
and bans offenders — which then breaks genre enrichment silently. The sync
also makes ~4 HTTP calls × 100 artists sequentially, so a full refresh takes
minutes and overlaps user traffic. **MusicSeer 2** gets rank/listeners/
playcount from the single chart response, enriches genres for the top 25 at a
polite 0.9 req/s in the background, and never blocks anything.

### 5. Shipping development builds

`docker-compose.yml` builds both apps with `target: development` and bind-
mounts source. Production traffic was being served by `next dev` and
`nest start --watch`: no minification, no static optimization, hot-reload
watchers burning CPU, >1 GB RSS across containers. **MusicSeer 2** ships a
production Vite build (62 KB gzipped) embedded in a stripped Go binary.

### 6. Security gaps

- **Open registration:** `POST /auth/register` has no guard
  (`auth.controller.ts`) — anyone who can reach the port can create an
  account. v2 has a first-run-only setup endpoint; every other account is
  admin-created.
- **JWT in `localStorage`** (`login/page.tsx`) — readable by any XSS payload.
  v2 uses HttpOnly, SameSite cookies over hashed server-side sessions.
- **Plaintext API keys** in Postgres (`docker/init.sql: api_key VARCHAR`).
  v2 encrypts them AES-256-GCM with a key file created `0600` on first run.
- **No login throttling.** v2: 10 failures / 15 min / IP, constant-shape
  error responses.
- **Schema drift:** entities used by the code (`library_snapshot.userId`,
  `playCount`) don't exist in `init.sql` — the app only works with TypeORM
  auto-sync mutating the schema at boot. v2 owns its schema in one embedded
  SQL file.

### 7. Operational weight

Postgres is a fine database — and overkill for a single-family request tool
that stores a few thousand rows. It costs a container, a volume, credentials,
and its own backup story. **MusicSeer 2's** entire state is two files
(`musicseer.db`, `secret.key`); backup is `cp`.

## What was kept

The product concept and the recommendation model carried over on purpose: the
same scoring weights (popularity 0.4 / similarity 0.3 / diversity 0.2 /
freshness 0.1), the same seed-artist approach, the same data sources
(Last.fm, MusicBrainz, Deezer, TheAudioDB), the Navidrome-credentials login
idea, per-user auto-approve, and the pending → approved → sent-to-Lidarr
workflow. The rewrite changes *where and when* the work happens — not what
the app does.
