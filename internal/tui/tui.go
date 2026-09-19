// Package tui implements the native terminal launcher / control center of
// RemoraSFTP.
//
// # Design notes
//
// The launcher is a deliberately small, dependency-free TUI framework: a
// raw-mode input reader (keyboard + SGR mouse + bracketed paste), a
// cell-based double-buffered framebuffer rendered with 256-color ANSI,
// SIGWINCH-driven resize handling, and a screen stack for sub-screens.
//
// The TUI never implements file or protocol logic itself. It drives the same
// app.Engine (config store, connection manager, transfer manager, loopback
// server) that the browser and the CLI use, so there is exactly one
// filesystem/protocol implementation in the process.
package tui

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"time"

	"golang.org/x/term"
)

// ---- palette (256-color indices) ----------------------------------------

const (
	ColDefault = -1
	ColBg      = 235 // deep neutral background
	ColPanel   = 234
	ColText    = 252
	ColSubtle  = 245
	ColFaint   = 240
	ColBorder  = 238
	ColAccent  = 75
	ColAccent2 = 117
	ColOK      = 114
	ColWarn    = 214
	ColErr     = 203
	ColSelFg   = 255
	ColSelBg   = 33
)

// Style describes cell attributes.
type Style struct {
	FG      int
	BG      int
	Bold    bool
	Reverse bool
}

// ---- input events ---------------------------------------------------------

// KeyAction classifies a key press.
type KeyAction int

const (
	KeyChar KeyAction = iota
	KeyEnter
	KeyEsc
	KeyUp
	KeyDown
	KeyLeft
	KeyRight
	KeyBackspace
	KeyDelete
	KeyHome
	KeyEnd
	KeyPageUp
	KeyPageDown
	KeyTab
	KeyF5
)

// Key is one keyboard event.
type Key struct {
	Action KeyAction
	Rune   rune // for KeyChar
	Ctrl   bool
}

// MouseButton identifiers.
type MouseButton int

const (
	BtnLeft MouseButton = iota
	BtnMiddle
	BtnRight
)

// MouseAction identifies press/release.
type MouseAction int

const (
	MousePress MouseAction = iota
	MouseRelease
)

// MouseEvent is one mouse event (0-based coordinates).
type MouseEvent struct {
	Action MouseAction
	Button MouseButton
	X, Y   int
}

// EventKind identifies an event.
type EventKind int

const (
	EvKey EventKind = iota
	EvMouse
	EvResize
	EvTick
	EvQuit
)

// Event is one input/size/refresh event for the run loop.
type Event struct {
	Kind   EventKind
	Key    Key
	Mouse  MouseEvent
	Width  int
	Height int
}

// ---- screens ---------------------------------------------------------------

// Screen is a renderable, focusable unit. Key/Mouse return true when the
// event was handled. Tick is called periodically so screens can refresh live
// status; it must be cheap.
type Screen interface {
	Draw(c *Canvas)
	Key(a *App, k Key) bool
	Mouse(a *App, m MouseEvent) bool
	Tick(a *App)
}

// screenBase provides a no-op Tick and minimum-size guard helpers used by
// every launcher screen.
type screenBase struct{ minW, minH int }

func (b *screenBase) Tick(*App) {}

// tooSmall reports whether the terminal is below the screen's minimum size.
func (b *screenBase) tooSmall(c *Canvas) bool { return c.w < b.minW || c.h < b.minH }

// drawTooSmall renders the standard "resize your terminal" notice.
func (b *screenBase) drawTooSmall(c *Canvas) {
	c.FillRect(0, 0, c.w, c.h, ColBg)
	c.TextEllipsis(2, 2, c.w-4, "Terminal too small - RemoraSFTP needs at least "+
		fmt.Sprintf("%dx%d", b.minW, b.minH), Style{FG: ColWarn, BG: ColBg})
}

// ---- canvas ----------------------------------------------------------------

type cell struct {
	r     rune
	style Style
}

// Canvas is one frame buffer.
type Canvas struct {
	w, h  int
	cells []cell
}

func newCanvas(w, h int) *Canvas {
	c := &Canvas{w: w, h: h, cells: make([]cell, w*h)}
	for i := range c.cells {
		c.cells[i] = cell{r: ' ', style: Style{FG: ColText, BG: ColBg}}
	}
	return c
}

