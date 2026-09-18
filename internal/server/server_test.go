package server

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"remorasftp/internal/config"
	"remorasftp/internal/credentials"
	"remorasftp/internal/events"
	"remorasftp/internal/manager"
	"remorasftp/internal/protocol"
	"remorasftp/internal/transfers"
	"remorasftp/internal/trust"
)

// newTestServer starts an engine on an ephemeral loopback port for tests.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SFTPBOX_DATA_DIR", dir)

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	creds, err := credentials.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	tr, err := trust.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	bus := events.NewBus(dir, "info")
	mgr := manager.New(cfg, creds, tr, bus)
	tm := transfers.New(bus, mgr, 2)
	srv := New(Options{Config: cfg, Manager: mgr, Transfers: tm, Bus: bus})

	if err := srv.Start("127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	return srv
}

// injectClient registers a connected session backed by cl (test support).
func injectClient(srv *Server, sessionID string, cl protocol.Client) {
	caps := cl.Capabilities()
	srv.mgr.InjectTestSession(sessionID, manager.TestSession{
		ConnID:      "test-conn",
		ConnName:    "Test",
		Protocol:    caps.Protocol,
		Host:        "test.local",
		StartDir:    "/",
		CWD:         "/",
		Client:      cl,
		ConnectedAt: time.Now(),
	})
}

func TestBindsLoopbackOnly(t *testing.T) {
	srv := newTestServer(t)
	host, _, err := net.SplitHostPort(srv.Addr())
	if err != nil {
		t.Fatal(err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		t.Fatalf("server bound to non-loopback address: %s", host)
	}
}

func TestBootstrapFlow(t *testing.T) {
	srv := newTestServer(t)
	base := "http://" + srv.Addr()

	// Forged code rejected.
	resp, err := http.Post(base+"/api/bootstrap", "application/json",
		strings.NewReader(`{"code":"forged"}`))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("forged code: got %d, want 401", resp.StatusCode)
	}
	resp.Body.Close()

	// Valid single-use code redeems once.
	code := srv.auth.newBootstrapCode(time.Minute)
	body, _ := json.Marshal(map[string]string{"code": code})
	resp, err = http.Post(base+"/api/bootstrap", "application/json",
		strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Token string `json:"token"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()
	if out.Token == "" || out.Token != srv.Token() {
		t.Fatal("bootstrap did not return the instance token")
	}

	// Second redemption fails.
	resp, _ = http.Post(base+"/api/bootstrap", "application/json",
		strings.NewReader(string(body)))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("reused code: got %d, want 401", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestConnectionProfileDoesNotLeakSecret(t *testing.T) {
	srv := newTestServer(t)
	base := "http://" + srv.Addr()
	token := srv.Token()

	// do always sends the CSRF header that state-changing requests require.
	do := func(method, path, body string) *http.Response {
		req, _ := http.NewRequest(method, base+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Requested-With", "RemoraSFTP") // required for non-GET
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	// Create (POST -> needs X-Requested-With).
	resp := do("POST", "/api/connections",
		`{"name":"T","protocol":"sftp","host":"h","port":22,"username":"u","password":"secret-pw"}`)
	if resp.StatusCode != http.StatusCreated {
		buf, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("create: %d body=%s", resp.StatusCode, string(buf))
	}
	var created struct {
		Connection struct {
			ID string `json:"id"`
		} `json:"connection"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()
	id := created.Connection.ID

	// List must never echo the password.
	resp = do("GET", "/api/connections", "")
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if strings.Contains(string(raw), "secret-pw") {
		t.Fatal("secret leaked through the connection list response")
	}

	// Delete (DELETE -> needs X-Requested-With).
	resp = do("DELETE", "/api/connections/"+id, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete: %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestSecurityHeaders(t *testing.T) {
	srv := newTestServer(t)
	resp, err := http.Get("http://" + srv.Addr() + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	for _, h := range []string{"X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy"} {
		if resp.Header.Get(h) == "" {
			t.Errorf("security header %q missing", h)
		}
	}
}

// TestSPAFallbackForClientRoutes is the regression test for the
// "/bootstrap/<code> -> 404 page not found" bug: client-side routes must be
// served index.html, and only truly-missing assets return 404.
func TestSPAFallbackForClientRoutes(t *testing.T) {
	srv := newTestServer(t)
	base := "http://" + srv.Addr()

	for _, route := range []string{
		"/",
		"/bootstrap/GB5p___P0qIOUnF8xZmweg",
		"/transfers",
		"/settings",
		"/activity",
	} {
		resp, err := http.Get(base + route)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			// The body carries the actionable diagnosis when the built UI
			// is absent from the embed (503 + instructions).
			t.Fatalf("GET %s: status %d, want 200 (SPA fallback); body: %s",
				route, resp.StatusCode, truncate(string(body), 160))
		}
		if !strings.Contains(string(body), `<div id="root">`) {
			t.Fatalf("GET %s: expected SPA index.html, body started %q",
				route, truncate(string(body), 80))
		}
	}

	// Missing hashed asset stays a real 404 (must not return HTML as JS).
	resp, err := http.Get(base + "/assets/does-not-exist-0000.js")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing /assets file: got %d, want 404", resp.StatusCode)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
