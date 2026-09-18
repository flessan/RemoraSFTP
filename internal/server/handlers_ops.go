package server

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"remorasftp/internal/config"
	"remorasftp/internal/events"
	"remorasftp/internal/localfs"
	"remorasftp/internal/manager"
	"remorasftp/internal/protocol"
	"remorasftp/internal/safepath"

	"github.com/google/uuid"
)

// ---- remote copy / move (session-scoped) ----------------------------------

// POST /api/sessions/{id}/copy  {from, to, move, policy}
func (s *Server) handleCopy(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	var req struct {
		From   string `json:"from"`
		To     string `json:"to"`
		Move   bool   `json:"move"`
		Policy string `json:"policy"`
	}
	if err := decodeBody(w, r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	policy, ok := protocol.ParseCopyPolicy(req.Policy)
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid policy")
		return
	}
	job, err := s.tm.StartCopy(r.Context(), sessionID, req.From, req.To, req.Move, policy)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.bus.Info(events.TypeInfo, "copy started: "+req.From+" -> "+req.To, events.P("session", sessionID))
	writeJSON(w, http.StatusOK, map[string]any{"job": s.tm.Snapshot(job.ID)})
}

// ---- remote search (session-scoped) ---------------------------------------

// POST /api/sessions/{id}/search
// {path, query, startsWith, extension, caseSensitive, includeHidden, recursive}
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	var req struct {
		Path          string `json:"path"`
		Query         string `json:"query"`
		StartsWith    bool   `json:"startsWith"`
		Extension     string `json:"extension"`
		CaseSensitive bool   `json:"caseSensitive"`
		IncludeHidden bool   `json:"includeHidden"`
		Recursive     bool   `json:"recursive"`
	}
	if err := decodeBody(w, r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.Query == "" && req.Extension == "" {
		writeErr(w, http.StatusBadRequest, "provide a query or an extension")
		return
	}
	q := manager.SearchQuery{
		Path:          safepath.Clean(req.Path),
		Query:         req.Query,
		StartsWith:    req.StartsWith,
		Extension:     req.Extension,
		CaseSensitive: req.CaseSensitive,
		IncludeHidden: req.IncludeHidden,
		Recursive:     req.Recursive,
	}
	results, canceled, err := s.mgr.Search(r.Context(), sessionID, q)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if results == nil {
		results = []protocol.Entry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"results":  results,
		"count":    len(results),
		"canceled": canceled,
	})
}

// ---- favorites / bookmarks ---------------------------------------------------

// GET /api/favorites
// POST /api/favorites {connectionId, path, label}
func (s *Server) handleFavorites(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		favs := s.cfg.Favorites()
		if favs == nil {
			favs = []config.Favorite{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"favorites": favs})
	case http.MethodPost:
		var req struct {
			ConnectionID string `json:"connectionId"`
			Path         string `json:"path"`
			Label        string `json:"label"`
		}
		if err := decodeBody(w, r, &req); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid body")
			return
		}
		f := config.Favorite{
			ID:           uuid.NewString(),
			ConnectionID: req.ConnectionID,
			Path:         safepath.Clean(req.Path),
			Label:        req.Label,
		}
		if err := s.cfg.AddFavorite(f); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		s.bus.Info(events.TypeInfo, "favorite added: "+f.Path, events.P("conn", f.ConnectionID))
		writeJSON(w, http.StatusCreated, map[string]any{"favorite": f})
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// DELETE /api/favorites/{id}
func (s *Server) handleFavoriteItem(w http.ResponseWriter, r *http.Request) {
	tail := pathTail("/api/favorites/", r.URL.Path)
	if len(tail) != 1 || tail[0] == "" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodDelete {
		writeErr(w, http.StatusMethodNotAllowed, "DELETE required")
		return
	}
	if err := s.cfg.RemoveFavorite(tail[0]); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"removed": tail[0]})
}

// ---- recent items ---------------------------------------------------------------