func (c *Canvas) at(x, y int) *cell {
	if x < 0 || y < 0 || x >= c.w || y >= c.h {
		return nil
	}
	return &c.cells[y*c.w+x]
}

// Text writes s at (x,y), clipping at the frame edge.
func (c *Canvas) Text(x, y int, s string, st Style) {
	for _, r := range s {
		cell := c.at(x, y)
		if cell == nil {
			return
		}
		cell.r = r
		cell.style = st
		x++
	}
}

// TextEllipsis writes s at (x,y) clipped to width columns, appending … when
// truncated.
func (c *Canvas) TextEllipsis(x, y, width int, s string, st Style) {
	if width <= 0 {
		return
	}
	runes := []rune(s)
	if len(runes) > width {
		if width > 1 {
			runes = runes[:width-1]
			c.Text(x, y, string(runes), st)
			cell := c.at(x+width-1, y)
			if cell != nil {
				cell.r = '…'
				cell.style = st
			}
		}
		return
	}
	c.Text(x, y, s, st)
}

// FillRect paints a rectangle with a background color (text cleared).
func (c *Canvas) FillRect(x, y, w, h, bg int) {
	for yy := y; yy < y+h; yy++ {
		for xx := x; xx < x+w; xx++ {
			cell := c.at(xx, yy)
			if cell == nil {
				continue
			}
			cell.r = ' '
			cell.style = Style{FG: ColText, BG: bg}
		}
	}
}

// Box draws a single-line frame plus the background fill.
func (c *Canvas) Box(x, y, w, h int, st Style) {
	if w < 2 || h < 2 {
		return
	}
	c.FillRect(x, y, w, h, st.BG)
	for i := 0; i < w; i++ {
		if cell := c.at(x+i, y); cell != nil {
			cell.r = '─'
			cell.style = st
		}
		if cell := c.at(x+i, y+h-1); cell != nil {
			cell.r = '─'
			cell.style = st
		}
	}
	for i := 0; i < h; i++ {
		if left := c.at(x, y+i); left != nil {
			left.r = '│'
			left.style = st
		}
		if right := c.at(x+w-1, y+i); right != nil {
			right.r = '│'
			right.style = st
		}
	}
	corners := [][2]int{{0, 0}, {w - 1, 0}, {0, h - 1}, {w - 1, h - 1}}
	for _, p := range corners {
		cell := c.at(x+p[0], y+p[1])
		if cell == nil {
			continue
		}
		switch {
		case p[0] == 0 && p[1] == 0:
			cell.r = '╭'
		case p[0] == w-1 && p[1] == 0:
			cell.r = '╮'
		case p[0] == 0 && p[1] == h-1:
			cell.r = '╰'
		default:
			cell.r = '╯'
		}
		cell.style = st
	}
}

// HRule draws a horizontal line at row y from x for length n.
func (c *Canvas) HRule(x, y, n int, st Style) {
	for i := 0; i < n; i++ {
		cell := c.at(x+i, y)
		if cell != nil {
			cell.r = '─'
			cell.style = st
		}
	}
}

// ButtonHit reports whether (mx,my) falls inside a pill button drawn at
// (x,y) with the given label (2 columns of padding on each side).
func ButtonHit(mx, my, x, y int, label string) bool {
	w := len(label) + 4
	return mx >= x && mx < x+w && my == y
}

// DrawPillButton renders a focused/unfocused pill button.
func DrawPillButton(c *Canvas, x, y int, label string, focused bool) {
	st := Style{FG: ColText, BG: ColPanel}
	if focused {
		st = Style{FG: ColSelFg, BG: ColSelBg, Bold: true}
	}
	c.FillRect(x, y, len(label)+4, 1, st.BG)
	c.Text(x+2, y, " "+label+" ", st)
}

// ---- app -------------------------------------------------------------------

// App is the TUI core: input loop, screen stack, renderer.
type App struct {
	stdin  *os.File
	stdout *os.File

	rawOK bool
	rawSt term.State

	events chan Event
	stopCh chan struct{}
	quit   bool
	readWG sync.WaitGroup

	mu      sync.Mutex
	stack   []Screen
	width   int
	height  int
	needClr bool
}

