// The RemoraSFTP TUI launcher: the native front door of the application.
//
// Running `remorasftp` with no arguments opens this interactive control
// center. It starts/stops the SAME local engine the browser uses (via the
// app package), manages connection profiles through the SAME connection
// manager, and reads settings through the SAME config store. It never
// implements file/protocol logic of its own and never displays secret
// material.
package tui

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"remorasftp/internal/app"
	"remorasftp/internal/browser"
	"remorasftp/internal/config"
	"remorasftp/internal/manager"
	"remorasftp/internal/protocol"
	"remorasftp/internal/server"
	"remorasftp/internal/transfers"
	"remorasftp/internal/version"
)

// Run opens the interactive launcher. It returns nil when the user exits
// cleanly.
func Run(stdin, stdout *os.File) error {
	a, err := NewApp(stdin, stdout)
	if err != nil {
		return err
	}
	l := &launcher{a: a}
	if _, err := l.ensureEngine(); err != nil {
		return fmt.Errorf("cannot initialize local services: %w", err)
	}
	st := l.cfg().Settings()
	idx := 0
	if st.StartupMode == "no-browser" {
		idx = 1
	}
	a.SetScreen(&mainMenu{l: l, idx: idx})
	return a.Run()
}

// engineKind describes where a running engine (if any) lives.
type engineKind int

const (
	engOff      engineKind = iota
	engOurs                // started by this TUI process
	engExternal            // started by another RemoraSFTP process
)

// launcher is the shared state every screen acts on. All fields are only
// touched from the run-loop goroutine (screens, probes), except where noted.
type launcher struct {
	a    *App
	eng  *app.Engine
	kind engineKind

	browserState string // "" | "opened" | "failed"
	extURL       string
	extPID       int

	lastProbe time.Time
}

func (l *launcher) cfg() *config.Store {
	if l.eng == nil {
		return nil
	}
	return l.eng.Config
}

func (l *launcher) ensureEngine() (*app.Engine, error) {
	if l.eng != nil {
		return l.eng, nil
	}
	eng, err := app.New()
	if err != nil {
		return nil, err
	}
	l.eng = eng
	return eng, nil
}

// credentialBackend reports the active secret storage backend.
func (l *launcher) credentialBackend() string {
	if l.eng == nil {
		return "-"
	}
	return l.eng.CredentialBackend()
}

// probeExternal detects an engine started by another process (instance file
// + health check). It never takes control of a foreign engine.
func (l *launcher) probeExternal() {
	if l.kind == engOurs {
		return
	}
	now := time.Now()
	if now.Sub(l.lastProbe) < 2*time.Second {
		return
	}
	l.lastProbe = now
	inst, err := server.ReadInstanceFile()
	if err != nil {
		if l.kind == engExternal {
			l.kind = engOff
			l.extURL = ""
		}
		return
	}
	client := &http.Client{Timeout: 800 * time.Millisecond}
	resp, err := client.Get(inst.URL + "/api/health")
	if err != nil {
		l.kind = engOff
		return
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		l.kind = engExternal
		l.extURL = inst.URL
		l.extPID = inst.PID
	} else {
		l.kind = engOff
	}
}

// listenPlan returns the bind address/port honoring settings, plus a flag
// when the plan leaves loopback (which requires explicit confirmation).
func (l *launcher) listenPlan() (addr string, port int, beyondLoopback bool) {
	eng, err := l.ensureEngine()
	if err != nil {
		return "127.0.0.1", 0, false
	}
	addr, port = eng.ListenSettings()
	host := addr
	if h, _, err := net.SplitHostPort(addr); err == nil {
		host = h
	}
	if host == "localhost" {
		return addr, port, false
	}
	ip := net.ParseIP(host)
	beyondLoopback = ip == nil || !ip.IsLoopback()
	return addr, port, beyondLoopback
}

// startEngine brings up the local engine (idempotent).
func (l *launcher) startEngine(openBrowser bool) error {
	if l.kind == engOurs {
		return nil
	}
	if l.kind == engExternal {
		return fmt.Errorf("another RemoraSFTP engine is already running (PID %d). Stop it first.", l.extPID)
	}
	eng, err := l.ensureEngine()
	if err != nil {
		return err
	}
	// The beyond-loopback flag (third value) is intentionally not used
	// here: callers that start from user input (doStart) already enforce
	// the explicit confirmation before reaching this point.
	addr, port, _ := l.listenPlan()
	if err := eng.Start(addr, port); err != nil {
		return err
	}
	l.kind = engOurs
	if openBrowser {
		if err := browser.Open(eng.BrowserURL()); err != nil {
			l.browserState = "failed"
		} else {
			l.browserState = "opened"
		}
	} else {
		l.browserState = "off"
	}
	return nil
}

func (l *launcher) stopEngine() {
	if l.kind == engOurs && l.eng != nil {
		l.eng.Shutdown()
	}
	l.kind = engOff
	l.browserState = ""
}

func (l *launcher) openBrowser() error {
	if l.kind != engOurs {
		return errors.New("the engine was not started from this session")
	}
	if err := browser.Open(l.eng.BrowserURL()); err != nil {
		l.browserState = "failed"
		return err
	}
	l.browserState = "opened"
	return nil
}

func (l *launcher) engineAddr() string {
	switch l.kind {
	case engOurs:
		if l.eng != nil {
			return l.eng.Addr()
		}
	case engExternal:
		if l.extURL != "" {
			return strings.TrimPrefix(l.extURL, "http://")
		}
	}
	return ""
}

// activeTransfers counts queued/running/paused jobs.
func (l *launcher) activeTransfers() int {
	if l.eng == nil {
		return 0
	}
	n := 0
	for _, j := range l.eng.Transfer.List() {
		switch j.Status {
		case transfers.StatusQueued, transfers.StatusRunning, transfers.StatusPaused:
			n++
		}
	}
	return n
}

func (l *launcher) savedConnections() int {
	if l.eng == nil {
		return 0
	}
	return len(l.eng.Manager.Profiles())
}

// quitAttempt enforces the safe-exit contract: confirm before stopping a
// running engine and warn about active transfers.
func (l *launcher) quitAttempt() {
	switch l.kind {
	case engOurs:
		n := l.activeTransfers()
		var b strings.Builder
		b.WriteString("The local engine is running. Stopping it will:\n")
		b.WriteString("- close every server session\n")
		if n > 0 {
			fmt.Fprintf(&b, "- CANCEL %d active transfer(s)\n", n)
		}
		l.a.Push(&confirmScreen{
			title: "Stop engine and exit?",
			body:  b.String(),
			onYes: func() {
				l.stopEngine()
				l.a.RequestQuit()
			},
		})
	case engExternal:
		l.a.Push(&confirmScreen{
			title: "Exit?",
			body:  fmt.Sprintf("A RemoraSFTP engine started elsewhere (PID %d) will keep running.", l.extPID),
			onYes: func() { l.a.RequestQuit() },
		})
	default:
		l.a.RequestQuit()
	}
}

