// Package engine contains all background work: trending sync, library sync,
// recommendation computation and image resolution.
//
// Design rule that fixes the old app's performance: the interactive API
// NEVER calls an external service. Handlers only read SQLite. Everything
// slow happens here, on schedules or triggered refreshes, with bounded
// concurrency and per-provider rate limits.
package engine

import (
	"context"
	"encoding/json"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"musicseer/internal/clients"
	"musicseer/internal/config"
	"musicseer/internal/secrets"
	"musicseer/internal/store"
)

type Engine struct {
	cfg     config.Config
	st      *store.Store
	box     *secrets.Box
	log     *slog.Logger
	LastFM  *clients.LastFM
	Deezer  *clients.Deezer
	AudioDB *clients.AudioDB
	MB      *clients.MusicBrainz
	Sub     clients.Subsonic
	Lidarr  clients.Lidarr

	imageQueue chan imageJob
	inflight   sync.Map // dedup for async refresh triggers

	mu     sync.Mutex
	status map[string]string // job name -> last outcome (for admin UI)
}

type imageJob struct{ name, mbid string }

func New(cfg config.Config, st *store.Store, box *secrets.Box, log *slog.Logger) *Engine {
	return &Engine{
		cfg: cfg, st: st, box: box, log: log,
		LastFM:     clients.NewLastFM(cfg.LastFMKey),
		Deezer:     clients.NewDeezer(),
		AudioDB:    clients.NewAudioDB(),
		MB:         clients.NewMusicBrainz(cfg.Contact),
		imageQueue: make(chan imageJob, 4096),
		status:     map[string]string{},
	}
}

func (e *Engine) setStatus(job, outcome string) {
	e.mu.Lock()
	e.status[job] = time.Now().UTC().Format(time.RFC3339) + " — " + outcome
	e.mu.Unlock()
}

func (e *Engine) Status() map[string]string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make(map[string]string, len(e.status))
	for k, v := range e.status {
		out[k] = v
	}
	return out
}

// Start launches the scheduler and the image worker.
func (e *Engine) Start(ctx context.Context) {
	go e.imageWorker(ctx)
	go e.schedule(ctx, "trending", e.cfg.TrendingEvery, e.SyncTrending)
	go e.schedule(ctx, "library", e.cfg.LibraryEvery, e.SyncLibraries)
	go e.schedule(ctx, "sessions", time.Hour, func(context.Context) error { e.st.PruneSessions(); return nil })
}

func (e *Engine) schedule(ctx context.Context, name string, every time.Duration, fn func(context.Context) error) {
	// Run shortly after boot, then on the interval.
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		if err := fn(ctx); err != nil {
			e.log.Warn("job failed", "job", name, "err", err)
			e.setStatus(name, "error: "+err.Error())
		} else {
			e.setStatus(name, "ok")
		}
		timer.Reset(every)
	}
}

// ---------- trending ----------

// SyncTrending refreshes the Last.fm global chart and enqueues image lookups.
// One API call for the chart; metadata comes back in the same payload, so a
// full refresh is seconds, not the old app's sequential 100-artist crawl.
func (e *Engine) SyncTrending(ctx context.Context) error {
	if e.cfg.LastFMKey == "" {
		return nil // discovery disabled until a key is configured
	}
	top, err := e.LastFM.TopArtists(ctx, 100)
	if err != nil {
		return err
	}
	if len(top) == 0 {
		return nil
	}
	trending := make([]store.TrendingArtist, 0, len(top))
	for i, a := range top {
		trending = append(trending, store.TrendingArtist{Rank: i + 1, Name: a.Name, MBID: a.MBID})
		listeners, _ := strconv.ParseInt(a.Listeners, 10, 64)
		playcount, _ := strconv.ParseInt(a.Playcount, 10, 64)
		e.st.UpsertArtist(&store.Artist{Name: a.Name, MBID: a.MBID, Listeners: listeners, Playcount: playcount})
		e.enqueueImage(a.Name, a.MBID)
	}
	if err := e.st.ReplaceTrending("global", trending); err != nil {
		return err
	}
	e.log.Info("trending synced", "artists", len(trending))
	// Genres enrichment for the top of the chart (rate-limited MusicBrainz).
	go e.enrichGenres(ctx, top[:min(25, len(top))])
	return nil
}