// GET /api/recents  → {files: RecentFile[], dirs: RecentLocation[]}
// POST /api/recents {connectionId, path, kind: "file"|"dir"}
func (s *Server) handleRecents(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		files := s.cfg.RecentFiles()
		dirs := s.cfg.Recents()
		if files == nil {
			files = []config.RecentFile{}
		}
		if dirs == nil {
			dirs = []config.RecentLocation{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"files": files, "dirs": dirs})
	case http.MethodPost:
		var req struct {
			ConnectionID string `json:"connectionId"`
			Path         string `json:"path"`
			Kind         string `json:"kind"`
		}
		if err := decodeBody(w, r, &req); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid body")
			return
		}
		if req.ConnectionID == "" || req.Path == "" {
			writeErr(w, http.StatusBadRequest, "connectionId and path required")
			return
		}
		var err error
		if req.Kind == "file" {
			err = s.cfg.AddRecentFile(req.ConnectionID, safepath.Clean(req.Path))
		} else {
			err = s.cfg.AddRecent(req.ConnectionID, safepath.Clean(req.Path))
		}
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// ---- local disk (dual-pane "Local" side) ----------------------------------------

// /api/local            GET → {home}
// /api/local/list?path=
// /api/local/stat?path=
// /api/local/mkdir      POST {path}
// /api/local/remove     POST {path, recursive}
// /api/local/rename     POST {from, to}
// /api/local/copy       POST {from, to, move, policy}
// /api/local/download?path=
// /api/local/upload?dir=&name=
func (s *Server) handleLocal(w http.ResponseWriter, r *http.Request) {
	action := pathTail("/api/local", r.URL.Path)
	if len(action) == 0 {
		if r.Method != http.MethodGet {
			writeErr(w, http.StatusMethodNotAllowed, "GET required")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"home": s.local.Home()})
		return
	}
	switch action[0] {
	case "list":
		if r.Method != http.MethodGet {
			writeErr(w, http.StatusMethodNotAllowed, "GET required")
			return
		}
		p := r.URL.Query().Get("path")
		entries, err := s.local.List(p)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"path": p, "entries": entries})
	case "stat":
		if r.Method != http.MethodGet {
			writeErr(w, http.StatusMethodNotAllowed, "GET required")
			return
		}
		e, err := s.local.Stat(r.URL.Query().Get("path"))
		if err != nil {
			writeErr(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"entry": e})
	case "mkdir":
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "POST required")
			return
		}
		var req struct {
			Path string `json:"path"`
		}
		if err := decodeBody(w, r, &req); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid body")
			return
		}
		if err := s.local.Mkdir(req.Path); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case "remove":
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "POST required")
			return
		}
		var req struct {
			Path      string `json:"path"`
			Recursive bool   `json:"recursive"`
		}
		if err := decodeBody(w, r, &req); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid body")
			return
		}
		if err := s.local.Remove(req.Path, req.Recursive); err != nil {
			writeErr(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case "rename":
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "POST required")
			return
		}
		var req struct {
			From string `json:"from"`
			To   string `json:"to"`
		}
		if err := decodeBody(w, r, &req); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid body")
			return
		}
		if err := s.local.Rename(req.From, req.To); err != nil {
			writeErr(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case "copy":
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "POST required")
			return
		}
		var req struct {
			From   string `json:"from"`
			To     string `json:"to"`
			Move   bool   `json:"move"`
			Policy string `json:"policy"`
		}
		if err := decodeBody(w, r, &req); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid body")
			return
		}
		policy, ok := protocol.ParseCopyPolicy(req.Policy)
		if !ok {
			writeErr(w, http.StatusBadRequest, "invalid policy")
			return
		}
		if err := s.local.Copy(req.From, req.To, req.Move, policy); err != nil {
			writeErr(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case "download":
		if r.Method != http.MethodGet {
			writeErr(w, http.StatusMethodNotAllowed, "GET required")
			return
		}
		p := r.URL.Query().Get("path")
		st, err := s.local.Stat(p)
		if err != nil || st.Type != protocol.EntryFile {
			writeErr(w, http.StatusNotFound, "not a file")
			return
		}
		f, size, err := s.local.Read(p)
		if err != nil {
			writeErr(w, http.StatusNotFound, err.Error())
			return
		}
		defer f.Close()
		name := localfs.Base(p)
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", safeHeaderName(name)))
		if size > 0 {
			w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
		}
		http.ServeContent(w, r, name, time.Now(), f)
		return
	case "upload":
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "POST required")
			return
		}
		dir := r.URL.Query().Get("dir")
		name := r.URL.Query().Get("name")
		full, err := localfs.JoinLocal(dir, name)
		if err != nil || full == "" {
			writeErr(w, http.StatusBadRequest, "dir and name required")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 256<<30)
		if err := s.local.Write(full, r.Body); err != nil {
			writeErr(w, http.StatusBadGateway, err.Error())
			return
		}
		s.bus.Info(events.TypeInfo, "local file written: "+full)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		writeErr(w, http.StatusNotFound, "unknown local action")
	}
}
