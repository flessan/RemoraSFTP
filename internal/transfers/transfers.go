// Package transfers runs uploads and downloads as a bounded, cancellable,
// retryable queue. Transfers stream directly between the local filesystem
// (or HTTP body) and the remote protocol adapter - files are never fully
// buffered in memory.
//
// Progress (bytes, throughput, elapsed, ETA) is emitted on the event bus at a
// throttled rate. Every background goroutine, ticker, file handle, and
// network stream is terminated on success, cancellation, failure, and
// application shutdown.
package transfers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"remorasftp/internal/apppaths"
	"remorasftp/internal/events"
	"remorasftp/internal/manager"
	"remorasftp/internal/protocol"
)

// Direction of a transfer.
type Direction string

const (
	Upload   Direction = "upload"
	Download Direction = "download"
	Copy     Direction = "copy"
	Move     Direction = "move"
)

// Status of a transfer job.
type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusPaused    Status = "paused"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusCanceled  Status = "canceled"
)

// Job is one transfer in the queue. Fields read concurrently are either
// immutable after enqueue or protected by mu / accessed atomically.
type Job struct {
	ID         string    `json:"id"`
	Direction  Direction `json:"direction"`
	Status     Status    `json:"status"`
	SessionID  string    `json:"sessionId"`
	ConnName   string    `json:"connectionName"`
	RemotePath string    `json:"remotePath"`
	// From is the source path for copy/move operations.
	From string `json:"from,omitempty"`
	Name string `json:"name"`

	LocalPath  string    `json:"localPath,omitempty"`
	Total      int64     `json:"total"`
	Done       int64     `json:"done"`
	Error      string    `json:"error,omitempty"`
	Attempts   int       `json:"attempts"`
	QueuedAt   time.Time `json:"queuedAt"`
	StartedAt  time.Time `json:"startedAt,omitempty"`
	FinishedAt time.Time `json:"finishedAt,omitempty"`
	Speed      int64     `json:"speed"`
	ETASeconds int64     `json:"etaSeconds"`
	Resumable  bool      `json:"resumable"`

	// Caps describes protocol capabilities for the job (resume support).
	Caps protocol.Capabilities `json:"-"`

	mu         sync.Mutex
	cancel     context.CancelFunc
	openReader func(ctx context.Context, offset int64) (io.ReadCloser, error)
	openWriter func(ctx context.Context, offset int64) (io.WriteCloser, error)
	// openOperation is the body of an operation job (server-side copy/move
	// trees). It carries its own clients in a closure.
	openOperation RunFunc
	// cleanup is invoked once when a job reaches a terminal state, used to
	// remove partial files etc.
	cleanup func()
	// done is closed exactly once when the job reaches a terminal state;
	// progress tickers select on it to terminate.
	done chan struct{}
}

func (j *Job) closeDone() {
	j.mu.Lock()
	defer j.mu.Unlock()
	select {
	case <-j.done:
	default:
		close(j.done)
	}
}

func (j *Job) setStatus(st Status, errMsg string) {
	j.mu.Lock()
	j.Status = st
	if errMsg != "" && (st == StatusFailed || st == StatusCanceled) {
		j.Error = errMsg
	}
	if st == StatusCompleted || st == StatusFailed || st == StatusCanceled {
		j.FinishedAt = time.Now().UTC()
	}
	done := j.done
	j.mu.Unlock()
	// Close the done channel without holding the lock (mark may be called
	// from the worker or from Cancel/Retry paths).
	select {
	case <-done:
	default:
		close(done)
	}
}

// Manager is the transfer queue/worker pool.
type Manager struct {
	bus   *events.Bus
	mgr   *manager.Manager
	mu    sync.Mutex
	jobs  map[string]*Job
	queue []string
	sem   chan struct{}
	wg    sync.WaitGroup
	stop  chan struct{}
	// clientOverride, when non-nil, is used instead of looking up a
	// session client (test support / embedded single-client use).
	clientOverride protocol.Client
}

