package engine

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"musicseer/internal/clients"
)

// Preview samples are cached in memory for 12h — Deezer's sample URLs expire,
// so persisting them would serve dead links after a restart anyway.
type previewEntry struct {
	tracks []clients.DeezerTrack
	at     time.Time
}

var (
	previewMu    sync.Mutex
	previewCache = map[string]previewEntry{}
)

// Previews returns 30-second sample tracks: an artist's top tracks, or —
// when album is set — the tracks of that specific album (matched by
// artist+title, which disambiguates identically-named artists).
func (e *Engine) Previews(ctx context.Context, artist, album string) ([]clients.DeezerTrack, error) {
	ctx, cancel := context.WithTimeout(ctx, interactiveDeadline)
	defer cancel()
	key := artist + "\x00" + album
	previewMu.Lock()
	if entry, ok := previewCache[key]; ok && time.Since(entry.at) < 12*time.Hour {
		previewMu.Unlock()
		return entry.tracks, nil
	}
	previewMu.Unlock()

	var tracks []clients.DeezerTrack
	var err error
	if album != "" {
		tracks, err = e.Deezer.AlbumPreviews(ctx, artist, album, 50)
	} else {
		tracks, err = e.Deezer.TopPreviews(ctx, artist, 5)
	}
	if err != nil {
		return nil, err
	}
	previewMu.Lock()
	if len(previewCache) > 2000 {
		previewCache = map[string]previewEntry{}
	}
	previewCache[key] = previewEntry{tracks: tracks, at: time.Now()}
	previewMu.Unlock()
	return tracks, nil
}

// AlbumTrack is one row in an album's track list.
type AlbumTrack struct {
	Position int    `json:"position"`
	Title    string `json:"title"`
	Duration int    `json:"duration,omitempty"` // seconds
	Preview  string `json:"preview,omitempty"`  // 30s sample URL, when Deezer has it
}

// AlbumTrackList returns an album's tracks: Deezer first (titles + durations
// + playable samples, matched by artist+title), falling back to MusicBrainz
// (authoritative track list, no samples). MB results cache 30 days; Deezer
// results ride the in-memory preview cache since its sample URLs expire.
func (e *Engine) AlbumTrackList(ctx context.Context, artist, album, rgMBID string) ([]AlbumTrack, string, error) {
	ctx, cancel := context.WithTimeout(ctx, interactiveDeadline)
	defer cancel()
	// Deezer path (also serves the play-button previews).
	if tracks, err := e.Previews(ctx, artist, album); err == nil && len(tracks) > 0 {
		out := make([]AlbumTrack, 0, len(tracks))
		for i, t := range tracks {
			out = append(out, AlbumTrack{Position: i + 1, Title: t.Title, Duration: t.Duration, Preview: t.Preview})
		}
		return out, "deezer", nil
	}

	// MusicBrainz fallback, DB-cached.
	if rgMBID == "" {
		return nil, "", nil
	}
	if raw, ok := e.st.AlbumTracksCached(rgMBID, 30*24*time.Hour); ok {
		var out []AlbumTrack
		if json.Unmarshal(raw, &out) == nil {
			return out, "musicbrainz", nil
		}
	}
	mbTracks, err := e.MB.ReleaseGroupTracks(ctx, rgMBID)
	if err != nil {
		return nil, "", err
	}
	out := make([]AlbumTrack, 0, len(mbTracks))
	for _, t := range mbTracks {
		out = append(out, AlbumTrack{Position: t.Position, Title: t.Title, Duration: t.LengthMs / 1000})
	}
	e.st.SaveAlbumTracks(rgMBID, out)
	return out, "musicbrainz", nil
}
