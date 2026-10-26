package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
)

type guestLimits struct {
	MaxPreviewSize int  `json:"maxPreviewSize"`
	AllowZoom      bool `json:"allowZoom"`
	AllowExact     bool `json:"allowExact"`
	AllowPNG       bool `json:"allowPNG"`
}

func defaultGuestLimits() guestLimits {
	return guestLimits{MaxPreviewSize: guestMaxPreviewSize}
}

func (s *server) loadGuestLimits() (guestLimits, error) {
	limits := defaultGuestLimits()
	if s.auth == nil {
		return limits, nil
	}
	var raw string
	err := s.auth.db.QueryRow(`SELECT value FROM app_settings WHERE key = 'guest_limits'`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return limits, nil
	}
	if err != nil {
		return limits, err
	}
	err = json.Unmarshal([]byte(raw), &limits)
	return limits, err
}

func (s *server) handleGuestLimits(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		limits, err := s.loadGuestLimits()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, limits)
	case http.MethodPut:
		// Start from defaults so omitted properties retain safe defaults.
		limits := defaultGuestLimits()
		if err := decodeJSONRequest(r, &limits); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if limits.MaxPreviewSize < 512 || limits.MaxPreviewSize > 4096 {
			writeError(w, http.StatusBadRequest, "maximum guest preview size must be between 512 and 4096 pixels")
			return
		}
		raw, err := json.Marshal(limits)
		if err == nil {
			_, err = s.auth.db.Exec(`INSERT INTO app_settings(key, value) VALUES ('guest_limits', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, string(raw))
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.auth.logAudit(actor, "update", "settings", "guest_limits", string(raw))
		writeJSON(w, http.StatusOK, limits)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}