// NewApp prepares the terminal (raw mode, mouse) and sizes the framebuffer.
// It does not start the run loop; call Run.
func NewApp(stdin, stdout *os.File) (*App, error) {
	fd := int(stdin.Fd())
	if !term.IsTerminal(fd) {
		return nil, errors.New("standard input is not a terminal")
	}
	w, h, err := term.GetSize(fd)
	if err != nil || w < 4 || h < 2 {
		return nil, errors.New("cannot determine terminal size")
	}
	st, err := term.MakeRaw(fd)
	if err != nil {
		return nil, err
	}
	return &App{
		stdin:  stdin,
		stdout: stdout,
		rawOK:  true,
		rawSt:  *st,
		events: make(chan Event, 64),
		stopCh: make(chan struct{}),
		width:  w,
		height: h,
	}, nil
}

// ---- screen stack -----------------------------------------------------------

func (a *App) top() Screen {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.stack) == 0 {
		return nil
	}
	return a.stack[len(a.stack)-1]
}

// SetScreen replaces the whole stack with a single screen.
func (a *App) SetScreen(s Screen) {
	a.mu.Lock()
	a.stack = []Screen{s}
	a.needClr = true
	a.mu.Unlock()
}

// Push adds a sub-screen on top of the current one.
func (a *App) Push(s Screen) {
	a.mu.Lock()
	a.stack = append(a.stack, s)
	a.needClr = true
	a.mu.Unlock()
}

// Pop removes the top screen (if there is one below it).
func (a *App) Pop() {
	a.mu.Lock()
	if len(a.stack) > 1 {
		a.stack = a.stack[:len(a.stack)-1]
	}
	a.needClr = true
	a.mu.Unlock()
}

// Replace swaps the top screen.
func (a *App) Replace(s Screen) {
	a.mu.Lock()
	if len(a.stack) > 0 {
		a.stack[len(a.stack)-1] = s
	} else {
		a.stack = []Screen{s}
	}
	a.needClr = true
	a.mu.Unlock()
}

// SetScreenIfTop replaces the top screen only when it is still `s`
// (check-and-set under one lock), so async callbacks can safely publish
// screen transitions from other goroutines.
func (a *App) SetScreenIfTop(s, replacement Screen) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.stack) > 0 && a.stack[len(a.stack)-1] == s {
		a.stack[len(a.stack)-1] = replacement
		a.needClr = true
		return true
	}
	return false
}

// Height returns the current terminal height (for overlay positioning).
func (a *App) Height() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.height
}

// RequestQuit asks the run loop to stop (terminal is restored on exit).
func (a *App) RequestQuit() {
	select {
	case a.events <- Event{Kind: EvQuit}:
	default:
	}
}

// emit is a non-blocking event enqueue; a full queue means the UI is behind
// and the event is dropped (never blocks the input reader).
func (a *App) emit(e Event) {
	select {
	case a.events <- e:
	default:
	}
}

// ---- input ------------------------------------------------------------------

const (
	pStateNormal = iota
	pStateEsc
	pStateCSI
	pStateSS3
	pStateEscPaste
)

// pasteTerminator is the bracketed-paste end marker (ESC [201 ~) with the
// ESC excluded - the bytes that follow an ESC while pasting and must match
// for the paste to end.
const pasteTerminator = "[201~"

type inputReader struct {
	a         *App
	state     int
	params    []byte
	pasteMode bool
	pasteBuf  []byte // bytes after an ESC seen inside a paste (terminator match)
}

// readLoop consumes raw terminal input and enqueues events. It terminates
// when stdin is closed or the app stops - after the run loop ends the
// process is leaving, so the loop is never waited on.
//
// A pending lone ESC (not yet part of a sequence) is flushed as an Escape
// key after a short quiet period, so "press Escape and stop" works instead
// of waiting for the next keystroke.
func (a *App) readLoop() {
	defer a.readWG.Done()
	r := &inputReader{a: a}
	buf := make([]byte, 512)

	// Try to make stdin non-blocking so we can interleave the ESC timeout
	// with the read. If that is not possible (unsupported platform), fall
	// back to a plain blocking loop.
	if enableNonBlocking(uintptr(int(a.stdin.Fd()))) {
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		var escPending time.Time
		for {
			n, err := a.stdin.Read(buf)
			if n > 0 {
				for i := 0; i < n; i++ {
					r.feed(buf[i])
				}
				if r.state == pStateEsc {
					escPending = time.Now()
				} else {
					escPending = time.Time{}
				}
				continue
			}
			if !isWouldBlock(err) {
				return
			}
			select {
			case <-a.stopCh:
				return
			case <-ticker.C:
				if !escPending.IsZero() && time.Since(escPending) >= 30*time.Millisecond {
					r.flushEsc()
					escPending = time.Time{}
				}
			}
		}
	}

	// Blocking fallback (no O_NONBLOCK support).
	for {
		n, err := a.stdin.Read(buf)
		if err != nil {
			return
		}
		for i := 0; i < n; i++ {
			r.feed(buf[i])
		}
	}
}

