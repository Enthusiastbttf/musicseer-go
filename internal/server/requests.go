package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"musicseer/internal/clients"
	"musicseer/internal/store"
)

func (s *Server) handleRequestsList(w http.ResponseWriter, r *http.Request, u *store.User) {
	all := r.URL.Query().Get("all") == "1" && u.Role == "admin"
	reqs, err := s.st.RequestsList(u.ID, all)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if reqs == nil {
		reqs = []store.Request{}
	}
	jsonWrite(w, http.StatusOK, reqs)
}

func (s *Server) handleRequestCreate(w http.ResponseWriter, r *http.Request, u *store.User) {
	var body struct {
		ArtistName string `json:"artistName"`
		ArtistMBID string `json:"artistMbid"`
		AlbumName  string `json:"albumName"`
		AlbumMBID  string `json:"albumMbid"`
	}
	if err := decodeBody(r, &body); err != nil || strings.TrimSpace(body.ArtistName) == "" {
		jsonError(w, http.StatusBadRequest, "artistName required")
		return
	}
	body.ArtistName = strings.TrimSpace(body.ArtistName)
	if body.AlbumMBID != "" && body.ArtistMBID == "" {
		jsonError(w, http.StatusBadRequest, "album requests need the artist's MusicBrainz id")
		return
	}

	status := "pending"
	if u.CanAutoApprove || u.Role == "admin" {
		status = "approved"
	}
	id, err := s.st.CreateRequest(u.ID, body.ArtistName, body.ArtistMBID, body.AlbumName, body.AlbumMBID, status)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			jsonError(w, http.StatusConflict, "you already have an open request for this artist")
			return
		}
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Auto-approved requests go straight to Lidarr, in the background so the
	// UI gets an instant response.
	if status == "approved" {
		go s.pushToLidarr(id)
	}
	req, _ := s.st.RequestByID(id)
	jsonWrite(w, http.StatusCreated, req)
}

func (s *Server) handleRequestApprove(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, err := pathID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := s.st.UpdateRequestStatus(id, "approved", "", "", 0); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	go s.pushToLidarr(id)
	req, _ := s.st.RequestByID(id)
	jsonWrite(w, http.StatusOK, req)
}

func (s *Server) handleRequestReject(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, err := pathID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "bad id")
		return
	}
	var body struct {
		Notes string `json:"notes"`
	}
	decodeBody(r, &body) // notes optional
	if err := s.st.UpdateRequestStatus(id, "rejected", body.Notes, "", 0); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	req, _ := s.st.RequestByID(id)
	jsonWrite(w, http.StatusOK, req)
}

func (s *Server) handleRequestRetry(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, err := pathID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := s.st.UpdateRequestStatus(id, "approved", "", "", 0); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	go s.pushToLidarr(id)
	req, _ := s.st.RequestByID(id)
	jsonWrite(w, http.StatusOK, req)
}

