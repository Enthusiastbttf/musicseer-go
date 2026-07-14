package engine

import (
	"context"
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
		tracks, err = e.Deezer.AlbumPreviews(ctx, artist, album, 12)
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
