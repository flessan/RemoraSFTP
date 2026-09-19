package server

import (
	"net/http"

	"remorasftp/internal/config"

	"github.com/gorilla/websocket"
)

// GET /api/settings, PUT /api/settings
func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"settings": s.cfg.Settings()})
	case http.MethodPut:
		var st config.Settings
		if err := decodeBody(w, r, &st); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := s.cfg.SetSettings(st); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		s.bus.SetLevel(st.LogLevel)
		writeJSON(w, http.StatusOK, map[string]any{"settings": st})
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// POST /api/onboarding  - mark first-run complete
func (s *Server) handleOnboarding(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	if err := s.cfg.SetOnboarded(true); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"onboarded": true})
}

// GET /api/activity, DELETE /api/activity
func (s *Server) handleActivity(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"activity": s.bus.Activity()})
	case http.MethodDelete:
		if err := s.bus.ClearActivity(); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"cleared": true})
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// GET /api/transfers
func (s *Server) handleTransfers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "GET required")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"transfers": s.tm.List()})
}

// POST /api/transfers/{id}/cancel
// POST /api/transfers/{id}/retry
// POST /api/transfers/clear
func (s *Server) handleTransferItem(w http.ResponseWriter, r *http.Request) {
	tail := pathTail("/api/transfers/", r.URL.Path)
	if r.Method == http.MethodPost && len(tail) == 1 && tail[0] == "clear" {
		s.tm.Cleanup()
		writeJSON(w, http.StatusOK, map[string]any{"cleared": true})
		return
	}
	if len(tail) < 2 || r.Method != http.MethodPost {
		writeErr(w, http.StatusNotFound, "unknown transfer action")
		return
	}
	id, action := tail[0], tail[1]
	switch action {
	case "cancel":
		if err := s.tm.Cancel(id); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"canceled": id})
	case "retry":
		if _, err := s.tm.Retry(id); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"retried": id})
	default:
		writeErr(w, http.StatusNotFound, "unknown action")
	}
}

// GET /api/events - WebSocket stream of bus events. The token is carried via
// the Sec-WebSocket-Protocol subprotocol (never the URL query string).
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	up := s.wsUpgrader
	// Do NOT set Sec-WebSocket-Protocol manually here: the Upgrader's
	// Subprotocols field negotiates it (and echoes the chosen value). Setting
	// it again produces a duplicate response header, which Firefox rejects,
	// failing the whole handshake.
	conn, err := up.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	s.wsMu.Lock()
	s.wsClients[conn] = struct{}{}
	s.wsMu.Unlock()
	// Read pump: client messages are ignored; connection close removes it.
	go func() {
		defer func() {
			s.wsMu.Lock()
			delete(s.wsClients, conn)
			s.wsMu.Unlock()
			_ = conn.Close()
		}()
		conn.SetReadLimit(1024)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
	_ = websocket.TextMessage
}
