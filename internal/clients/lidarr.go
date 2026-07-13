package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Lidarr client for the v1 API.
type Lidarr struct{}

func (l Lidarr) do(ctx context.Context, method, baseURL, path, apiKey string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(baseURL, "/")+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("X-Api-Key", apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		if strings.Contains(string(data), "already been added") || strings.Contains(string(data), "already exists") {
			return ErrLidarrDuplicate
		}
		return fmt.Errorf("lidarr %s: HTTP %d: %.300s", path, resp.StatusCode, string(data))
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

var ErrLidarrDuplicate = fmt.Errorf("artist already exists in Lidarr")

func (l Lidarr) Ping(ctx context.Context, baseURL, apiKey string) (string, error) {
	var status struct {
		Version string `json:"version"`
	}
	err := l.do(ctx, http.MethodGet, baseURL, "/api/v1/system/status", apiKey, nil, &status)
	return status.Version, err
}

type LidarrProfile struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type LidarrRootFolder struct {
	Path string `json:"path"`
}

// Options fetches quality profiles, metadata profiles and root folders so the
// admin UI can offer dropdowns instead of hardcoding IDs like the old app did.
func (l Lidarr) Options(ctx context.Context, baseURL, apiKey string) (quality, metadata []LidarrProfile, roots []string, err error) {
	if err = l.do(ctx, http.MethodGet, baseURL, "/api/v1/qualityprofile", apiKey, nil, &quality); err != nil {
		return
	}
	if err = l.do(ctx, http.MethodGet, baseURL, "/api/v1/metadataprofile", apiKey, nil, &metadata); err != nil {
		return
	}
	var rf []LidarrRootFolder
	if err = l.do(ctx, http.MethodGet, baseURL, "/api/v1/rootfolder", apiKey, nil, &rf); err != nil {
		return
	}
	for _, r := range rf {
		roots = append(roots, r.Path)
	}
	return
}

// AddArtist adds an artist by MusicBrainz ID, monitored, and kicks off a search.
func (l Lidarr) AddArtist(ctx context.Context, baseURL, apiKey, mbid, name string,
	qualityProfileID, metadataProfileID int64, rootFolder string) (int64, error) {
	payload := map[string]any{
		"foreignArtistId":   mbid,
		"artistName":        name,
		"qualityProfileId":  qualityProfileID,
		"metadataProfileId": metadataProfileID,
		"rootFolderPath":    rootFolder,
		"monitored":         true,
		"addOptions":        map[string]any{"searchForMissingAlbums": true},
	}
	var created struct {
		ID int64 `json:"id"`
	}
	err := l.do(ctx, http.MethodPost, baseURL, "/api/v1/artist", apiKey, payload, &created)
	return created.ID, err
}

// LidarrArtist is one entry from Lidarr's library.
type LidarrArtist struct {
	ArtistName      string `json:"artistName"`
	ForeignArtistID string `json:"foreignArtistId"`
	Monitored       bool   `json:"monitored"`
	Statistics      struct {
		AlbumCount     int `json:"albumCount"`
		TrackFileCount int `json:"trackFileCount"`
	} `json:"statistics"`
}

// Artists returns every artist in the Lidarr library — used as a library
// source for recommendations when no Navidrome instance exists.
func (l Lidarr) Artists(ctx context.Context, baseURL, apiKey string) ([]LidarrArtist, error) {
	var artists []LidarrArtist
	err := l.do(ctx, http.MethodGet, baseURL, "/api/v1/artist", apiKey, nil, &artists)
	return artists, err
}

// LookupMBID resolves an artist name to a MusicBrainz ID via Lidarr's own
// search proxy (avoids hitting MusicBrainz directly in the request path).
func (l Lidarr) LookupMBID(ctx context.Context, baseURL, apiKey, name string) (string, error) {
	var results []struct {
		ForeignArtistID string `json:"foreignArtistId"`
		ArtistName      string `json:"artistName"`
	}
	err := l.do(ctx, http.MethodGet, baseURL, "/api/v1/artist/lookup?term="+url.QueryEscape(name), apiKey, nil, &results)
	if err != nil || len(results) == 0 {
		return "", err
	}
	return results[0].ForeignArtistID, nil
}
