// Stage-trace reproduction for the copy/move pipeline, written for the
// "job completes but destination file does not exist" failure report.
//
// It is a minimal single-file reproduction that verifies, at every stage of
//
//	StartCopy -> EnqueueOperation -> run() -> copyRecursive -> copyOne ->
//	copyFile -> Download -> temp file -> Upload -> destination Stat
//
// the source path, target path, source size, staged file size (as the byte
// count streamed into Upload), the FakeFS destination key, the destination's
// existence immediately after Upload, and the final job status.
//
// If the failure reproduces, the trace output pinpoints the exact stage at
// which the pipeline diverges (FakeFS, copyFile, path calculation, worker,
// or session/client wiring). The assertions themselves are the same invariants
// the regular copy tests check — nothing is weakened.
package transfers

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"remorasftp/internal/config"
	"remorasftp/internal/manager"
	"remorasftp/internal/protocol"
	"remorasftp/internal/protocoltest"
)

// spyClient wraps the shared FakeFS and logs every stage of the pipeline.
// It never alters behavior: reads pass through untouched and are only
// counted.
type spyClient struct {
	*protocoltest.FakeFS
	t *testing.T
}

func (c *spyClient) logf(format string, args ...any) {
	c.t.Logf("trace: "+format, args...)
}

func (c *spyClient) Stat(ctx context.Context, p string) (protocol.Entry, error) {
	e, err := c.FakeFS.Stat(ctx, p)
	c.logf("Stat(%q) -> type=%v size=%d err=%v", p, e.Type, e.Size, err)
	return e, err
}

func (c *spyClient) Download(ctx context.Context, src string, w io.Writer, off int64, prog protocol.ProgressFunc) error {
	st, _ := c.FakeFS.Stat(ctx, src)
	c.logf("Download(%q) start: sourceType=%v sourceSize=%d", src, st.Type, st.Size)
	var n int64
	err := c.FakeFS.Download(ctx, src, &countingWriter{w: w, n: &n}, off, prog)
	c.logf("Download(%q) done: bytes=%d err=%v", src, n, err)
	return err
}

func (c *spyClient) Upload(ctx context.Context, dst string, r io.Reader, off int64, prog protocol.ProgressFunc) error {
	c.logf("Upload(%q) start", dst)
	var n int64
	err := c.FakeFS.Upload(ctx, dst, &countingReader{r: r, n: &n}, off, prog)
	c.logf("Upload(%q) done: bytesRead=%d err=%v", dst, n, err)
	// Destination existence immediately after Upload, under the FakeFS key.
	st, serr := c.FakeFS.Stat(ctx, dst)
	c.logf("Stat(%q) immediately after Upload: exists=%v type=%v size=%d err=%v", dst, serr == nil, st.Type, st.Size, serr)
	return err
}

func (c *spyClient) Mkdir(ctx context.Context, p string) error {
	err := c.FakeFS.Mkdir(ctx, p)
	c.logf("Mkdir(%q) err=%v", p, err)
	return err
}

func (c *spyClient) Remove(ctx context.Context, p string, recursive bool) error {
	err := c.FakeFS.Remove(ctx, p, recursive)
	c.logf("Remove(%q recursive=%v) err=%v", p, recursive, err)
	return err
}

func (c *spyClient) List(ctx context.Context, dir string) ([]protocol.Entry, error) {
	entries, err := c.FakeFS.List(ctx, dir)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name)
	}
	c.logf("List(%q) -> %v err=%v", dir, strings.Join(names, ","), err)
	return entries, err
}

type countingWriter struct {
	w io.Writer
	n *int64
}

func (cw *countingWriter) Write(p []byte) (int, error) {
	n, err := cw.w.Write(p)
	*cw.n += int64(n)
	return n, err
}

type countingReader struct {
	r io.Reader
	n *int64
}

func (cr *countingReader) Read(p []byte) (int, error) {
	n, err := cr.r.Read(p)
	*cr.n += int64(n)
	return n, err
}

// TestTraceCopyFileAndTree reproduces TestCopyFileAndTree with stage tracing
// (single file + nested directory, copy).
func TestTraceCopyFileAndTree(t *testing.T) {
	fs := protocoltest.NewFakeFS()
	fs.Add("/src/a.txt", false, []byte("alpha"))
	fs.Add("/src/sub", true, nil)
	fs.Add("/src/sub/b.txt", false, []byte("beta"))
	fs.Add("/dst", true, nil)

	// Wire the spy client as the session client (the exact same wiring the
	// regular tests use, so the client layer is under the trace too).
	mgr, tm, sess := newTestManager(t, fs)
	// Re-inject the session with the spy client (same wiring, observable
	// client layer).
	testSessionWithClient(t, mgr, sess, &spyClient{FakeFS: fs, t: t})

	job, err := tm.StartCopy(context.Background(), sess, "/src", "/dst/src", false, protocol.PolicyRefuse)
	if err != nil {
		t.Fatal(err)
	}
	snap := waitJob(t, tm, job.ID)
	t.Logf("final job status=%s err=%q total=%d done=%d attempts=%d", snap.Status, snap.Error, snap.Total, snap.Done, snap.Attempts)
	if snap.Status != StatusCompleted {
		t.Fatalf("status = %s, err=%q", snap.Status, snap.Error)
	}
	if got := string(fs.Read("/dst/src/a.txt")); got != "alpha" {
		t.Errorf("file a.txt not copied (content %q)", got)
	}
	if got := string(fs.Read("/dst/src/sub/b.txt")); got != "beta" {
		t.Errorf("nested file b.txt not copied (content %q)", got)
	}
	if !fs.Has("/src/a.txt") {
		t.Error("source removed by copy")
	}
}

// TestTraceMoveTreeRemovesSource reproduces TestMoveTreeRemovesSource with
// stage tracing (move: the source must be removed only after the destination
// write succeeded).
func TestTraceMoveTreeRemovesSource(t *testing.T) {
	fs := protocoltest.NewFakeFS()
	fs.Add("/src/a.txt", false, []byte("alpha"))
	fs.Add("/dst", true, nil)

	mgr, tm, sess := newTestManager(t, fs)
	// Re-inject the session with the spy client (same wiring, observable
	// client layer).
	testSessionWithClient(t, mgr, sess, &spyClient{FakeFS: fs, t: t})

	job, err := tm.StartCopy(context.Background(), sess, "/src", "/dst/src", true, protocol.PolicyRefuse)
	if err != nil {
		t.Fatal(err)
	}
	snap := waitJob(t, tm, job.ID)
	t.Logf("final job status=%s err=%q total=%d done=%d attempts=%d", snap.Status, snap.Error, snap.Total, snap.Done, snap.Attempts)
	if snap.Status != StatusCompleted {
		t.Fatalf("status = %s, err=%q", snap.Status, snap.Error)
	}
	if !fs.Has("/dst/src/a.txt") {
		t.Error("moved file missing at destination")
	}
	if fs.Has("/src/a.txt") {
		t.Error("source file still present after move")
	}
}

// testSessionWithClient re-injects session id with the given protocol
// client, keeping the same metadata newTestManager established. Test support
// for the stage-trace tests.
func testSessionWithClient(t *testing.T, m *manager.Manager, id string, cl protocol.Client) {
	t.Helper()
	m.InjectTestSession(id, manager.TestSession{
		ConnID: "test-conn", ConnName: "Test", Protocol: config.ProtoSFTP,
		Host: "fake", StartDir: "/", Client: cl, ConnectedAt: time.Now(),
	})
}
