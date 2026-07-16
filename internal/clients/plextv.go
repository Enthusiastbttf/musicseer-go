package clients

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// PlexTV implements the plex.tv PIN-link authentication flow (the same one
// Overseerr uses): create a PIN, the user approves it at plex.tv/link (or the
// app.plex.tv auth page), we poll until a token appears, then verify the
// account has access to the admin's Plex server.
type PlexTV struct{}

func plexBase() string {
	if b := os.Getenv("MUSICSEER_PLEXTV_BASE"); b != "" { // test hook
		return b
	}
	return "https://plex.tv"
}

func (p PlexTV) headers(clientID, token string) map[string]string {
	h := map[string]string{
		"Accept":                   "application/json",
		"X-Plex-Product":           "MusicSeer Enhanced",
		"X-Plex-Version":           "2.0",
		"X-Plex-Client-Identifier": clientID,
		"X-Plex-Device":            "MusicSeer Enhanced",
		"X-Plex-Platform":          "Web",
	}
	if token != "" {
		h["X-Plex-Token"] = token
	}
	return h
}

func (p PlexTV) do(ctx context.Context, method, path, clientID, token string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, plexBase()+path, nil)
	if err != nil {
		return err
	}
	for k, v := range p.headers(clientID, token) {
		req.Header.Set(k, v)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := readCapped(resp.Body, 4<<20)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("plex.tv %s: HTTP %d: %.200s", path, resp.StatusCode, string(body))
	}
	if out != nil {
		return json.Unmarshal(body, out)
	}
	return nil
}

type PlexPin struct {
	ID   int64  `json:"id"`
	Code string `json:"code"`
}

// CreatePin starts a login attempt. The user approves it via the returned
// auth URL; poll CheckPin until a token shows up.
func (p PlexTV) CreatePin(ctx context.Context, clientID string) (*PlexPin, string, error) {
	var pin PlexPin
	if err := p.do(ctx, http.MethodPost, "/api/v2/pins?strong=true", clientID, "", &pin); err != nil {
		return nil, "", err
	}
	authURL := "https://app.plex.tv/auth#?" + url.Values{
		"clientID":                    {clientID},
		"code":                        {pin.Code},
		"context[device][product]":    {"MusicSeer Enhanced"},
		"context[device][deviceName]": {"MusicSeer Enhanced"},
	}.Encode()
	return &pin, authURL, nil
}

// CheckPin returns the auth token once the user has approved the PIN
// (empty string while still pending).
func (p PlexTV) CheckPin(ctx context.Context, clientID string, pinID int64) (string, error) {
	var pin struct {
		AuthToken string `json:"authToken"`
	}
	err := p.do(ctx, http.MethodGet, "/api/v2/pins/"+strconv.FormatInt(pinID, 10), clientID, "", &pin)
	return pin.AuthToken, err
}

type PlexUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
}

func (p PlexTV) User(ctx context.Context, clientID, token string) (*PlexUser, error) {
	var u PlexUser
	if err := p.do(ctx, http.MethodGet, "/api/v2/user", clientID, token, &u); err != nil {
		return nil, err
	}
	if u.Username == "" {
		return nil, fmt.Errorf("plex.tv returned no username")
	}
	return &u, nil
}

type PlexServer struct {
	Name              string `json:"name"`
	MachineIdentifier string `json:"machineIdentifier"`
	Owned             bool   `json:"owned"`
}

// Servers lists the Plex Media Servers this account can reach (owned + shared).
func (p PlexTV) Servers(ctx context.Context, clientID, token string) ([]PlexServer, error) {
	var resources []struct {
		Name             string `json:"name"`
		Provides         string `json:"provides"`
		ClientIdentifier string `json:"clientIdentifier"`
		Owned            bool   `json:"owned"`
	}
	if err := p.do(ctx, http.MethodGet, "/api/v2/resources?includeHttps=1", clientID, token, &resources); err != nil {
		return nil, err
	}
	var out []PlexServer
	for _, r := range resources {
		if strings.Contains(r.Provides, "server") {
			out = append(out, PlexServer{Name: r.Name, MachineIdentifier: r.ClientIdentifier, Owned: r.Owned})
		}
	}
	return out, nil
}

// HasServer reports whether the account has access to the given server.
func (p PlexTV) HasServer(ctx context.Context, clientID, token, machineID string) (bool, error) {
	servers, err := p.Servers(ctx, clientID, token)
	if err != nil {
		return false, err
	}
	for _, s := range servers {
		if s.MachineIdentifier == machineID {
			return true, nil
		}
	}
	return false, nil
}
