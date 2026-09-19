package tui

import (
	"testing"
)

// fakeApp builds a minimal App (events channel only) for parser tests - no
// terminal is required.
func fakeApp() *App {
	return &App{events: make(chan Event, 64)}
}

func feedBytes(t *testing.T, a *App, data []byte) []Event {
	t.Helper()
	r := &inputReader{a: a}
	for _, b := range data {
		r.feed(b)
	}
	var out []Event
	for len(a.events) > 0 {
		out = append(out, <-a.events)
	}
	return out
}

func TestParseBasicKeys(t *testing.T) {
	a := fakeApp()
	evs := feedBytes(t, a, []byte{'q', '\r', 0x7f, '\t'})
	if len(evs) != 4 {
		t.Fatalf("want 4 events, got %d: %+v", len(evs), evs)
	}
	if evs[0].Kind != EvKey || evs[0].Key.Action != KeyChar || evs[0].Key.Rune != 'q' {
		t.Errorf("event 0 = %+v", evs[0])
	}
	if evs[1].Key.Action != KeyEnter {
		t.Errorf("event 1 = %+v", evs[1])
	}
	if evs[2].Key.Action != KeyBackspace {
		t.Errorf("event 2 = %+v", evs[2])
	}
	if evs[3].Key.Action != KeyTab {
		t.Errorf("event 3 = %+v", evs[3])
	}
}

func TestParseCtrlKeys(t *testing.T) {
	a := fakeApp()
	evs := feedBytes(t, a, []byte{0x03, 0x01})
	if len(evs) != 2 {
		t.Fatalf("want 2 events, got %d", len(evs))
	}
	if evs[0].Key.Ctrl != true || evs[0].Key.Rune != 'c' {
		t.Errorf("ctrl+c = %+v", evs[0])
	}
	if evs[1].Key.Ctrl != true || evs[1].Key.Rune != 'a' {
		t.Errorf("ctrl+a = %+v", evs[1])
	}
}

func TestParseEscape(t *testing.T) {
	a := fakeApp()
	// A lone ESC is ambiguous (it may start a sequence), so the parser only
	// emits it once it sees the next byte - double ESC yields one Escape.
	// (A trailing lone ESC is flushed by the read loop after 30ms of quiet.)
	evs := feedBytes(t, a, []byte{0x1b, 0x1b})
	if len(evs) != 1 || evs[0].Key.Action != KeyEsc {
		t.Fatalf("escape = %+v", evs)
	}
	// ESC followed by an ordinary byte: Escape, then the byte.
	a2 := fakeApp()
	evs2 := feedBytes(t, a2, []byte{0x1b, 'x'})
	if len(evs2) != 2 || evs2[0].Key.Action != KeyEsc || evs2[1].Key.Rune != 'x' {
		t.Fatalf("esc+char = %+v", evs2)
	}
}

func TestParseArrows(t *testing.T) {
	cases := []struct {
		seq []byte
		act KeyAction
	}{
		{[]byte{0x1b, '[', 'A'}, KeyUp},
		{[]byte{0x1b, '[', 'B'}, KeyDown},
		{[]byte{0x1b, '[', 'C'}, KeyRight},
		{[]byte{0x1b, '[', 'D'}, KeyLeft},
		{[]byte{0x1b, 'O', 'A'}, KeyUp}, // SS3 mode
		{[]byte{0x1b, '[', 'H'}, KeyHome},
		{[]byte{0x1b, '[', 'F'}, KeyEnd},
		{[]byte{0x1b, '[', '5', '~'}, KeyPageUp},
		{[]byte{0x1b, '[', '6', '~'}, KeyPageDown},
		{[]byte{0x1b, '[', '1', '~'}, KeyHome},
		{[]byte{0x1b, '[', '4', '~'}, KeyEnd},
		{[]byte{0x1b, '[', '3', '~'}, KeyDelete},
		{[]byte{0x1b, '[', '1', '5', '~'}, KeyF5},
	}
	for _, c := range cases {
		a := fakeApp()
		evs := feedBytes(t, a, c.seq)
		if len(evs) != 1 {
			t.Errorf("seq %q: want 1 event, got %d (%+v)", c.seq, len(evs), evs)
			continue
		}
		if evs[0].Key.Action != c.act {
			t.Errorf("seq %q: want %v, got %v", c.seq, c.act, evs[0].Key.Action)
		}
	}
}

