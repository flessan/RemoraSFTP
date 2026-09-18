package transfers

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"remorasftp/internal/apppaths"
	"remorasftp/internal/protocol"
	"remorasftp/internal/safepath"
)

// StartCopy queues a recursive copy (or move) of remote path `from` to
// destination `to` as a first-class transfer job. All data movement goes
// through the protocol client (the single filesystem implementation);
// files are staged in isolated temp artifacts that are always removed.
//
// This orchestration lives in the transfers package (not manager) because
// the job/queue it enqueues onto belong to the transfer subsystem, and
// transfers already depends on manager — the reverse would be an import
// cycle. It reaches the session's protocol client and metadata through the
// engine manager held by the transfer manager.
//
// Guards (invalid operations are rejected up front):
//   - destination equal to or inside the source (folder into itself),
//   - missing destination parent folder.
func (tm *Manager) StartCopy(ctx context.Context, sessionID, from, to string, move bool, policy protocol.CopyPolicy) (*Job, error) {
	if tm.mgr == nil {
		return nil, fmt.Errorf("transfer manager is not attached to the engine")
	}
	from = safepath.Clean(from)
	to = safepath.Clean(to)
	if from == "/" {
		return nil, fmt.Errorf("cannot copy the root directory")
	}
	if safepath.Within(from, to) {
		return nil, fmt.Errorf("destination is inside the source: cannot copy a folder into itself")
	}
	cl, err := tm.mgr.Client(sessionID)
	if err != nil {
		return nil, err
	}
	// The destination parent must exist (or the destination is the root).
	parent := safepath.Dir(to)
	if parent != to {
		st, err := cl.Stat(ctx, parent)
		if err != nil {
			return nil, fmt.Errorf("destination folder does not exist: %s", parent)
		}
		if st.Type != protocol.EntryDir {
			return nil, fmt.Errorf("destination parent is not a folder: %s", parent)
		}
	}

	sess, err := tm.mgr.Sessions0(sessionID)
	if err != nil {
		return nil, err
	}

	direction := Copy
	if move {
		direction = Move
	}
	job := &Job{
		Direction:  direction,
		SessionID:  sessionID,
		ConnName:   sess.ConnName,
		RemotePath: to,
		From:       from,
		Name:       safepath.Base(from),
		Caps:       cl.Capabilities(),
	}
	// Pre-compute the total when it is a single file (2x size: the file
	// travels remote→temp and temp→remote). Trees report indeterminate
	// progress (Total stays 0).
	st, statErr := cl.Stat(ctx, from)
	if statErr == nil && st.Type != protocol.EntryDir {
		if st.Size > 0 {
			job.Total = st.Size * 2
		}
	}
	j := tm.EnqueueOperation(job, func(opCtx context.Context, prog protocol.ProgressFunc) error {
		return tm.copyRecursive(opCtx, cl, from, to, move, policy, prog)
	})
	return j, nil
}

// copyStats accumulates per-item errors without aborting a bulk operation.
type copyStats struct {
	items   int
	skipped int
	errs    []string
}

func (s *copyStats) addErr(msg string) {
	if len(s.errs) < 20 {
		s.errs = append(s.errs, msg)
	}
}

// opProgress makes per-file progress reports (which restart at zero for
// each file) monotonic across the whole operation.
type opProgress struct {
	prog protocol.ProgressFunc
	base int64
}

func (o *opProgress) report(n int64) {
	if o.prog != nil {
		o.prog(o.base + n)
	}
}

func (o *opProgress) advance(b int64) { o.base += b }

