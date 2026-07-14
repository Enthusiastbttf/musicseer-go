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

// Previews returns up to five 30-second sample tracks for an artist.
func (e *Engine) Previews(ctx context.Context, artist string) ([]clients.DeezerTrack, error) {
	key := artist
	previewMu.Lock()
	if entry, ok := previewCache[key]; ok && time.Since(entry.at) < 12*time.Hour {
		previewMu.Unlock()
		return entry.tracks, nil
	}
	previewMu.Unlock()

	tracks, err := e.Deezer.TopPreviews(ctx, artist, 5)
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
