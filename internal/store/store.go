// Package store wraps the SQLite database. All queries the app runs live
// here so the rest of the code never touches SQL directly.
package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "embed"

	_ "github.com/mattn/go-sqlite3"
)

//go:embed schema.sql
var schemaSQL string

var ErrNotFound = errors.New("not found")

type Store struct{ DB *sql.DB }

func Open(dataDir string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_journal_mode=WAL&_foreign_keys=on&_busy_timeout=5000",
		filepath.Join(dataDir, "musicseer.db"))
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite is single-writer; one connection avoids SQLITE_BUSY entirely and
	// is plenty for a homelab request tool (reads are microseconds in WAL mode).
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schemaSQL); err != nil {
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	if err := migrate(db); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{DB: db}, nil
}

// migrate upgrades databases created by older versions in place.
func migrate(db *sql.DB) error {
	// v2.3.0: album-level requests.
	var hasAlbum bool
	rows, err := db.Query("PRAGMA table_info(requests)")
	if err != nil {
		return err
	}
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			rows.Close()
			return err
		}
		if name == "album_mbid" {
			hasAlbum = true
		}
	}
	rows.Close()
	if !hasAlbum {
		stmts := []string{
			"ALTER TABLE requests ADD COLUMN album_name TEXT",
			"ALTER TABLE requests ADD COLUMN album_mbid TEXT",
			"DROP INDEX IF EXISTS idx_requests_unique_open",
		}
		for _, q := range stmts {
			if _, err := db.Exec(q); err != nil {
				return err
			}
		}
	}
	// Created here (not in schema.sql) so it always runs after the album
	// columns exist, on both fresh and upgraded databases.
	if _, err = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_requests_unique_open2
		ON requests(user_id, artist_name, IFNULL(album_mbid,'')) WHERE status IN ('pending','approved','sent')`); err != nil {
		return err
	}

	// v2.5.0: Plex account linkage. v2.9.0: per-user Last.fm username.
	var hasPlex, hasLfUser bool
	rows, err = db.Query("PRAGMA table_info(users)")
	if err != nil {
		return err
	}
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			rows.Close()
			return err
		}
		if name == "plex_id" {
			hasPlex = true
		}
		if name == "lastfm_user" {
			hasLfUser = true
		}
	}
	rows.Close()
	if !hasPlex {
		if _, err := db.Exec("ALTER TABLE users ADD COLUMN plex_id TEXT"); err != nil {
			return err
		}
	}
	if !hasLfUser {
		if _, err := db.Exec("ALTER TABLE users ADD COLUMN lastfm_user TEXT"); err != nil {
			return err
		}
	}
	return nil
}

func now() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000Z") }

// ---------- users ----------

type User struct {
	ID             int64  `json:"id"`
	Username       string `json:"username"`
	Email          string `json:"email,omitempty"`
	PasswordHash   string `json:"-"`
	Role           string `json:"role"`
	CanAutoApprove bool   `json:"canAutoApprove"`
	LastfmUser     string `json:"lastfmUser,omitempty"`
	CreatedAt      string `json:"createdAt"`
}

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	var u User
	var email, hash, lfUser sql.NullString
	err := row.Scan(&u.ID, &u.Username, &email, &hash, &u.Role, &u.CanAutoApprove, &lfUser, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	u.Email, u.PasswordHash, u.LastfmUser = email.String, hash.String, lfUser.String
	return &u, nil
}

const userCols = "id, username, email, password_hash, role, can_auto_approve, lastfm_user, created_at"

func (s *Store) UserCount() (int, error) {
	var n int
	return n, s.DB.QueryRow("SELECT COUNT(*) FROM users").Scan(&n)
}

func (s *Store) UserByID(id int64) (*User, error) {
	return scanUser(s.DB.QueryRow("SELECT "+userCols+" FROM users WHERE id=?", id))
}

func (s *Store) UserByLogin(login string) (*User, error) {
	return scanUser(s.DB.QueryRow("SELECT "+userCols+" FROM users WHERE username=? OR email=?", login, login))
}

func (s *Store) Users() ([]User, error) {
	rows, err := s.DB.Query("SELECT " + userCols + " FROM users ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

func (s *Store) CreateUser(username, email, hash, role string, autoApprove bool) (int64, error) {
	res, err := s.DB.Exec(
		"INSERT INTO users (username, email, password_hash, role, can_auto_approve) VALUES (?,?,?,?,?)",
		username, nullIfEmpty(email), nullIfEmpty(hash), role, autoApprove)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateUser(id int64, role *string, autoApprove *bool, hash *string, lastfmUser *string) error {
	sets, args := []string{"updated_at=?"}, []any{now()}
	if role != nil {
		sets, args = append(sets, "role=?"), append(args, *role)
	}
	if autoApprove != nil {
		sets, args = append(sets, "can_auto_approve=?"), append(args, *autoApprove)
	}
	if hash != nil {
		sets, args = append(sets, "password_hash=?"), append(args, *hash)
	}
	if lastfmUser != nil {
		sets, args = append(sets, "lastfm_user=?"), append(args, nullIfEmpty(strings.TrimSpace(*lastfmUser)))
	}
	args = append(args, id)
	_, err := s.DB.Exec("UPDATE users SET "+strings.Join(sets, ", ")+" WHERE id=?", args...)
	return err
}

// UserByPlexID finds the local account linked to a plex.tv account id.
func (s *Store) UserByPlexID(plexID string) (*User, error) {
	return scanUser(s.DB.QueryRow("SELECT "+userCols+" FROM users WHERE plex_id=?", plexID))
}

// LinkPlex attaches a plex.tv account id to a local user.
func (s *Store) LinkPlex(userID int64, plexID string) error {
	_, err := s.DB.Exec("UPDATE users SET plex_id=?, updated_at=? WHERE id=?", plexID, now(), userID)
	return err
}

func (s *Store) DeleteUser(id int64) error {
	_, err := s.DB.Exec("DELETE FROM users WHERE id=?", id)
	return err
}

// ---------- sessions ----------

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func (s *Store) CreateSession(token string, userID int64, ttl time.Duration) error {
	_, err := s.DB.Exec("INSERT INTO sessions (token_hash, user_id, expires_at) VALUES (?,?,?)",
		hashToken(token), userID, time.Now().UTC().Add(ttl).Format(time.RFC3339))
	return err
}

func (s *Store) SessionUser(token string) (*User, error) {
	return scanUser(s.DB.QueryRow(`
		SELECT u.id, u.username, u.email, u.password_hash, u.role, u.can_auto_approve, u.lastfm_user, u.created_at
		FROM sessions se JOIN users u ON u.id = se.user_id
		WHERE se.token_hash=? AND se.expires_at > ?`, hashToken(token), time.Now().UTC().Format(time.RFC3339)))
}

func (s *Store) DeleteSession(token string) error {
	_, err := s.DB.Exec("DELETE FROM sessions WHERE token_hash=?", hashToken(token))
	return err
}

func (s *Store) PruneSessions() {
	s.DB.Exec("DELETE FROM sessions WHERE expires_at <= ?", time.Now().UTC().Format(time.RFC3339))
}

// ---------- instances ----------

type Instance struct {
	ID                int64  `json:"id"`
	Name              string `json:"name"`
	Type              string `json:"type"`
	BaseURL           string `json:"baseUrl"`
	Username          string `json:"username,omitempty"`
	APIKeyEnc         string `json:"-"`
	IsActive          bool   `json:"isActive"`
	IsAuthSource      bool   `json:"isAuthSource"`
	QualityProfileID  int64  `json:"qualityProfileId,omitempty"`
	MetadataProfileID int64  `json:"metadataProfileId,omitempty"`
	RootFolder        string `json:"rootFolder,omitempty"`
}

const instCols = "id, name, type, base_url, COALESCE(username,''), api_key, is_active, is_auth_source, COALESCE(quality_profile_id,0), COALESCE(metadata_profile_id,0), COALESCE(root_folder,'')"

func scanInstance(row interface{ Scan(...any) error }) (*Instance, error) {
	var i Instance
	err := row.Scan(&i.ID, &i.Name, &i.Type, &i.BaseURL, &i.Username, &i.APIKeyEnc,
		&i.IsActive, &i.IsAuthSource, &i.QualityProfileID, &i.MetadataProfileID, &i.RootFolder)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &i, err
}

func (s *Store) Instances(onlyType string) ([]Instance, error) {
	q, args := "SELECT "+instCols+" FROM instances", []any{}
	if onlyType != "" {
		q, args = q+" WHERE type=?", append(args, onlyType)
	}
	rows, err := s.DB.Query(q+" ORDER BY id", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Instance
	for rows.Next() {
		i, err := scanInstance(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *i)
	}
	return out, rows.Err()
}

func (s *Store) InstanceByID(id int64) (*Instance, error) {
	return scanInstance(s.DB.QueryRow("SELECT "+instCols+" FROM instances WHERE id=?", id))
}

func (s *Store) FirstActiveInstance(typ string) (*Instance, error) {
	return scanInstance(s.DB.QueryRow(
		"SELECT "+instCols+" FROM instances WHERE type=? AND is_active=1 ORDER BY is_auth_source DESC, id LIMIT 1", typ))
}

func (s *Store) CreateInstance(i *Instance) (int64, error) {
	res, err := s.DB.Exec(`INSERT INTO instances
		(name, type, base_url, username, api_key, is_active, is_auth_source, quality_profile_id, metadata_profile_id, root_folder)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		i.Name, i.Type, i.BaseURL, nullIfEmpty(i.Username), i.APIKeyEnc, i.IsActive, i.IsAuthSource,
		zeroToNull(i.QualityProfileID), zeroToNull(i.MetadataProfileID), nullIfEmpty(i.RootFolder))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateInstance(i *Instance) error {
	_, err := s.DB.Exec(`UPDATE instances SET name=?, base_url=?, username=?, api_key=?, is_active=?,
		is_auth_source=?, quality_profile_id=?, metadata_profile_id=?, root_folder=?, updated_at=? WHERE id=?`,
		i.Name, i.BaseURL, nullIfEmpty(i.Username), i.APIKeyEnc, i.IsActive, i.IsAuthSource,
		zeroToNull(i.QualityProfileID), zeroToNull(i.MetadataProfileID), nullIfEmpty(i.RootFolder), now(), i.ID)
	return err
}