// copyRecursive copies the tree. Symlinked entries are never followed (and
// are skipped in tree copies). Returns a context error when canceled.
//
// A completed operation is verified against the destination before being
// reported successful: the destination must exist (a skipped-only operation
// is the sole exception). This makes "finished but nothing landed" a loud
// failure instead of a silent success.
func (tm *Manager) copyRecursive(ctx context.Context, cl protocol.Client, from, to string, move bool, policy protocol.CopyPolicy, prog protocol.ProgressFunc) error {
	stats := &copyStats{}
	op := &opProgress{prog: prog}
	if err := tm.copyOne(ctx, cl, from, to, move, policy, op, stats); err != nil {
		return err
	}
	if len(stats.errs) > 0 {
		return fmt.Errorf("%d issue(s): %s", len(stats.errs), strings.Join(stats.errs, "; "))
	}
	// Verify the destination actually exists before reporting success
	// (unless every item was explicitly skipped or renamed elsewhere).
	if stats.items > 0 {
		if _, err := cl.Stat(ctx, to); err != nil {
			return fmt.Errorf("destination missing after operation: %s (%v)", to, err)
		}
	}
	return nil
}

func (tm *Manager) copyOne(ctx context.Context, cl protocol.Client, from, to string, move bool, policy protocol.CopyPolicy, op *opProgress, st *copyStats) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	src, err := cl.Stat(ctx, from)
	if err != nil {
		st.addErr("stat " + from + ": " + err.Error())
		return nil
	}

	if src.Type == protocol.EntrySymlink || src.IsSymlink {
		// Never follow symlinks during copy; a lone symlink entry is
		// skipped with a note (re-creating remote symlinks is not
		// supported by the protocol abstraction).
		st.addErr("skipped symlink " + from)
		st.skipped++
		return nil
	}

	if src.Type != protocol.EntryDir {
		// ---- file -----------------------------------------------------
		// A directory occupying the destination is a conflict.
		if dst, err := cl.Stat(ctx, to); err == nil && dst.Type == protocol.EntryDir {
			if policy == protocol.PolicySkip {
				st.skipped++
				return nil
			}
			st.addErr("destination is a folder: " + to)
			return nil
		}
		target, _, skip, rerr := tm.resolveDest(ctx, cl, to, policy)
		if rerr != nil {
			st.addErr(rerr.Error())
			return nil
		}
		if skip {
			st.skipped++
			return nil
		}
		if err := tm.copyFile(ctx, cl, from, target, src.Size, op); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			st.addErr("copy " + from + ": " + err.Error())
			return nil
		}
		if move {
			if err := cl.Remove(ctx, from, false); err != nil {
				st.addErr("remove " + from + ": " + err.Error())
			}
		}
		st.items++
		return nil
	}

	// ---- directory ----------------------------------------------------
	dst, dstErr := cl.Stat(ctx, to)
	var target string
	switch {
	case dstErr != nil:
		// Assignment (not ':='): a short declaration here would create a
		// new inner `target` that shadows the outer one, leaving the
		// outer value empty and copying every child to the filesystem
		// root. rerr stays scoped to this case.
		var rerr error
		target, _, _, rerr = tm.resolveDest(ctx, cl, to, policy)
		if rerr != nil {
			st.addErr(rerr.Error())
			return nil
		}
		if err := cl.Mkdir(ctx, target); err != nil {
			st.addErr("mkdir " + target + ": " + err.Error())
			return nil
		}
	case dst.Type == protocol.EntryDir:
		// The destination folder already exists.
		switch policy {
		case protocol.PolicySkip:
			st.skipped++
			return nil
		case protocol.PolicyRefuse:
			st.addErr("destination folder already exists: " + to)
			return nil
		case protocol.PolicyRename:
			t := tm.uniqueDest(ctx, cl, to)
			if err := cl.Mkdir(ctx, t); err != nil {
				st.addErr("mkdir " + t + ": " + err.Error())
				return nil
			}
			target = t
		default:
			// Replace (and the plain copy default): merge into the
			// existing directory, overwriting same-named files.
			target = to
		}
	default:
		if policy == protocol.PolicySkip {
			st.skipped++
			return nil
		}
		if policy == protocol.PolicyRename {
			t := tm.uniqueDest(ctx, cl, to)
			if err := cl.Mkdir(ctx, t); err != nil {
				st.addErr("mkdir " + t + ": " + err.Error())
				return nil
			}
			target = t
		} else {
			st.addErr("cannot replace a file with a folder: " + to)
			return nil
		}
	}

	entries, err := cl.List(ctx, from)
	if err != nil {
		st.addErr("list " + from + ": " + err.Error())
		return nil
	}
	for _, e := range entries {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		childFrom := safepath.Join(from, e.Name)
		childTo := safepath.Join(target, e.Name)
		if e.Type == protocol.EntryDir && !e.IsSymlink {
			// Recurse with merge semantics (childTo inside existing
			// directory merges naturally).
			if err := tm.copyOne(ctx, cl, childFrom, childTo, move, policy, op, st); err != nil {
				return err
			}
		} else if e.Type == protocol.EntrySymlink || e.IsSymlink {
			st.skipped++
		} else {
			if err := tm.copyOne(ctx, cl, childFrom, childTo, move, policy, op, st); err != nil {
				return err
			}
		}
	}

	if move {
		// Best-effort cleanup of the (now empty) source tree.
		if err := cl.Remove(ctx, from, true); err != nil {
			st.addErr("remove " + from + ": " + err.Error())
		}
	}
	return nil
}

