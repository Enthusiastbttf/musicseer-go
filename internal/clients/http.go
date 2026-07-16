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
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

var httpClient = &http.Client{Timeout: 10 * time.Second}

// sensitiveParams are query keys that carry credentials and must never appear
// in error strings or logs (Last.fm key, Subsonic salt/token, generic tokens).
var sensitiveParams = []string{"api_key", "apikey", "token", "t", "s", "sk"}

// sanitizeURL redacts credential-bearing query parameters and any userinfo from
// a URL so it is safe to embed in an error or log line.
func sanitizeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "[unparseable-url]"
	}
	if q := u.Query(); len(q) > 0 {
		changed := false
		for _, k := range sensitiveParams {
			if q.Has(k) {
				q.Set(k, "REDACTED")
				changed = true
			}
		}
		if changed {
			u.RawQuery = q.Encode()
		}
	}
	return u.Redacted() // also masks any user:password in the URL
}

// sanitizeErr scrubs the URL embedded in *url.Error (what net/http returns on
// transport failures) so a network hiccup can't leak a key into the logs.
func sanitizeErr(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		ue.URL = sanitizeURL(ue.URL)
		return ue
	}
	return err
}

// readCapped reads up to limit bytes and reports an explicit error if the body
// is larger, instead of silently truncating into invalid JSON.
func readCapped(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("response body exceeds %d bytes (raise the limit or paginate)", limit)
	}
	return data, nil
}

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
// Transient failures (dropped connections, 429/5xx) are retried twice with
// backoff — free public APIs like MusicBrainz shed load by hanging up.
func getJSON(ctx context.Context, lim *limiter, url string, headers map[string]string, out any) error {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(time.Duration(attempt) * 2 * time.Second):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if lim != nil {
			if err := lim.wait(ctx); err != nil {
				return err
			}
		}
		body, status, err := doGET(ctx, url, headers)
		if err != nil {
			lastErr = err // network-level: dropped conn, EOF, timeout — retry
			continue
		}
		if status == 429 || status >= 500 {
			lastErr = fmt.Errorf("%s: HTTP %d: %.200s", sanitizeURL(url), status, string(body))
			continue
		}
		if status >= 400 {
			return fmt.Errorf("%s: HTTP %d: %.200s", sanitizeURL(url), status, string(body))
		}
		return json.Unmarshal(body, out)
	}
	return lastErr
}

func doGET(ctx context.Context, url string, headers map[string]string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, 0, sanitizeErr(err)
	}
	defer resp.Body.Close()
	body, err := readCapped(resp.Body, 8<<20)
	if err != nil {
		return nil, 0, err
	}
	return body, resp.StatusCode, nil
}