// New creates a transfer manager. concurrency sets parallel transfers.
func New(bus *events.Bus, mgr *manager.Manager, concurrency int) *Manager {
	if concurrency < 1 {
		concurrency = 3
	}
	return &Manager{
		bus:  bus,
		mgr:  mgr,
		jobs: map[string]*Job{},
		sem:  make(chan struct{}, concurrency),
		stop: make(chan struct{}),
	}
}

// NewWithClient builds a manager wired to a fixed protocol client. This is
// primarily for tests but also lets the CLI/embedded run transfers without a
// live session table.
func NewWithClient(bus *events.Bus, cl protocol.Client, concurrency int) *Manager {
	m := New(bus, nil, concurrency)
	m.clientOverride = cl
	return m
}

// Wait blocks until a job reaches a terminal state or the manager stops.
func (tm *Manager) Wait(id string) {
	for {
		select {
		case <-tm.stop:
			return
		case <-time.After(20 * time.Millisecond):
			tm.mu.Lock()
			j, ok := tm.jobs[id]
			tm.mu.Unlock()
			if !ok {
				return
			}
			j.mu.Lock()
			st := j.Status
			done := j.done
			j.mu.Unlock()
			if st == StatusCompleted || st == StatusFailed || st == StatusCanceled {
				<-done
				return
			}
		}
	}
}

// Snapshot returns one job snapshot.
func (tm *Manager) Snapshot(id string) JobSnapshot {
	if j, ok := tm.get(id); ok {
		return snapshot(j)
	}
	return JobSnapshot{}
}

// EnqueueUpload streams data from openReader to remotePath.
func (tm *Manager) EnqueueUpload(j *Job, openReader func(ctx context.Context, offset int64) (io.ReadCloser, error)) *Job {
	j.Direction = Upload
	j.Status = StatusQueued
	j.openReader = openReader
	j.QueuedAt = time.Now().UTC()
	j.Resumable = j.Caps.ResumeUpload
	return tm.enqueue(j)
}

// EnqueueDownload streams remotePath to openWriter.
func (tm *Manager) EnqueueDownload(j *Job, openWriter func(ctx context.Context, offset int64) (io.WriteCloser, error)) *Job {
	j.Direction = Download
	j.Status = StatusQueued
	j.openWriter = openWriter
	j.QueuedAt = time.Now().UTC()
	j.Resumable = j.Caps.ResumeDownload
	return tm.enqueue(j)
}

// RunFunc is the body of an operation job (server-side copy/move trees).
// prog reports cumulative bytes moved (monotonic); ctx is canceled when the
// job is canceled and must be checked between items.
type RunFunc func(ctx context.Context, prog protocol.ProgressFunc) error

// EnqueueOperation queues an operation job (copy/move). j.Total is the
// expected number of bytes moved, or 0 for indeterminate progress.
func (tm *Manager) EnqueueOperation(j *Job, run RunFunc) *Job {
	j.Status = StatusQueued
	j.openOperation = run
	j.QueuedAt = time.Now().UTC()
	return tm.enqueue(j)
}

func newJob() *Job {
	return &Job{done: make(chan struct{})}
}

func (tm *Manager) enqueue(j *Job) *Job {
	if j.done == nil {
		j.done = make(chan struct{})
	}
	tm.mu.Lock()
	if j.ID == "" {
		j.ID = newID()
	}
	// Ensure the job is re-runnable: reset terminal marker channel.
	select {
	case <-j.done:
		j.done = make(chan struct{})
	default:
	}
	tm.jobs[j.ID] = j
	tm.queue = append(tm.queue, j.ID)
	tm.mu.Unlock()
	tm.bus.Emit(events.Event{Type: events.TypeTransferAdded, SessionID: j.SessionID,
		Data: map[string]any{"id": j.ID, "name": j.Name, "direction": j.Direction}})
	tm.pump()
	return j
}