// ---- generic helpers -------------------------------------------------------

func wrapText(s string, width int) []string {
	if width < 4 {
		width = 4
	}
	var lines []string
	for _, raw := range strings.Split(s, "\n") {
		if len(raw) == 0 {
			lines = append(lines, "")
			continue
		}
		for len(raw) > width {
			cut := width
			if i := strings.LastIndexByte(raw[:width], ' '); i > width/2 {
				cut = i
			}
			lines = append(lines, raw[:cut])
			raw = strings.TrimLeft(raw[cut:], " ")
		}
		lines = append(lines, raw)
	}
	return lines
}

// footerHelp draws the bottom help bar (right-aligned segments).
func footerHelp(c *Canvas, y int, segments ...string) {
	total := 0
	for _, s := range segments {
		total += len(s)
	}
	total += 3 * (len(segments) - 1)
	x := c.w - 2 - total
	if x < 2 {
		x = 2
	}
	for i, s := range segments {
		if i > 0 {
			x += 3
		}
		c.Text(x, y, s, Style{FG: ColSubtle, BG: ColBg})
		x += len(s)
	}
}

// titleBar draws the standard screen header (title + rule).
func titleBar(c *Canvas, title, sub string) {
	c.Text(2, 1, title, Style{FG: ColText, BG: ColBg, Bold: true})
	if sub != "" {
		c.TextEllipsis(4+len(title), 1, c.w-6-len(title), "  "+sub, Style{FG: ColFaint, BG: ColBg})
	}
	c.HRule(2, 2, c.w-4, Style{FG: ColBorder, BG: ColBg})
}

// rowAt maps a mouse click to a row index (or -1). Rows are drawn at
// y0 + i*rowH; row i covers the lines [y0+i*rowH, y0+(i+1)*rowH), except
// the last row, which covers only its own text line (y0+(count-1)*rowH) —
// the trailing gap below the last row belongs to no row.
func rowAt(mx, my, y0, rowH int, count int) int {
	if my < y0 || count <= 0 {
		return -1
	}
	if my > y0+(count-1)*rowH {
		return -1
	}
	idx := (my - y0) / rowH
	if idx >= count {
		return -1
	}
	return idx
}

// ---- main menu ---------------------------------------------------------------

type menuRow struct {
	title string
	sub   string
}

type mainMenu struct {
	screenBase
	l   *launcher
	idx int
}

func (m *mainMenu) rows() []menuRow {
	startSub := "Start the local engine and open the browser"
	if m.l.cfg() != nil {
		if m.l.cfg().Settings().StartupMode == "no-browser" {
			startSub = "Start the local engine (browser off by setting)"
		}
	}
	return []menuRow{
		{title: "Start RemoraSFTP", sub: startSub},
		{title: "Start without browser", sub: "Start the local engine only"},
		{title: "Connections", sub: "Manage saved server profiles"},
		{title: "Settings", sub: "Application preferences"},
		{title: "Diagnostics", sub: "Version, engine and storage info"},
		{title: "About", sub: "RemoraSFTP"},
		{title: "Exit", sub: "Quit the launcher"},
	}
}

const mainMenuY0 = 5
const mainMenuRowH = 2

func (m *mainMenu) Draw(c *Canvas) {
	m.screenBase.minW, m.screenBase.minH = 64, 24
	if m.tooSmall(c) {
		m.drawTooSmall(c)
		return
	}
	m.l.probeExternal()
	c.Box(0, 0, c.w, c.h, Style{FG: ColBorder, BG: ColBg})
	titleBar(c, "RemoraSFTP", "Local-first FTP / FTPS / SFTP Manager")

	rows := m.rows()
	for i, r := range rows {
		y := mainMenuY0 + i*mainMenuRowH
		if y+1 >= c.h-2 {
			break
		}
		if i == m.idx {
			c.FillRect(2, y, c.w-4, 1, ColSelBg)
		}
		bg := ColBg
		if i == m.idx {
			bg = ColSelBg
			c.Text(2, y, ">", Style{FG: ColAccent2, BG: bg, Bold: true})
		}
		c.Text(5, y, r.title, Style{FG: ColText, BG: bg, Bold: i == m.idx})
		c.TextEllipsis(5+len(r.title)+2, y, c.w-10-len(r.title), r.sub, Style{FG: ColSubtle, BG: bg})
	}

	// Engine status chip (left) + help (right) on the footer line.
	c.HRule(2, c.h-2, c.w-4, Style{FG: ColBorder, BG: ColBg})
	switch m.l.kind {
	case engOurs:
		c.Text(2, c.h-1, "● engine running at "+m.l.engineAddr(), Style{FG: ColOK, BG: ColBg})
	case engExternal:
		c.Text(2, c.h-1, "● engine already running (PID "+fmt.Sprint(m.l.extPID)+") — start disabled", Style{FG: ColWarn, BG: ColBg})
	default:
		c.Text(2, c.h-1, "engine stopped", Style{FG: ColFaint, BG: ColBg})
	}
	footerHelp(c, c.h-1, "↑ ↓ Navigate", "Enter Select", "Q Quit")
}

func (m *mainMenu) Key(a *App, k Key) bool {
	rows := m.rows()
	switch k.Action {
	case KeyUp:
		m.idx = (m.idx - 1 + len(rows)) % len(rows)
		return true
	case KeyDown, KeyTab:
		m.idx = (m.idx + 1) % len(rows)
		return true
	case KeyHome:
		m.idx = 0
		return true
	case KeyEnd:
		m.idx = len(rows) - 1
		return true
	case KeyEnter:
		m.activate(a, m.idx)
		return true
	}
	switch k.Rune {
	case 'q', 'Q':
		m.l.quitAttempt()
		return true
	case 's':
		m.activate(a, 0)
		return true
	case 'c':
		m.activate(a, 2)
		return true
	case 'g':
		m.activate(a, 3)
		return true
	case 'd':
		m.activate(a, 4)
		return true
	case 'a':
		m.activate(a, 5)
		return true
	}
	return false
}

func (m *mainMenu) Mouse(a *App, e MouseEvent) bool {
	if e.Action != MousePress || e.Button != BtnLeft {
		return false
	}
	idx := rowAt(e.X, e.Y, mainMenuY0, mainMenuRowH, len(m.rows()))
	if idx < 0 {
		return false
	}
	if idx == m.idx {
		m.activate(a, idx)
	} else {
		m.idx = idx
	}
	return true
}

