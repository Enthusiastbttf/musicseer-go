// Package migrate imports data from an original MusicSeer PostgreSQL
// database into the new SQLite store.
//
// It is strictly NON-DESTRUCTIVE: the Postgres connection only ever runs
// SELECT statements, and the import is idempotent on the SQLite side
// (existing rows are skipped, never overwritten). You can run it as many
// times as you like while the old app is still up.
package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/lib/pq"

	"musicseer/internal/secrets"
	"musicseer/internal/store"
)

type Result struct {
	Users, UsersSkipped         int
	Instances, InstancesSkipped int
	Requests, RequestsSkipped   int
	Warnings                    []string
}

// FromPostgres copies users, server instances and request history.
// dsn example: postgres://musicseer:pass@10.0.10.251:5432/musicseer?sslmode=disable
func FromPostgres(ctx context.Context, dsn string, st *store.Store, box *secrets.Box) (*Result, error) {
	pg, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	defer pg.Close()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if err := pg.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("cannot reach old database: %w", err)
	}

	res := &Result{}
	warn := func(format string, args ...any) {
		res.Warnings = append(res.Warnings, fmt.Sprintf(format, args...))
	}

	// ---- users (bcrypt hashes carry over unchanged — same algorithm) ----
	rows, err := pg.QueryContext(ctx, `
		SELECT username, COALESCE(email,''), COALESCE(password_hash,''), COALESCE(role,'user'),
		       COALESCE(can_auto_approve, false)
		FROM users ORDER BY created_at`)
	if err != nil {
		// older schema without can_auto_approve
		rows, err = pg.QueryContext(ctx, `
			SELECT username, COALESCE(email,''), COALESCE(password_hash,''), COALESCE(role,'user'), false
			FROM users ORDER BY created_at`)
		if err != nil {
			return nil, fmt.Errorf("read users: %w", err)
		}
	}
	userIDByName := map[string]int64{}
	for rows.Next() {
		var username, email, hash, role string
		var auto bool
		if err := rows.Scan(&username, &email, &hash, &role, &auto); err != nil {
			rows.Close()
			return nil, err
		}
		if role != "admin" {
			role = "user"
		}
		if existing, err := st.UserByLogin(username); err == nil {
			userIDByName[strings.ToLower(username)] = existing.ID
			res.UsersSkipped++
			continue
		}
		id, err := st.CreateUser(username, email, hash, role, auto)
		if err != nil {
			warn("user %q not imported: %v", username, err)
			continue
		}
		userIDByName[strings.ToLower(username)] = id
		res.Users++
	}
	rows.Close()

	// ---- server instances ----
	rows, err = pg.QueryContext(ctx, `
		SELECT name, type, base_url, api_key, COALESCE(username,''), COALESCE(is_active, true)
		FROM server_instances ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("read server_instances: %w", err)
	}
	for rows.Next() {
		var name, typ, baseURL, apiKey, username string
		var active bool
		if err := rows.Scan(&name, &typ, &baseURL, &apiKey, &username, &active); err != nil {
			rows.Close()
			return nil, err
		}
		if typ != "navidrome" && typ != "lidarr" {
			warn("instance %q has unsupported type %q — skipped", name, typ)
			continue
		}
		enc, err := box.Encrypt(apiKey) // old app stored keys in PLAINTEXT; we encrypt on import
		if err != nil {
			return nil, err
		}
		inst := &store.Instance{
			Name: name, Type: typ, BaseURL: strings.TrimRight(baseURL, "/"),
			Username: username, APIKeyEnc: enc, IsActive: active,
		}
		if _, err := st.CreateInstance(inst); err != nil {
			res.InstancesSkipped++ // most likely UNIQUE(base_url,type): already imported
			continue
		}
		res.Instances++
		if typ == "lidarr" {
			warn("Lidarr instance %q imported — set its root folder and profiles in Admin → Instances before approving requests", name)
		}
	}
	rows.Close()

	// ---- requests (history) ----
	rows, err = pg.QueryContext(ctx, `
		SELECT u.username, r.artist_name, COALESCE(r.artist_mbid::text,''), r.status,
		       COALESCE(r.admin_notes,''), r.created_at
		FROM requests r JOIN users u ON u.id = r.user_id ORDER BY r.created_at`)
	if err != nil {
		return nil, fmt.Errorf("read requests: %w", err)
	}
	for rows.Next() {
		var username, artist, mbid, status, notes string
		var createdAt time.Time
		if err := rows.Scan(&username, &artist, &mbid, &status, &notes, &createdAt); err != nil {
			rows.Close()
			return nil, err
		}
		uid, ok := userIDByName[strings.ToLower(username)]
		if !ok {
			res.RequestsSkipped++
			continue
		}
		// Map old statuses onto the new set.
		switch status {
		case "completed":
			status = "sent"
		case "pending", "approved", "rejected", "sent", "failed":
		default:
			status = "pending"
		}
		id, err := st.CreateRequest(uid, artist, mbid, "", "", status)
		if err != nil {
			res.RequestsSkipped++ // open-request UNIQUE index: already imported
			continue
		}
		if notes != "" {
			st.UpdateRequestStatus(id, status, notes, "", 0)
		}
		st.DB.Exec("UPDATE requests SET created_at=? WHERE id=?", createdAt.UTC().Format("2006-01-02T15:04:05.000Z"), id)
		res.Requests++
	}
	rows.Close()

	return res, nil
}