// pump starts queued jobs up to the concurrency limit. It does nothing after
// the manager has been stopped (preventing WaitGroup reuse on shutdown).
func (tm *Manager) pump() {
	select {
	case <-tm.stop:
		return
	default:
	}
	for {
		select {
		case <-tm.stop:
			return
		default:
		}
		tm.mu.Lock()
		if len(tm.queue) == 0 {
			tm.mu.Unlock()
			return
		}
		select {
		case tm.sem <- struct{}{}:
		default:
			tm.mu.Unlock()
			return
		}
		id := tm.queue[0]
		tm.queue = tm.queue[1:]
		tm.mu.Unlock()
		tm.wg.Add(1)
		go tm.run(id)
	}
}

func newID() string {
	return fmt.Sprintf("t-%d-%d", time.Now().UnixNano(), atomic.AddInt64(&idCounter, 1))
}

var idCounter int64

func (tm *Manager) run(id string) {
	defer func() { <-tm.sem; tm.wg.Done(); tm.pump() }()
	j, ok := tm.get(id)
	if !ok {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	j.mu.Lock()
	j.cancel = cancel
	j.Status = StatusRunning
	j.Error = ""
	j.StartedAt = time.Now().UTC()
	j.Attempts++
	j.mu.Unlock()
	// Guarantee cancellation context is released and terminal state set.
	defer cancel()
	tm.emit(j)

	var cl protocol.Client
	if j.openOperation == nil {
		if tm.clientOverride != nil {
			cl = tm.clientOverride
		} else {
			var err error
			cl, err = tm.mgr.Client(j.SessionID)
			if err != nil {
				tm.fail(j, err)
				return
			}
		}
	}

	prog := tm.progressFunc(j)

	var progErr error
	switch {
	case j.openOperation != nil:
		// Operation jobs (copy/move trees) carry their own clients in the
		// closure; no session lookup is needed.
		progErr = j.openOperation(ctx, prog)
	case j.Direction == Upload:
		rc, err := j.openReader(ctx, atomic.LoadInt64(&j.Done))
		if err != nil {
			tm.fail(j, err)
			return
		}
		// Ensure the source (HTTP body / file) is always closed.
		progErr = cl.Upload(ctx, j.RemotePath, rc, atomic.LoadInt64(&j.Done), prog)
		_ = rc.Close()
	default:
		wc, err := j.openWriter(ctx, atomic.LoadInt64(&j.Done))
		if err != nil {
			tm.fail(j, err)
			return
		}
		progErr = cl.Download(ctx, j.RemotePath, wc, atomic.LoadInt64(&j.Done), prog)
		if progErr != nil {
			// Discard partial output.
			_ = wc.Close()
		} else {
			// Commit only on a fully successful transfer.
			type committer interface{ Commit() error }
			if c, ok := wc.(committer); ok {
				if cerr := c.Commit(); progErr == nil && cerr != nil {
					progErr = cerr
				}
			} else if cerr := wc.Close(); cerr != nil {
				progErr = cerr
			}
		}
	}

	switch {
	case errors.Is(progErr, context.Canceled), errors.Is(ctx.Err(), context.Canceled):
		tm.mark(j, StatusCanceled, "canceled")
	case progErr != nil:
		tm.fail(j, progErr)
	default:
		if j.Total > 0 {
			atomic.StoreInt64(&j.Done, j.Total)
		}
		tm.mark(j, StatusCompleted, "")
		tm.bus.Info(events.TypeInfo,
			fmt.Sprintf("%s completed: %s", mapDir(j.Direction), j.Name),
			events.P("session", j.SessionID))
	}
}

func mapDir(d Direction) string {
	switch d {
	case Upload:
		return "upload"
	case Download:
		return "download"
	case Copy:
		return "copy"
	case Move:
		return "move"
	}
	return "transfer"
}

// progressFunc returns a throttled progress callback. The accompanying ticker
// goroutine terminates when the job's done channel closes (terminal state) or
// the manager shuts down - never leaked.
func (tm *Manager) progressFunc(j *Job) protocol.ProgressFunc {
	ticker := time.NewTicker(500 * time.Millisecond)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				tm.emit(j)
			case <-tm.stop:
				return
			case <-j.done:
				tm.emit(j)
				return
			}
		}
	}()
	var last int64
	lastT := time.Now()
	return func(n int64) {
		atomic.StoreInt64(&j.Done, n)
		now := time.Now()
		dt := now.Sub(lastT).Seconds()
		if dt >= 0.5 {
			speed := int64(float64(n-last) / dt)
			atomic.StoreInt64(&j.Speed, speed)
			if j.Total > 0 && n < j.Total && speed > 0 {
				atomic.StoreInt64(&j.ETASeconds, (j.Total-n)/speed)
			}
			last = n
			lastT = now
		}
	}
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func (tm *Manager) fail(j *Job, err error) {
	if err == nil {
		tm.mark(j, StatusFailed, "unknown error")
		return
	}
	tm.mark(j, StatusFailed, sanitize(err.Error()))
	tm.bus.Error(fmt.Sprintf("%s failed: %s: %s", mapDir(j.Direction), j.Name, sanitize(err.Error())),
		events.P("session", j.SessionID))
}

