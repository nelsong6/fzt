package tui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/nelsong6/fzt/core"
)

// Session holds a headless TUI instance for WASM or testing use.
// It wraps state, config, and a MemScreen so external code can
// feed key events and receive rendered ANSI frames.
type Session struct {
	state      *core.State
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
func NewSession(items []core.Item, cfg Config, w, h int) *Session {
	s, searchCols := core.NewState(items, cfg)
	s.TopCtx().Index = -1
	return &Session{
		state:      s,
		cfg:        cfg,
		searchCols: searchCols,
		screen:     NewMemScreen(w, h),
	}
}

// NewTreeSession creates a headless TUI session in unified tree+search mode.
func NewTreeSession(items []core.Item, cfg Config, w, h int) *Session {
	s, searchCols := core.NewState(items, cfg)
	ctx := s.TopCtx()
	ctx.Index = -1
	ctx.TreeExpanded = make(map[int]bool)
	ctx.QueryExpanded = make(map[int]bool)
	ctx.TreeCursor = -1
	ctx.TreeOffset = 0
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
	ctx := sess.state.TopCtx()
	if ctx.TreeExpanded != nil {
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
	ctx := sess.state.TopCtx()
	if ctx.TreeExpanded != nil {
		action = handleUnifiedKey(sess.state, key, ch, sess.cfg, sess.searchCols)
	} else {
		action = handleKeyEvent(sess.state, key, ch, sess.cfg, sess.searchCols)
	}
	frame := sess.Render()
	return frame, action
}

// ClickRow handles a mouse click on a visual row in unified mode.
func (sess *Session) ClickRow(row int) (SessionFrame, string) {
	ctx := sess.state.TopCtx()
	if ctx.TreeExpanded == nil {
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

// SetFrontendCommands registers frontend-specific commands for the `:` palette.
func (sess *Session) SetFrontendCommands(commands []core.CommandItem) {
	sess.state.FrontendCommands = commands
}

// SelectedURL returns the URL of the currently selected item, if any.
func (sess *Session) SelectedURL() string {
	s := sess.state
	ctx := s.TopCtx()
	if ctx.TreeExpanded != nil {
		// Unified mode: tree cursor is the only selection
		visible := core.TreeVisibleItems(s)
		if ctx.TreeCursor >= 0 && ctx.TreeCursor < len(visible) {
			return visible[ctx.TreeCursor].Item.URL
		}
		return ""
	}
	if ctx.Index >= 0 && ctx.Index < len(ctx.Filtered) {
		return ctx.Filtered[ctx.Index].URL
	}
	return ""
}