func (e *Engine) enrichGenres(ctx context.Context, artists []clients.LFArtist) {
	for _, a := range artists {
		if a.MBID == "" {
			continue
		}
		known, _ := e.st.ArtistsByNames([]string{a.Name})
		if k := known[strings.ToLower(a.Name)]; k != nil && len(k.Genres) > 0 {
			continue
		}
		tags, err := e.MB.ArtistTags(ctx, a.MBID)
		if err != nil || len(tags) == 0 {
			continue
		}
		e.st.UpsertArtist(&store.Artist{Name: a.Name, MBID: a.MBID, Genres: tags})
	}
}

// ---------- library sync ----------

func (e *Engine) SyncLibraries(ctx context.Context) error {
	instances, err := e.st.Instances("")
	if err != nil {
		return err
	}
	for _, inst := range instances {
		if !inst.IsActive {
			continue
		}
		secret, err := e.box.Decrypt(inst.APIKeyEnc)
		if err != nil {
			e.log.Warn("cannot decrypt instance credentials", "instance", inst.Name, "err", err)
			continue
		}

		var lib []store.LibraryArtist
		switch inst.Type {
		case "navidrome":
			artists, err := e.Sub.GetArtists(ctx, inst.BaseURL, inst.Username, secret)
			if err != nil {
				e.log.Warn("library sync failed", "instance", inst.Name, "err", err)
				continue
			}
			for _, a := range artists {
				weight := a.AlbumCount // baseline: bigger presence = stronger signal
				if a.UserRating > 0 {
					weight = a.UserRating * 10
				}
				if a.Starred != "" {
					weight = 100
				}
				lib = append(lib, store.LibraryArtist{Name: a.Name, MBID: a.MBID, Weight: weight})
			}
		case "lidarr":
			// No Navidrome? No problem — Lidarr's artist list IS the library.
			artists, err := e.Lidarr.Artists(ctx, inst.BaseURL, secret)
			if err != nil {
				e.log.Warn("library sync failed", "instance", inst.Name, "err", err)
				continue
			}
			for _, a := range artists {
				if a.Statistics.TrackFileCount == 0 && !a.Monitored {
					continue // stub entries with no files and no interest
				}
				// Presence-strength: more albums on disk = stronger seed,
				// monitored artists get a boost (explicit user interest).
				weight := a.Statistics.AlbumCount * 5
				if a.Monitored {
					weight += 20
				}
				if weight > 100 {
					weight = 100
				}
				lib = append(lib, store.LibraryArtist{Name: a.ArtistName, MBID: a.ForeignArtistID, Weight: weight})
			}
		default:
			continue
		}

		if err := e.st.ReplaceLibrary(inst.ID, lib); err != nil {
			return err
		}
		e.log.Info("library synced", "instance", inst.Name, "type", inst.Type, "artists", len(lib))
	}
	// Refresh recommendations for every user now that the library moved.
	users, err := e.st.Users()
	if err != nil {
		return err
	}
	for _, u := range users {
		if err := e.ComputeRecommendations(ctx, u.ID); err != nil {
			e.log.Warn("recommendation compute failed", "user", u.Username, "err", err)
		}
	}
	return nil
}

// ---------- recommendations ----------

type Recommendation struct {
	Name       string   `json:"name"`
	MBID       string   `json:"mbid,omitempty"`
	ImageURL   string   `json:"imageUrl,omitempty"`
	Genres     []string `json:"genres,omitempty"`
	Score      float64  `json:"score"`
	Similarity float64  `json:"similarity"`
	Listeners  int64    `json:"listeners"`
	Reason     string   `json:"reason"`
}

// Scoring weights — identical to the original app's model.
const (
	wPopularity = 0.4
	wSimilarity = 0.3
	wDiversity  = 0.2
	wFreshness  = 0.1
)

