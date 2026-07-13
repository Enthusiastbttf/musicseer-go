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
	}
	if err := decodeBody(r, &body); err != nil || strings.TrimSpace(body.ArtistName) == "" {
		jsonError(w, http.StatusBadRequest, "artistName required")
		return
	}
	body.ArtistName = strings.TrimSpace(body.ArtistName)

	status := "pending"
	if u.CanAutoApprove || u.Role == "admin" {
		status = "approved"
	}
	id, err := s.st.CreateRequest(u.ID, body.ArtistName, body.ArtistMBID, status)
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

	lidarrID, err := s.eng.Lidarr.AddArtist(ctx, inst.BaseURL, apiKey, mbid, req.ArtistName,
		inst.QualityProfileID, inst.MetadataProfileID, inst.RootFolder)
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