// flushEsc emits a pending lone Escape (only called from the read goroutine).
func (r *inputReader) flushEsc() {
	if r.state == pStateEsc {
		r.state = pStateNormal
		r.emitKey(Key{Action: KeyEsc})
	}
}

// feedEscPaste consumes bytes after an ESC seen inside a bracketed paste.
// If they spell the paste terminator ("[201~"), paste mode ends and the
// whole sequence is consumed. Otherwise the ESC was literal paste content:
// the buffered bytes are emitted as pasted characters and the current byte
// is reprocessed in normal paste mode.
func (r *inputReader) feedEscPaste(b byte) {
	if len(r.pasteBuf) < len(pasteTerminator) && pasteTerminator[len(r.pasteBuf)] == b {
		// Still a prefix of the terminator.
		r.pasteBuf = append(r.pasteBuf, b)
		if string(r.pasteBuf) == pasteTerminator {
			r.pasteBuf = r.pasteBuf[:0]
			r.pasteMode = false
			r.state = pStateNormal
		}
		return
	}
	// Not the terminator: the ESC and the buffered bytes were literal
	// paste content - emit the buffered bytes as pasted text and
	// reprocess the deciding byte in normal paste mode.
	for _, c := range r.pasteBuf {
		if c >= 0x20 && c != 0x7f {
			r.emitKey(Key{Action: KeyChar, Rune: rune(c)})
		}
	}
	r.pasteBuf = r.pasteBuf[:0]
	r.state = pStateNormal
	r.feed(b)
}

func (r *inputReader) feed(b byte) {
	switch r.state {
	case pStateNormal:
		r.feedNormal(b)
	case pStateEsc:
		switch {
		case b == '[':
			r.state = pStateCSI
			r.params = r.params[:0]
		case b == 'O':
			r.state = pStateSS3
		case b == 0x1b:
			r.state = pStateNormal
			r.emitKey(Key{Action: KeyEsc})
		default:
			// ESC followed by something unknown: treat as plain Escape and
			// reprocess the byte.
			r.state = pStateNormal
			r.emitKey(Key{Action: KeyEsc})
			r.feed(b)
		}
	case pStateEscPaste:
		r.feedEscPaste(b)
	case pStateSS3:
		r.state = pStateNormal
		switch b {
		case 'A':
			r.emitKey(Key{Action: KeyUp})
		case 'B':
			r.emitKey(Key{Action: KeyDown})
		case 'C':
			r.emitKey(Key{Action: KeyRight})
		case 'D':
			r.emitKey(Key{Action: KeyLeft})
		case 'H':
			r.emitKey(Key{Action: KeyHome})
		case 'F':
			r.emitKey(Key{Action: KeyEnd})
		}
	case pStateCSI:
		r.feedCSI(b)
	}
}

func (r *inputReader) feedNormal(b byte) {
	if r.pasteMode {
		if b == 0x1b {
			// The paste can only end with the bracketed-paste terminator
			// (ESC[201~). Track potential terminators explicitly instead of
			// dropping the ESC, or "[201~" would leak into pasted text and
			// paste mode would never turn off.
			r.pasteBuf = r.pasteBuf[:0]
			r.state = pStateEscPaste
			return
		}
		if b >= 0x20 && b != 0x7f {
			r.emitKey(Key{Action: KeyChar, Rune: rune(b)})
		}
		return
	}
	switch b {
	case 0x1b:
		r.state = pStateEsc
	case '\r', '\n':
		r.emitKey(Key{Action: KeyEnter})
	case 0x7f, 0x08:
		r.emitKey(Key{Action: KeyBackspace})
	case '\t':
		r.emitKey(Key{Action: KeyTab})
	case 0x0b, 0x0c, 0x1d:
		// Ctrl+K / Ctrl+L / Ctrl+] - consume, no action
	case 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
		0x0e, 0x0f,
		0x10, 0x11, 0x12, 0x13, 0x14, 0x15,
		0x16, 0x17, 0x18, 0x19, 0x1a:
		// Ctrl+letter: control byte N (1..26) is Ctrl + (Nth letter),
		// so 0x01 -> 'a', 0x03 -> 'c', 0x1a -> 'z'.
		r.emitKey(Key{Action: KeyChar, Rune: rune(b - 1 + 'a'), Ctrl: true})
	default:
		if b >= 0x20 {
			r.emitKey(Key{Action: KeyChar, Rune: rune(b)})
		}
	}
}

