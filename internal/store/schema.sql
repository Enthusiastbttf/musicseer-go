PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS users (
    id               INTEGER PRIMARY KEY,
    username         TEXT NOT NULL UNIQUE COLLATE NOCASE,
    email            TEXT UNIQUE COLLATE NOCASE,
    password_hash    TEXT,
    role             TEXT NOT NULL DEFAULT 'user' CHECK (role IN ('admin','user')),
    can_auto_approve INTEGER NOT NULL DEFAULT 0,
    created_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE IF NOT EXISTS sessions (
    token_hash TEXT PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);

CREATE TABLE IF NOT EXISTS instances (
    id                  INTEGER PRIMARY KEY,
    name                TEXT NOT NULL,
    type                TEXT NOT NULL CHECK (type IN ('navidrome','lidarr')),
    base_url            TEXT NOT NULL,
    username            TEXT,
    api_key             TEXT NOT NULL, -- AES-GCM encrypted
    is_active           INTEGER NOT NULL DEFAULT 1,
    is_auth_source      INTEGER NOT NULL DEFAULT 0,
    quality_profile_id  INTEGER,
    metadata_profile_id INTEGER,
    root_folder         TEXT,
    created_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    UNIQUE (base_url, type)
);

-- Snapshot of artists present in each Navidrome instance.
CREATE TABLE IF NOT EXISTS library_artists (
    instance_id INTEGER NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    name        TEXT NOT NULL COLLATE NOCASE,
    mbid        TEXT,
    weight      INTEGER NOT NULL DEFAULT 0, -- starred=100, rating*10, else album_count
    album_count INTEGER NOT NULL DEFAULT 0,
    synced_at   TEXT NOT NULL,
    PRIMARY KEY (instance_id, name)
);

-- Global artist metadata cache (Last.fm counts, genres, resolved image).
CREATE TABLE IF NOT EXISTS artists (
    id               INTEGER PRIMARY KEY,
    name             TEXT NOT NULL UNIQUE COLLATE NOCASE,
    mbid             TEXT,
    listeners        INTEGER NOT NULL DEFAULT 0,
    playcount        INTEGER NOT NULL DEFAULT 0,
    genres           TEXT NOT NULL DEFAULT '[]',
    image_url        TEXT,
    image_checked_at TEXT,
    synced_at        TEXT
);
CREATE INDEX IF NOT EXISTS idx_artists_mbid ON artists(mbid);

-- Last.fm similar-artist lists, cached whole as JSON.
CREATE TABLE IF NOT EXISTS similar (
    source    TEXT PRIMARY KEY COLLATE NOCASE,
    data      TEXT NOT NULL,
    cached_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS trending (
    chart     TEXT NOT NULL DEFAULT 'global',
    rank      INTEGER NOT NULL,
    name      TEXT NOT NULL,
    mbid      TEXT,
    cached_at TEXT NOT NULL,
    PRIMARY KEY (chart, rank)
);

-- Precomputed per-user recommendation payloads.
CREATE TABLE IF NOT EXISTS recommendations (
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind        TEXT NOT NULL CHECK (kind IN ('similar','gems')),
    data        TEXT NOT NULL,
    computed_at TEXT NOT NULL,
    PRIMARY KEY (user_id, kind)
);

CREATE TABLE IF NOT EXISTS requests (
    id          INTEGER PRIMARY KEY,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    artist_name TEXT NOT NULL,
    artist_mbid TEXT,
    album_name  TEXT,
    album_mbid  TEXT,
    status      TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','approved','rejected','sent','failed')),
    instance_id INTEGER REFERENCES instances(id) ON DELETE SET NULL,
    lidarr_id   INTEGER,
    notes       TEXT,
    error       TEXT,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX IF NOT EXISTS idx_requests_user ON requests(user_id);
CREATE INDEX IF NOT EXISTS idx_requests_status ON requests(status);
-- The open-request uniqueness index is created in store.migrate() so it runs
-- AFTER the album columns are added to databases from older versions.

CREATE TABLE IF NOT EXISTS user_instances (
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    instance_id INTEGER NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, instance_id)
);

-- Cached genre browse results (7-day TTL).
CREATE TABLE IF NOT EXISTS genre_cache (
    genre     TEXT PRIMARY KEY COLLATE NOCASE,
    data      TEXT NOT NULL,
    cached_at TEXT NOT NULL
);

-- Cached album track lists (30-day TTL).
CREATE TABLE IF NOT EXISTS album_tracks (
    mbid      TEXT PRIMARY KEY,
    data      TEXT NOT NULL,
    cached_at TEXT NOT NULL
);

-- Cached artist detail pages: bio + discography (7-day TTL).
CREATE TABLE IF NOT EXISTS artist_detail (
    mbid      TEXT PRIMARY KEY,
    name      TEXT NOT NULL,
    data      TEXT NOT NULL,
    cached_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