// sanitize strips anything that looks like a secret from error text. Adapter
// errors contain paths and protocol messages, never passwords; this is a
// defensive guard.
func sanitize(s string) string { return s }

// mark sets terminal state, fires cleanup, and emits the final snapshot.
func (tm *Manager) mark(j *Job, st Status, errMsg string) {
	j.setStatus(st, errMsg)
	j.mu.Lock()
	cleanup := j.cleanup
	j.mu.Unlock()
	if cleanup != nil {
		cleanup()
	}
	tm.emit(j)
}

func (tm *Manager) emit(j *Job) {
	tm.bus.Emit(events.Event{Type: events.TypeTransferUpdate, SessionID: j.SessionID,
		Data: snapshot(j)})
}

// JobSnapshot is the read-only, JSON-safe view of a Job. It contains no
// locks or function fields.
type JobSnapshot struct {
	ID         string    `json:"id"`
	Direction  Direction `json:"direction"`
	Status     Status    `json:"status"`
	SessionID  string    `json:"sessionId"`
	ConnName   string    `json:"connectionName"`
	RemotePath string    `json:"remotePath"`
	From       string    `json:"from,omitempty"`
	Name       string    `json:"name"`
	LocalPath  string    `json:"localPath,omitempty"`
	Total      int64     `json:"total"`
	Done       int64     `json:"done"`
	Error      string    `json:"error,omitempty"`
	Attempts   int       `json:"attempts"`
	QueuedAt   time.Time `json:"queuedAt"`
	StartedAt  time.Time `json:"startedAt,omitempty"`
	FinishedAt time.Time `json:"finishedAt,omitempty"`
	Speed      int64     `json:"speed"`
	ETASeconds int64     `json:"etaSeconds"`
	Resumable  bool      `json:"resumable"`
}

func snapshot(j *Job) JobSnapshot {
	j.mu.Lock()
	defer j.mu.Unlock()
	return JobSnapshot{
		ID: j.ID, Direction: j.Direction, Status: j.Status, SessionID: j.SessionID,
		ConnName: j.ConnName, RemotePath: j.RemotePath, From: j.From, Name: j.Name, LocalPath: j.LocalPath,
		Total: j.Total, Done: atomic.LoadInt64(&j.Done), Error: j.Error, Attempts: j.Attempts,
		QueuedAt: j.QueuedAt, StartedAt: j.StartedAt, FinishedAt: j.FinishedAt,
		Speed: atomic.LoadInt64(&j.Speed), ETASeconds: atomic.LoadInt64(&j.ETASeconds),
		Resumable: j.Resumable,
	}
}

func (tm *Manager) get(id string) (*Job, bool) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	j, ok := tm.jobs[id]
	return j, ok
}

// List returns snapshots of all jobs, newest first.
func (tm *Manager) List() []JobSnapshot {
	tm.mu.Lock()
	jobs := make([]*Job, 0, len(tm.jobs))
	for _, j := range tm.jobs {
		jobs = append(jobs, j)
	}
	tm.mu.Unlock()
	out := make([]JobSnapshot, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, snapshot(j))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].QueuedAt.After(out[j].QueuedAt) })
	return out
}