func (m *mainMenu) activate(a *App, idx int) {
	l := m.l
	switch idx {
	case 0:
		l.doStart(true)
	case 1:
		l.doStart(false)
	case 2:
		a.SetScreen(&connectionsScreen{l: l})
	case 3:
		a.SetScreen(&settingsScreen{l: l})
	case 4:
		a.SetScreen(&diagnosticsScreen{l: l})
	case 5:
		a.SetScreen(&aboutScreen{l: l})
	case 6:
		l.quitAttempt()
	}
}

// doStart implements "Start RemoraSFTP" / "Start without browser", including
// the explicit confirmation for binding beyond loopback.
func (l *launcher) doStart(openBrowser bool) {
	if l.kind == engExternal {
		l.a.SetScreen(&errorScreen{l: l, msg: fmt.Sprintf("Another RemoraSFTP engine is already running (PID %d) at %s.\n\nStop that process first, then start again from here.", l.extPID, l.extURL)})
		return
	}
	_, _, beyond := l.listenPlan()
	begin := func() {
		if err := l.startEngine(openBrowser); err != nil {
			l.a.SetScreen(&errorScreen{l: l, msg: "Could not start the local engine:\n\n" + err.Error()})
			return
		}
		l.a.SetScreen(&runningScreen{l: l})
	}
	if beyond {
		l.a.Push(&confirmScreen{
			title: "Bind beyond loopback?",
			body:  "Settings request binding the local API beyond loopback.\n\nThis exposes the local API to your network. Continue only if you understand the consequences (docs/security.md).",
			onYes: begin,
		})
		return
	}
	begin()
}

// ---- running screen ----------------------------------------------------------

type infoRow struct {
	k   string
	v   string
	col int
}

type runningScreen struct {
	screenBase
	l   *launcher
	idx int
}

const runningButtonsY = 18

func (r *runningScreen) Draw(c *Canvas) {
	r.screenBase.minW, r.screenBase.minH = 64, 24
	if r.tooSmall(c) {
		r.drawTooSmall(c)
		return
	}
	l := r.l
	c.Box(0, 0, c.w, c.h, Style{FG: ColBorder, BG: ColBg})
	titleBar(c, "RemoraSFTP — Running", "local engine active")

	browserV, browserC := "Not opened (start without browser)", ColText
	switch l.browserState {
	case "opened":
		browserV, browserC = "Opened", ColOK
	case "failed":
		browserV, browserC = "Launch failed — use [Open Browser]", ColWarn
	}
	rows := []infoRow{
		{k: "Status", v: "● Running", col: ColOK},
		{k: "Address", v: "http://" + l.engineAddr(), col: ColText},
		{k: "Browser", v: browserV, col: browserC},
		{k: "Credential", v: l.credentialBackend(), col: ColText},
		{k: "Connections", v: fmt.Sprintf("%d saved", l.savedConnections()), col: ColText},
		{k: "Transfers", v: fmt.Sprintf("%d active", l.activeTransfers()), col: ColText},
	}
	y := 4
	for _, row := range rows {
		c.Text(4, y, row.k, Style{FG: ColSubtle, BG: ColBg})
		c.TextEllipsis(22, y, c.w-26, row.v, Style{FG: row.col, BG: ColBg})
		y += 2
	}

	DrawPillButton(c, 6, runningButtonsY, "Open Browser", r.idx == 0)
	DrawPillButton(c, 6+len("Open Browser")+4, runningButtonsY, "Stop Engine", r.idx == 1)

	c.HRule(2, c.h-2, c.w-4, Style{FG: ColBorder, BG: ColBg})
	footerHelp(c, c.h-1, "↑ ↓ Select", "Enter Action", "Esc Back", "Q Quit")
}

func (r *runningScreen) Key(a *App, k Key) bool {
	switch k.Action {
	case KeyLeft, KeyUp:
		r.idx = 0
		return true
	case KeyRight, KeyDown:
		r.idx = 1
		return true
	case KeyEnter:
		if r.idx == 0 {
			_ = r.l.openBrowser() // failure is shown on the Browser row
		} else {
			r.l.stopEngine()
			a.SetScreen(&mainMenu{l: r.l})
		}
		return true
	case KeyEsc:
		a.SetScreen(&mainMenu{l: r.l})
		return true
	}
	switch k.Rune {
	case 'o':
		_ = r.l.openBrowser()
		return true
	case 's':
		r.l.stopEngine()
		a.SetScreen(&mainMenu{l: r.l})
		return true
	case 'q', 'Q':
		r.l.quitAttempt()
		return true
	}
	return false
}

func (r *runningScreen) Mouse(a *App, e MouseEvent) bool {
	if e.Action != MousePress || e.Button != BtnLeft {
		return false
	}
	if ButtonHit(e.X, e.Y, 6, runningButtonsY, "Open Browser") {
		_ = r.l.openBrowser()
		return true
	}
	if ButtonHit(e.X, e.Y, 6+len("Open Browser")+4, runningButtonsY, "Stop Engine") {
		r.l.stopEngine()
		a.SetScreen(&mainMenu{l: r.l})
		return true
	}
	return false
}

// ---- confirm / error ---------------------------------------------------------

type confirmScreen struct {
	screenBase
	title string
	body  string
	focus bool // true = Yes
	onYes func()
}

func (s *confirmScreen) Draw(c *Canvas) {
	s.screenBase.minW, s.screenBase.minH = 50, 14
	if s.tooSmall(c) {
		s.drawTooSmall(c)
		return
	}
	c.Box(0, 0, c.w, c.h, Style{FG: ColBorder, BG: ColBg})
	lines := wrapText(s.title+"\n\n"+s.body, c.w-8)
	y := 3
	for _, ln := range lines {
		if y >= c.h-4 {
			break
		}
		st := Style{FG: ColText, BG: ColBg}
		if strings.TrimSpace(ln) == "" {
			st = Style{FG: ColBg, BG: ColBg}
		}
		c.Text(4, y, ln, st)
		y++
	}
	by := c.h - 4
	DrawPillButton(c, 6, by, "Yes", s.focus)
	DrawPillButton(c, 12, by, "No", !s.focus)
	footerHelp(c, c.h-1, "Y Yes", "N No", "Esc Cancel")
}

