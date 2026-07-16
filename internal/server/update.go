package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"musicseer/internal/store"
)

// In-app self-update: check GitHub for the latest release and, on admin
// request, download the published binary, verify its SHA-256 against the
// release's checksums.txt, atomically swap the running executable, and re-exec
// into the new version. This is admin-only and, by design, runs downloaded
// code — so the download is always integrity-checked before it is swapped in.
//
// The service can only replace its own binary if the executable's directory is
// writable by the service user (see deploy notes / the systemd unit). If it is
// not, apply returns a clear error and the operator falls back to the CLI.

const (
	updateRepo  = "Enthusiastbttf/musicseer-go"
	updateAsset = "musicseer-linux-amd64"
)

var updateClient = &http.Client{Timeout: 3 * time.Minute}

type ghRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

func latestRelease(ctx context.Context) (*ghRelease, error) {
	url := "https://api.github.com/repos/" + updateRepo + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "musicseer-updater")
	resp, err := updateClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := readCappedLocal(resp.Body, 1<<20)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub returned HTTP %d", resp.StatusCode)
	}
	var rel ghRelease
	if err := json.Unmarshal(body, &rel); err != nil {
		return nil, err
	}
	if rel.TagName == "" {
		return nil, fmt.Errorf("no release found")
	}
	return &rel, nil
}

// handleUpdateCheck reports the running version, the latest published version,
// and whether an update is available.
func (s *Server) handleUpdateCheck(w http.ResponseWriter, r *http.Request, _ *store.User) {
	rel, err := latestRelease(r.Context())
	if err != nil {
		jsonError(w, http.StatusBadGateway, "could not reach GitHub: "+err.Error())
		return
	}
	latest := strings.TrimPrefix(rel.TagName, "v")
	jsonWrite(w, http.StatusOK, map[string]any{
		"current":         Version,
		"latest":          latest,
		"updateAvailable": versionLess(Version, latest),
		"releaseUrl":      rel.HTMLURL,
	})
}

// handleUpdateApply downloads the latest release binary, verifies it, swaps the
// running executable, and re-execs into the new version.
func (s *Server) handleUpdateApply(w http.ResponseWriter, r *http.Request, _ *store.User) {
	exePath, err := os.Executable()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "cannot locate running binary: "+err.Error())
		return
	}
	if resolved, err := filepath.EvalSymlinks(exePath); err == nil {
		exePath = resolved
	}
	dir := filepath.Dir(exePath)

	// Fail early (and clearly) if we can't replace our own binary — the common
	// case on a hardened install where /opt is read-only for the service.
	probe, err := os.CreateTemp(dir, ".musicseer-update-*")
	if err != nil {
		jsonError(w, http.StatusConflict,
			"the app cannot write its own binary at "+dir+" — one-time setup needed: make "+
				exePath+" writable by the service user (chown + add it to the systemd unit's ReadWritePaths), "+
				"or upgrade from the CLI. Details: "+err.Error())
		return
	}
	probe.Close()
	os.Remove(probe.Name())

	rel, err := latestRelease(r.Context())
	if err != nil {
		jsonError(w, http.StatusBadGateway, "could not reach GitHub: "+err.Error())
		return
	}
	latest := strings.TrimPrefix(rel.TagName, "v")
	if !versionLess(Version, latest) {
		jsonWrite(w, http.StatusOK, map[string]any{"status": "up-to-date", "version": Version})
		return
	}

	var binURL, sumURL string
	for _, a := range rel.Assets {
		switch a.Name {
		case updateAsset:
			binURL = a.URL
		case "checksums.txt":
			sumURL = a.URL
		}
	}
	if binURL == "" || sumURL == "" {
		jsonError(w, http.StatusBadGateway, "release "+latest+" is missing the binary or checksums.txt asset")
		return
	}

	wantSum, err := fetchChecksum(r.Context(), sumURL, updateAsset)
	if err != nil {
		jsonError(w, http.StatusBadGateway, "could not read published checksum: "+err.Error())
		return
	}

	tmpPath, gotSum, err := downloadTo(r.Context(), dir, binURL)
	if err != nil {
		jsonError(w, http.StatusBadGateway, "download failed: "+err.Error())
		return
	}
	if gotSum != wantSum {
		os.Remove(tmpPath)
		jsonError(w, http.StatusBadGateway,
			"checksum mismatch — refusing to install (expected "+wantSum[:12]+"…, got "+gotSum[:12]+"…)")
		return
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		os.Remove(tmpPath)
		jsonError(w, http.StatusInternalServerError, "chmod failed: "+err.Error())
		return
	}
	if err := os.Rename(tmpPath, exePath); err != nil {
		os.Remove(tmpPath)
		jsonError(w, http.StatusInternalServerError, "could not replace binary: "+err.Error())
		return
	}

	s.log.Info("self-update applied; restarting", "from", Version, "to", latest)
	jsonWrite(w, http.StatusOK, map[string]any{"status": "updating", "version": latest})

	// Restart into the new binary after the response has flushed. Re-exec keeps
	// this self-contained (no dependency on the systemd Restart policy).
	go func() {
		time.Sleep(800 * time.Millisecond)
		if s.st != nil && s.st.DB != nil {
			s.st.DB.Close() // flush the SQLite WAL before we replace the process image
		}
		if err := syscall.Exec(exePath, os.Args, os.Environ()); err != nil {
			s.log.Error("re-exec after update failed; exiting so the service manager restarts", "err", err)
			os.Exit(1)
		}
	}()
}

// fetchChecksum downloads checksums.txt and returns the hex SHA-256 for asset.
func fetchChecksum(ctx context.Context, url, asset string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "musicseer-updater")
	resp, err := updateClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := readCappedLocal(resp.Body, 1<<20)
	if err != nil {
		return "", err
	}
	// Lines are "<sha256>  <filename>".
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && filepath.Base(fields[1]) == asset {
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("no checksum entry for %s", asset)
}

// downloadTo streams url into a temp file in dir, returning the path and the
// hex SHA-256 of the downloaded bytes.
func downloadTo(ctx context.Context, dir, url string) (string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", "musicseer-updater")
	resp, err := updateClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	f, err := os.CreateTemp(dir, ".musicseer-update-*")
	if err != nil {
		return "", "", err
	}
	h := sha256.New()
	// 200 MB ceiling — the binary is ~10 MB; this just bounds a runaway body.
	if _, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, 200<<20)); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", "", err
	}
	return f.Name(), hex.EncodeToString(h.Sum(nil)), nil
}

func readCappedLocal(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("response exceeds %d bytes", limit)
	}
	return data, nil
}

// versionLess reports whether current is an older semantic version than latest.
// A non-numeric current (e.g. the "dev" build) is always considered older.
func versionLess(current, latest string) bool {
	cur := parseSemver(current)
	lat := parseSemver(latest)
	if cur == nil {
		return lat != nil // dev/unknown -> any real release is newer
	}
	if lat == nil {
		return false
	}
	for i := 0; i < 3; i++ {
		if cur[i] != lat[i] {
			return cur[i] < lat[i]
		}
	}
	return false
}

// parseSemver parses "X.Y.Z" (ignoring a leading v and any -suffix/+build) into
// a 3-element slice, or nil if it isn't a plain numeric version.
func parseSemver(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return nil
	}
	out := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil
		}
		out[i] = n
	}
	return out
}
