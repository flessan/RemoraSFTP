package server

import (
	"net/http"
	"strings"

	"remorasftp/internal/config"
	"remorasftp/internal/manager"
	"remorasftp/internal/protocol"
)

// GET /api/connections
// POST /api/connections
func (s *Server) handleConnections(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"connections": s.mgr.Profiles()})
	case http.MethodPost:
		var req profileRequest
		if err := decodeBody(w, r, &req); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid request body: "+err.Error())
			return
		}
		in := req.toInput("")
		c, err := s.mgr.SaveProfile(in)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"connection": c})
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// /api/connections/{id}        GET, PUT, DELETE
// /api/connections/{id}/connect   POST
// /api/connections/{id}/test      POST
// /api/connections/{id}/duplicate POST
func (s *Server) handleConnectionItem(w http.ResponseWriter, r *http.Request) {
	tail := pathTail("/api/connections/", r.URL.Path)
	if len(tail) == 0 || tail[0] == "" {
		http.NotFound(w, r)
		return
	}
	id := tail[0]
	action := ""
	if len(tail) > 1 {
		action = tail[1]
	}

	if action == "" {
		switch r.Method {
		case http.MethodGet:
			c, ok := s.mgr.Profile(id)
			if !ok {
				writeErr(w, http.StatusNotFound, "connection not found")
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"connection": c})
		case http.MethodPut:
			var req profileRequest
			if err := decodeBody(w, r, &req); err != nil {
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			c, err := s.mgr.SaveProfile(req.toInput(id))
			if err != nil {
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"connection": c})
		case http.MethodDelete:
			if err := s.mgr.DeleteProfile(id); err != nil {
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
		default:
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}

	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	ctx := r.Context()
	switch action {
	case "connect":
		sess, err := s.mgr.Connect(ctx, id)
		if err != nil {
			s.writeConnectError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"session": sess})
	case "test":
		err := s.mgr.TestConnection(ctx, id)
		if err != nil {
			s.writeConnectError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "connection test succeeded"})
	case "duplicate":
		c, err := s.mgr.DuplicateProfile(id)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"connection": c})
	default:
		writeErr(w, http.StatusNotFound, "unknown action")
	}
}

// writeConnectError maps trust/auth errors into structured responses so the
// UI can show the fingerprint prompt or an auth message.
func (s *Server) writeConnectError(w http.ResponseWriter, err error) {
	if u, ok := protocol.AsUnknownHostKey(err); ok {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":       "untrusted-host-key",
			"kind":        "ssh",
			"hostPort":    u.HostPort,
			"fingerprint": u.Fingerprint,
			"keyType":     u.KeyType,
		})
		return
	}
	if ce, ok := protocol.AsCertError(err); ok {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":       "untrusted-certificate",
			"kind":        "tls",
			"hostPort":    ce.HostPort,
			"fingerprint": ce.Fingerprint,
			"subject":     ce.Subject,
			"detail":      ce.Detail,
		})
		return
	}
	if ae, ok := err.(*protocol.AuthError); ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "authentication failed", "detail": ae.Detail})
		return
	}
	writeErr(w, http.StatusBadGateway, err.Error())
}

// /api/sessions
func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "GET required")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": s.mgr.Sessions()})
}