func (s *confirmScreen) Key(a *App, k Key) bool {
	switch k.Action {
	case KeyLeft:
		s.focus = true
		return true
	case KeyRight:
		s.focus = false
		return true
	case KeyEnter:
		if s.focus {
			s.onYes()
		} else {
			a.Pop()
		}
		return true
	case KeyEsc:
		a.Pop()
		return true
	}
	switch k.Rune {
	case 'y', 'Y':
		s.onYes()
		return true
	case 'n', 'N':
		a.Pop()
		return true
	}
	return false
}

func (s *confirmScreen) Mouse(a *App, e MouseEvent) bool {
	if e.Action != MousePress || e.Button != BtnLeft {
		return false
	}
	by := a.Height() - 4
	if ButtonHit(e.X, e.Y, 6, by, "Yes") {
		s.onYes()
		return true
	}
	if ButtonHit(e.X, e.Y, 12, by, "No") {
		a.Pop()
		return true
	}
	return false
}

type errorScreen struct {
	screenBase
	l   *launcher
	msg string
}

func (s *errorScreen) Draw(c *Canvas) {
	s.screenBase.minW, s.screenBase.minH = 50, 14
	if s.tooSmall(c) {
		s.drawTooSmall(c)
		return
	}
	c.Box(0, 0, c.w, c.h, Style{FG: ColBorder, BG: ColBg})
	titleBar(c, "Problem", "an action failed — details below")
	lines := wrapText(s.msg, c.w-8)
	y := 4
	for _, ln := range lines {
		if y >= c.h-3 {
			break
		}
		c.TextEllipsis(4, y, c.w-8, ln, Style{FG: ColErr, BG: ColBg})
		y++
	}
	c.HRule(2, c.h-2, c.w-4, Style{FG: ColBorder, BG: ColBg})
	footerHelp(c, c.h-1, "Esc Back", "Q Quit")
}

func (s *errorScreen) Key(a *App, k Key) bool {
	switch k.Action {
	case KeyEsc:
		if a.depth() > 1 {
			a.Pop()
		} else {
			a.SetScreen(&mainMenu{l: s.l})
		}
		return true
	}
	if k.Rune == 'q' || k.Rune == 'Q' {
		s.l.quitAttempt()
		return true
	}
	return false
}

func (s *errorScreen) Mouse(*App, MouseEvent) bool { return false }

// ---- connections ---------------------------------------------------------------

type connRow struct {
	c      *config.Connection
	status string
	stCol  int
}

func (s *connectionsScreen) computeRows() []connRow {
	var out []connRow
	for _, p := range s.l.eng.Manager.Profiles() {
		st := "—"
		col := ColFaint
		for _, sess := range s.l.eng.Manager.Sessions() {
			if sess.ConnID == p.ID {
				switch sess.State {
				case manager.StateConnected:
					st, col = "connected", ColOK
				case manager.StateConnecting:
					st, col = "connecting", ColWarn
				case manager.StateError:
					st, col = "error", ColErr
				default:
					st, col = "disconnected", ColFaint
				}
			}
		}
		out = append(out, connRow{c: p, status: st, stCol: col})
	}
	return out
}

type connectionsScreen struct {
	screenBase
	l   *launcher
	idx int

	mu   sync.Mutex
	msg  string
	col  int
	busy bool
}

// setStatus updates the status line from any goroutine (mutex-guarded).
func (s *connectionsScreen) setStatus(msg string, col int, busy bool) {
	s.mu.Lock()
	s.msg, s.col, s.busy = msg, col, busy
	s.mu.Unlock()
	s.l.a.emit(Event{Kind: EvTick})
}

func (s *connectionsScreen) status() (string, int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.msg, s.col, s.busy
}

const connRowsY = 5

func (s *connectionsScreen) Draw(c *Canvas) {
	s.screenBase.minW, s.screenBase.minH = 78, 24
	if s.tooSmall(c) {
		s.drawTooSmall(c)
		return
	}
	c.Box(0, 0, c.w, c.h, Style{FG: ColBorder, BG: ColBg})
	titleBar(c, "Connections", fmt.Sprintf("%d saved — shared with the browser UI", s.l.savedConnections()))

	cols := []struct {
		x int
		w int
	}{
		{4, 22},  // name
		{26, 6},  // protocol
		{33, 24}, // host:port
		{58, 14}, // user
		{73, 12}, // status
	}
	c.Text(cols[0].x, 4, "NAME", Style{FG: ColFaint, BG: ColBg, Bold: true})
	c.Text(cols[1].x, 4, "PROTO", Style{FG: ColFaint, BG: ColBg, Bold: true})
	c.Text(cols[2].x, 4, "HOST:PORT", Style{FG: ColFaint, BG: ColBg, Bold: true})
	c.Text(cols[3].x, 4, "USER", Style{FG: ColFaint, BG: ColBg, Bold: true})
	c.Text(cols[4].x, 4, "STATUS", Style{FG: ColFaint, BG: ColBg, Bold: true})
	c.HRule(2, 5, c.w-4, Style{FG: ColBorder, BG: ColBg})

	rows := s.computeRows()
	y := connRowsY
	if len(rows) == 0 {
		c.Text(4, y, "No saved connections yet.", Style{FG: ColSubtle, BG: ColBg})
		c.Text(4, y+1, "Create one in the browser UI (it is shared with this launcher).", Style{FG: ColFaint, BG: ColBg})
	}
	for i, r := range rows {
		if y >= c.h-4 {
			break
		}
		bg := ColBg
		if i == s.idx {
			c.FillRect(2, y, c.w-4, 1, ColSelBg)
			bg = ColSelBg
		}
		st := Style{FG: ColText, BG: bg}
		c.TextEllipsis(cols[0].x, y, cols[0].w, r.c.Name, st)
		c.TextEllipsis(cols[1].x, y, cols[1].w, string(r.c.Protocol), st)
		c.TextEllipsis(cols[2].x, y, cols[2].w, fmt.Sprintf("%s:%d", r.c.Host, r.c.Port), st)
		user := r.c.Username
		if user == "" {
			user = "-"
		}
		c.TextEllipsis(cols[3].x, y, cols[3].w, user, st)
		c.Text(cols[4].x, y, r.status, Style{FG: r.stCol, BG: bg})
		y++
	}

	by := c.h - 4
	msg, col, busy := s.status()
	if busy {
		c.Text(4, by, "Working… (test/connect in progress)", Style{FG: ColWarn, BG: ColBg})
	} else if msg != "" {
		c.TextEllipsis(4, by, c.w-8, msg, Style{FG: col, BG: ColBg})
	}
	c.HRule(2, c.h-2, c.w-4, Style{FG: ColBorder, BG: ColBg})
	footerHelp(c, c.h-1, "↑↓ Move", "Enter Connect", "E Edit", "T Test", "U Duplicate", "D Delete", "Esc Back")
}

