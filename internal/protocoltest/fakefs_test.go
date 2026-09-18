package protocoltest

import (
	"bytes"
	"context"
	"testing"

	"remorasftp/internal/protocol"
)

// TestFakeFSUploadDownloadRoundTrip pins the exact pipeline the transfer
// engine relies on: Download source -> buffer -> Upload destination ->
// Stat/Read destination. If this fails, the test double itself is broken.
func TestFakeFSUploadDownloadRoundTrip(t *testing.T) {
	ctx := context.Background()
	fs := NewFakeFS()
	fs.Add("/src/a.txt", false, []byte("alpha"))
	fs.Add("/dst", true, nil)

	// Download source into a buffer.
	var buf bytes.Buffer
	if err := fs.Download(ctx, "/src/a.txt", &buf, 0, nil); err != nil {
		t.Fatalf("Download: %v", err)
	}
	if got := buf.String(); got != "alpha" {
		t.Fatalf("Download buffer = %q, want %q", got, "alpha")
	}

	// Upload the buffer to the destination.
	if err := fs.Upload(ctx, "/dst/a.txt", &buf, 0, nil); err != nil {
		t.Fatalf("Upload: %v", err)
	}

	// Stat/Read the destination.
	st, err := fs.Stat(ctx, "/dst/a.txt")
	if err != nil {
		t.Fatalf("Stat destination: %v", err)
	}
	if st.Type != protocol.EntryFile {
		t.Errorf("destination type = %q, want file", st.Type)
	}
	if st.Size != int64(len("alpha")) {
		t.Errorf("destination size = %d, want %d", st.Size, len("alpha"))
	}
	if got := fs.Read("/dst/a.txt"); !bytes.Equal(got, []byte("alpha")) {
		t.Errorf("destination content = %q, want %q", got, "alpha")
	}
	if !fs.Has("/dst/a.txt") {
		t.Error("destination missing after Upload")
	}
}

// TestFakeFSListMkdirRemove covers the structural operations the copy
// engine uses: List of a directory, Mkdir, recursive Remove of a subtree
// must not touch sibling subtrees with similar names.
func TestFakeFSListMkdirRemove(t *testing.T) {
	ctx := context.Background()
	fs := NewFakeFS()
	fs.Add("/src/a.txt", false, []byte("alpha"))
	fs.Add("/src/sub", true, nil)
	fs.Add("/src/sub/b.txt", false, []byte("beta"))
	fs.Add("/dst/src/a.txt", false, []byte("copy"))

	entries, err := fs.List(ctx, "/src")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("List(/src) = %d entries, want 2", len(entries))
	}

	if err := fs.Mkdir(ctx, "/dst/src/sub"); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if !fs.Has("/dst/src/sub") {
		t.Fatal("Mkdir did not create the directory")
	}

	// Recursive remove of /src must not touch /dst/src/...
	if err := fs.Remove(ctx, "/src", true); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if fs.Has("/src") || fs.Has("/src/a.txt") || fs.Has("/src/sub/b.txt") {
		t.Error("source tree still present after recursive Remove")
	}
	if !fs.Has("/dst/src/a.txt") || !fs.Has("/dst/src/sub") {
		t.Error("recursive Remove of /src deleted unrelated /dst/src subtree")
	}
}
