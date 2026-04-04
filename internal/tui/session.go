package tui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/nelsong6/fzt/internal/model"
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
	s.index = -1
	return &Session{
		state:      s,
		cfg:        cfg,
		searchCols: searchCols,
		screen:     NewMemScreen(w, h),
	}
}

// NewTreeSession creates a headless TUI session in unified tree+search mode.
func NewTreeSession(items []model.Item, cfg Config, w, h int) *Session {
	s, searchCols := initState(items, cfg)
	s.index = -1
	s.treeExpanded = make(map[int]bool)
	s.queryExpanded = make(map[int]bool)
	s.treeCursor = -1
	s.treeOffset = 0
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
	if sess.state.treeExpanded != nil {
		w, h := sess.screen.Size()
		drawUnified(sess.screen, sess.state, sess.cfg, w, 0, h)
	} else {
		renderFrame(sess.screen, sess.state, sess.cfg)
	}
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
func (sess *Session) HandleKey(key tcell.Key, ch rune) (SessionFrame, string) {
	var action string
	if sess.state.treeExpanded != nil {
		action = handleUnifiedKey(sess.state, key, ch, sess.cfg, sess.searchCols)
	} else {
		action = handleKeyEvent(sess.state, key, ch, sess.cfg, sess.searchCols)
	}
	frame := sess.Render()
	return frame, action
}

// ClickRow handles a mouse click on a visual row in unified mode.
func (sess *Session) ClickRow(row int) (SessionFrame, string) {
	if sess.state.treeExpanded == nil {
		return sess.Render(), ""
	}
	_, h := sess.screen.Size()
	action := clickUnifiedRow(sess.state, row, sess.cfg, h)
	frame := sess.Render()
	return frame, action
}

// SelectedURL returns the URL of the currently selected item, if any.
func (sess *Session) SelectedURL() string {
	s := sess.state
	if s.treeExpanded != nil {
		// Unified mode: tree cursor is the only selection
		visible := treeVisibleItems(s)
		if s.treeCursor >= 0 && s.treeCursor < len(visible) {
			return visible[s.treeCursor].item.URL
		}
		return ""
	}
	if s.index >= 0 && s.index < len(s.filtered) {
		return s.filtered[s.index].URL
	}
	return ""
}