func (s *connectionsScreen) Key(a *App, k Key) bool {
	rows := s.computeRows()
	switch k.Action {
	case KeyUp:
		if s.idx > 0 {
			s.idx--
		}
		return true
	case KeyDown:
		if s.idx < len(rows)-1 {
			s.idx++
		}
		return true
	case KeyHome:
		s.idx = 0
		return true
	case KeyEnd:
		if len(rows) > 0 {
			s.idx = len(rows) - 1
		}
		return true
	case KeyEnter:
		if len(rows) > 0 && s.idx < len(rows) {
			s.connect(rows[s.idx].c.ID)
		}
		return true
	case KeyEsc:
		a.SetScreen(&mainMenu{l: s.l})
		return true
	}
	switch k.Rune {
	case 'c':
		if len(rows) > 0 && s.idx < len(rows) {
			s.connect(rows[s.idx].c.ID)
		}
		return true
	case 'e':
		if len(rows) > 0 && s.idx < len(rows) {
			if ed := newEditConnectionScreen(s.l, rows[s.idx].c.ID); ed != nil {
				a.Push(ed)
			}
		}
		return true
	case 't':
		if len(rows) > 0 && s.idx < len(rows) {
			s.test(rows[s.idx].c)
		}
		return true
	case 'u':
		if len(rows) > 0 && s.idx < len(rows) {
			s.duplicate(rows[s.idx].c.ID)
		}
		return true
	case 'd':
		if len(rows) > 0 && s.idx < len(rows) {
			s.delete(rows[s.idx].c)
		}
		return true
	case 'q', 'Q':
		s.l.quitAttempt()
		return true
	}
	return false
}

func (s *connectionsScreen) Mouse(a *App, e MouseEvent) bool {
	if e.Action != MousePress || e.Button != BtnLeft {
		return false
	}
	idx := rowAt(e.X, e.Y, connRowsY, 1, len(s.computeRows()))
	if idx < 0 {
		return false
	}
	if idx == s.idx {
		// Second click on the selected row: connect.
		s.connect(s.computeRows()[idx].c.ID)
	} else {
		s.idx = idx
	}
	return true
}

func (s *connectionsScreen) connect(id string) {
	_, _, busy := s.status()
	if busy {
		return
	}
	s.setStatus("Connecting…", ColSubtle, true)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, err := s.l.eng.Manager.Connect(ctx, id)
		if s.l.a.top() != s {
			return // user navigated away; do not disturb
		}
		if err != nil {
			if u, ok := protocol.AsUnknownHostKey(err); ok {
				s.setStatus("", ColText, false)
				s.l.a.SetScreenIfTop(s, &trustScreen{
					l:           s.l,
					connID:      id,
					hostPort:    u.HostPort,
					fingerprint: u.Fingerprint,
					kind:        "ssh",
					extra:       "Key type: " + u.KeyType,
				})
				return
			}
			if ce, ok := protocol.AsCertError(err); ok {
				s.setStatus("", ColText, false)
				s.l.a.SetScreenIfTop(s, &trustScreen{
					l:           s.l,
					connID:      id,
					hostPort:    ce.HostPort,
					fingerprint: ce.Fingerprint,
					kind:        "tls",
					extra:       ce.Subject,
				})
				return
			}
			s.setStatus("Connect failed: "+err.Error(), ColErr, false)
			return
		}
		s.setStatus("Connected. The session is available to the browser UI too.", ColOK, false)
	}()
}

func (s *connectionsScreen) test(c *config.Connection) {
	_, _, busy := s.status()
	if busy {
		return
	}
	s.setStatus(fmt.Sprintf("Testing %s…", c.Name), ColSubtle, true)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := s.l.eng.Manager.TestConnection(ctx, c.ID)
		if s.l.a.top() != s {
			return
		}
		if err != nil {
			if u, ok := protocol.AsUnknownHostKey(err); ok {
				s.setStatus("Unknown host key: "+u.HostPort+" ("+u.Fingerprint+")", ColErr, false)
			} else {
				s.setStatus("Test failed: "+err.Error(), ColErr, false)
			}
			return
		}
		s.setStatus("Test succeeded: connection and authentication work.", ColOK, false)
	}()
}

func (s *connectionsScreen) duplicate(id string) {
	if c, err := s.l.eng.Manager.DuplicateProfile(id); err != nil {
		s.setStatus("Duplicate failed: "+err.Error(), ColErr, false)
	} else {
		s.setStatus("Duplicated as “"+c.Name+"”", ColOK, false)
	}
}

func (s *connectionsScreen) delete(c *config.Connection) {
	s.l.a.Push(&confirmScreen{
		title: "Delete connection?",
		body:  fmt.Sprintf("“%s” (%s://%s:%d)\n\nStored credentials for this profile are also removed from the credential vault.", c.Name, c.Protocol, c.Host, c.Port),
		onYes: func() {
			if err := s.l.eng.Manager.DeleteProfile(c.ID); err != nil {
				s.setStatus("Delete failed: "+err.Error(), ColErr, false)
			} else {
				s.setStatus("Deleted “"+c.Name+"”", ColOK, false)
			}
			if s.idx >= len(s.computeRows()) && s.idx > 0 {
				s.idx--
			}
		},
	})
}

// ---- trust prompt ---------------------------------------------------------------

type trustScreen struct {
	screenBase
	l           *launcher
	connID      string
	hostPort    string
	fingerprint string
	kind        string
	extra       string
	focus       bool
}

const trustButtonsYOffset = 4

func (s *trustScreen) Draw(c *Canvas) {
	s.screenBase.minW, s.screenBase.minH = 60, 20
	if s.tooSmall(c) {
		s.drawTooSmall(c)
		return
	}
	c.Box(0, 0, c.w, c.h, Style{FG: ColBorder, BG: ColBg})
	trustSub := "unknown SSH host key — review the fingerprint"
	if s.kind != "ssh" {
		trustSub = "untrusted TLS certificate — review the fingerprint"
	}
	titleBar(c, "Verify server identity", trustSub)
	y := 4
	var lines []string
	if s.kind == "ssh" {
		lines = append(lines, "This SFTP server presented a host key that has not been trusted yet.")
	} else {
		lines = append(lines, "This FTPS server presented a certificate that could not be verified.")
	}
	lines = append(lines, "", "Server        "+s.hostPort)
	if s.extra != "" {
		lines = append(lines, "              "+s.extra)
	}
	lines = append(lines, "", "Fingerprint (review it against a trusted channel):", "  "+s.fingerprint, "",
		"Unknown keys are never accepted silently. Only continue if you", "recognize this server.")
	for _, ln := range lines {
		if y >= c.h-trustButtonsYOffset {
			break
		}
		st := Style{FG: ColText, BG: ColBg}
		if strings.HasPrefix(ln, "  ") {
			st = Style{FG: ColAccent2, BG: ColBg}
		}
		c.TextEllipsis(4, y, c.w-8, ln, st)
		y++
	}
	by := c.h - trustButtonsYOffset
	DrawPillButton(c, 6, by, "Trust & connect", s.focus)
	DrawPillButton(c, 26, by, "Cancel", !s.focus)
	footerHelp(c, c.h-1, "Y Trust", "N Cancel", "Esc Cancel")
}