func (s *Store) DeleteInstance(id int64) error {
	_, err := s.DB.Exec("DELETE FROM instances WHERE id=?", id)
	return err
}

// ---------- library ----------

type LibraryArtist struct {
	Name   string
	MBID   string
	Weight int
}

// ReplaceLibrary swaps the snapshot for an instance atomically.
func (s *Store) ReplaceLibrary(instanceID int64, artists []LibraryArtist) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec("DELETE FROM library_artists WHERE instance_id=?", instanceID); err != nil {
		return err
	}
	stmt, err := tx.Prepare("INSERT OR REPLACE INTO library_artists (instance_id, name, mbid, weight, synced_at) VALUES (?,?,?,?,?)")
	if err != nil {
		return err
	}
	defer stmt.Close()
	ts := now()
	for _, a := range artists {
		if _, err := stmt.Exec(instanceID, a.Name, nullIfEmpty(a.MBID), a.Weight, ts); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// LibraryTop returns the highest-weighted artists across all active instances.
func (s *Store) LibraryTop(limit int) ([]LibraryArtist, error) {
	rows, err := s.DB.Query(`
		SELECT la.name, COALESCE(la.mbid,''), MAX(la.weight)
		FROM library_artists la JOIN instances i ON i.id = la.instance_id AND i.is_active=1
		GROUP BY la.name ORDER BY MAX(la.weight) DESC, la.name LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LibraryArtist
	for rows.Next() {
		var a LibraryArtist
		if err := rows.Scan(&a.Name, &a.MBID, &a.Weight); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// LibraryIndex returns two sets for membership checks: MBIDs of library
// artists that have one, and lowercase names of those that do not. Matching
// by MBID first prevents identically-named artists from inheriting each
// other's "in library" status.
func (s *Store) LibraryIndex() (mbids, namesNoMbid map[string]bool, err error) {
	rows, err := s.DB.Query("SELECT lower(name), lower(COALESCE(mbid,'')) FROM library_artists")
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	mbids, namesNoMbid = map[string]bool{}, map[string]bool{}
	for rows.Next() {
		var name, mbid string
		if err := rows.Scan(&name, &mbid); err != nil {
			return nil, nil, err
		}
		if mbid != "" {
			mbids[mbid] = true
		} else {
			namesNoMbid[name] = true
		}
	}
	return mbids, namesNoMbid, rows.Err()
}

// RequestedIndex is the same shape for open artist-level requests.
func (s *Store) RequestedIndex() (mbids, namesNoMbid map[string]bool, err error) {
	rows, err := s.DB.Query(`SELECT lower(artist_name), lower(COALESCE(artist_mbid,'')) FROM requests
		WHERE album_mbid IS NULL AND status IN ('pending','approved','sent')`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	mbids, namesNoMbid = map[string]bool{}, map[string]bool{}
	for rows.Next() {
		var name, mbid string
		if err := rows.Scan(&name, &mbid); err != nil {
			return nil, nil, err
		}
		if mbid != "" {
			mbids[mbid] = true
		} else {
			namesNoMbid[name] = true
		}
	}
	return mbids, namesNoMbid, rows.Err()
}

// LibraryNames returns a lowercase set of every artist name in any library.
func (s *Store) LibraryNames() (map[string]bool, error) {
	rows, err := s.DB.Query("SELECT DISTINCT lower(name) FROM library_artists")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	set := map[string]bool{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		set[n] = true
	}
	return set, rows.Err()
}

func (s *Store) LibraryCount() (int, error) {
	var n int
	return n, s.DB.QueryRow("SELECT COUNT(DISTINCT name) FROM library_artists").Scan(&n)
}

// ---------- artist metadata cache ----------

type Artist struct {
	Name      string   `json:"name"`
	MBID      string   `json:"mbid,omitempty"`
	Listeners int64    `json:"listeners"`
	Playcount int64    `json:"playcount"`
	Genres    []string `json:"genres"`
	ImageURL  string   `json:"imageUrl,omitempty"`
}

func (s *Store) UpsertArtist(a *Artist) error {
	genres, _ := json.Marshal(a.Genres)
	_, err := s.DB.Exec(`INSERT INTO artists (name, mbid, listeners, playcount, genres, synced_at)
		VALUES (?,?,?,?,?,?)
		ON CONFLICT(name) DO UPDATE SET
			mbid=COALESCE(NULLIF(excluded.mbid,''), artists.mbid),
			listeners=excluded.listeners, playcount=excluded.playcount,
			genres=CASE WHEN excluded.genres='[]' THEN artists.genres ELSE excluded.genres END,
			synced_at=excluded.synced_at`,
		a.Name, nullIfEmpty(a.MBID), a.Listeners, a.Playcount, string(genres), now())
	return err
}

func (s *Store) SetArtistImage(name, url string) error {
	_, err := s.DB.Exec(`INSERT INTO artists (name, image_url, image_checked_at) VALUES (?,?,?)
		ON CONFLICT(name) DO UPDATE SET image_url=excluded.image_url, image_checked_at=excluded.image_checked_at`,
		name, nullIfEmpty(url), now())
	return err
}

// ArtistsByNames fetches metadata for many artists in one query.
func (s *Store) ArtistsByNames(names []string) (map[string]*Artist, error) {
	out := map[string]*Artist{}
	if len(names) == 0 {
		return out, nil
	}
	const chunk = 400
	for start := 0; start < len(names); start += chunk {
		end := min(start+chunk, len(names))
		batch := names[start:end]
		ph := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		args := make([]any, len(batch))
		for i, n := range batch {
			args[i] = n
		}
		rows, err := s.DB.Query(
			"SELECT name, COALESCE(mbid,''), listeners, playcount, genres, COALESCE(image_url,'') FROM artists WHERE name IN ("+ph+")", args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var a Artist
			var genres string
			if err := rows.Scan(&a.Name, &a.MBID, &a.Listeners, &a.Playcount, &genres, &a.ImageURL); err != nil {
				rows.Close()
				return nil, err
			}
			json.Unmarshal([]byte(genres), &a.Genres)
			out[strings.ToLower(a.Name)] = &a
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ArtistsMissingImages lists artists that still need an image lookup.
func (s *Store) ArtistsMissingImages(limit int) ([]Artist, error) {
	rows, err := s.DB.Query(`SELECT name, COALESCE(mbid,'') FROM artists
		WHERE (image_url IS NULL OR image_url='') AND image_checked_at IS NULL LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Artist
	for rows.Next() {
		var a Artist
		if err := rows.Scan(&a.Name, &a.MBID); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) MarkImageChecked(name string) {
	s.DB.Exec("UPDATE artists SET image_checked_at=? WHERE name=?", now(), name)
}

func (s *Store) ArtistCount() (int, error) {
	var n int
	return n, s.DB.QueryRow("SELECT COUNT(*) FROM artists").Scan(&n)
}

// ---------- similar cache ----------

type SimilarArtist struct {
	Name  string  `json:"name"`
	MBID  string  `json:"mbid,omitempty"`
	Match float64 `json:"match"`
}

func (s *Store) SimilarCached(source string, maxAge time.Duration) ([]SimilarArtist, bool) {
	var data, cachedAt string
	err := s.DB.QueryRow("SELECT data, cached_at FROM similar WHERE source=?", source).Scan(&data, &cachedAt)
	if err != nil {
		return nil, false
	}
	t, err := time.Parse("2006-01-02T15:04:05.000Z", cachedAt)
	if err != nil || time.Since(t) > maxAge {
		return nil, false
	}
	var out []SimilarArtist
	if json.Unmarshal([]byte(data), &out) != nil {
		return nil, false
	}
	return out, true
}

// ClearSimilar wipes the similar-artist cache — used when the discovery
// backend switches (Last.fm vs ListenBrainz scores are not comparable).
func (s *Store) ClearSimilar() error {
	_, err := s.DB.Exec("DELETE FROM similar")
	return err
}

func (s *Store) SaveSimilar(source string, artists []SimilarArtist) error {
	data, _ := json.Marshal(artists)
	_, err := s.DB.Exec(`INSERT INTO similar (source, data, cached_at) VALUES (?,?,?)
		ON CONFLICT(source) DO UPDATE SET data=excluded.data, cached_at=excluded.cached_at`,
		source, string(data), now())
	return err
}

// ---------- trending ----------

type TrendingArtist struct {
	Rank int    `json:"rank"`
	Name string `json:"name"`
	MBID string `json:"mbid,omitempty"`
}

func (s *Store) ReplaceTrending(chart string, artists []TrendingArtist) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec("DELETE FROM trending WHERE chart=?", chart); err != nil {
		return err
	}
	ts := now()
	for _, a := range artists {
		if _, err := tx.Exec("INSERT INTO trending (chart, rank, name, mbid, cached_at) VALUES (?,?,?,?,?)",
			chart, a.Rank, a.Name, nullIfEmpty(a.MBID), ts); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) Trending(chart string, limit int) ([]TrendingArtist, string, error) {
	rows, err := s.DB.Query("SELECT rank, name, COALESCE(mbid,''), cached_at FROM trending WHERE chart=? ORDER BY rank LIMIT ?", chart, limit)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	var out []TrendingArtist
	var cachedAt string
	for rows.Next() {
		var a TrendingArtist
		if err := rows.Scan(&a.Rank, &a.Name, &a.MBID, &cachedAt); err != nil {
			return nil, "", err
		}
		out = append(out, a)
	}
	return out, cachedAt, rows.Err()
}

// ---------- recommendations ----------

func (s *Store) SaveRecommendations(userID int64, kind string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(`INSERT INTO recommendations (user_id, kind, data, computed_at) VALUES (?,?,?,?)
		ON CONFLICT(user_id, kind) DO UPDATE SET data=excluded.data, computed_at=excluded.computed_at`,
		userID, kind, string(data), now())
	return err
}

func (s *Store) Recommendations(userID int64, kind string) (json.RawMessage, time.Time, error) {
	var data, computedAt string
	err := s.DB.QueryRow("SELECT data, computed_at FROM recommendations WHERE user_id=? AND kind=?", userID, kind).
		Scan(&data, &computedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, time.Time{}, ErrNotFound
	}
	if err != nil {
		return nil, time.Time{}, err
	}
	t, _ := time.Parse("2006-01-02T15:04:05.000Z", computedAt)
	return json.RawMessage(data), t, nil
}

// ---------- requests ----------

type Request struct {
	ID         int64  `json:"id"`
	UserID     int64  `json:"userId"`
	Username   string `json:"username"`
	ArtistName string `json:"artistName"`
	ArtistMBID string `json:"artistMbid,omitempty"`
	AlbumName  string `json:"albumName,omitempty"`
	AlbumMBID  string `json:"albumMbid,omitempty"`
	Status     string `json:"status"`
	LidarrID   int64  `json:"lidarrId,omitempty"`
	Notes      string `json:"notes,omitempty"`
	Error      string `json:"error,omitempty"`
	CreatedAt  string `json:"createdAt"`
	UpdatedAt  string `json:"updatedAt"`
}

func (s *Store) CreateRequest(userID int64, name, mbid, albumName, albumMbid, status string) (int64, error) {
	res, err := s.DB.Exec(
		"INSERT INTO requests (user_id, artist_name, artist_mbid, album_name, album_mbid, status) VALUES (?,?,?,?,?,?)",
		userID, name, nullIfEmpty(mbid), nullIfEmpty(albumName), nullIfEmpty(albumMbid), status)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

const reqQuery = `SELECT r.id, r.user_id, u.username, r.artist_name, COALESCE(r.artist_mbid,''),
	COALESCE(r.album_name,''), COALESCE(r.album_mbid,''), r.status,
	COALESCE(r.lidarr_id,0), COALESCE(r.notes,''), COALESCE(r.error,''), r.created_at, r.updated_at
	FROM requests r JOIN users u ON u.id = r.user_id`

func (s *Store) RequestsList(userID int64, all bool) ([]Request, error) {
	q, args := reqQuery+" ORDER BY r.created_at DESC LIMIT 500", []any{}
	if !all {
		q = reqQuery + " WHERE r.user_id=? ORDER BY r.created_at DESC LIMIT 500"
		args = append(args, userID)
	}
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Request
	for rows.Next() {
		var r Request
		if err := rows.Scan(&r.ID, &r.UserID, &r.Username, &r.ArtistName, &r.ArtistMBID,
			&r.AlbumName, &r.AlbumMBID, &r.Status,
			&r.LidarrID, &r.Notes, &r.Error, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) RequestByID(id int64) (*Request, error) {
	var r Request
	err := s.DB.QueryRow(reqQuery+" WHERE r.id=?", id).Scan(&r.ID, &r.UserID, &r.Username, &r.ArtistName,
		&r.ArtistMBID, &r.AlbumName, &r.AlbumMBID, &r.Status, &r.LidarrID, &r.Notes, &r.Error, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &r, err
}

func (s *Store) UpdateRequestStatus(id int64, status, notes, errMsg string, lidarrID int64) error {
	_, err := s.DB.Exec(`UPDATE requests SET status=?, notes=COALESCE(NULLIF(?,''), notes),
		error=NULLIF(?,''), lidarr_id=COALESCE(NULLIF(?,0), lidarr_id), updated_at=? WHERE id=?`,
		status, notes, errMsg, lidarrID, now(), id)
	return err
}

func (s *Store) DeleteRequest(id int64) error {
	_, err := s.DB.Exec("DELETE FROM requests WHERE id=?", id)
	return err
}

// OpenAlbumRequests returns album MBIDs with an open request for this user or all users.
func (s *Store) OpenAlbumRequests(artistMbid string) (map[string]bool, error) {
	rows, err := s.DB.Query(`SELECT DISTINCT album_mbid FROM requests
		WHERE artist_mbid=? AND album_mbid IS NOT NULL AND status IN ('pending','approved','sent')`, artistMbid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	set := map[string]bool{}
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			return nil, err
		}
		set[m] = true
	}
	return set, rows.Err()
}

// LibraryGenres returns genre tags across library artists, most common first.
func (s *Store) LibraryGenres(limit int) ([]string, error) {
	rows, err := s.DB.Query(`
		SELECT a.genres FROM artists a
		JOIN library_artists la ON lower(la.name) = lower(a.name)
		WHERE a.genres != '[]'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var g string
		if err := rows.Scan(&g); err != nil {
			return nil, err
		}
		var genres []string
		json.Unmarshal([]byte(g), &genres)
		for _, genre := range genres {
			counts[strings.ToLower(genre)]++
		}
	}
	type gc struct {
		g string
		n int
	}
	all := make([]gc, 0, len(counts))
	for g, n := range counts {
		all = append(all, gc{g, n})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].n > all[j].n })
	if len(all) > limit {
		all = all[:limit]
	}
	out := make([]string, len(all))
	for i, e := range all {
		out[i] = e.g
	}
	return out, rows.Err()
}

// ---------- genre cache ----------

func (s *Store) GenreCached(genre string, maxAge time.Duration) (json.RawMessage, bool) {
	var data, cachedAt string
	if err := s.DB.QueryRow("SELECT data, cached_at FROM genre_cache WHERE genre=?", genre).Scan(&data, &cachedAt); err != nil {
		return nil, false
	}
	t, err := time.Parse("2006-01-02T15:04:05.000Z", cachedAt)
	if err != nil || time.Since(t) > maxAge {
		return nil, false
	}
	return json.RawMessage(data), true
}

func (s *Store) SaveGenre(genre string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(`INSERT INTO genre_cache (genre, data, cached_at) VALUES (?,?,?)
		ON CONFLICT(genre) DO UPDATE SET data=excluded.data, cached_at=excluded.cached_at`,
		genre, string(data), now())
	return err
}

// LibraryArtistsMissingGenres lists library artists (with MBIDs) whose genre
// tags haven't been fetched yet — fed to the MusicBrainz enrichment loop.
func (s *Store) LibraryArtistsMissingGenres(limit int) ([]LibraryArtist, error) {
	rows, err := s.DB.Query(`
		SELECT DISTINCT la.name, la.mbid FROM library_artists la
		LEFT JOIN artists a ON lower(a.name) = lower(la.name)
		WHERE la.mbid IS NOT NULL AND la.mbid != ''
		  AND (a.name IS NULL OR a.genres = '[]')
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LibraryArtist
	for rows.Next() {
		var a LibraryArtist
		if err := rows.Scan(&a.Name, &a.MBID); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ---------- album tracks cache ----------

func (s *Store) AlbumTracksCached(mbid string, maxAge time.Duration) (json.RawMessage, bool) {
	var data, cachedAt string
	if err := s.DB.QueryRow("SELECT data, cached_at FROM album_tracks WHERE mbid=?", mbid).Scan(&data, &cachedAt); err != nil {
		return nil, false
	}
	t, err := time.Parse("2006-01-02T15:04:05.000Z", cachedAt)
	if err != nil || time.Since(t) > maxAge {
		return nil, false
	}
	return json.RawMessage(data), true
}

func (s *Store) SaveAlbumTracks(mbid string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(`INSERT INTO album_tracks (mbid, data, cached_at) VALUES (?,?,?)
		ON CONFLICT(mbid) DO UPDATE SET data=excluded.data, cached_at=excluded.cached_at`,
		mbid, string(data), now())
	return err
}

// ---------- artist detail cache ----------

func (s *Store) ArtistDetailCached(mbid string, maxAge time.Duration) (json.RawMessage, bool) {
	var data, cachedAt string
	if err := s.DB.QueryRow("SELECT data, cached_at FROM artist_detail WHERE mbid=?", mbid).Scan(&data, &cachedAt); err != nil {
		return nil, false
	}
	t, err := time.Parse("2006-01-02T15:04:05.000Z", cachedAt)
	if err != nil || time.Since(t) > maxAge {
		return nil, false
	}
	return json.RawMessage(data), true
}

func (s *Store) SaveArtistDetail(mbid, name string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(`INSERT INTO artist_detail (mbid, name, data, cached_at) VALUES (?,?,?,?)
		ON CONFLICT(mbid) DO UPDATE SET name=excluded.name, data=excluded.data, cached_at=excluded.cached_at`,
		mbid, name, string(data), now())
	return err
}

// RequestedNames returns lowercase artist names with an open request.
func (s *Store) RequestedNames() (map[string]bool, error) {
	rows, err := s.DB.Query("SELECT DISTINCT lower(artist_name) FROM requests WHERE album_mbid IS NULL AND status IN ('pending','approved','sent')")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	set := map[string]bool{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		set[n] = true
	}
	return set, rows.Err()
}

func (s *Store) RequestCounts() (map[string]int, error) {
	rows, err := s.DB.Query("SELECT status, COUNT(*) FROM requests GROUP BY status")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var st string
		var n int
		if err := rows.Scan(&st, &n); err != nil {
			return nil, err
		}
		out[st] = n
	}
	return out, rows.Err()
}

// ---------- settings ----------

func (s *Store) Setting(key string) string {
	var v string
	s.DB.QueryRow("SELECT value FROM settings WHERE key=?", key).Scan(&v)
	return v
}

func (s *Store) SetSetting(key, value string) error {
	_, err := s.DB.Exec("INSERT INTO settings (key, value) VALUES (?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", key, value)
	return err
}

// ---------- helpers ----------

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func zeroToNull(n int64) any {
	if n == 0 {
		return nil
	}
	return n
}
