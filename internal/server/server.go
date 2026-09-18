package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"remorasftp/internal/apppaths"
	"remorasftp/internal/config"
	"remorasftp/internal/events"
	"remorasftp/internal/localfs"
	"remorasftp/internal/manager"
	"remorasftp/internal/transfers"
	"remorasftp/internal/version"

	"github.com/gorilla/websocket"
)

// Server is the local loopback HTTP/WebSocket server.
type Server struct {
	auth  *authState
	cfg   *config.Store
	mgr   *manager.Manager
	tm    *transfers.Manager
	bus   *events.Bus
	local *localfs.Service

	listener   net.Listener
	httpSrv    *http.Server
	wsUpgrader websocket.Upgrader
	wsMu       sync.Mutex
	wsClients  map[*websocket.Conn]struct{}

	startTime time.Time
	handler   http.Handler
}

// Options configures a new server.
type Options struct {
	Config        *config.Store
	Manager       *manager.Manager
	Transfers     *transfers.Manager
	Bus           *events.Bus
	ListenAddress string // "127.0.0.1" default; only changed by explicit opt-in
	Port          int    // 0 = pick an available port
}

// New creates the server (not yet listening).
func New(opts Options) *Server {
	auth := newAuthState()
	s := &Server{
		auth:      auth,
		cfg:       opts.Config,
		mgr:       opts.Manager,
		tm:        opts.Transfers,
		bus:       opts.Bus,
		local:     localfs.New(),
		wsClients: map[*websocket.Conn]struct{}{},
		startTime: time.Now(),
		wsUpgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 4096,
			// Origin is validated in auth middleware; accept the
			// subprotocol we use to carry the token (see extractToken).
			Subprotocols: []string{"remorasftp"},
		},
	}
	s.mux(auth)
	_ = opts
	return s
}

// Start binds the listener. It defaults to loopback only.
func (s *Server) Start(listenAddr string, port int) error {
	if listenAddr == "" {
		listenAddr = "127.0.0.1"
	}
	addr := net.JoinHostPort(listenAddr, strconv.Itoa(port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	s.listener = ln
	host, p, _ := net.SplitHostPort(ln.Addr().String())
	s.auth.bindAddr = net.JoinHostPort(host, p)
	s.auth.loopbackOnly = isLoopback(host)
	if !s.auth.loopbackOnly {
		// Non-loopback binding is an explicit, user-opted choice; record a
		// visible warning in the activity log.
		s.bus.Warn(fmt.Sprintf("local API bound to non-loopback address %s - reachable from the network", s.auth.bindAddr))
	}

	s.httpSrv = &http.Server{
		Handler:      securityHeaders(s.auth.authMiddleware(s.handler)),
		ReadTimeout:  0, // transfers may be long-lived
		WriteTimeout: 0,
		IdleTimeout:  120 * time.Second,
	}
	go s.fanout()
	go func() { _ = s.httpSrv.Serve(ln) }()

	if err := s.writeInstanceFile(); err != nil {
		s.bus.Warn("could not write instance file: " + err.Error())
	}
	s.bus.Info(events.TypeEngineReady, "RemoraSFTP engine listening on "+s.auth.bindAddr)
	return nil
}

// Addr returns the bound host:port.
func (s *Server) Addr() string { return s.auth.bindAddr }

// Token returns the per-instance session token (shown in CLI output, used for
// automation). Never logged beyond explicit CLI/terminal display.
func (s *Server) Token() string {
	s.auth.mu.Lock()
	defer s.auth.mu.Unlock()
	return s.auth.token
}

// BrowserURL returns the loopback URL including a single-use bootstrap code.
func (s *Server) BrowserURL() string {
	host, port, _ := net.SplitHostPort(s.auth.bindAddr)
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	code := s.auth.newBootstrapCode(2 * time.Minute)
	return fmt.Sprintf("http://%s/bootstrap/%s", net.JoinHostPort(host, port), code)
}

// Shutdown stops the HTTP server gracefully.
func (s *Server) Shutdown(ctx context.Context) error {
	// Only remove the instance file if this server actually created a
	// listener (one-shot CLI commands build a server without starting it
	// and must not delete a running GUI engine's instance file).
	if s.httpSrv != nil {
		_ = s.removeInstanceFile()
	}
	s.wsMu.Lock()
	for c := range s.wsClients {
		_ = c.Close()
	}
	s.wsClients = map[*websocket.Conn]struct{}{}
	s.wsMu.Unlock()
	if s.httpSrv != nil {
		return s.httpSrv.Shutdown(ctx)
	}
	return nil
}

func isLoopback(host string) bool {
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// instanceFile records the running instance for `remorasftp status`. Mode 0600;
// contains host:port plus the token so the CLI can talk to a locally running
// instance. It is removed on shutdown.
type instanceFile struct {
	URL   string `json:"url"`
	Token string `json:"token"`
	PID   int    `json:"pid"`
}

func (s *Server) instancePath() (string, error) {
	return apppaths.File("instance.json")
}

func (s *Server) writeInstanceFile() error {
	p, err := s.instancePath()
	if err != nil {
		return err
	}
	host, port, _ := net.SplitHostPort(s.auth.bindAddr)
	doc := instanceFile{
		URL:   fmt.Sprintf("http://%s", net.JoinHostPort(host, port)),
		Token: s.Token(),
		PID:   os.Getpid(),
	}
	raw, _ := json.MarshalIndent(doc, "", "  ")
	tmp := filepath.Join(filepath.Dir(p), ".instance.tmp")
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func (s *Server) removeInstanceFile() error {
	p, err := s.instancePath()
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ReadInstanceFile is used by `remorasftp status`.
func ReadInstanceFile() (*instanceFile, error) {
	p, err := apppaths.File("instance.json")
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var f instanceFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

// fanout broadcasts bus events to all WebSocket clients.
func (s *Server) fanout() {
	ch, unsub := s.bus.Subscribe()
	defer unsub()
	for ev := range ch {
		payload, err := json.Marshal(ev)
		if err != nil {
			continue
		}
		s.wsMu.Lock()
		clients := make([]*websocket.Conn, 0, len(s.wsClients))
		for c := range s.wsClients {
			clients = append(clients, c)
		}
		s.wsMu.Unlock()
		for _, c := range clients {
			_ = c.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.WriteMessage(websocket.TextMessage, payload); err != nil {
				s.wsMu.Lock()
				delete(s.wsClients, c)
				s.wsMu.Unlock()
				_ = c.Close()
			}
		}
	}
}

var errServerStopped = errors.New("server stopped")

// versionResponse describes the running build for the UI.
type versionResponse struct {
	version.Info
	StartTime time.Time `json:"startTime"`
}