func (s *trustScreen) Key(a *App, k Key) bool {
	switch k.Action {
	case KeyLeft:
		s.focus = true
		return true
	case KeyRight:
		s.focus = false
		return true
	case KeyEnter:
		if s.focus {
			s.trust()
		} else {
			a.Pop()
		}
		return true
	case KeyEsc:
		a.Pop()
		return true
	}
	switch k.Rune {
	case 'y', 'Y':
		s.trust()
		return true
	case 'n', 'N':
		a.Pop()
		return true
	}
	return false
}

func (s *trustScreen) Mouse(a *App, e MouseEvent) bool {
	if e.Action != MousePress || e.Button != BtnLeft {
		return false
	}
	by := a.Height() - trustButtonsYOffset
	if ButtonHit(e.X, e.Y, 6, by, "Trust & connect") {
		s.trust()
		return true
	}
	if ButtonHit(e.X, e.Y, 26, by, "Cancel") {
		a.Pop()
		return true
	}
	return false
}

func (s *trustScreen) trust() {
	if err := s.l.eng.Manager.TrustPending(s.hostPort, s.fingerprint); err != nil {
		s.l.a.SetScreen(&errorScreen{l: s.l, msg: "Could not record the trust decision:\n\n" + err.Error()})
		return
	}
	sc := &connectionsScreen{l: s.l}
	s.l.a.SetScreen(sc)
	sc.setStatus("Identity trusted — connecting…", ColSubtle, false)
	sc.connect(s.connID)
}

// ---- edit connection ---------------------------------------------------------------

type editField struct {
	label   string
	value   *string
	isPort  bool
	isProto bool
}

type editConnectionScreen struct {
	screenBase
	l      *launcher
	id     string
	active int
	caret  []int
	fields []editField
	err    string
}

func newEditConnectionScreen(l *launcher, id string) *editConnectionScreen {
	p, ok := l.eng.Manager.Profile(id)
	if !ok {
		return nil
	}
	name := p.Name
	proto := string(p.Protocol)
	host := p.Host
	port := fmt.Sprintf("%d", p.Port)
	user := p.Username
	dir := p.StartDir
	return &editConnectionScreen{
		l:  l,
		id: id,
		fields: []editField{
			{label: "Name", value: &name},
			{label: "Protocol", value: &proto, isProto: true},
			{label: "Host", value: &host},
			{label: "Port", value: &port, isPort: true},
			{label: "Username", value: &user},
			{label: "Start dir", value: &dir},
		},
		caret: make([]int, 6),
	}
}

func (s *editConnectionScreen) Draw(c *Canvas) {
	s.screenBase.minW, s.screenBase.minH = 64, 24
	if s.tooSmall(c) {
		s.drawTooSmall(c)
		return
	}
	c.Box(0, 0, c.w, c.h, Style{FG: ColBorder, BG: ColBg})
	titleBar(c, "Edit connection", "credentials are managed in the browser UI")
	y := 4
	for i, f := range s.fields {
		active := i == s.active
		c.Text(4, y, f.label+":", Style{FG: ColSubtle, BG: ColBg})
		bg, fg := ColBg, ColText
		if active {
			bg, fg = ColSelBg, ColSelFg
		}
		val := *f.value
		caret := s.caret[i]
		if caret > len(val) {
			caret = len(val)
		}
		for col := 0; col < c.w-10; col++ {
			ch := ' '
			st := Style{FG: fg, BG: bg}
			if col < len(val) {
				ch = rune(val[col])
			}
			if active && col == caret {
				st = Style{FG: bg, BG: fg}
			}
			cell := c.at(18+col, y)
			if cell != nil {
				cell.r = ch
				cell.style = st
			}
		}
		y += 2
	}
	if s.err != "" {
		c.TextEllipsis(4, y+1, c.w-8, s.err, Style{FG: ColErr, BG: ColBg})
	}
	c.HRule(2, c.h-2, c.w-4, Style{FG: ColBorder, BG: ColBg})
	footerHelp(c, c.h-1, "↑↓ Fields", "←→ Edit", "Enter Save", "Esc Cancel")
}

func (s *editConnectionScreen) Key(a *App, k Key) bool {
	n := len(s.fields)
	switch k.Action {
	case KeyUp:
		s.active = (s.active - 1 + n) % n
		s.err = ""
		return true
	case KeyDown, KeyTab:
		s.active = (s.active + 1) % n
		s.err = ""
		return true
	case KeyLeft:
		f := &s.fields[s.active]
		if f.isProto {
			s.cycleProto(f.value)
			return true
		}
		if s.caret[s.active] > 0 {
			s.caret[s.active]--
		}
		return true
	case KeyRight:
		f := &s.fields[s.active]
		if f.isProto {
			s.cycleProto(f.value)
			return true
		}
		if s.caret[s.active] < len(*f.value) {
			s.caret[s.active]++
		}
		return true
	case KeyBackspace:
		f := &s.fields[s.active]
		if f.isProto {
			return true
		}
		v := *f.value
		ci := s.caret[s.active]
		if ci > 0 {
			*f.value = v[:ci-1] + v[ci:]
			s.caret[s.active]--
		}
		s.err = ""
		return true
	case KeyDelete:
		f := &s.fields[s.active]
		if f.isProto {
			return true
		}
		v := *f.value
		ci := s.caret[s.active]
		if ci < len(v) {
			*f.value = v[:ci] + v[ci+1:]
		}
		s.err = ""
		return true
	case KeyEnter:
		s.save(a)
		return true
	case KeyEsc:
		a.Pop()
		return true
	}
	if k.Action == KeyChar && !k.Ctrl {
		f := &s.fields[s.active]
		if f.isProto {
			return true
		}
		v := *f.value
		ci := s.caret[s.active]
		if f.isPort {
			if k.Rune < '0' || k.Rune > '9' || len(v) >= 5 {
				return true
			}
		}
		*f.value = v[:ci] + string(k.Rune) + v[ci:]
		s.caret[s.active] = ci + 1
		s.err = ""
		return true
	}
	return false
}