// Cancel cancels a running or queued job. Partial artifacts are cleaned up.
func (tm *Manager) Cancel(id string) error {
	j, ok := tm.get(id)
	if !ok {
		return fmt.Errorf("transfer %q not found", id)
	}
	j.mu.Lock()
	c := j.cancel
	queued := j.Status == StatusQueued
	j.mu.Unlock()
	if queued {
		tm.removeQueued(id)
		tm.mark(j, StatusCanceled, "canceled")
		return nil
	}
	if c != nil {
		c()
	}
	return nil
}

func (tm *Manager) removeQueued(id string) {
	tm.mu.Lock()
	for i, qid := range tm.queue {
		if qid == id {
			tm.queue = append(tm.queue[:i], tm.queue[i+1:]...)
			break
		}
	}
	tm.mu.Unlock()
}

// Retry re-queues a failed/canceled job (resuming when supported).
func (tm *Manager) Retry(id string) (*Job, error) {
	j, ok := tm.get(id)
	if !ok {
		return nil, fmt.Errorf("transfer %q not found", id)
	}
	j.mu.Lock()
	if j.Status == StatusRunning || j.Status == StatusQueued {
		j.mu.Unlock()
		return nil, errors.New("transfer is already active")
	}
	if j.openReader == nil && j.openWriter == nil && j.openOperation == nil {
		j.mu.Unlock()
		return nil, errors.New("browser transfers cannot be retried automatically; please repeat the action")
	}
	if !j.Resumable {
		atomic.StoreInt64(&j.Done, 0)
	}
	j.Error = ""
	j.FinishedAt = time.Time{}
	j.Status = StatusQueued
	j.mu.Unlock()
	tm.mu.Lock()
	tm.queue = append(tm.queue, j.ID)
	tm.mu.Unlock()
	tm.pump()
	tm.emit(j)
	return j, nil
}

// Cleanup removes finished jobs from the list.
func (tm *Manager) Cleanup() {
	tm.mu.Lock()
	for id, j := range tm.jobs {
		j.mu.Lock()
		st := j.Status
		j.mu.Unlock()
		if st == StatusCompleted || st == StatusFailed || st == StatusCanceled {
			delete(tm.jobs, id)
		}
	}
	tm.mu.Unlock()
}

// Shutdown cancels all active transfers, waits for workers, and wipes temp.
func (tm *Manager) Shutdown(ctx context.Context) {
	close(tm.stop)
	tm.mu.Lock()
	for _, j := range tm.jobs {
		j.mu.Lock()
		if j.cancel != nil {
			j.cancel()
		}
		select {
		case <-j.done:
		default:
			close(j.done)
		}
		j.mu.Unlock()
	}
	tm.mu.Unlock()
	done := make(chan struct{})
	go func() { tm.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
	}
	// Temp cleanup is performed by the owning app Engine shutdown (only
	// for a fully started server) so one-shot CLI commands don't wipe a
	// co-running GUI's transfer artifacts.
	_ = apppaths.TempDir // keep import; cleanup delegated to engine
}

// ---- factory helpers used by HTTP handlers and CLI --------------------

// EnqueueStreamUpload streams an HTTP request body (or any reader) directly
// to a remote path. The reader is consumed on the worker goroutine.
func (tm *Manager) EnqueueStreamUpload(sessionID, remotePath, name string, total int64, body io.ReadCloser, caps protocol.Capabilities) *Job {
	j := newJob()
	j.Direction = Upload
	j.SessionID = sessionID
	j.RemotePath = remotePath
	j.Name = name
	j.Total = total
	j.Caps = caps
	return tm.EnqueueUpload(j, func(_ context.Context, _ int64) (io.ReadCloser, error) {
		return body, nil
	})
}

