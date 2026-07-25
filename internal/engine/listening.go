package engine

import (
	"context"
	"strings"
	"sync"
	"time"
)

// Cache the "recent listening" summary so the Discover page can show what a
// user's recommendations are based on without calling Last.fm on every load.
type listeningEntry struct {
	artists []string
	at      time.Time
}

var (
	listeningMu    sync.Mutex
	listeningCache = map[string]listeningEntry{}
)

// UserListeningArtists returns a linked Last.fm user's recent top artists — the
// same 3-month window that seeds their recommendations — cached ~1h. Returns nil
// when no Last.fm key is active or no username is linked (i.e. when the listening
// history is NOT feeding recommendations), which the UI uses to prompt linking.
func (e *Engine) UserListeningArtists(ctx context.Context, lastfmUser string, limit int) []string {
	if !e.UsingLastFM() || strings.TrimSpace(lastfmUser) == "" {
		return nil
	}
	key := strings.ToLower(lastfmUser)

	listeningMu.Lock()
	if ent, ok := listeningCache[key]; ok && time.Since(ent.at) < time.Hour {
		listeningMu.Unlock()
		return ent.artists
	}
	listeningMu.Unlock()

	top, err := e.LastFM.UserTopArtists(ctx, lastfmUser, "3month", limit)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(top))
	for _, a := range top {
		if a.Name != "" {
			names = append(names, a.Name)
		}
	}
	listeningMu.Lock()
	listeningCache[key] = listeningEntry{artists: names, at: time.Now()}
	listeningMu.Unlock()
	return names
}