func (s *editConnectionScreen) Mouse(a *App, e MouseEvent) bool {
	if e.Action != MousePress || e.Button != BtnLeft {
		return false
	}
	for i := range s.fields {
		y := 4 + i*2
		if e.Y == y && e.X >= 18 {
			s.active = i
			ci := e.X - 18
			if ci < 0 {
				ci = 0
			}
			if ci > len(*s.fields[i].value) {
				ci = len(*s.fields[i].value)
			}
			s.caret[i] = ci
			return true
		}
	}
	return false
}

func (s *editConnectionScreen) cycleProto(v *string) {
	order := []string{"ftp", "ftps", "sftp"}
	for i, p := range order {
		if *v == p {
			*v = order[(i+1)%len(order)]
			return
		}
	}
	*v = "sftp"
}

func (s *editConnectionScreen) save(a *App) {
	port := 0
	for _, ch := range *s.fields[3].value {
		port = port*10 + int(ch-'0')
	}
	if port > 65535 {
		s.err = "Port must be between 1 and 65535 (empty = default)"
		return
	}
	if strings.TrimSpace(*s.fields[0].value) == "" {
		s.err = "Name is required"
		return
	}
	if strings.TrimSpace(*s.fields[2].value) == "" {
		s.err = "Host is required"
		return
	}
	p, ok := s.l.eng.Manager.Profile(s.id)
	if !ok {
		s.err = "Profile no longer exists"
		return
	}
	in := manager.ProfileInput{
		ID:           s.id,
		Name:         strings.TrimSpace(*s.fields[0].value),
		Protocol:     config.Protocol(strings.ToLower(strings.TrimSpace(*s.fields[1].value))),
		Host:         strings.TrimSpace(*s.fields[2].value),
		Port:         port,
		Username:     strings.TrimSpace(*s.fields[4].value),
		StartDir:     strings.TrimSpace(*s.fields[5].value),
		FTPSImplicit: p.Settings.FTPSImplicit, // preserve protocol options
	}
	if _, err := s.l.eng.Manager.SaveProfile(in); err != nil {
		s.err = err.Error()
		return
	}
	a.Pop()
}

// ---- settings -------------------------------------------------------------------

type settingRow struct {
	label     string
	value     string
	onEnter   func()
	warnAfter bool
}

type settingsScreen struct {
	screenBase
	l   *launcher
	idx int
	msg string
}

func (s *settingsScreen) rows() []settingRow {
	st := s.l.cfg().Settings()
	eng := s.l.eng
	set := func(mut func(ss *config.Settings)) {
		next := st
		mut(&next)
		if err := eng.Config.SetSettings(next); err == nil {
			s.msg = "Saved ✓"
		}
	}
	onOff := func(v *bool) string {
		if *v {
			return "on"
		}
		return "off"
	}
	cycle := func(curr string, opts ...string) string {
		for i, o := range opts {
			if curr == o {
				return opts[(i+1)%len(opts)]
			}
		}
		return opts[0]
	}
	return []settingRow{
		{
			label: "Language", value: st.Language,
			onEnter: func() { set(func(ss *config.Settings) { ss.Language = cycle(st.Language, "en", "id") }) },
		},
		{
			label: "Theme", value: st.Theme,
			onEnter: func() { set(func(ss *config.Settings) { ss.Theme = cycle(st.Theme, "system", "light", "dark") }) },
		},
		{
			label: "Default view", value: st.DefaultView,
			onEnter: func() {
				set(func(ss *config.Settings) { ss.DefaultView = cycle(st.DefaultView, "details", "list", "icons") })
			},
		},
		{
			label: "Show hidden files", value: onOff(&st.ShowHidden),
			onEnter: func() { set(func(ss *config.Settings) { ss.ShowHidden = !st.ShowHidden }) },
		},
		{
			label: "Confirm deletes", value: onOff(&st.ConfirmDeletes),
			onEnter: func() { set(func(ss *config.Settings) { ss.ConfirmDeletes = !st.ConfirmDeletes }) },
		},
		{
			label: "Open browser on start", value: onOff(&st.OpenBrowserOnStart),
			onEnter: func() { set(func(ss *config.Settings) { ss.OpenBrowserOnStart = !st.OpenBrowserOnStart }) },
		},
		{
			label: "Startup behavior", value: st.StartupMode,
			onEnter: func() {
				set(func(ss *config.Settings) { ss.StartupMode = cycle(st.StartupMode, "ask", "browser", "no-browser") })
			},
		},
		{
			label: "Concurrent transfers", value: fmt.Sprintf("%d", st.ConcurrentTransfers),
			onEnter: func() {
				cur := st.ConcurrentTransfers
				if cur >= 8 {
					cur = 1
				} else {
					cur++
				}
				set(func(ss *config.Settings) { ss.ConcurrentTransfers = cur })
			},
		},
		{
			label: "Log level", value: st.LogLevel,
			onEnter: func() {
				set(func(ss *config.Settings) { ss.LogLevel = cycle(st.LogLevel, "off", "error", "warning", "info") })
			},
		},
		{
			label: "Reduce motion", value: onOff(&st.ReducedMotion),
			onEnter: func() { set(func(ss *config.Settings) { ss.ReducedMotion = !st.ReducedMotion }) },
		},
		{
			label: "Allow remote access", value: onOff(&st.RemoteAccess),
			onEnter:   func() { set(func(ss *config.Settings) { ss.RemoteAccess = !st.RemoteAccess }) },
			warnAfter: true,
		},
	}
}

func (s *settingsScreen) Draw(c *Canvas) {
	s.screenBase.minW, s.screenBase.minH = 64, 24
	if s.tooSmall(c) {
		s.drawTooSmall(c)
		return
	}
	c.Box(0, 0, c.w, c.h, Style{FG: ColBorder, BG: ColBg})
	titleBar(c, "Settings", "stored with the shared configuration system")
	rows := s.rows()
	y0 := 4
	for i, r := range rows {
		y := y0 + i*2
		if y >= c.h-4 {
			break
		}
		bg, fg := ColBg, ColText
		if i == s.idx {
			c.FillRect(2, y, c.w-4, 1, ColSelBg)
			bg, fg = ColSelBg, ColSelFg
		}
		c.Text(4, y, r.label, Style{FG: fg, BG: bg, Bold: i == s.idx})
		c.Text(30, y, r.value, Style{FG: ColAccent2, BG: bg})
		if r.warnAfter {
			c.Text(30, y+1, "beyond loopback — use with care", Style{FG: ColFaint, BG: ColBg})
		}
	}
	if s.msg != "" {
		c.Text(4, c.h-4, s.msg, Style{FG: ColOK, BG: ColBg})
	}
	c.HRule(2, c.h-2, c.w-4, Style{FG: ColBorder, BG: ColBg})
	footerHelp(c, c.h-1, "↑↓ Select", "Enter Change", "Esc Back")
}