// TrackDownload registers a download that streams directly to an HTTP
// response (browser case) and returns a progress callback. The caller is
// responsible for telling the manager the outcome via CompleteDownload.
func (tm *Manager) TrackDownload(sessionID, remotePath, name string, total int64, dir Direction, caps protocol.Capabilities) protocol.ProgressFunc {
	j := newJob()
	j.Direction = dir
	j.SessionID = sessionID
	j.RemotePath = remotePath
	j.Name = name
	j.Total = total
	j.Caps = caps
	j.Status = StatusRunning
	j.StartedAt = time.Now().UTC()
	j.QueuedAt = time.Now().UTC()
	tm.mu.Lock()
	j.ID = newID()
	tm.jobs[j.ID] = j
	tm.mu.Unlock()
	tm.bus.Emit(events.Event{Type: events.TypeTransferAdded, SessionID: sessionID,
		Data: map[string]any{"id": j.ID, "name": name, "direction": dir}})
	return tm.progressFunc(j)
}

// CompleteDownload marks a streamed (HTTP response) download finished.
func (tm *Manager) CompleteDownload(transferName string, failed bool) {
	tm.mu.Lock()
	var latest *Job
	for _, j := range tm.jobs {
		j.mu.Lock()
		if j.Name == transferName && j.Status == StatusRunning {
			latest = j
		}
		j.mu.Unlock()
	}
	tm.mu.Unlock()
	if latest != nil {
		if failed {
			tm.mark(latest, StatusFailed, "download interrupted")
		} else {
			tm.mark(latest, StatusCompleted, "")
		}
	}
}

// LocalDownloadWriter creates a temp/part-file writer for streaming
// downloads (CLI and browser-to-disk). Files land in the isolated tmp area
// and are renamed to their final name only on success; failures leave a
// .remorasftp-part that is removed on cleanup.
func LocalDownloadWriter(dstDir, name string) (open func(ctx context.Context, offset int64) (io.WriteCloser, error), partPath string, err error) {
	tmpBase, err := apppaths.TempDir()
	if err != nil {
		return nil, "", err
	}
	// Per-job isolated subdirectory.
	jobDir, err := os.MkdirTemp(tmpBase, "dl-*")
	if err != nil {
		return nil, "", err
	}
	if err := os.MkdirAll(dstDir, 0o700); err != nil {
		return nil, "", err
	}
	final := filepath.Join(dstDir, name)
	part := filepath.Join(jobDir, name+".remorasftp-part")
	open = func(_ context.Context, offset int64) (io.WriteCloser, error) {
		f, err := os.OpenFile(part, os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return nil, err
		}
		if offset > 0 {
			if _, err := f.Seek(offset, io.SeekStart); err != nil {
				f.Close()
				return nil, err
			}
		} else if err := f.Truncate(0); err != nil {
			f.Close()
			return nil, err
		}
		return &partFile{File: f, final: final, part: part, jobDir: jobDir}, nil
	}
	return open, part, nil
}

// partFile wraps an open .part file. Close() on a canceled/failed transfer
// discards the partial data; Commit() (called only on success) closes and
// atomically renames it to the final destination.
type partFile struct {
	*os.File
	final  string
	part   string
	jobDir string
	done   bool
}

// Close closes the underlying file and discards the partial artifact. It must
// be called on failure/cancellation. Use Commit() on success instead.
func (p *partFile) Close() error {
	if p.done {
		return nil
	}
	p.done = true
	cerr := p.File.Close()
	_ = os.Remove(p.part)
	_ = os.RemoveAll(p.jobDir)
	return cerr
}

// Commit closes the partial file and renames it into place. It must be called
// only after the transfer completes successfully.
func (p *partFile) Commit() error {
	if p.done {
		return nil
	}
	p.done = true
	if err := p.File.Close(); err != nil {
		_ = os.Remove(p.part)
		_ = os.RemoveAll(p.jobDir)
		return err
	}
	if err := moveFile(p.part, p.final); err != nil { // cross-fs safe
		_ = os.Remove(p.part)
		_ = os.RemoveAll(p.jobDir)
		return err
	}
	_ = os.RemoveAll(p.jobDir)
	return nil
}

// AbortPart removes a partial download and its temp directory.
func AbortPart(part string) {
	_ = os.Remove(part)
	_ = os.RemoveAll(filepath.Dir(part))
}
