package tui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/nelsong6/fzh/internal/model"
)

// Session holds a headless TUI instance for WASM or testing use.
// It wraps state, config, and a MemScreen so external code can
// feed key events and receive rendered ANSI frames.
type Session struct {
	state      *state
	cfg        Config
	searchCols []int
	screen     *MemScreen
}

// SessionFrame is the result of rendering: ANSI text plus cursor position.
type SessionFrame struct {
	ANSI    string
	CursorX int
	CursorY int
}

// NewSession creates a headless TUI session with the given items, config, and dimensions.
func NewSession(items []model.Item, cfg Config, w, h int) *Session {
	s, searchCols := initState(items, cfg)
	// Auto-select first item like the terminal TUI does when typing
	if len(s.filtered) > 0 {
		s.index = 0
	}
	return &Session{
		state:      s,
		cfg:        cfg,
		searchCols: searchCols,
		screen:     NewMemScreen(w, h),
	}
}

// Render draws the current state onto the MemScreen and returns an ANSI frame.
func (sess *Session) Render() SessionFrame {
	sess.screen.Clear()
	renderFrame(sess.screen, sess.state, sess.cfg)
	return SessionFrame{
		ANSI:    sess.screen.ToANSI(),
		CursorX: sess.screen.CursorX,
		CursorY: sess.screen.CursorY,
	}
}

// Resize changes the terminal dimensions and re-renders.
func (sess *Session) Resize(w, h int) SessionFrame {
	sess.screen = NewMemScreen(w, h)
	return sess.Render()
}

// HandleKey processes a key event and returns the new frame.
// Returns the action result: "" for normal, "select:output" for leaf selection, "cancel" for escape at root.
func (sess *Session) HandleKey(key tcell.Key, ch rune) (SessionFrame, string) {
	action := handleKeyEvent(sess.state, key, ch, sess.cfg, sess.searchCols)
	frame := sess.Render()
	return frame, action
}
