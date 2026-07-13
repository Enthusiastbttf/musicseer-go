// Package clients contains thin, rate-limited HTTP clients for every
// external service MusicSeer talks to. Every client shares two rules:
//  1. a per-host rate limiter, so we are a polite API citizen and never
//     get throttled or banned (MusicBrainz bans clients that exceed 1 rps);
//  2. short timeouts, so a slow third party can never stall a page load —
//     nothing in the interactive request path waits on these anyway.
package clients

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

var httpClient = &http.Client{Timeout: 10 * time.Second}

// limiter is a minimal token-interval rate limiter (one request per interval).
type limiter struct {
	mu   sync.Mutex
	next time.Time
	gap  time.Duration
}

func newLimiter(perSecond float64) *limiter {
	return &limiter{gap: time.Duration(float64(time.Second) / perSecond)}
}

func (l *limiter) wait(ctx context.Context) error {
	l.mu.Lock()
	now := time.Now()
	if l.next.Before(now) {
		l.next = now
	}
	wait := l.next.Sub(now)
	l.next = l.next.Add(l.gap)
	l.mu.Unlock()
	if wait <= 0 {
		return nil
	}
	select {
	case <-time.After(wait):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// getJSON performs a rate-limited GET and decodes the JSON response.
func getJSON(ctx context.Context, lim *limiter, url string, headers map[string]string, out any) error {
	if lim != nil {
		if err := lim.wait(ctx); err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("%s: HTTP %d: %.200s", url, resp.StatusCode, string(body))
	}
	return json.Unmarshal(body, out)
}