func TestParseMouseSGR(t *testing.T) {
	a := fakeApp()
	// ESC[<0;10;5M → left press at (9,4); ESC[<3;10;5m → release.
	evs := feedBytes(t, a, []byte{
		0x1b, '[', '<', '0', ';', '1', '0', ';', '5', 'M',
		0x1b, '[', '<', '3', ';', '1', '0', ';', '5', 'm',
		0x1b, '[', '<', '2', ';', '3', ';', '4', 'M', // right press at (2,3)
	})
	if len(evs) != 3 {
		t.Fatalf("want 3 events, got %d: %+v", len(evs), evs)
	}
	if evs[0].Kind != EvMouse || evs[0].Mouse.Action != MousePress || evs[0].Mouse.Button != BtnLeft || evs[0].Mouse.X != 9 || evs[0].Mouse.Y != 4 {
		t.Errorf("press = %+v", evs[0])
	}
	if evs[1].Mouse.Action != MouseRelease || evs[1].Mouse.Button != BtnLeft {
		t.Errorf("release = %+v", evs[1])
	}
	if evs[2].Mouse.Button != BtnRight || evs[2].Mouse.X != 2 || evs[2].Mouse.Y != 3 {
		t.Errorf("right = %+v", evs[2])
	}
}

func TestParseBracketedPaste(t *testing.T) {
	a := fakeApp()
	evs := feedBytes(t, a, append([]byte{0x1b, '[', '2', '0', '0', '~'},
		append([]byte("he llo"), 0x1b, '[', '2', '0', '1', '~')...))
	var chars []rune
	for _, e := range evs {
		if e.Kind == EvKey && e.Key.Action == KeyChar {
			chars = append(chars, e.Key.Rune)
		}
	}
	if string(chars) != "he llo" {
		t.Fatalf("pasted chars = %q", string(chars))
	}
}

func TestScreenStack(t *testing.T) {
	a := fakeApp()
	s1 := &stubScreen{}
	s2 := &stubScreen{}
	a.SetScreen(s1)
	if a.top() != s1 {
		t.Fatal("top after SetScreen != s1")
	}
	a.Push(s2)
	if a.top() != s2 || a.depth() != 2 {
		t.Fatal("push did not add screen")
	}
	a.Pop()
	if a.top() != s1 || a.depth() != 1 {
		t.Fatal("pop did not restore s1")
	}
	a.Pop() // no-op at depth 1
	if a.depth() != 1 {
		t.Fatal("pop at depth 1 must be a no-op")
	}
	s3 := &stubScreen{}
	a.Replace(s3)
	if a.top() != s3 {
		t.Fatal("replace failed")
	}
}

type stubScreen struct{}

func (s *stubScreen) Draw(*Canvas)                {}
func (s *stubScreen) Key(*App, Key) bool          { return false }
func (s *stubScreen) Mouse(*App, MouseEvent) bool { return false }
func (s *stubScreen) Tick(*App)                   {}

func TestCanvasTextAndEllipsis(t *testing.T) {
	c := newCanvas(20, 5)
	c.Text(2, 1, "hello", Style{FG: ColText})
	if got := c.cells[1*20+2+1].r; got != 'e' {
		t.Errorf("cell = %q", got)
	}
	c.TextEllipsis(0, 0, 4, "abcdefgh", Style{FG: ColText})
	if c.cells[3].r != '…' {
		t.Errorf("ellipsis cell = %q", c.cells[3].r)
	}
	// Out-of-bounds writes must be safe no-ops.
	c.Text(19, 4, "xy", Style{FG: ColText})
	c.Text(-1, 0, "z", Style{FG: ColText})
}

func TestCanvasBox(t *testing.T) {
	c := newCanvas(10, 6)
	c.Box(1, 1, 8, 4, Style{FG: ColBorder, BG: ColPanel})
	if c.cells[1*10+1].r != '╭' {
		t.Errorf("top-left = %q", c.cells[1*10+1].r)
	}
	if c.cells[1*10+8].r != '╮' {
		t.Errorf("top-right = %q", c.cells[1*10+8].r)
	}
	if c.cells[4*10+1].r != '╰' {
		t.Errorf("bottom-left = %q", c.cells[4*10+1].r)
	}
	if c.cells[4*10+8].r != '╯' {
		t.Errorf("bottom-right = %q", c.cells[4*10+8].r)
	}
	if c.cells[2*10+1].r != '│' {
		t.Errorf("left edge = %q", c.cells[2*10+1].r)
	}
}

func TestButtonHit(t *testing.T) {
	if !ButtonHit(10, 3, 8, 3, "Yes") {
		t.Error("expected hit inside button")
	}
	if ButtonHit(8, 4, 8, 3, "Yes") {
		t.Error("row below must not hit")
	}
	if ButtonHit(20, 3, 8, 3, "Yes") {
		t.Error("x beyond label width must not hit")
	}
}

func TestWrapText(t *testing.T) {
	lines := wrapText("aaaa bbbb cccc dddd", 9)
	for _, l := range lines {
		if len(l) > 9 {
			t.Errorf("line too long: %q", l)
		}
	}
	joined := ""
	for _, l := range lines {
		joined += l
	}
	if len(joined) != 18 {
		t.Errorf("wrap lost characters: %q", joined)
	}
}

func TestRowAt(t *testing.T) {
	if rowAt(5, 5, 5, 2, 3) != 0 {
		t.Error("row 0")
	}
	if rowAt(5, 8, 5, 2, 3) != 1 {
		t.Error("row 1")
	}
	if rowAt(5, 10, 5, 2, 3) != -1 {
		t.Error("below range")
	}
}
