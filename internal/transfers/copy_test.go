package transfers

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"remorasftp/internal/apppaths"
	"remorasftp/internal/config"
	"remorasftp/internal/credentials"
	"remorasftp/internal/events"
	"remorasftp/internal/manager"
	"remorasftp/internal/protocol"
	"remorasftp/internal/protocoltest"
	"remorasftp/internal/trust"
)

// newTestManager wires a manager + transfer manager around an in-memory FS.
func newTestManager(t *testing.T, fs *protocoltest.FakeFS) (*manager.Manager, *Manager, string) {
	t.Helper()
	dir := t.TempDir()
	apppaths.SetDir(dir)
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
	bus := events.NewBus(dir, "off")
	mgr := manager.New(cfg, creds, tr, bus)
	tm := New(bus, mgr, 2)
	mgr.InjectTestSession("sess", manager.TestSession{
		ConnID: "test-conn", ConnName: "Test", Protocol: config.ProtoSFTP,
		Host: "fake", StartDir: "/", Client: fs, ConnectedAt: time.Now(),
	})
	return mgr, tm, "sess"
}

func waitJob(t *testing.T, tm *Manager, id string) JobSnapshot {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		s := tm.Snapshot(id)
		switch s.Status {
		case StatusCompleted, StatusFailed, StatusCanceled:
			return s
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("job did not finish in time")
	return JobSnapshot{}
}

func TestStartCopyGuards(t *testing.T) {
	fs := protocoltest.NewFakeFS()
	_, tm, sess := newTestManager(t, fs)

	cases := []struct {
		from, to string
		wantErr  string
	}{
		{"/", "/x", "root"},
		{"/a", "/a", "inside"},
		{"/a", "/a/b", "inside"},
		{"/a", "/nope/b", "does not exist"},
	}
	for i, c := range cases {
		fs.Add("/a", true, nil)
		if c.to != "/a" && c.to != "/a/b" {
			fs.Add("/a/b", true, nil)
		}
		_, err := tm.StartCopy(context.Background(), sess, c.from, c.to, true, protocol.PolicyRefuse)
		if err == nil {
			t.Errorf("case %d: expected error for %s -> %s", i, c.from, c.to)
			continue
		}
		if c.wantErr != "" && !strings.Contains(err.Error(), c.wantErr) {
			t.Errorf("case %d: error %q does not contain %q", i, err.Error(), c.wantErr)
		}
	}
}

func TestCopyFileAndTree(t *testing.T) {
	fs := protocoltest.NewFakeFS()
	_, tm, sess := newTestManager(t, fs)

	fs.Add("/src/a.txt", false, []byte("alpha"))
	fs.Add("/src/sub", true, nil)
	fs.Add("/src/sub/b.txt", false, []byte("beta"))
	fs.Add("/dst", true, nil)

	job, err := tm.StartCopy(context.Background(), sess, "/src", "/dst/src", false, protocol.PolicyRefuse)
	if err != nil {
		t.Fatal(err)
	}
	snap := waitJob(t, tm, job.ID)
	if snap.Status != StatusCompleted {
		t.Fatalf("status = %s, err=%q", snap.Status, snap.Error)
	}
	// Regression: recursive copy destinations must be under /dst/src -
	// the target variable in the directory branch must not be shadowed,
	// which used to send every child to the filesystem root.
	if string(fs.Read("/dst/src/a.txt")) != "alpha" {
		t.Error("file a.txt not copied to /dst/src/a.txt")
	}
	if string(fs.Read("/dst/src/sub/b.txt")) != "beta" {
		t.Error("nested file b.txt not copied to /dst/src/sub/b.txt")
	}
	for _, stray := range []string{"/a.txt", "/sub", "/b.txt"} {
		if fs.Has(stray) {
			t.Errorf("child copied to filesystem root: %s", stray)
		}
	}
	// Source must survive a copy.
	if !fs.Has("/src/a.txt") {
		t.Error("source removed by copy")
	}
}

func TestMoveTreeRemovesSource(t *testing.T) {
	fs := protocoltest.NewFakeFS()
	_, tm, sess := newTestManager(t, fs)

	fs.Add("/src/a.txt", false, []byte("alpha"))
	fs.Add("/dst", true, nil)

	job, err := tm.StartCopy(context.Background(), sess, "/src", "/dst/src", true, protocol.PolicyRefuse)
	if err != nil {
		t.Fatal(err)
	}
	snap := waitJob(t, tm, job.ID)
	if snap.Status != StatusCompleted {
		t.Fatalf("status = %s, err=%q", snap.Status, snap.Error)
	}
	if !fs.Has("/dst/src/a.txt") {
		t.Error("moved file missing at destination")
	}
	if fs.Has("/a.txt") {
		t.Error("moved child landed at filesystem root instead of /dst/src")
	}
	if fs.Has("/src/a.txt") {
		t.Error("source file still present after move")
	}
}

func TestCopyConflictPolicies(t *testing.T) {
	fs := protocoltest.NewFakeFS()
	_, tm, sess := newTestManager(t, fs)

	// refuse: destination exists → job fails with conflict note.
	fs.Add("/src/a.txt", false, []byte("new"))
	fs.Add("/dst", true, nil)
	fs.Add("/dst/a.txt", false, []byte("old"))
	job, err := tm.StartCopy(context.Background(), sess, "/src/a.txt", "/dst/a.txt", false, protocol.PolicyRefuse)
	if err != nil {
		t.Fatal(err)
	}
	snap := waitJob(t, tm, job.ID)
	if snap.Status == StatusCompleted {
		t.Error("refuse policy must fail on conflict")
	}

	// replace: destination overwritten.
	job, err = tm.StartCopy(context.Background(), sess, "/src/a.txt", "/dst/a.txt", false, protocol.PolicyReplace)
	if err != nil {
		t.Fatal(err)
	}
	snap = waitJob(t, tm, job.ID)
	if snap.Status != StatusCompleted {
		t.Fatalf("replace status = %s, err=%q", snap.Status, snap.Error)
	}
	if string(fs.Read("/dst/a.txt")) != "new" {
		t.Error("replace did not overwrite")
	}

	// skip: an existing destination is left untouched, job completes.
	fs.Add("/src2", true, nil)
	fs.Add("/src2/b.txt", false, []byte("b-new"))
	job, err = tm.StartCopy(context.Background(), sess, "/src2/b.txt", "/dst/a.txt", false, protocol.PolicySkip)
	if err != nil {
		t.Fatal(err)
	}
	snap = waitJob(t, tm, job.ID)
	if snap.Status != StatusCompleted {
		t.Fatalf("skip status = %s, err=%q", snap.Status, snap.Error)
	}
	if string(fs.Read("/dst/a.txt")) != "new" {
		t.Error("skip policy modified the destination")
	}

	// rename: non-colliding name created.
	fs.Add("/src3", true, nil)
	fs.Add("/src3/c.txt", false, []byte("c-new"))
	fs.Add("/dst/c.txt", false, []byte("c-old"))
	job, err = tm.StartCopy(context.Background(), sess, "/src3/c.txt", "/dst/c.txt", false, protocol.PolicyRename)
	if err != nil {
		t.Fatal(err)
	}
	snap = waitJob(t, tm, job.ID)
	if snap.Status != StatusCompleted {
		t.Fatalf("rename status = %s, err=%q", snap.Status, snap.Error)
	}
	if !fs.Has("/dst/c (1).txt") {
		t.Error("renamed copy missing: /dst/c (1).txt")
	}
	if string(fs.Read("/dst/c.txt")) != "c-old" {
		t.Error("original overwritten by rename policy")
	}
}

func TestCopyFolderPolicies(t *testing.T) {
	fs := protocoltest.NewFakeFS()
	_, tm, sess := newTestManager(t, fs)

	// rename: an existing same-named destination folder must NOT be merged
	// into - a sibling "src (1)" is created instead.
	fs.Add("/src", true, nil)
	fs.Add("/src/a.txt", false, []byte("a"))
	fs.Add("/dst", true, nil)
	fs.Add("/dst/src", true, nil)
	fs.Add("/dst/src/keep.txt", false, []byte("keep"))
	job, err := tm.StartCopy(context.Background(), sess, "/src", "/dst/src", false, protocol.PolicyRename)
	if err != nil {
		t.Fatal(err)
	}
	snap := waitJob(t, tm, job.ID)
	if snap.Status != StatusCompleted {
		t.Fatalf("rename folder status = %s, err=%q", snap.Status, snap.Error)
	}
	if !fs.Has("/dst/src (1)/a.txt") {
		t.Error("renamed folder copy missing: /dst/src (1)/a.txt")
	}
	if string(fs.Read("/dst/src/keep.txt")) != "keep" {
		t.Error("rename policy merged into the existing folder")
	}

	// replace: merge into the existing folder (Explorer semantics).
	fs.Add("/src2", true, nil)
	fs.Add("/src2/x.txt", false, []byte("x"))
	job, err = tm.StartCopy(context.Background(), sess, "/src2", "/dst/src", false, protocol.PolicyReplace)
	if err != nil {
		t.Fatal(err)
	}
	snap = waitJob(t, tm, job.ID)
	if snap.Status != StatusCompleted {
		t.Fatalf("replace folder status = %s, err=%q", snap.Status, snap.Error)
	}
	if !fs.Has("/dst/src/x.txt") {
		t.Error("merge did not copy into the existing folder")
	}
	if string(fs.Read("/dst/src/keep.txt")) != "keep" {
		t.Error("merge clobbered unrelated files")
	}

	// refuse: existing destination folder fails the job.
	fs.Add("/src3", true, nil)
	job, err = tm.StartCopy(context.Background(), sess, "/src3", "/dst/src", false, protocol.PolicyRefuse)
	if err != nil {
		t.Fatal(err)
	}
	snap = waitJob(t, tm, job.ID)
	if snap.Status == StatusCompleted {
		t.Error("refuse policy must fail when the destination folder exists")
	}
	if !strings.Contains(snap.Error, "already exists") {
		t.Errorf("expected a conflict note, got %q", snap.Error)
	}
}

// droppingClient is a FakeFS whose Upload pretends to succeed without
// writing anything. It simulates a transport that silently loses data; an
// operation using it must fail loudly, never report success.
type droppingClient struct {
	*protocoltest.FakeFS
}

func (c droppingClient) Upload(_ context.Context, _ string, r io.Reader, _ int64, _ protocol.ProgressFunc) error {
	_, _ = io.ReadAll(r)
	return nil
}

func TestCopyFailsWhenDestinationMissing(t *testing.T) {
	fs := protocoltest.NewFakeFS()
	fs.Add("/src/a.txt", false, []byte("alpha"))
	fs.Add("/dst", true, nil)

	dir := t.TempDir()
	apppaths.SetDir(dir)
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
	bus := events.NewBus(dir, "off")
	mgr := manager.New(cfg, creds, tr, bus)
	tm := New(bus, mgr, 2)
	mgr.InjectTestSession("sess", manager.TestSession{
		ConnID: "test-conn", ConnName: "Test", Protocol: config.ProtoSFTP,
		Host: "fake", StartDir: "/", Client: droppingClient{fs}, ConnectedAt: time.Now(),
	})

	job, err := tm.StartCopy(context.Background(), "sess", "/src/a.txt", "/dst/a.txt", false, protocol.PolicyRefuse)
	if err != nil {
		t.Fatal(err)
	}
	snap := waitJob(t, tm, job.ID)
	if snap.Status != StatusFailed {
		t.Fatalf("silent data loss must fail the job, got status %q", snap.Status)
	}
	if !strings.Contains(snap.Error, "destination missing") {
		t.Errorf("expected a destination-missing error, got %q", snap.Error)
	}
}
