package clients

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
)

// Subsonic implements the slice of the Subsonic API that Navidrome exposes
// and MusicSeer needs: ping (auth test / login) and getArtists (library sync).
type Subsonic struct{}

func subsonicParams(username, password string) url.Values {
	saltBytes := make([]byte, 6)
	rand.Read(saltBytes)
	salt := hex.EncodeToString(saltBytes)
	sum := md5.Sum([]byte(password + salt))
	return url.Values{
		"u": {username},
		"t": {hex.EncodeToString(sum[:])},
		"s": {salt},
		"v": {"1.16.1"},
		"c": {"MusicSeer"},
		"f": {"json"},
	}
}

type subsonicEnvelope struct {
	Response struct {
		Status string `json:"status"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
		Artists *struct {
			Index []struct {
				Artist json.RawMessage `json:"artist"`
			} `json:"index"`
		} `json:"artists"`
	} `json:"subsonic-response"`
}

func (s Subsonic) call(ctx context.Context, baseURL, endpoint, username, password string) (*subsonicEnvelope, error) {
	u := fmt.Sprintf("%s/rest/%s?%s", baseURL, endpoint, subsonicParams(username, password).Encode())
	var env subsonicEnvelope
	if err := getJSON(ctx, nil, u, nil, &env); err != nil {
		return nil, err
	}
	if env.Response.Status != "ok" {
		msg := "unknown Subsonic error"
		if env.Response.Error != nil {
			msg = env.Response.Error.Message
		}
		return nil, fmt.Errorf("subsonic: %s", msg)
	}
	return &env, nil
}

// Ping returns nil when the credentials are valid.
func (s Subsonic) Ping(ctx context.Context, baseURL, username, password string) error {
	_, err := s.call(ctx, baseURL, "ping", username, password)
	return err
}

type SubsonicArtist struct {
	Name        string `json:"name"`
	MBID        string `json:"musicBrainzId"`
	AlbumCount  int    `json:"albumCount"`
	Starred     string `json:"starred"`
	UserRating  int    `json:"userRating"`
}

// GetArtists returns the full artist index flattened.
func (s Subsonic) GetArtists(ctx context.Context, baseURL, username, password string) ([]SubsonicArtist, error) {
	env, err := s.call(ctx, baseURL, "getArtists", username, password)
	if err != nil {
		return nil, err
	}
	var out []SubsonicArtist
	if env.Response.Artists == nil {
		return out, nil
	}
	for _, idx := range env.Response.Artists.Index {
		if len(idx.Artist) == 0 {
			continue
		}
		// "artist" may be an object or an array of objects.
		var many []SubsonicArtist
		if err := json.Unmarshal(idx.Artist, &many); err == nil {
			out = append(out, many...)
			continue
		}
		var one SubsonicArtist
		if err := json.Unmarshal(idx.Artist, &one); err == nil {
			out = append(out, one)
		}
	}
	return out, nil
}