// ComputeRecommendations builds both the "similar to you" and "hidden gems"
// lists for one user and persists them. External calls: at most one Last.fm
// similar-artists fetch per seed (30-day cached), executed with bounded
// concurrency. Everything else is batch SQLite.
func (e *Engine) ComputeRecommendations(ctx context.Context, userID int64) error {
	if e.cfg.LastFMKey == "" {
		return nil
	}
	seeds, err := e.st.LibraryTop(20)
	if err != nil {
		return err
	}
	if len(seeds) == 0 {
		// Nothing in the library yet: store empty lists so the UI can fall
		// back to trending without a "computing…" state.
		e.st.SaveRecommendations(userID, "similar", []Recommendation{})
		e.st.SaveRecommendations(userID, "gems", []Recommendation{})
		return nil
	}

	libNames, err := e.st.LibraryNames()
	if err != nil {
		return err
	}

	// Fetch similar lists for all seeds (cache-first, 4 concurrent fetches).
	type seedResult struct {
		seed    store.LibraryArtist
		similar []store.SimilarArtist
	}
	results := make([]seedResult, len(seeds))
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for idx, seed := range seeds {
		wg.Add(1)
		go func(idx int, seed store.LibraryArtist) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[idx] = seedResult{seed: seed, similar: e.similarFor(ctx, seed.Name, seed.MBID)}
		}(idx, seed)
	}
	wg.Wait()

	// Aggregate candidates.
	type cand struct {
		name, mbid string
		matchSum   float64
		hits       int
		seeds      []string
	}
	candidates := map[string]*cand{}
	for _, r := range results {
		for _, sim := range r.similar {
			key := strings.ToLower(sim.Name)
			if libNames[key] {
				continue // already in the library
			}
			c := candidates[key]
			if c == nil {
				c = &cand{name: sim.Name, mbid: sim.MBID}
				candidates[key] = c
			}
			c.matchSum += sim.Match
			c.hits++
			if len(c.seeds) < 3 {
				c.seeds = append(c.seeds, r.seed.Name)
			}
		}
	}
	if len(candidates) == 0 {
		e.st.SaveRecommendations(userID, "similar", []Recommendation{})
		e.st.SaveRecommendations(userID, "gems", []Recommendation{})
		return nil
	}

	// One batched metadata lookup for every candidate (vs. the old app's
	// per-candidate SELECT + inline Deezer/AudioDB calls).
	names := make([]string, 0, len(candidates))
	for _, c := range candidates {
		names = append(names, c.name)
	}
	meta, err := e.st.ArtistsByNames(names)
	if err != nil {
		return err
	}

	// User genre profile for the diversity score.
	seedNames := make([]string, len(seeds))
	for i, s := range seeds {
		seedNames[i] = s.Name
	}
	seedMeta, _ := e.st.ArtistsByNames(seedNames)
	userGenres := map[string]bool{}
	for _, m := range seedMeta {
		for _, g := range m.Genres {
			userGenres[strings.ToLower(g)] = true
		}
	}

	recs := make([]Recommendation, 0, len(candidates))
	for key, c := range candidates {
		m := meta[key]
		var listeners int64
		var genres []string
		var image string
		if m != nil {
			listeners, genres, image = m.Listeners, m.Genres, m.ImageURL
		} else {
			// Unknown artist: remember it and queue an image lookup for next time.
			e.st.UpsertArtist(&store.Artist{Name: c.name, MBID: c.mbid})
		}
		if image == "" {
			e.enqueueImage(c.name, c.mbid)
		}

		avgMatch := c.matchSum / float64(c.hits)
		popularity := minF(float64(listeners)/5_000_000, 1)
		diversity := 0.5
		if len(genres) > 0 {
			fresh := 0
			for _, g := range genres {
				if !userGenres[strings.ToLower(g)] {
					fresh++
				}
			}
			diversity = minF(float64(fresh)/3, 1)
		}
		score := popularity*wPopularity + avgMatch*wSimilarity + diversity*wDiversity + 0.5*wFreshness

		recs = append(recs, Recommendation{
			Name: c.name, MBID: c.mbid, ImageURL: image, Genres: genres,
			Score: score, Similarity: avgMatch, Listeners: listeners,
			Reason: "Similar to " + strings.Join(c.seeds, ", "),
		})
	}

	// Similar-to-you: by blended score.
	sort.Slice(recs, func(i, j int) bool { return recs[i].Score > recs[j].Score })
	top := recs
	if len(top) > 60 {
		top = top[:60]
	}
	if err := e.st.SaveRecommendations(userID, "similar", top); err != nil {
		return err
	}

	// Hidden gems: strong similarity, small audience.
	gems := make([]Recommendation, 0, 60)
	for _, r := range recs {
		if r.Similarity >= 0.3 && r.Listeners > 0 && r.Listeners < 500_000 {
			gems = append(gems, r)
		}
	}
	sort.Slice(gems, func(i, j int) bool { return gems[i].Similarity > gems[j].Similarity })
	if len(gems) > 60 {
		gems = gems[:60]
	}
	if err := e.st.SaveRecommendations(userID, "gems", gems); err != nil {
		return err
	}

	e.log.Info("recommendations computed", "user", userID, "candidates", len(candidates))
	return nil
}