func (s *Server) handleRequestDelete(w http.ResponseWriter, r *http.Request, u *store.User) {
	id, err := pathID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "bad id")
		return
	}
	req, err := s.st.RequestByID(id)
	if err != nil {
		jsonError(w, http.StatusNotFound, "request not found")
		return
	}
	if u.Role != "admin" && req.UserID != u.ID {
		jsonError(w, http.StatusForbidden, "not your request")
		return
	}
	if err := s.st.DeleteRequest(id); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonWrite(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// pushToLidarr sends an approved request to the active Lidarr instance.
// Runs in a goroutine; the request row records the outcome either way.
func (s *Server) pushToLidarr(requestID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	req, err := s.st.RequestByID(requestID)
	if err != nil {
		return
	}
	fail := func(msg string) {
		s.log.Warn("lidarr push failed", "request", requestID, "artist", req.ArtistName, "err", msg)
		s.st.UpdateRequestStatus(requestID, "failed", "", msg, 0)
	}

	inst, err := s.st.FirstActiveInstance("lidarr")
	if err != nil {
		fail("no active Lidarr instance configured")
		return
	}
	apiKey, err := s.box.Decrypt(inst.APIKeyEnc)
	if err != nil {
		fail("cannot decrypt Lidarr API key")
		return
	}
	if inst.RootFolder == "" || inst.QualityProfileID == 0 || inst.MetadataProfileID == 0 {
		fail("Lidarr instance is missing root folder / profile configuration — edit it in Admin → Instances")
		return
	}

	mbid := req.ArtistMBID
	if mbid == "" {
		// Resolve via Lidarr's own lookup (it proxies MusicBrainz server-side).
		mbid, err = s.eng.Lidarr.LookupMBID(ctx, inst.BaseURL, apiKey, req.ArtistName)
		if err != nil || mbid == "" {
			fail("could not resolve a MusicBrainz ID for this artist")
			return
		}
	}

	// ---- album-level request ----
	if req.AlbumMBID != "" {
		s.pushAlbumToLidarr(ctx, requestID, req, inst, apiKey, mbid, fail)
		return
	}

	// ---- artist-level request ----
	lidarrID, err := s.eng.Lidarr.AddArtist(ctx, inst.BaseURL, apiKey, mbid, req.ArtistName,
		inst.QualityProfileID, inst.MetadataProfileID, inst.RootFolder, "all", true)
	if err != nil {
		if errors.Is(err, clients.ErrLidarrDuplicate) {
			s.st.UpdateRequestStatus(requestID, "sent", "already in Lidarr", "", 0)
			return
		}
		fail(err.Error())
		return
	}
	s.st.UpdateRequestStatus(requestID, "sent", "", "", lidarrID)
	s.log.Info("request sent to lidarr", "artist", req.ArtistName, "lidarrId", lidarrID)
}

// pushAlbumToLidarr fulfils a single-album request: ensure the artist exists
// in Lidarr (added unmonitored if new), wait for Lidarr to populate its album
// list, then monitor + search just the requested album.
func (s *Server) pushAlbumToLidarr(ctx context.Context, requestID int64, req *store.Request,
	inst *store.Instance, apiKey, artistMBID string, fail func(string)) {

	artistID, err := s.eng.Lidarr.FindArtistID(ctx, inst.BaseURL, apiKey, artistMBID)
	if err != nil {
		fail("lidarr lookup failed: " + err.Error())
		return
	}
	if artistID == 0 {
		// New artist: add with nothing monitored so only the requested album
		// gets grabbed, not the whole discography.
		artistID, err = s.eng.Lidarr.AddArtist(ctx, inst.BaseURL, apiKey, artistMBID, req.ArtistName,
			inst.QualityProfileID, inst.MetadataProfileID, inst.RootFolder, "none", false)
		if err != nil && !errors.Is(err, clients.ErrLidarrDuplicate) {
			fail(err.Error())
			return
		}
		if artistID == 0 { // duplicate race: look it up again
			artistID, _ = s.eng.Lidarr.FindArtistID(ctx, inst.BaseURL, apiKey, artistMBID)
		}
	}
	if artistID == 0 {
		fail("could not find or add the artist in Lidarr")
		return
	}

	// Lidarr populates albums asynchronously after an artist is added — poll
	// briefly until the requested release group shows up.
	var albumID int64
	for attempt := 0; attempt < 12; attempt++ {
		albums, err := s.eng.Lidarr.Albums(ctx, inst.BaseURL, apiKey, artistID)
		if err == nil {
			for _, a := range albums {
				if strings.EqualFold(a.ForeignAlbumID, req.AlbumMBID) {
					albumID = a.ID
					break
				}
			}
		}
		if albumID != 0 {
			break
		}
		select {
		case <-ctx.Done():
			fail("timed out waiting for Lidarr to load the artist's albums")
			return
		case <-time.After(5 * time.Second):
		}
	}
	if albumID == 0 {
		fail("Lidarr did not list this album for the artist (it may still be refreshing — retry in a minute)")
		return
	}

	if err := s.eng.Lidarr.MonitorAlbums(ctx, inst.BaseURL, apiKey, []int64{albumID}, true); err != nil {
		fail("could not monitor the album: " + err.Error())
		return
	}
	if err := s.eng.Lidarr.SearchAlbums(ctx, inst.BaseURL, apiKey, []int64{albumID}); err != nil {
		fail("album monitored but search failed to start: " + err.Error())
		return
	}
	s.st.UpdateRequestStatus(requestID, "sent", "", "", albumID)
	s.log.Info("album request sent to lidarr", "artist", req.ArtistName, "album", req.AlbumName, "albumId", albumID)
}