// /api/sessions/{id}/disconnect
// /api/sessions/{id}/reconnect
// /api/sessions/{id}/list?path=
// /api/sessions/{id}/stat?path=
// /api/sessions/{id}/mkdir
// /api/sessions/{id}/remove
// /api/sessions/{id}/rename
// /api/sessions/{id}/chmod
// /api/sessions/{id}/copy
// /api/sessions/{id}/search
// /api/sessions/{id}/upload
// /api/sessions/{id}/download?path=
// /api/sessions/{id}/preview?path=
func (s *Server) handleSessionItem(w http.ResponseWriter, r *http.Request) {
	tail := pathTail("/api/sessions/", r.URL.Path)
	if len(tail) == 0 {
		http.NotFound(w, r)
		return
	}
	sessionID := tail[0]
	action := ""
	if len(tail) > 1 {
		action = tail[1]
	}

	switch action {
	case "disconnect":
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "POST required")
			return
		}
		if err := s.mgr.Disconnect(sessionID); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"disconnected": sessionID})
	case "reconnect":
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "POST required")
			return
		}
		sess, err := s.mgr.Reconnect(r.Context(), sessionID)
		if err != nil {
			s.writeConnectError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"session": sess})
	case "list":
		s.handleList(w, r, sessionID)
	case "stat":
		s.handleStat(w, r, sessionID)
	case "mkdir", "remove", "rename", "chmod":
		s.handleFileOp(w, r, sessionID, action)
	case "copy":
		s.handleCopy(w, r, sessionID)
	case "search":
		s.handleSearch(w, r, sessionID)
	case "upload":
		s.handleUpload(w, r, sessionID)
	case "download":
		s.handleDownload(w, r, sessionID)
	case "preview":
		s.handlePreview(w, r, sessionID)
	default:
		writeErr(w, http.StatusNotFound, "unknown session action")
	}
}

// /api/trust               GET (list), POST {hostPort,fingerprint} trust
// /api/trust/{id}          DELETE
func (s *Server) handleTrust(w http.ResponseWriter, r *http.Request) {
	tail := pathTail("/api/trust", r.URL.Path)
	if r.Method == http.MethodGet && len(tail) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"trusted": s.mgr.TrustedHosts()})
		return
	}
	if r.Method == http.MethodPost && len(tail) == 0 {
		var req struct {
			HostPort    string `json:"hostPort"`
			Fingerprint string `json:"fingerprint"`
		}
		if err := decodeBody(w, r, &req); err != nil || req.HostPort == "" || req.Fingerprint == "" {
			writeErr(w, http.StatusBadRequest, "hostPort and fingerprint required")
			return
		}
		if err := s.mgr.TrustPending(req.HostPort, req.Fingerprint); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"trusted": req.HostPort})
		return
	}
	if r.Method == http.MethodDelete && len(tail) > 0 {
		if err := s.mgr.RemoveTrust(tail[0]); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"removed": tail[0]})
		return
	}
	writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
}

// profileRequest is the wire shape for create/update. Secret fields are
// write-only; they are never returned.
type profileRequest struct {
	Name          string  `json:"name"`
	Protocol      string  `json:"protocol"`
	Host          string  `json:"host"`
	Port          int     `json:"port"`
	Username      string  `json:"username"`
	Auth          string  `json:"auth"`
	StartDir      string  `json:"startDir"`
	FTPSImplicit  bool    `json:"ftpsImplicit"`
	TLSVerify     *bool   `json:"tlsVerify"`
	PassiveMode   *bool   `json:"passiveMode"`
	KeepAlive     int     `json:"keepAliveSeconds"`
	Encoding      string  `json:"encoding"`
	Password      *string `json:"password"`
	PrivateKeyPEM *string `json:"privateKeyPem"`
	KeyPassphrase *string `json:"keyPassphrase"`
}

func (req *profileRequest) toInput(id string) manager.ProfileInput {
	p := config.Protocol(strings.ToLower(req.Protocol))
	auth := config.AuthMethod(req.Auth)
	if auth == "" {
		auth = config.AuthPassword
	}
	return manager.ProfileInput{
		ID:            id,
		Name:          req.Name,
		Protocol:      p,
		Host:          req.Host,
		Port:          req.Port,
		Username:      req.Username,
		Auth:          auth,
		StartDir:      req.StartDir,
		FTPSImplicit:  req.FTPSImplicit,
		TLSVerify:     req.TLSVerify,
		PassiveMode:   req.PassiveMode,
		KeepAlive:     req.KeepAlive,
		Encoding:      req.Encoding,
		Password:      req.Password,
		PrivateKeyPEM: req.PrivateKeyPEM,
		KeyPassphrase: req.KeyPassphrase,
	}
}
