package localfs

import (
	"os"
	"path/filepath"
	"testing"

	"remorasftp/internal/protocol"
)

func TestListAndStat(t *testing.T) {
	dir := t.TempDir()
	s := New()

	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, err := s.List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	for _, e := range entries {
		switch e.Name {
		case "sub":
			if e.Type != protocol.EntryDir {
				t.Errorf("sub type = %s", e.Type)
			}
		case "a.txt":
			if e.Type != protocol.EntryFile || e.Size != 5 {
				t.Errorf("a.txt = %+v", e)
			}
		}
	}

	e, err := s.Stat(filepath.Join(dir, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if e.Type != protocol.EntryFile {
		t.Errorf("stat type = %s", e.Type)
	}

	// Invalid paths are rejected.
	if _, err := s.List(""); err == nil {
		t.Error("empty list must fail")
	}
	if _, err := s.Stat("bad\x00name"); err == nil {
		t.Error("NUL path must fail")
	}
}

func TestCopyPolicies(t *testing.T) {
	dir := t.TempDir()
	s := New()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	if err := os.WriteFile(src, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}

	// New destination: copy works.
	if err := s.Copy(src, dst, false, protocol.PolicyRefuse); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "one" {
		t.Error("copy content mismatch")
	}

	// Refuse: destination exists.
	if err := s.Copy(src, dst, false, protocol.PolicyRefuse); err == nil {
		t.Error("refuse must fail when destination exists")
	}

	// Replace.
	if err := os.WriteFile(src, []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Copy(src, dst, false, protocol.PolicyReplace); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "two" {
		t.Error("replace failed")
	}

	// Rename.
	if err := s.Copy(src, dst, false, protocol.PolicyRename); err != nil {
		t.Fatal(err)
	}
	renamed := filepath.Join(dir, "dst (1).txt")
	if _, err := os.Stat(renamed); err != nil {
		t.Errorf("renamed copy missing: %v", err)
	}

	// Move.
	moved := filepath.Join(dir, "moved.txt")
	if err := s.Copy(src, moved, true, protocol.PolicyRefuse); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Error("source still present after move")
	}
	if _, err := os.Stat(moved); err != nil {
		t.Error("moved file missing")
	}
}

func TestCopyFolderIntoItselfRejected(t *testing.T) {
	dir := t.TempDir()
	s := New()
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	inner := filepath.Join(sub, "inner")
	if err := os.Mkdir(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.Copy(sub, inner, false, protocol.PolicyRefuse); err == nil {
		t.Fatal("copying a folder into its own child must be rejected")
	}
}

func TestTreeCopyAndMove(t *testing.T) {
	dir := t.TempDir()
	s := New()
	srcDir := filepath.Join(dir, "tree")
	deep := filepath.Join(srcDir, "a", "b")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deep, "x.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	dstDir := filepath.Join(dir, "target")
	if err := os.Mkdir(dstDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Copy (merge into a pre-existing target dir with the same name).
	if err := s.Copy(srcDir, filepath.Join(dstDir, "tree"), false, protocol.PolicyRefuse); err != nil {
		t.Fatal(err)
	}
	got := filepath.Join(dstDir, "tree", "a", "b", "x.txt")
	if b, _ := os.ReadFile(got); string(b) != "x" {
		t.Fatal("nested file not copied")
	}

	// Move.
	dst2 := filepath.Join(dir, "target2")
	if err := os.Mkdir(dst2, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.Copy(srcDir, filepath.Join(dst2, "tree"), true, protocol.PolicyRefuse); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(srcDir, "a", "b", "x.txt")); !os.IsNotExist(err) {
		t.Error("source tree not removed by move")
	}
}

func TestJoinLocal(t *testing.T) {
	p, err := JoinLocal("/tmp/dir", "file.txt")
	if err != nil {
		t.Fatal(err)
	}
	if p != filepath.Join("/tmp/dir", "file.txt") {
		t.Errorf("join = %q", p)
	}
	if _, err := JoinLocal("/tmp", ".."); err == nil {
		t.Error("dotdot name must be rejected")
	}
	if _, err := JoinLocal("/tmp", "a/b"); err == nil {
		t.Error("nested name must be rejected")
	}
	if _, err := JoinLocal("", "x"); err == nil {
		t.Error("empty dir must fail")
	}
}
