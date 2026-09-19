package server

import (
	"encoding/json"
	"io/fs"
	"net/http"

	"remorasftp/internal/events"
)

// mux builds the HTTP routing table.
func (s *Server) mux(auth *authState) {
	mux := http.NewServeMux()

	// Public (origin-checked, no token): health and one-time bootstrap.
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/bootstrap", s.handleBootstrap)

	// Authenticated endpoints.
	mux.HandleFunc("/api/handshake", s.handleHandshake)
	mux.HandleFunc("/api/version", s.handleVersion)

	mux.HandleFunc("/api/connections", s.handleConnections)
	mux.HandleFunc("/api/connections/", s.handleConnectionItem)
	mux.HandleFunc("/api/sessions", s.handleSessions)
	mux.HandleFunc("/api/sessions/", s.handleSessionItem)
	mux.HandleFunc("/api/trust", s.handleTrust)

	mux.HandleFunc("/api/settings", s.handleSettings)
	mux.HandleFunc("/api/onboarding", s.handleOnboarding)
	mux.HandleFunc("/api/activity", s.handleActivity)
	mux.HandleFunc("/api/transfers", s.handleTransfers)
	mux.HandleFunc("/api/transfers/", s.handleTransferItem)

	mux.HandleFunc("/api/favorites", s.handleFavorites)
	mux.HandleFunc("/api/favorites/", s.handleFavoriteItem)
	mux.HandleFunc("/api/recents", s.handleRecents)

	mux.HandleFunc("/api/local", s.handleLocal)
	mux.HandleFunc("/api/local/", s.handleLocal)

	mux.HandleFunc("/api/events", s.handleEvents)

	// Embedded single-page application for everything else.
	// The embed directive in assets.go captures webassets/ at compile
	// time: if the frontend was not built (cd web && npm run build)
	// before the Go binary was compiled, index.html is absent from the
	// embed. That must be a loud, actionable 503 - not an opaque 404
	// that looks like a routing bug.
	assets, err := webAssets()
	uiMissing := err != nil
	if err == nil {
		if _, serr := fs.Stat(assets, "index.html"); serr != nil {
			uiMissing = true
		}
	}
	if uiMissing {
		if s.bus != nil {
			s.bus.Info(events.TypeWarning,
				"embedded web UI is missing (index.html) - run 'cd web && npm run build', then rebuild the Go binary")
		}
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "embedded web UI is missing (index.html) - run 'cd web && npm run build', then rebuild the Go binary", http.StatusServiceUnavailable)
		})
	} else {
		spa := &spaHandler{assets: assets}
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			spa.ServeHTTP(w, r)
		})
	}

	s.handler = mux
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"product": "RemoraSFTP",
		"engine":  "running",
		"version": versionInfo().Version,
	})
}

func (s *Server) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Code == "" {
		writeErr(w, http.StatusBadRequest, "missing bootstrap code")
		return
	}
	if !s.auth.redeemBootstrapCode(req.Code) {
		writeErr(w, http.StatusUnauthorized, "invalid or expired bootstrap code")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token":   s.Token(),
		"version": versionInfo(),
	})
}

func (s *Server) handleHandshake(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"version":         versionInfo(),
		"startTime":       s.startTime,
		"credentialStore": s.mgr.CredentialBackend(),
		"onboarded":       s.cfg.Onboarded(),
		"settings":        s.cfg.Settings(),
		"sessions":        s.mgr.Sessions(),
	})
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, versionInfo())
}
