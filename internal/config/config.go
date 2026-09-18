// Package config persists non-secret application state: connection profiles
// (without passwords or private keys), user preferences, and schema metadata.
//
// Secrets never live in this file. See the credentials package.
//
// The file format is versioned (ConfigVersion). Future releases migrate older
// documents forward through explicit migration steps rather than silently
// rewriting unknown fields.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"remorasftp/internal/apppaths"
)

// CurrentSchemaVersion is bumped whenever the on-disk shape changes.
const CurrentSchemaVersion = 2

// Protocol identifies a remote file protocol.
type Protocol string

const (
	ProtoFTP  Protocol = "ftp"
	ProtoFTPS Protocol = "ftps"
	ProtoSFTP Protocol = "sftp"
)

// AuthMethod selects how a connection authenticates.
type AuthMethod string

const (
	AuthPassword AuthMethod = "password"
	AuthKey      AuthMethod = "key" // SFTP private key (key may have passphrase)
	AuthKeyAgent AuthMethod = "keyagent"
	AuthNone     AuthMethod = "none" // anonymous FTP
)

// Connection is a reusable server profile. Secret fields (password, private
// key, key passphrase) are stored via the credentials provider, keyed by
// connection ID - never serialized here.
type Connection struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Protocol Protocol   `json:"protocol"`
	Host     string     `json:"host"`
	Port     int        `json:"port"`
	Username string     `json:"username"`
	Auth     AuthMethod `json:"auth"`
	StartDir string     `json:"startDir,omitempty"`
	// HasSecret is true when a password/passphrase is stored in the
	// credential vault for this connection. UI uses it to show
	// "••••••••" state without ever receiving the secret.
	HasSecret bool `json:"hasSecret,omitempty"`
	// HasPrivateKey is true when a PEM private key is stored in the vault.
	HasPrivateKey bool               `json:"hasPrivateKey,omitempty"`
	Settings      ConnectionSettings `json:"settings,omitempty"`
	CreatedAt     time.Time          `json:"createdAt"`
	UpdatedAt     time.Time          `json:"updatedAt"`
}

// ConnectionSettings holds advanced, protocol-specific options.
type ConnectionSettings struct {
	// FTPS
	FTPSImplicit        bool   `json:"ftpsImplicit,omitempty"`  // implicit TLS (port 990) vs explicit AUTH TLS
	TLSVerify           bool   `json:"tlsVerify"`               // verify server certificate (default true)
	TLSPinnedCertSHA256 string `json:"tlsPinnedCert,omitempty"` // optional pin, hex sha256 of DER leaf cert
	// SFTP
	KeepAliveSeconds int `json:"keepAliveSeconds,omitempty"`
	// FTP/FTPS
	PassiveMode bool `json:"passiveMode"` // default true
	// Encoding for legacy FTP servers (blank = UTF-8)
	Encoding string `json:"encoding,omitempty"`
}

// Settings stores user preferences.
type Settings struct {
	Language            string `json:"language"`    // "en" (default), "id", ...
	Theme               string `json:"theme"`       // "system" | "light" | "dark"
	DefaultView         string `json:"defaultView"` // "details" | "list" | "icons"
	ShowHidden          bool   `json:"showHidden"`
	ConfirmDeletes      bool   `json:"confirmDeletes"`
	OpenBrowserOnStart  bool   `json:"openBrowserOnStart"`
	StartupMode         string `json:"startupMode"`         // "ask" | "browser" | "no-browser"
	ConcurrentTransfers int    `json:"concurrentTransfers"` // worker count
	RemoteAccess        bool   `json:"remoteAccess"`        // advanced: bind beyond loopback
	ListenAddress       string `json:"listenAddress"`       // default "127.0.0.1"
	LogLevel            string `json:"logLevel"`            // "off" | "error" | "info" | "debug"
	ReducedMotion       bool   `json:"reducedMotion"`
}

func defaultSettings() Settings {
	return Settings{
		Language:            "en",
		Theme:               "system",
		DefaultView:         "details",
		ConfirmDeletes:      true,
		OpenBrowserOnStart:  true,
		StartupMode:         "ask",
		ConcurrentTransfers: 3,
		ListenAddress:       "127.0.0.1",
		LogLevel:            "info",
	}
}

// RecentLocation is a breadcrumb-history entry (recently visited
// directories, per connection).
type RecentLocation struct {
	ConnectionID string    `json:"connectionId"`
	Path         string    `json:"path"`
	VisitedAt    time.Time `json:"visitedAt"`
}