func (s *settingsScreen) Key(a *App, k Key) bool {
	rows := s.rows()
	switch k.Action {
	case KeyUp:
		if s.idx > 0 {
			s.idx--
		}
		s.msg = ""
		return true
	case KeyDown:
		if s.idx < len(rows)-1 {
			s.idx++
		}
		s.msg = ""
		return true
	case KeyEnter:
		rows[s.idx].onEnter()
		return true
	case KeyEsc:
		a.SetScreen(&mainMenu{l: s.l})
		return true
	}
	if k.Rune == 'q' || k.Rune == 'Q' {
		s.l.quitAttempt()
		return true
	}
	return false
}

func (s *settingsScreen) Mouse(a *App, e MouseEvent) bool {
	if e.Action != MousePress || e.Button != BtnLeft {
		return false
	}
	idx := rowAt(e.X, e.Y, 4, 2, len(s.rows()))
	if idx < 0 {
		return false
	}
	if idx == s.idx {
		s.rows()[idx].onEnter()
	} else {
		s.idx = idx
	}
	return true
}

// ---- diagnostics ------------------------------------------------------------------

type diagnosticsScreen struct {
	screenBase
	l *launcher
}

func (s *diagnosticsScreen) Draw(c *Canvas) {
	s.screenBase.minW, s.screenBase.minH = 64, 24
	if s.tooSmall(c) {
		s.drawTooSmall(c)
		return
	}
	if s.l.kind == engExternal || s.l.kind == engOff {
		s.l.probeExternal()
	}
	info := version.Get()
	c.Box(0, 0, c.w, c.h, Style{FG: ColBorder, BG: ColBg})
	titleBar(c, "Diagnostics", "non-sensitive information only")

	engState := "not started"
	engCol := ColFaint
	switch s.l.kind {
	case engOurs:
		engState = "running at http://" + s.l.engineAddr()
		engCol = ColOK
	case engExternal:
		engState = fmt.Sprintf("running elsewhere at %s (PID %d)", s.l.extURL, s.l.extPID)
		engCol = ColWarn
	}

	sessions := 0
	if s.l.eng != nil {
		sessions = len(s.l.eng.Manager.Sessions())
	}
	listen, _, beyond := s.l.listenPlan()
	listenV := listen + " (loopback)"
	if beyond {
		listenV = listen + " (BEYOND loopback — advanced)"
	}

	type kvRow struct {
		k   string
		v   string
		col int
	}
	rows := []kvRow{
		{k: "RemoraSFTP", v: fmt.Sprintf("%s (commit %s, built %s)", info.Version, info.Commit, info.BuildTime), col: ColText},
		{k: "Go", v: info.GoVersion, col: ColText},
		{k: "OS / arch", v: runtime.GOOS + "/" + runtime.GOARCH, col: ColText},
		{k: "Engine", v: engState, col: engCol},
		{k: "Listen address", v: listenV, col: ColText},
		{k: "Credential backend", v: s.l.credentialBackend(), col: ColText},
		{k: "Data directory", v: s.l.eng.DataDir(), col: ColText},
		{k: "Saved connections", v: fmt.Sprintf("%d", s.l.savedConnections()), col: ColText},
		{k: "Active sessions", v: fmt.Sprintf("%d", sessions), col: ColText},
		{k: "Active transfers", v: fmt.Sprintf("%d", s.l.activeTransfers()), col: ColText},
	}
	y := 4
	for _, row := range rows {
		if y >= c.h-3 {
			break
		}
		c.Text(4, y, row.k, Style{FG: ColSubtle, BG: ColBg})
		c.TextEllipsis(24, y, c.w-28, row.v, Style{FG: row.col, BG: ColBg})
		y++
	}
	c.TextEllipsis(4, y+1, c.w-8, "Passwords, private keys, tokens, and secrets are never displayed.", Style{FG: ColFaint, BG: ColBg})
	c.HRule(2, c.h-2, c.w-4, Style{FG: ColBorder, BG: ColBg})
	footerHelp(c, c.h-1, "Esc Back", "Q Quit")
}

func (s *diagnosticsScreen) Key(a *App, k Key) bool {
	switch k.Action {
	case KeyEsc:
		a.SetScreen(&mainMenu{l: s.l})
		return true
	}
	if k.Rune == 'q' || k.Rune == 'Q' {
		s.l.quitAttempt()
		return true
	}
	return false
}

func (s *diagnosticsScreen) Mouse(*App, MouseEvent) bool { return false }

// ---- about ------------------------------------------------------------------------

type aboutScreen struct {
	screenBase
	l *launcher
}

func (s *aboutScreen) Draw(c *Canvas) {
	s.screenBase.minW, s.screenBase.minH = 64, 20
	if s.tooSmall(c) {
		s.drawTooSmall(c)
		return
	}
	info := version.Get()
	c.Box(0, 0, c.w, c.h, Style{FG: ColBorder, BG: ColBg})
	titleBar(c, "About RemoraSFTP", "version and build information")
	lines := []string{
		"RemoraSFTP — a local-first browser file manager for FTP, FTPS, and SFTP.",
		"",
		fmt.Sprintf("Version %s  ·  commit %s", info.Version, info.Commit),
		fmt.Sprintf("Built %s  ·  %s on %s/%s", info.BuildTime, info.GoVersion, info.OS, info.Arch),
		"",
		"The file engine runs entirely on this device. The browser is only the",
		"interface. This TUI is the native front door of the same engine.",
		"",
		"One binary. No cloud. No telemetry. Local credentials. Explicit trust.",
	}
	y := 4
	for _, ln := range lines {
		if y >= c.h-3 {
			break
		}
		c.Text(4, y, ln, Style{FG: ColText, BG: ColBg})
		y++
	}
	c.HRule(2, c.h-2, c.w-4, Style{FG: ColBorder, BG: ColBg})
	footerHelp(c, c.h-1, "Esc Back", "Q Quit")
}

func (s *aboutScreen) Key(a *App, k Key) bool {
	switch k.Action {
	case KeyEsc:
		a.SetScreen(&mainMenu{l: s.l})
		return true
	}
	if k.Rune == 'q' || k.Rune == 'Q' {
		s.l.quitAttempt()
		return true
	}
	return false
}

func (s *aboutScreen) Mouse(*App, MouseEvent) bool { return false }