// similarFor returns the similar-artist list for a seed, cache-first.
// On a cache miss it fetches from Last.fm, stores artist stats it got for
// free in the payload, and caches the list for 30 days.
func (e *Engine) similarFor(ctx context.Context, name, mbid string) []store.SimilarArtist {
	if cached, ok := e.st.SimilarCached(name, 30*24*time.Hour); ok {
		return cached
	}
	similar, err := e.LastFM.SimilarArtists(ctx, name, mbid, 50)
	if err != nil {
		e.log.Warn("similar fetch failed", "artist", name, "err", err)
		if cached, ok := e.st.SimilarCached(name, 365*24*time.Hour); ok {
			return cached // stale beats empty
		}
		return nil
	}
	out := make([]store.SimilarArtist, 0, len(similar))
	for _, s := range similar {
		match, _ := strconv.ParseFloat(s.Match, 64)
		out = append(out, store.SimilarArtist{Name: s.Name, MBID: s.MBID, Match: match})
	}
	e.st.SaveSimilar(name, out)
	return out
}

// RefreshUserAsync recomputes a user's recommendations in the background,
// deduplicating concurrent triggers (stale-while-revalidate).
func (e *Engine) RefreshUserAsync(userID int64) {
	key := "recs-" + strconv.FormatInt(userID, 10)
	if _, busy := e.inflight.LoadOrStore(key, true); busy {
		return
	}
	go func() {
		defer e.inflight.Delete(key)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := e.ComputeRecommendations(ctx, userID); err != nil {
			e.log.Warn("async recommendation refresh failed", "user", userID, "err", err)
		}
	}()
}

// ---------- images ----------

func (e *Engine) enqueueImage(name, mbid string) {
	select {
	case e.imageQueue <- imageJob{name: name, mbid: mbid}:
	default: // queue full — the periodic backfill will catch it later
	}
}

// imageWorker resolves artist images (Deezer, then AudioDB) at a polite pace.
// This is the ONLY place image lookups happen — never during a page load.
func (e *Engine) imageWorker(ctx context.Context) {
	backfill := time.NewTicker(10 * time.Minute)
	defer backfill.Stop()
	seen := map[string]time.Time{}
	for {
		var job imageJob
		select {
		case <-ctx.Done():
			return
		case job = <-e.imageQueue:
		case <-backfill.C:
			missing, _ := e.st.ArtistsMissingImages(50)
			for _, a := range missing {
				e.enqueueImage(a.Name, a.MBID)
			}
			continue
		}
		key := strings.ToLower(job.name)
		if t, ok := seen[key]; ok && time.Since(t) < 24*time.Hour {
			continue
		}
		seen[key] = time.Now()
		if len(seen) > 20000 {
			seen = map[string]time.Time{}
		}

		// Skip if some other path already resolved it.
		existing, _ := e.st.ArtistsByNames([]string{job.name})
		if a := existing[key]; a != nil && a.ImageURL != "" {
			continue
		}

		url, err := e.Deezer.ArtistImage(ctx, job.name)
		if err != nil || url == "" {
			url, _ = e.AudioDB.ArtistImage(ctx, job.name, job.mbid)
		}
		if url != "" {
			e.st.SetArtistImage(job.name, url)
		} else {
			e.st.MarkImageChecked(job.name)
		}
	}
}

// ---------- helpers ----------

func minF(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// MarshalStatus is used by the admin stats endpoint.
func (e *Engine) MarshalStatus() json.RawMessage {
	b, _ := json.Marshal(e.Status())
	return b
}
