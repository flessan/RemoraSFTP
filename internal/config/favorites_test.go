package config

import (
	"testing"

	"remorasftp/internal/apppaths"
)

// newTestStore loads a store rooted at a fresh temp directory. The apppaths
// package caches its resolved dir process-wide, so callers re-set it when
// they need to reload from the same directory.
func newTestStore(t *testing.T) (s *Store, dir string) {
	t.Helper()
	dir = t.TempDir()
	apppaths.SetDir(dir)
	s, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	return s, dir
}

func TestFavorites(t *testing.T) {
	s, dir := newTestStore(t)

	f1 := Favorite{ID: "f1", ConnectionID: "c1", Path: "/home/user"}
	if err := s.AddFavorite(f1); err != nil {
		t.Fatal(err)
	}
	f2 := Favorite{ID: "f2", ConnectionID: "c1", Path: "/var/www"}
	if err := s.AddFavorite(f2); err != nil {
		t.Fatal(err)
	}
	if got := len(s.Favorites()); got != 2 {
		t.Fatalf("favorites = %d, want 2", got)
	}

	// Same (connection, path) refreshes instead of duplicating.
	f1b := Favorite{ID: "f3", ConnectionID: "c1", Path: "/home/user", Label: "Home"}
	if err := s.AddFavorite(f1b); err != nil {
		t.Fatal(err)
	}
	got := s.Favorites()
	if len(got) != 2 {
		t.Fatalf("favorites after dedup = %d, want 2", len(got))
	}
	if got[0].Label != "Home" && got[1].Label != "Home" {
		t.Error("refreshed label missing")
	}

	// Reload from disk.
	apppaths.SetDir(dir)
	s2, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := len(s2.Favorites()); got != 2 {
		t.Fatalf("reloaded favorites = %d, want 2", got)
	}

	if err := s2.RemoveFavorite("f2"); err != nil {
		t.Fatal(err)
	}
	if got := len(s2.Favorites()); got != 1 {
		t.Fatalf("after remove = %d, want 1", got)
	}
	if err := s2.RemoveFavorite("missing"); err == nil {
		t.Error("removing a missing favorite must fail")
	}
}

func TestRecentFiles(t *testing.T) {
	s, _ := newTestStore(t)
	_ = s.AddRecentFile("c1", "/a.txt")
	_ = s.AddRecentFile("c1", "/b.txt")
	_ = s.AddRecentFile("c1", "/a.txt") // dedup
	r := s.RecentFiles()
	if len(r) != 2 {
		t.Fatalf("recent files = %d, want 2", len(r))
	}
	if r[0].Path != "/a.txt" {
		t.Fatalf("most recent = %s, want /a.txt", r[0].Path)
	}
}

func TestStartupModeDefault(t *testing.T) {
	s, _ := newTestStore(t)
	if got := s.Settings().StartupMode; got != "ask" {
		t.Fatalf("startupMode = %q, want ask", got)
	}
}
