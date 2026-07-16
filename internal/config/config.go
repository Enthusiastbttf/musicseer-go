// Package config loads runtime configuration from environment variables
// (optionally seeded from a .env-style file) with sane defaults.
package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port           int           // HTTP listen port
	DataDir        string        // where musicseer.db and secrets live
	LastFMKey      string        // Last.fm API key (required for discovery)
	LogLevel       string        // debug|info|warn|error
	SessionTTL     time.Duration // session lifetime
	TrendingEvery  time.Duration // trending refresh interval
	LibraryEvery   time.Duration // library sync interval
	RecsTTL        time.Duration // recommendation staleness threshold
	Contact        string        // contact email for MusicBrainz user-agent
	TrustedProxies []string      // CIDRs/IPs whose X-Forwarded-For header is trusted
}

func Load() Config {
	// Optional env file (systemd EnvironmentFile also works; this helps Docker/dev).
	for _, p := range []string{os.Getenv("MUSICSEER_ENV_FILE"), ".env"} {
		if p != "" {
			loadEnvFile(p)
		}
	}

	c := Config{
		Port:           envInt("MUSICSEER_PORT", 8688),
		DataDir:        envStr("MUSICSEER_DATA_DIR", "./data"),
		LastFMKey:      envStr("LASTFM_API_KEY", ""),
		LogLevel:       envStr("MUSICSEER_LOG_LEVEL", "info"),
		SessionTTL:     envDur("MUSICSEER_SESSION_TTL", 30*24*time.Hour),
		TrendingEvery:  envDur("MUSICSEER_TRENDING_INTERVAL", 6*time.Hour),
		LibraryEvery:   envDur("MUSICSEER_LIBRARY_INTERVAL", 12*time.Hour),
		RecsTTL:        envDur("MUSICSEER_RECS_TTL", 12*time.Hour),
		Contact:        envStr("MUSICBRAINZ_CONTACT", "admin@example.com"),
		TrustedProxies: envList("MUSICSEER_TRUSTED_PROXIES"),
	}
	abs, err := filepath.Abs(c.DataDir)
	if err == nil {
		c.DataDir = abs
	}
	return c
}

func loadEnvFile(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.Trim(strings.TrimSpace(v), `"'`)
		if os.Getenv(k) == "" {
			os.Setenv(k, v)
		}
	}
}

func envStr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// envList parses a comma-separated env var into a trimmed, non-empty slice.
func envList(k string) []string {
	v := os.Getenv(k)
	if v == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(v, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func envInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envDur(k string, def time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