func (r *inputReader) feedCSI(b byte) {
	switch {
	case b >= 0x30 && b <= 0x3f: // parameter bytes
		r.params = append(r.params, b)
	case b >= 0x20 && b <= 0x2f: // intermediate bytes (consume)
	case b >= 0x40 && b <= 0x7e: // final byte
		r.state = pStateNormal
		r.dispatchCSI(b)
	default:
		r.state = pStateNormal
	}
}

func (r *inputReader) dispatchCSI(final byte) {
	params := r.params
	r.params = r.params[:0]

	if len(params) > 0 && params[0] == '<' {
		// SGR mouse: ESC[<b;x;yM (press) or m (release).
		action := MouseRelease
		if final == 'M' {
			action = MousePress
		}
		var btn, x, y int
		_, _ = fmt.Sscanf(string(params[1:]), "%d;%d;%d", &btn, &x, &y)
		mouse := MouseEvent{Action: action, X: x - 1, Y: y - 1}
		switch btn & 0x3 {
		case 0:
			mouse.Button = BtnLeft
		case 1:
			mouse.Button = BtnMiddle
		case 2:
			mouse.Button = BtnRight
		default:
			// Release (3): the SGR code does not encode which button was
			// released; the launcher only acts on presses.
			mouse.Button = BtnLeft
		}
		if btn&0x20 != 0 {
			return // drag/motion - not needed by the launcher
		}
		r.a.emit(Event{Kind: EvMouse, Mouse: mouse})
		return
	}

	if len(params) > 0 && params[0] == '?' {
		return // mode set/reset (e.g. mouse enable) - nothing to do
	}

	num := 0
	for _, p := range params {
		if p >= '0' && p <= '9' {
			num = num*10 + int(p-'0')
		}
	}
	switch final {
	case 'A':
		r.emitKey(Key{Action: KeyUp})
	case 'B':
		r.emitKey(Key{Action: KeyDown})
	case 'C':
		r.emitKey(Key{Action: KeyRight})
	case 'D':
		r.emitKey(Key{Action: KeyLeft})
	case 'H':
		r.emitKey(Key{Action: KeyHome})
	case 'F':
		r.emitKey(Key{Action: KeyEnd})
	case 'u':
		// kitty keyboard protocol - consume only.
	case '~':
		switch num {
		case 1, 7:
			r.emitKey(Key{Action: KeyHome})
		case 3:
			r.emitKey(Key{Action: KeyDelete})
		case 4, 8:
			r.emitKey(Key{Action: KeyEnd})
		case 5:
			r.emitKey(Key{Action: KeyPageUp})
		case 6:
			r.emitKey(Key{Action: KeyPageDown})
		case 15:
			r.emitKey(Key{Action: KeyF5})
		case 200:
			r.pasteMode = true
		case 201:
			r.pasteMode = false
		}
	}
}

func (r *inputReader) emitKey(k Key) {
	r.a.emit(Event{Kind: EvKey, Key: k})
}

// ---- run loop ----------------------------------------------------------------

