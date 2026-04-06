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
	s.topCtx().index = -1
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
	ctx := s.topCtx()
	ctx.index = -1
	ctx.treeExpanded = make(map[int]bool)
	ctx.queryExpanded = make(map[int]bool)
	ctx.treeCursor = -1
	ctx.treeOffset = 0
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
	ctx := sess.state.topCtx()
	if ctx.treeExpanded != nil {
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
	ctx := sess.state.topCtx()
	if ctx.treeExpanded != nil {
		action = handleUnifiedKey(sess.state, key, ch, sess.cfg, sess.searchCols)
	} else {
		action = handleKeyEvent(sess.state, key, ch, sess.cfg, sess.searchCols)
	}
	frame := sess.Render()
	return frame, action
}

// ClickRow handles a mouse click on a visual row in unified mode.
func (sess *Session) ClickRow(row int) (SessionFrame, string) {
	ctx := sess.state.topCtx()
	if ctx.treeExpanded == nil {
		return sess.Render(), ""
	}
	_, h := sess.screen.Size()
	action := clickUnifiedRow(sess.state, row, sess.cfg, h)
	frame := sess.Render()
	return frame, action
}

// SetLabel sets the border label displayed on the top-left of the border.
func (sess *Session) SetLabel(label string) {
	sess.cfg.Label = label
}

// SelectedURL returns the URL of the currently selected item, if any.
func (sess *Session) SelectedURL() string {
	s := sess.state
	ctx := s.topCtx()
	if ctx.treeExpanded != nil {
		// Unified mode: tree cursor is the only selection
		visible := treeVisibleItems(s)
		if ctx.treeCursor >= 0 && ctx.treeCursor < len(visible) {
			return visible[ctx.treeCursor].item.URL
		}
		return ""
	}
	if ctx.index >= 0 && ctx.index < len(ctx.filtered) {
		return ctx.filtered[ctx.index].URL
	}
	return ""
}