// copyFile streams one file remote → isolated temp file → remote. Progress
// is reported through op (download phase 0..size, upload phase size..2size,
// offset so the global total keeps advancing).
func (tm *Manager) copyFile(ctx context.Context, cl protocol.Client, from, to string, size int64, op *opProgress) error {
	tmp, err := apppaths.TempDir()
	if err != nil {
		return err
	}
	jobDir, err := os.MkdirTemp(tmp, "copy-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(jobDir)
	part := filepath.Join(jobDir, safepath.Base(from)+".part")

	f, err := os.Create(part)
	if err != nil {
		return err
	}
	derr := cl.Download(ctx, from, f, 0, op.report)
	if cerr := f.Close(); derr == nil {
		derr = cerr
	}
	if derr != nil {
		return derr
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	// A non-empty source must produce a non-empty staged file; anything
	// else means the download side lost data silently.
	if size > 0 {
		if info, serr := os.Stat(part); serr != nil || info.Size() == 0 {
			return fmt.Errorf("staged file is empty after download of non-empty source: %s", from)
		}
	}
	op.advance(size)

	f, err = os.Open(part)
	if err != nil {
		return err
	}
	up := &opProgress{prog: op.report}
	err = cl.Upload(ctx, to, &offsetReader{r: f, report: up.report}, 0, nil)
	// Close explicitly (and account for its error) so the handle is
	// definitely released before jobDir cleanup on every platform.
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	op.advance(size)
	return err
}

// offsetReader wraps a reader and reports cumulative bytes read within the
// file (the enclosing opProgress adds the global offset).
type offsetReader struct {
	r      io.Reader
	report func(int64)
	n      int64
}

func (o *offsetReader) Read(p []byte) (int, error) {
	n, err := o.r.Read(p)
	if n > 0 {
		o.n += int64(n)
		o.report(o.n)
	}
	return n, err
}

// resolveDest decides what to do when `to` already exists.
func (tm *Manager) resolveDest(ctx context.Context, cl protocol.Client, to string, policy protocol.CopyPolicy) (target string, action string, skip bool, err error) {
	if _, err := cl.Stat(ctx, to); err != nil {
		return to, "new", false, nil
	}
	switch policy {
	case protocol.PolicyRefuse:
		return "", "", false, fmt.Errorf("destination already exists: %s", to)
	case protocol.PolicyReplace:
		return to, "replace", false, nil
	case protocol.PolicySkip:
		return "", "", true, nil
	case protocol.PolicyRename:
		return tm.uniqueDest(ctx, cl, to), "rename", false, nil
	}
	return to, "new", false, nil
}

// uniqueDest returns a non-colliding path: "name (1).ext", "name (2).ext", …
func (tm *Manager) uniqueDest(ctx context.Context, cl protocol.Client, to string) string {
	dir, base := safepath.Split(to)
	ext := path.Ext(base)
	name := strings.TrimSuffix(base, ext)
	for i := 1; ; i++ {
		cand := safepath.Join(dir, fmt.Sprintf("%s (%d)%s", name, i, ext))
		if _, err := cl.Stat(ctx, cand); err != nil {
			return cand
		}
	}
}