// Run starts the loop and blocks until the app quits. The terminal is always
// restored before returning.
func (a *App) Run() error {
	a.writeRaw("\x1b[?1049h")                       // alternate screen buffer
	a.writeRaw("\x1b[?25l")                         // hide cursor
	a.writeRaw("\x1b[?1006h\x1b[?1000h\x1b[?2004h") // SGR mouse, press/release, bracketed paste

	sigCh := make(chan os.Signal, 1)
	notifyWinch(sigCh) // SIGWINCH on unix; a no-op on Windows (terminal resize
	// is picked up on the next keypress there)

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	a.readWG.Add(1)
	go a.readLoop()

	resize := func() {
		w, h, err := term.GetSize(int(a.stdin.Fd()))
		if err == nil && w > 0 && h > 0 {
			a.mu.Lock()
			resized := w != a.width || h != a.height
			a.width, a.height = w, h
			if resized {
				a.needClr = true
			}
			a.mu.Unlock()
		}
	}
	resize()

	defer func() {
		a.writeRaw("\x1b[?1006l\x1b[?1000l\x1b[?2004l") // mouse/paste off
		a.writeRaw("\x1b[0m\x1b[?25h")                  // reset, show cursor
		a.writeRaw("\x1b[?1049l")                       // leave alternate screen (clears UI)
		if a.rawOK {
			_ = term.Restore(int(a.stdin.Fd()), &a.rawSt)
		}
		close(a.stopCh)
		signal.Stop(sigCh)
	}()

	for {
		select {
		case e := <-a.events:
			a.handle(e)
		case <-sigCh:
			resize()
			a.render()
		case <-ticker.C:
			a.handle(Event{Kind: EvTick})
		case <-a.stopCh:
			return nil
		}
		if a.quit {
			return nil
		}
	}
}

func (a *App) handle(e Event) {
	switch e.Kind {
	case EvQuit:
		a.quit = true
		return
	case EvResize:
		a.mu.Lock()
		a.width, a.height = e.Width, e.Height
		a.needClr = true
		a.mu.Unlock()
	case EvTick:
		if s := a.top(); s != nil {
			s.Tick(a)
		}
	default:
		s := a.top()
		if s == nil {
			a.quit = true
			return
		}
		switch e.Kind {
		case EvKey:
			// Global: Ctrl+C requests quit unless the screen handles it.
			if e.Key.Ctrl && e.Key.Rune == 'c' {
				if !s.Key(a, e.Key) {
					a.quit = true
					return
				}
			} else if !s.Key(a, e.Key) {
				if e.Key.Action == KeyEsc && a.depth() > 1 {
					a.Pop()
				}
			}
		case EvMouse:
			_ = s.Mouse(a, e.Mouse)
		}
	}
	a.render()
}

func (a *App) depth() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.stack)
}

// ---- rendering ----------------------------------------------------------------

func (a *App) writeRaw(s string) {
	_, _ = a.stdout.WriteString(s)
}

func (a *App) render() {
	a.mu.Lock()
	w, h := a.width, a.height
	clear := a.needClr
	a.needClr = false
	a.mu.Unlock()
	if w < 4 || h < 2 {
		return
	}

	c := newCanvas(w, h)
	if s := a.top(); s != nil {
		s.Draw(c)
	}

	var buf bytes.Buffer
	if clear {
		buf.WriteString("\x1b[2J\x1b[H")
	} else {
		buf.WriteString("\x1b[H")
	}
	for y := 0; y < h; y++ {
		buf.WriteString("\x1b[")
		buf.WriteString(itoa(y + 1))
		buf.WriteString(";1H")
		prevFg, prevBg, prevBold, prevRev := 999, 999, false, false
		for x := 0; x < w; x++ {
			cell := c.cells[y*w+x]
			st := cell.style
			fg, bg := st.FG, st.BG
			if fg == ColDefault {
				fg = 0
			}
			if bg == ColDefault {
				bg = 0
			}
			if fg != prevFg || bg != prevBg || st.Bold != prevBold || st.Reverse != prevRev {
				buf.WriteString("\x1b[0m")
				if st.Bold {
					buf.WriteString("\x1b[1m")
				}
				if st.Reverse {
					buf.WriteString("\x1b[7m")
				}
				if fg != 0 || bg != 0 {
					buf.WriteString("\x1b[38;5;")
					buf.WriteString(itoa(fg))
					buf.WriteString("m\x1b[48;5;")
					buf.WriteString(itoa(bg))
					buf.WriteString("m")
				}
				prevFg, prevBg, prevBold, prevRev = fg, bg, st.Bold, st.Reverse
			}
			if cell.r == 0 {
				buf.WriteByte(' ')
			} else {
				buf.WriteRune(cell.r)
			}
		}
		buf.WriteString("\x1b[K")
	}
	a.writeRaw(buf.String())
}

// itoa is a tiny non-negative int→string helper.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