// Favorite is a bookmarked remote path. Paths never carry credentials; the
// connection reference resolves credentials through the credential vault at
// connect time only.
type Favorite struct {
	ID           string    `json:"id"`
	ConnectionID string    `json:"connectionId"`
	Path         string    `json:"path"`
	Label        string    `json:"label,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
}

// RecentFile is a recently opened file (for the Recent section).
type RecentFile struct {
	ConnectionID string    `json:"connectionId"`
	Path         string    `json:"path"`
	OpenedAt     time.Time `json:"openedAt"`
}

// Root is the serialized config document.
type Root struct {
	SchemaVersion int              `json:"schemaVersion"`
	Connections   []*Connection    `json:"connections"`
	Settings      Settings         `json:"settings"`
	Recents       []RecentLocation `json:"recents"`
	Favorites     []Favorite       `json:"favorites"`
	RecentFiles   []RecentFile     `json:"recentFiles"`
	Onboarded     bool             `json:"onboarded"`
}

// Store is the concurrency-safe configuration store.
type Store struct {
	mu   sync.RWMutex
	path string
	root Root
}

// Load reads the config file, migrating older schema versions in place.
func Load() (*Store, error) {
	p, err := apppaths.File("config.json")
	if err != nil {
		return nil, err
	}
	s := &Store{path: p, root: Root{SchemaVersion: CurrentSchemaVersion, Settings: defaultSettings()}}
	raw, err := os.ReadFile(p)
	switch {
	case errors.Is(err, os.ErrNotExist):
		s.root.Connections = []*Connection{}
		return s, s.saveLocked()
	case err != nil:
		return nil, err
	}
	if err := json.Unmarshal(raw, &s.root); err != nil {
		return nil, fmt.Errorf("config file %s is corrupt: %w", p, err)
	}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	if s.root.Connections == nil {
		s.root.Connections = []*Connection{}
	}
	return s, nil
}

// migrate upgrades the document to CurrentSchemaVersion. Each case must be
// idempotent and must never destroy connection profiles.
func (s *Store) migrate() error {
	v := s.root.SchemaVersion
	if v == 0 {
		// Unversioned/unknown document: keep data, stamp current version.
		v = CurrentSchemaVersion
	}
	if v > CurrentSchemaVersion {
		return fmt.Errorf("config schema version %d is newer than this release supports (%d); please update RemoraSFTP", v, CurrentSchemaVersion)
	}
	// Version 1 is the initial schema. Version 2 adds favorites and recent
	// files (new optional fields; no data transformation required).
	if v < CurrentSchemaVersion {
		v = CurrentSchemaVersion
	}
	s.root.SchemaVersion = CurrentSchemaVersion
	// Re-apply defaults for missing preference fields.
	d := defaultSettings()
	if s.root.Settings.Language == "" {
		s.root.Settings.Language = d.Language
	}
	if s.root.Settings.Theme == "" {
		s.root.Settings.Theme = d.Theme
	}
	if s.root.Settings.DefaultView == "" {
		s.root.Settings.DefaultView = d.DefaultView
	}
	// Legacy default views ("list"/"grid") remain valid; the UI maps "grid"
	// to the icon views.
	if s.root.Settings.StartupMode == "" {
		s.root.Settings.StartupMode = d.StartupMode
	}
	if s.root.Settings.ConcurrentTransfers <= 0 {
		s.root.Settings.ConcurrentTransfers = d.ConcurrentTransfers
	}
	if s.root.Settings.ListenAddress == "" {
		s.root.Settings.ListenAddress = d.ListenAddress
	}
	if s.root.Settings.LogLevel == "" {
		s.root.Settings.LogLevel = d.LogLevel
	}
	return s.saveLocked()
}

// saveLocked writes atomically: temp file in same directory, fsync, rename.
// Caller must hold the write lock.
func (s *Store) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	s.root.SchemaVersion = CurrentSchemaVersion
	raw, err := json.MarshalIndent(&s.root, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".config-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, s.path)
}

// Connections returns a snapshot copy of all connection profiles.
func (s *Store) Connections() []*Connection {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Connection, len(s.root.Connections))
	for i, c := range s.root.Connections {
		cp := *c
		out[i] = &cp
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

// Connection returns one profile by ID.
func (s *Store) Connection(id string) (*Connection, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, c := range s.root.Connections {
		if c.ID == id {
			cp := *c
			return &cp, true
		}
	}
	return nil, false
}

// Upsert creates or replaces a connection profile.
func (s *Store) Upsert(c *Connection) error {
	if c.ID == "" {
		return errors.New("connection id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	for i, existing := range s.root.Connections {
		if existing.ID == c.ID {
			c.CreatedAt = existing.CreatedAt
			c.UpdatedAt = now
			cp := *c
			s.root.Connections[i] = &cp
			return s.saveLocked()
		}
	}
	c.CreatedAt = now
	c.UpdatedAt = now
	cp := *c
	s.root.Connections = append(s.root.Connections, &cp)
	return s.saveLocked()
}

// Delete removes a profile by ID.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.root.Connections[:0]
	found := false
	for _, c := range s.root.Connections {
		if c.ID == id {
			found = true
			continue
		}
		kept = append(kept, c)
	}
	if !found {
		return fmt.Errorf("connection %q not found", id)
	}
	s.root.Connections = kept
	return s.saveLocked()
}

// Settings returns the current settings.
func (s *Store) Settings() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.root.Settings
}

// SetSettings replaces settings.
func (s *Store) SetSettings(st Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st.ConcurrentTransfers <= 0 {
		st.ConcurrentTransfers = defaultSettings().ConcurrentTransfers
	}
	if st.ListenAddress == "" {
		st.ListenAddress = defaultSettings().ListenAddress
	}
	s.root.Settings = st
	return s.saveLocked()
}

// SetOnboarded marks first-run onboarding complete.
func (s *Store) SetOnboarded(v bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.root.Onboarded = v
	return s.saveLocked()
}

// Onboarded reports whether onboarding has been completed.
func (s *Store) Onboarded() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.root.Onboarded
}

// Favorites returns all bookmarks, most recently added first.
func (s *Store) Favorites() []Favorite {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Favorite, len(s.root.Favorites))
	copy(out, s.root.Favorites)
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

// AddFavorite stores a bookmark. A bookmark for the same (connection, path)
// is refreshed in place instead of duplicated. The caller supplies the ID
// (generated outside this package).
func (s *Store) AddFavorite(f Favorite) error {
	if f.ConnectionID == "" || f.Path == "" {
		return errors.New("favorite requires connectionId and path")
	}
	if f.ID == "" {
		return errors.New("favorite id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, existing := range s.root.Favorites {
		if existing.ConnectionID == f.ConnectionID && existing.Path == f.Path {
			f.CreatedAt = existing.CreatedAt
			s.root.Favorites[i] = f
			return s.saveLocked()
		}
	}
	f.CreatedAt = time.Now().UTC()
	s.root.Favorites = append(s.root.Favorites, f)
	return s.saveLocked()
}

// RemoveFavorite deletes a bookmark by ID.
func (s *Store) RemoveFavorite(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.root.Favorites[:0]
	found := false
	for _, f := range s.root.Favorites {
		if f.ID == id {
			found = true
			continue
		}
		kept = append(kept, f)
	}
	if !found {
		return fmt.Errorf("favorite %q not found", id)
	}
	s.root.Favorites = kept
	return s.saveLocked()
}

// RecentFiles returns recently opened files, newest first.
func (s *Store) RecentFiles() []RecentFile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]RecentFile, len(s.root.RecentFiles))
	copy(out, s.root.RecentFiles)
	sort.Slice(out, func(i, j int) bool { return out[i].OpenedAt.After(out[j].OpenedAt) })
	return out
}

// AddRecentFile records an opened remote file, keeping the 30 most recent.
func (s *Store) AddRecentFile(connID, path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec := RecentFile{ConnectionID: connID, Path: path, OpenedAt: time.Now().UTC()}
	kept := make([]RecentFile, 0, len(s.root.RecentFiles)+1)
	kept = append(kept, rec)
	for _, r := range s.root.RecentFiles {
		if r.ConnectionID == connID && r.Path == path {
			continue
		}
		kept = append(kept, r)
	}
	if len(kept) > 30 {
		kept = kept[:30]
	}
	s.root.RecentFiles = kept
	return s.saveLocked()
}

// AddRecent records a visited remote directory, keeping the 50 most recent.
func (s *Store) AddRecent(connID, path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec := RecentLocation{ConnectionID: connID, Path: path, VisitedAt: time.Now().UTC()}
	kept := make([]RecentLocation, 0, len(s.root.Recents)+1)
	kept = append(kept, rec)
	for _, r := range s.root.Recents {
		if r.ConnectionID == connID && r.Path == path {
			continue // dedup: the new entry replaces the old one
		}
		kept = append(kept, r)
	}
	if len(kept) > 50 {
		kept = kept[:50]
	}
	s.root.Recents = kept
	return s.saveLocked()
}

// Recents returns recent locations, newest first.
func (s *Store) Recents() []RecentLocation {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]RecentLocation, len(s.root.Recents))
	copy(out, s.root.Recents)
	sort.Slice(out, func(i, j int) bool { return out[i].VisitedAt.After(out[j].VisitedAt) })
	return out
}
