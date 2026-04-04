package tui

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/nelsong6/fzt/internal/column"
	"github.com/nelsong6/fzt/internal/model"
	"github.com/nelsong6/fzt/internal/scorer"
)

// Config holds all TUI options derived from CLI flags.
type Config struct {
	Layout       string // "reverse" or "default"
	Border       bool
	HeaderLines  int
	Nth          []int // 1-based field indices for search scope
	AcceptNth    []int // 1-based field indices for output
	Prompt       string
	Delimiter    string
	Tiered       bool
	DepthPenalty int
	SearchCols   []int // 1-based, overrides Nth for scoring
	Height       int   // percentage of terminal height (0 = full)
	ShowScores   bool   // annotate filter output with scores
	ANSI         bool   // preserve ANSI colors from input
	Title        string // title displayed at the top of the finder
	TitlePos     string // title position: "left", "center", "right"
	TreeMode     bool   // start in tree view mode
}

type scopeLevel struct {
	parentIdx   int // index into allItems (-1 for root)
	query       []rune
	cursor      int
	index       int
	offset      int
	wasExpanded bool // true if folder was already expanded before pushScope
}

type state struct {
	query     []rune
	cursor    int // cursor position within query
	index     int // selected item index in filtered list
	offset    int // scroll offset
	items     []model.Item // current scope's direct items
	allItems    []model.Item // all items (flat, for search)
	filtered    []model.Item
	headers     []model.Item
	widths      []int
	nameColWidth int // max width of the name (first field) across all items
	colGap      int
	cancelled bool
	scope     []scopeLevel // scope stack for drill-down navigation

	// Unified tree+search
	treeExpanded  map[int]bool // manual expand/collapse state
	treeCursor    int          // cursor in tree
	treeOffset    int          // scroll offset for tree viewport
	searchActive  bool         // true when query is active
	navMode       bool         // true = nav leads (arrows drive, name echoed to prompt)
	queryExpanded map[int]bool // auto-expansion driven by top match (temporary)

	// Command mode (:)
	commandMode     bool          // true when command palette is active
	commandGlobal   bool          // true = global (prompt takeover), false = contextual (bottom panel)
	commandQuery    []rune        // separate from main query so search/nav state is preserved
	commandCursor   int           // selected command index
	commandFiltered []commandItem // filtered command list
	commandRanName  string        // name of the executed command (shown in prompt)
	commandOutput   []string      // output lines from executed command (shown in list area)
}

func initState(items []model.Item, cfg Config) (*state, []int) {
	var headers []model.Item
	data := items
	if cfg.HeaderLines > 0 && cfg.HeaderLines <= len(items) {
		headers = items[:cfg.HeaderLines]
		data = items[cfg.HeaderLines:]
	}

	allWidths := column.ComputeWidths(items)

	// Compute max name width from data items (not headers)
	nameColW := 0
	for _, item := range data {
		if len(item.Fields) > 0 {
			w := len([]rune(item.Fields[0]))
			if w > nameColW {
				nameColW = w
			}
		}
	}

	s := &state{
		query:        nil,
		cursor:       0,
		index:        -1,
		offset:       0,
		items:        data,
		allItems:     data,
		headers:      headers,
		widths:       allWidths,
		nameColWidth: nameColW,
		colGap:       2,
		scope:        []scopeLevel{{parentIdx: -1}},
	}

	// In tiered mode, start showing only root-level items
	if cfg.Tiered {
		s.items = rootItems(data)
	}

	searchCols := cfg.SearchCols
	if len(searchCols) == 0 {
		searchCols = cfg.Nth
	}
	filterItems(s, cfg, searchCols)

	return s, searchCols
}

// findInAll finds the index of an item in allItems by matching Fields[0] and Depth.
func findInAll(allItems []model.Item, item model.Item) int {
	for i, ai := range allItems {
		if ai.Depth == item.Depth && len(ai.Fields) > 0 && len(item.Fields) > 0 && ai.Fields[0] == item.Fields[0] {
			return i
		}
	}
	return -1
}

// rootItems returns only depth-0 items.
func rootItems(items []model.Item) []model.Item {
	var out []model.Item
	for _, item := range items {
		if item.Depth == 0 {
			out = append(out, item)
		}
	}
	return out
}

// descendantsOf returns all items under a given parent (or all items if parentIdx is -1).
func descendantsOf(allItems []model.Item, parentIdx int) []model.Item {
	if parentIdx < 0 {
		return allItems
	}
	var out []model.Item
	var collect func(idx int)
	collect = func(idx int) {
		for _, childIdx := range allItems[idx].Children {
			if childIdx < len(allItems) {
				out = append(out, allItems[childIdx])
				collect(childIdx)
			}
		}
	}
	collect(parentIdx)
	return out
}

// childrenOf returns the direct children of the item at parentIdx in allItems.
func childrenOf(allItems []model.Item, parentIdx int) []model.Item {
	parent := allItems[parentIdx]
	var out []model.Item
	for _, childIdx := range parent.Children {
		if childIdx < len(allItems) {
			out = append(out, allItems[childIdx])
		}
	}
	return out
}

func renderFrame(c Canvas, s *state, cfg Config) {
	w, h := c.Size()

	usableH := h
	if cfg.Height > 0 && cfg.Height < 100 {
		usableH = h * cfg.Height / 100
		if usableH < 3 {
			usableH = 3
		}
	}

	startY := 0
	if cfg.Height > 0 && cfg.Height < 100 {
		startY = h - usableH
	}

	if cfg.Layout == "reverse" {
		drawReverse(c, s, cfg, w, startY, usableH)
	} else {
		drawDefault(c, s, cfg, w, startY, usableH)
	}
}

// Run launches the interactive TUI. Returns the selected item's output string, or "" if cancelled.
func Run(items []model.Item, cfg Config) (string, error) {
	if cfg.Height > 0 && cfg.Height < 100 {
		return RunInline(items, cfg)
	}

	screen, err := tcell.NewScreen()
	if err != nil {
		return "", fmt.Errorf("creating screen: %w", err)
	}
	if err := screen.Init(); err != nil {
		return "", fmt.Errorf("initializing screen: %w", err)
	}
	defer screen.Fini()

	screen.SetStyle(tcell.StyleDefault.Background(tcell.ColorDefault).Foreground(tcell.ColorDefault))
	screen.EnablePaste()

	if cfg.TreeMode {
		return runWithSession(screen, items, cfg)
	}

	s, searchCols := initState(items, cfg)
	canvas := &tcellCanvas{screen: screen}

	for {
		screen.Clear()
		renderFrame(canvas, s, cfg)
		screen.Show()

		ev := screen.PollEvent()
		switch ev := ev.(type) {
		case *tcell.EventKey:
			action := handleKeyEvent(s, ev.Key(), ev.Rune(), cfg, searchCols)
			switch {
			case action == "cancel":
				return "", nil
			case len(action) > 7 && action[:7] == "select:":
				return action[7:], nil
			}
		case *tcell.EventResize:
			screen.Sync()
		}
	}
}

// runWithSession renders directly to a tcell screen, supporting tree mode + search switching.
func runWithSession(screen tcell.Screen, items []model.Item, cfg Config) (string, error) {
	s, searchCols := initState(items, cfg)
	s.treeExpanded = make(map[int]bool)
	s.queryExpanded = make(map[int]bool)
	s.treeCursor = -1

	canvas := &tcellCanvas{screen: screen}

	for {
		screen.Clear()
		w, h := screen.Size()
		drawUnified(canvas, s, cfg, w, 0, h)
		screen.Sync() // full redraw — avoids stale content from layout changes

		ev := screen.PollEvent()
		switch ev := ev.(type) {
		case *tcell.EventKey:
			action := handleUnifiedKey(s, ev.Key(), ev.Rune(), cfg, searchCols)
			switch {
			case action == "cancel":
				return "", nil
			case len(action) > 7 && action[:7] == "select:":
				return action[7:], nil
			}
		case *tcell.EventResize:
			screen.Sync()
		}
	}
}

// handleUnifiedKey handles all key events in unified tree+search mode.
// The tree is the single navigation surface. Typing filters and auto-expands
// the tree to reveal matches. Up/Down always move the tree cursor.
func handleUnifiedKey(s *state, key tcell.Key, ch rune, cfg Config, searchCols []int) string {
	// Command output displayed — any key exits command mode
	if s.commandMode && len(s.commandOutput) > 0 {
		exitCommandMode(s)
		return ""
	}

	// Command mode active — handle command input
	if s.commandMode {
		return handleCommandKey(s, key, ch)
	}

	// ':' enters command mode
	if key == tcell.KeyRune && ch == ':' {
		hasSelection := s.treeCursor >= 0
		hasQuery := len(s.query) > 0
		hasMatches := len(s.filtered) > 0

		if !hasQuery && !hasSelection {
			// Empty state — global command mode (takes over prompt)
			enterCommandMode(s, true)
			return ""
		}
		if hasQuery && !hasMatches {
			// Query with no matches — global command mode
			enterCommandMode(s, true)
			return ""
		}
		if hasSelection || (hasQuery && hasMatches) {
			// Item highlighted — autocomplete + contextual command mode (bottom panel)
			if hasQuery && hasMatches && len(s.filtered[0].Fields) > 0 {
				// Lock in autocomplete
				s.query = []rune(s.filtered[0].Fields[0])
				s.cursor = len(s.query)
				filterItems(s, cfg, searchCols)
				updateQueryExpansion(s)
				syncTreeCursorToTopMatch(s)
			}
			enterCommandMode(s, false)
			return ""
		}
	}

	// Nav mode + Ctrl+U: clean slate — exit nav, clear query, deselect
	if s.navMode && key == tcell.KeyCtrlU {
		s.navMode = false
		s.query = nil
		s.cursor = 0
		s.treeCursor = -1
		s.queryExpanded = make(map[int]bool)
		if len(s.scope) <= 1 {
			s.searchActive = false
			s.filtered = nil
		} else {
			filterItems(s, cfg, searchCols)
		}
		return ""
	}

	// Nav mode + Backspace: edit the displayed item name (remove last char)
	if s.navMode && (key == tcell.KeyBackspace || key == tcell.KeyBackspace2) {
		visible := treeVisibleItems(s)
		if s.treeCursor >= 0 && s.treeCursor < len(visible) && len(visible[s.treeCursor].item.Fields) > 0 {
			name := []rune(visible[s.treeCursor].item.Fields[0])
			if len(name) > 1 {
				s.query = name[:len(name)-1]
				s.cursor = len(s.query)
			} else {
				s.query = nil
				s.cursor = 0
			}
		}
		s.navMode = false
		if len(s.query) > 0 {
			s.searchActive = true
			filterItems(s, cfg, searchCols)
			updateQueryExpansion(s)
			syncTreeCursorToTopMatch(s)
		} else {
			s.searchActive = false
			s.filtered = nil
			s.treeCursor = -1
			s.queryExpanded = make(map[int]bool)
		}
		return ""
	}

	// When no search active, delegate to tree navigation (except printable chars)
	if !s.searchActive {
		if key == tcell.KeyRune {
			// Printable character → activate search
			s.searchActive = true
			s.navMode = false
			s.query = []rune{ch}
			s.cursor = 1
			filterItems(s, cfg, searchCols)
			updateQueryExpansion(s)
			syncTreeCursorToTopMatch(s)
			return ""
		}
		action, _ := handleTreeKey(s, key, ch, cfg, searchCols)
		return action
	}

	// Search active — unified handling
	return handleSearchKey(s, key, ch, cfg, searchCols)
}

// handleKeyEvent processes a single key event against the TUI state.
// Returns "" for normal continuation, "cancel" to quit, or "select:<output>" for leaf selection.
func handleKeyEvent(s *state, key tcell.Key, ch rune, cfg Config, searchCols []int) string {
	switch key {
	case tcell.KeyCtrlC:
		s.cancelled = true
		return "cancel"

	case tcell.KeyEscape:
		if len(s.query) > 0 {
			s.query = nil
			s.cursor = 0
			s.offset = 0
			filterItems(s, cfg, searchCols)
			if len(s.filtered) > 0 {
				s.index = 0
			} else {
				s.index = -1
			}
			return ""
		}
		if cfg.Tiered && len(s.scope) > 1 {
			s.scope = s.scope[:len(s.scope)-1]
			prev := s.scope[len(s.scope)-1]
			if prev.parentIdx < 0 {
				s.items = rootItems(s.allItems)
			} else {
				s.items = childrenOf(s.allItems, prev.parentIdx)
			}
			s.query = prev.query
			s.cursor = prev.cursor
			s.index = prev.index
			s.offset = prev.offset
			filterItems(s, cfg, searchCols)
			return ""
		}
		s.cancelled = true
		return "cancel"

	case tcell.KeyEnter:
		if s.index >= 0 && s.index < len(s.filtered) {
			selected := s.filtered[s.index]
			if selected.HasChildren && cfg.Tiered {
				parentIdx := findInAll(s.allItems, selected)
				if parentIdx >= 0 {
					s.scope[len(s.scope)-1].query = s.query
					s.scope[len(s.scope)-1].cursor = s.cursor
					s.scope[len(s.scope)-1].index = s.index
					s.scope[len(s.scope)-1].offset = s.offset
					s.scope = append(s.scope, scopeLevel{parentIdx: parentIdx})
					s.items = childrenOf(s.allItems, parentIdx)
					s.query = nil
					s.cursor = 0
					s.index = -1
					s.offset = 0
					filterItems(s, cfg, searchCols)
				}
			} else {
				return "select:" + formatOutput(selected, cfg)
			}
		}

	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if s.cursor > 0 {
			s.query = append(s.query[:s.cursor-1], s.query[s.cursor:]...)
			s.cursor--
			s.offset = 0
			filterItems(s, cfg, searchCols)
			if len(s.filtered) > 0 {
				s.index = 0
			} else {
				s.index = -1
			}
		}

	case tcell.KeyDelete:
		if s.cursor < len(s.query) {
			s.query = append(s.query[:s.cursor], s.query[s.cursor+1:]...)
			filterItems(s, cfg, searchCols)
		}

	case tcell.KeyLeft:
		if cfg.Tiered && len(s.query) == 0 && len(s.scope) > 1 {
			s.scope = s.scope[:len(s.scope)-1]
			prev := s.scope[len(s.scope)-1]
			if prev.parentIdx < 0 {
				s.items = rootItems(s.allItems)
			} else {
				s.items = childrenOf(s.allItems, prev.parentIdx)
			}
			s.query = prev.query
			s.cursor = prev.cursor
			s.index = prev.index
			s.offset = prev.offset
			filterItems(s, cfg, searchCols)
		} else if s.index >= 0 {
			s.index = -1
		} else if s.cursor > 0 {
			s.cursor--
		}

	case tcell.KeyRight:
		if s.index >= 0 && cfg.Tiered && len(s.query) == 0 && len(s.filtered) > 0 && s.filtered[s.index].HasChildren {
			selected := s.filtered[s.index]
			parentIdx := findInAll(s.allItems, selected)
			if parentIdx >= 0 {
				s.scope[len(s.scope)-1].query = s.query
				s.scope[len(s.scope)-1].cursor = s.cursor
				s.scope[len(s.scope)-1].index = s.index
				s.scope[len(s.scope)-1].offset = s.offset
				s.scope = append(s.scope, scopeLevel{parentIdx: parentIdx})
				s.items = childrenOf(s.allItems, parentIdx)
				s.query = nil
				s.cursor = 0
				s.index = -1
				s.offset = 0
				filterItems(s, cfg, searchCols)
			}
		} else if s.index == -1 && s.cursor < len(s.query) {
			s.cursor++
		}

	case tcell.KeyTab:
		if len(s.filtered) > 0 {
			if s.index < len(s.filtered)-1 {
				s.index++
			} else {
				s.index = -1
			}
		}

	case tcell.KeyBacktab:
		if len(s.filtered) > 0 {
			if s.index == -1 {
				s.index = len(s.filtered) - 1
			} else if s.index > 0 {
				s.index--
			} else {
				s.index = -1
			}
		}

	case tcell.KeyUp, tcell.KeyCtrlP:
		if s.index > 0 {
			s.index--
		} else if s.index == 0 {
			s.index = -1
		}

	case tcell.KeyDown, tcell.KeyCtrlN:
		if s.index < len(s.filtered)-1 {
			s.index++
		}

	case tcell.KeyCtrlA:
		s.cursor = 0

	case tcell.KeyCtrlE:
		s.cursor = len(s.query)

	case tcell.KeyCtrlU:
		s.query = s.query[s.cursor:]
		s.cursor = 0
		s.offset = 0
		filterItems(s, cfg, searchCols)
		if len(s.filtered) > 0 {
			s.index = 0
		} else {
			s.index = -1
		}

	case tcell.KeyCtrlW:
		if s.cursor > 0 {
			end := s.cursor
			for s.cursor > 0 && s.query[s.cursor-1] == ' ' {
				s.cursor--
			}
			for s.cursor > 0 && s.query[s.cursor-1] != ' ' {
				s.cursor--
			}
			s.query = append(s.query[:s.cursor], s.query[end:]...)
			s.offset = 0
			filterItems(s, cfg, searchCols)
			if len(s.filtered) > 0 {
				s.index = 0
			} else {
				s.index = -1
			}
		}

	case tcell.KeyRune:
		s.query = append(s.query[:s.cursor], append([]rune{ch}, s.query[s.cursor:]...)...)
		s.cursor++
		s.offset = 0
		filterItems(s, cfg, searchCols)
		if len(s.filtered) > 0 {
			s.index = 0
		} else {
			s.index = -1
		}
	}

	return ""
}

// Simulate runs a headless simulation: renders the initial frame, then one frame
// per character of the query. Returns all frames as text snapshots.
// simKey represents a parsed key event from the sim-query string.
type simKey struct {
	key   tcell.Key
	ch    rune
	label string
}

// parseSimQuery parses a sim-query string into key events.
// Supports {up}, {down}, {left}, {right}, {enter}, {tab}, {esc}, {bs}, {space},
// {ctrl+u}, {ctrl+w}. Plain characters are literal key presses.
func parseSimQuery(query string) []simKey {
	var keys []simKey
	runes := []rune(query)
	for i := 0; i < len(runes); i++ {
		if runes[i] == '{' {
			end := -1
			for j := i + 1; j < len(runes); j++ {
				if runes[j] == '}' {
					end = j
					break
				}
			}
			if end > i {
				name := strings.ToLower(string(runes[i+1 : end]))
				var sk simKey
				switch name {
				case "up":
					sk = simKey{key: tcell.KeyUp, label: "Up"}
				case "down":
					sk = simKey{key: tcell.KeyDown, label: "Down"}
				case "left":
					sk = simKey{key: tcell.KeyLeft, label: "Left"}
				case "right":
					sk = simKey{key: tcell.KeyRight, label: "Right"}
				case "enter":
					sk = simKey{key: tcell.KeyEnter, label: "Enter"}
				case "tab":
					sk = simKey{key: tcell.KeyTab, label: "Tab"}
				case "esc":
					sk = simKey{key: tcell.KeyEscape, label: "Esc"}
				case "bs":
					sk = simKey{key: tcell.KeyBackspace2, label: "Backspace"}
				case "space":
					sk = simKey{key: tcell.KeyRune, ch: ' ', label: "Space"}
				case "ctrl+u":
					sk = simKey{key: tcell.KeyCtrlU, label: "Ctrl+U"}
				case "ctrl+w":
					sk = simKey{key: tcell.KeyCtrlW, label: "Ctrl+W"}
				default:
					// Unknown — skip
					i = end
					continue
				}
				keys = append(keys, sk)
				i = end
				continue
			}
		}
		keys = append(keys, simKey{key: tcell.KeyRune, ch: runes[i], label: fmt.Sprintf("'%c'", runes[i])})
	}
	return keys
}

func Simulate(items []model.Item, cfg Config, query string, w, h int, styled bool) []Frame {
	s, searchCols := initState(items, cfg)

	if cfg.TreeMode {
		s.treeExpanded = make(map[int]bool)
		s.queryExpanded = make(map[int]bool)
		s.treeCursor = -1
	}

	var frames []Frame

	renderOne := func() string {
		mem := NewMemScreen(w, h)
		if cfg.TreeMode {
			drawUnified(mem, s, cfg, w, 0, h)
		} else {
			renderFrame(mem, s, cfg)
		}
		if styled {
			return mem.StyledSnapshot()
		}
		return mem.Snapshot()
	}

	// Frame 0: initial state
	frames = append(frames, Frame{Label: "(initial)", Content: renderOne()})

	// One frame per key event
	keys := parseSimQuery(query)
	for _, sk := range keys {
		if cfg.TreeMode {
			handleUnifiedKey(s, sk.key, sk.ch, cfg, searchCols)
		} else {
			handleKeyEvent(s, sk.key, sk.ch, cfg, searchCols)
		}

		label := fmt.Sprintf("key: %s  query: \"%s\"", sk.label, string(s.query))
		frames = append(frames, Frame{Label: label, Content: renderOne()})
	}

	return frames
}

// Frame represents one rendered screen state.
type Frame struct {
	Label   string // description of what triggered this frame
	Content string // text grid snapshot
}

// FormatFrames renders all frames as a single string for file output.
func FormatFrames(frames []Frame) string {
	var b strings.Builder
	for i, f := range frames {
		fmt.Fprintf(&b, "=== Frame %d [%s] ===\n", i, f.Label)
		b.WriteString(f.Content)
		b.WriteString("\n\n")
	}
	return b.String()
}

// getAncestorNames walks up ParentIdx to collect parent folder names.
func getAncestorNames(allItems []model.Item, item model.Item) []string {
	var names []string
	idx := item.ParentIdx
	seen := make(map[int]bool)
	for idx >= 0 && idx < len(allItems) && !seen[idx] {
		seen[idx] = true
		parent := allItems[idx]
		if len(parent.Fields) > 0 {
			names = append(names, parent.Fields[0])
		}
		idx = parent.ParentIdx
	}
	return names
}

func filterItems(s *state, cfg Config, searchCols []int) {
	query := string(s.query)
	if query == "" {
		if cfg.Tiered {
			// Show only the current scope's direct items
			s.filtered = make([]model.Item, len(s.items))
			copy(s.filtered, s.items)
		} else {
			s.filtered = make([]model.Item, len(s.items))
			copy(s.filtered, s.items)
		}
		return
	}

	// When searching in tiered mode, search all descendants under current scope
	searchPool := s.items
	if cfg.Tiered {
		searchPool = descendantsOf(s.allItems, s.scope[len(s.scope)-1].parentIdx)
	}

	var matched []model.Item
	for _, item := range searchPool {
		ancestors := getAncestorNames(s.allItems, item)
		ts, indices := scorer.ScoreItem(item.Fields, query, searchCols, ancestors)
		if indices != nil {
			if cfg.Tiered {
				relativeDepth := item.Depth
				if len(s.scope) > 1 {
					scopeDepth := s.allItems[s.scope[len(s.scope)-1].parentIdx].Depth + 1
					relativeDepth = item.Depth - scopeDepth
				}
				ts.Name -= relativeDepth * cfg.DepthPenalty
			}
			m := item
			m.Score = ts
			m.MatchIndices = indices
			matched = append(matched, m)
		}
	}

	sort.SliceStable(matched, func(i, j int) bool {
		return matched[j].Score.Less(matched[i].Score)
	})
	s.filtered = matched
}

func buildScopePath(s *state) string {
	if len(s.scope) <= 1 {
		return ""
	}
	var parts []string
	for _, level := range s.scope[1:] {
		if level.parentIdx >= 0 && level.parentIdx < len(s.allItems) {
			parts = append(parts, s.allItems[level.parentIdx].Fields[0])
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " › ")
}

func drawItemRow(c Canvas, item model.Item, isSelected bool, isSearching bool, cfg Config, s *state, borderOffset, y, w int) {
	maxW := w - borderOffset*2

	// Selection highlight
	selStyle := tcell.StyleDefault
	if isSelected {
		selStyle = selStyle.Background(tcell.ColorDarkBlue)
	}

	// Fill entire row with background if selected
	if isSelected {
		for fx := borderOffset; fx < w-borderOffset; fx++ {
			c.SetContent(fx, y, ' ', nil, selStyle)
		}
	}

	x := borderOffset

	// Indicator: ▸ for selected, space otherwise
	if isSelected {
		drawText(c, x, y, "▸ ", selStyle.Foreground(tcell.ColorYellow).Bold(true), 2)
	} else {
		drawText(c, x, y, "  ", tcell.StyleDefault, 2)
	}
	x += 2

	// Name field
	if len(item.Fields) > 0 {
		nameStyle := tcell.StyleDefault
		if item.HasChildren {
			nameStyle = nameStyle.Foreground(tcell.ColorDarkCyan).Bold(true)
		}
		if isSelected {
			nameStyle = nameStyle.Background(tcell.ColorDarkBlue)
			if !item.HasChildren {
				nameStyle = nameStyle.Foreground(tcell.ColorWhite)
			}
		}

		var indices []int
		if item.MatchIndices != nil && len(item.MatchIndices) > 0 {
			indices = item.MatchIndices[0]
		}
		var sr []model.StyledRune
		if item.StyledFields != nil && len(item.StyledFields) > 0 {
			sr = item.StyledFields[0]
		}

		name := item.Fields[0]
		// Draw name text with highlighting
		startX := x
		x = drawFieldText(c, x, y, name, sr, indices, nameStyle, isSelected, maxW)
		// Pad name to fixed column width + gap
		padStyle := nameStyle
		targetX := startX + s.nameColWidth + s.colGap
		for x < targetX && x < maxW+borderOffset {
			c.SetContent(x, y, ' ', nil, padStyle)
			x++
		}
	}

	// Icon columns: file (selectable) + folder (drillable)
	// Nerd font icons may render as double-width, so allocate 2 cells each
	if cfg.Tiered {
		bgStyle := tcell.StyleDefault
		if isSelected {
			bgStyle = bgStyle.Background(tcell.ColorDarkBlue)
		}

		// Single icon: folder for containers, file for leaves
		if item.HasChildren {
			c.SetContent(x, y, '\U000F024B', nil, bgStyle.Foreground(tcell.ColorYellow).Bold(true))
		} else {
			c.SetContent(x, y, '\uF15B', nil, bgStyle.Foreground(tcell.ColorDarkGray))
		}
		x++
		c.SetContent(x, y, ' ', nil, bgStyle) // width buffer
		x++
	}

	// Description field (dimmer)
	if len(item.Fields) > 1 {
		descStyle := tcell.StyleDefault
		if isSelected {
			descStyle = descStyle.Background(tcell.ColorDarkBlue)
		}

		var indices []int
		if item.MatchIndices != nil && len(item.MatchIndices) > 1 {
			indices = item.MatchIndices[1]
		}
		var sr []model.StyledRune
		if item.StyledFields != nil && len(item.StyledFields) > 1 {
			sr = item.StyledFields[1]
		}

		x = drawFieldText(c, x, y, item.Fields[1], sr, indices, descStyle, isSelected, maxW)
	}

	// Breadcrumb path when searching nested results
	if isSearching && cfg.Tiered && item.Depth > 0 && item.Path != "" {
		pathStyle := tcell.StyleDefault.Foreground(tcell.ColorDarkGray).Italic(true)
		if isSelected {
			pathStyle = pathStyle.Background(tcell.ColorDarkBlue)
		}
		// Find the parent part of the path (everything before the last ›)
		parentPath := ""
		if lastSep := strings.LastIndex(item.Path, " › "); lastSep >= 0 {
			parentPath = item.Path[:lastSep]
		}
		if parentPath != "" {
			pathStr := "  (" + parentPath + ")"
			drawText(c, x, y, pathStr, pathStyle, maxW-x+borderOffset)
		}
	}

}

func drawReverse(c Canvas, s *state, cfg Config, w, startY, h int) {
	y := startY

	borderOffset := 0
	if cfg.Border {
		drawBorderTopWithTitle(c, w, y, cfg.Title, cfg.TitlePos)
		y++
		borderOffset = 1
	}

	promptStr := cfg.Prompt
	if promptStr == "" {
		promptStr = "> "
	}
	promptLen := len([]rune(promptStr))

	if len(s.query) > 0 {
		// Typing: show query with cursor
		promptStyle := tcell.StyleDefault.Foreground(tcell.ColorYellow).Bold(true)
		drawText(c, borderOffset, y, promptStr, promptStyle, w-borderOffset*2)
		drawText(c, promptLen+borderOffset, y, string(s.query), tcell.StyleDefault, w-promptLen-borderOffset*2)
		c.ShowCursor(promptLen+s.cursor+borderOffset, y)
	} else if s.index >= 0 && s.index < len(s.filtered) {
		// No query, item selected — show item name as preview, dim prompt
		dimPrompt := tcell.StyleDefault.Foreground(tcell.ColorDarkGray)
		drawText(c, borderOffset, y, promptStr, dimPrompt, w-borderOffset*2)
		previewText := s.filtered[s.index].Fields[0]
		drawText(c, promptLen+borderOffset, y, previewText, tcell.StyleDefault.Foreground(tcell.ColorDarkGray).Italic(true), w-promptLen-borderOffset*2)
		c.HideCursor()
	} else {
		promptStyle := tcell.StyleDefault.Foreground(tcell.ColorYellow).Bold(true)
		drawText(c, borderOffset, y, promptStr, promptStyle, w-borderOffset*2)
		c.ShowCursor(promptLen+borderOffset, y)
	}
	y++

	// Breadcrumb trail
	scopePath := buildScopePath(s)
	if scopePath != "" {
		bcStyle := tcell.StyleDefault.Foreground(tcell.ColorDarkCyan)
		sepStyle := tcell.StyleDefault.Foreground(tcell.ColorDarkGray)
		bx := borderOffset + 1
		drawText(c, bx, y, "◂ ", sepStyle, w-borderOffset*2)
		bx += 2
		drawText(c, bx, y, scopePath, bcStyle, w-borderOffset*2-bx)
	}
	y++

	for _, hdr := range s.headers {
		hdrStyle := tcell.StyleDefault.Foreground(tcell.ColorDarkCyan).Bold(true)
		hx := borderOffset + 2
		// Name header
		if len(hdr.Fields) > 0 {
			drawText(c, hx, y, hdr.Fields[0], hdrStyle, w-borderOffset*2-2)
			hx += s.nameColWidth + s.colGap
		}
		// Skip icon column width if tiered (icon + buffer = 2)
		if cfg.Tiered {
			hx += 2
		}
		// Description header
		if len(hdr.Fields) > 1 {
			drawText(c, hx, y, hdr.Fields[1], hdrStyle, w-borderOffset*2-hx)
		}
		y++
	}

	// Divider line between header and items
	if len(s.headers) > 0 {
		divStyle := tcell.StyleDefault.Foreground(tcell.ColorDarkGray)
		for dx := borderOffset + 1; dx < w-borderOffset-1; dx++ {
			c.SetContent(dx, y, '─', nil, divStyle)
		}
		y++
	}

	itemLines := startY + h - y
	if cfg.Border {
		itemLines--
	}
	if itemLines < 0 {
		itemLines = 0
	}

	if s.index >= 0 {
		if s.index < s.offset {
			s.offset = s.index
		}
		if s.index >= s.offset+itemLines {
			s.offset = s.index - itemLines + 1
		}
	} else {
		s.offset = 0
	}

	isSearching := len(s.query) > 0

	for i := 0; i < itemLines && i+s.offset < len(s.filtered); i++ {
		idx := i + s.offset
		item := s.filtered[idx]
		isSelected := idx == s.index
		drawItemRow(c, item, isSelected, isSearching, cfg, s, borderOffset, y+i, w)
	}

	if cfg.Border {
		drawBorderSides(c, w, startY, startY+h-1)
		drawBorderBottom(c, w, startY+h-1)
	}
}

func drawDefault(c Canvas, s *state, cfg Config, w, startY, h int) {
	y := startY

	borderOffset := 0
	if cfg.Border {
		drawBorderTopWithTitle(c, w, y, cfg.Title, cfg.TitlePos)
		y++
		borderOffset = 1
	}

	for _, hdr := range s.headers {
		hdrStyle := tcell.StyleDefault.Foreground(tcell.ColorDarkCyan).Bold(true)
		hx := borderOffset + 2
		// Name header
		if len(hdr.Fields) > 0 {
			drawText(c, hx, y, hdr.Fields[0], hdrStyle, w-borderOffset*2-2)
			hx += s.nameColWidth + s.colGap
		}
		// Skip icon column width if tiered (icon + buffer = 2)
		if cfg.Tiered {
			hx += 2
		}
		// Description header
		if len(hdr.Fields) > 1 {
			drawText(c, hx, y, hdr.Fields[1], hdrStyle, w-borderOffset*2-hx)
		}
		y++
	}

	// Divider line between header and items
	if len(s.headers) > 0 {
		divStyle := tcell.StyleDefault.Foreground(tcell.ColorDarkGray)
		for dx := borderOffset + 1; dx < w-borderOffset-1; dx++ {
			c.SetContent(dx, y, '─', nil, divStyle)
		}
		y++
	}

	promptLines := 2
	itemLines := startY + h - y - promptLines
	if cfg.Border {
		itemLines--
	}
	if itemLines < 0 {
		itemLines = 0
	}

	if s.index >= 0 {
		if s.index < s.offset {
			s.offset = s.index
		}
		if s.index >= s.offset+itemLines {
			s.offset = s.index - itemLines + 1
		}
	} else {
		s.offset = 0
	}

	isSearching := len(s.query) > 0

	for i := 0; i < itemLines && i+s.offset < len(s.filtered); i++ {
		idx := i + s.offset
		item := s.filtered[idx]
		isSelected := idx == s.index
		drawItemRow(c, item, isSelected, isSearching, cfg, s, borderOffset, y+i, w)
	}

	bottomY := startY + h - promptLines
	if cfg.Border {
		bottomY--
	}

	scopePath := buildScopePath(s)
	if scopePath != "" {
		bcStyle := tcell.StyleDefault.Foreground(tcell.ColorDarkCyan)
		sepStyle := tcell.StyleDefault.Foreground(tcell.ColorDarkGray)
		bx := borderOffset + 1
		drawText(c, bx, bottomY, "◂ ", sepStyle, w-borderOffset*2)
		bx += 2
		drawText(c, bx, bottomY, scopePath, bcStyle, w-borderOffset*2-bx)
	}

	promptStr := cfg.Prompt
	if promptStr == "" {
		promptStr = "> "
	}
	promptLen := len([]rune(promptStr))

	if len(s.query) > 0 {
		promptStyle := tcell.StyleDefault.Foreground(tcell.ColorYellow).Bold(true)
		drawText(c, borderOffset, bottomY+1, promptStr, promptStyle, w-borderOffset*2)
		drawText(c, promptLen+borderOffset, bottomY+1, string(s.query), tcell.StyleDefault, w-promptLen-borderOffset*2)
		c.ShowCursor(promptLen+s.cursor+borderOffset, bottomY+1)
	} else if s.index >= 0 && s.index < len(s.filtered) {
		dimPrompt := tcell.StyleDefault.Foreground(tcell.ColorDarkGray)
		drawText(c, borderOffset, bottomY+1, promptStr, dimPrompt, w-borderOffset*2)
		previewText := s.filtered[s.index].Fields[0]
		drawText(c, promptLen+borderOffset, bottomY+1, previewText, tcell.StyleDefault.Foreground(tcell.ColorDarkGray).Italic(true), w-promptLen-borderOffset*2)
		c.HideCursor()
	} else {
		promptStyle := tcell.StyleDefault.Foreground(tcell.ColorYellow).Bold(true)
		drawText(c, borderOffset, bottomY+1, promptStr, promptStyle, w-borderOffset*2)
		c.ShowCursor(promptLen+borderOffset, bottomY+1)
	}

	if cfg.Border {
		drawBorderSides(c, w, startY, startY+h-1)
		drawBorderBottom(c, w, startY+h-1)
	}
}

// drawFieldText draws text with optional ANSI styles and match highlighting. No column padding.
func drawFieldText(c Canvas, x, y int, field string, styledRunes []model.StyledRune, indices []int, baseStyle tcell.Style, isSelected bool, maxW int) int {
	runes := []rune(field)
	indexSet := make(map[int]bool)
	for _, idx := range indices {
		indexSet[idx] = true
	}

	hlStyle := baseStyle.Foreground(tcell.ColorGreen).Bold(true)
	if isSelected {
		hlStyle = hlStyle.Background(tcell.ColorDarkBlue)
	}

	for i, r := range runes {
		if x >= maxW {
			break
		}
		style := baseStyle
		if styledRunes != nil && i < len(styledRunes) {
			style = styledRunes[i].Style
			if isSelected {
				fg, _, attrs := style.Decompose()
				style = tcell.StyleDefault.Background(tcell.ColorDarkBlue).Foreground(fg).Attributes(attrs)
			}
		}
		if indexSet[i] {
			style = hlStyle
		}
		c.SetContent(x, y, r, nil, style)
		x++
	}
	return x
}

func drawHighlightedField(c Canvas, x, y int, field string, styledRunes []model.StyledRune, indices []int, baseStyle tcell.Style, isSelected bool, widths []int, fieldIdx, gap, maxW int) int {
	runes := []rune(field)
	indexSet := make(map[int]bool)
	for _, idx := range indices {
		indexSet[idx] = true
	}

	for i, r := range runes {
		if x >= maxW {
			break
		}

		style := baseStyle

		// Layer 1: Apply ANSI color if available
		if styledRunes != nil && i < len(styledRunes) {
			style = styledRunes[i].Style
			// If this row is selected, override the background but keep the foreground color
			if isSelected {
				fg, _, attrs := style.Decompose()
				style = tcell.StyleDefault.Background(tcell.ColorDarkBlue).Foreground(fg).Attributes(attrs)
			}
		}

		// Layer 2: Override with match highlight
		if indexSet[i] {
			if isSelected {
				style = style.Foreground(tcell.ColorGreen).Bold(true).Background(tcell.ColorDarkBlue)
			} else {
				style = style.Foreground(tcell.ColorGreen).Bold(true)
			}
		}

		c.SetContent(x, y, r, nil, style)
		x++
	}

	if fieldIdx < len(widths)-1 {
		padTo := widths[fieldIdx]
		for len(runes) < padTo {
			if x >= maxW {
				break
			}
			c.SetContent(x, y, ' ', nil, baseStyle)
			x++
			runes = append(runes, ' ')
		}
		for g := 0; g < gap; g++ {
			if x >= maxW {
				break
			}
			c.SetContent(x, y, ' ', nil, baseStyle)
			x++
		}
	}

	return x
}

func drawText(c Canvas, x, y int, text string, style tcell.Style, maxW int) {
	for i, r := range text {
		if i >= maxW {
			break
		}
		c.SetContent(x+i, y, r, nil, style)
	}
}

func drawBorderTop(c Canvas, w, y int) {
	drawBorderTopWithTitle(c, w, y, "", "")
}

func drawBorderTopWithTitle(c Canvas, w, y int, title, pos string) {
	borderStyle := tcell.StyleDefault.Foreground(tcell.ColorDarkGray)
	c.SetContent(0, y, '┌', nil, borderStyle)
	for x := 1; x < w-1; x++ {
		c.SetContent(x, y, '─', nil, borderStyle)
	}
	c.SetContent(w-1, y, '┐', nil, borderStyle)

	if title != "" {
		titleRunes := []rune(title)
		maxTitle := w - 6 // leave room for corners + at least one ─ + spaces on each side
		if maxTitle < 1 {
			return
		}
		if len(titleRunes) > maxTitle {
			titleRunes = titleRunes[:maxTitle]
		}
		var startX int
		switch pos {
		case "center":
			startX = (w - len(titleRunes) - 2) / 2
		case "right":
			startX = w - len(titleRunes) - 3 // 1 corner + 1 ─ minimum on right, plus space pad
		default: // "left"
			startX = 2
		}
		if startX < 2 {
			startX = 2
		}
		titleStyle := tcell.StyleDefault.Foreground(tcell.ColorDarkCyan).Bold(true)
		c.SetContent(startX, y, ' ', nil, borderStyle)
		for i, r := range titleRunes {
			c.SetContent(startX+1+i, y, r, nil, titleStyle)
		}
		c.SetContent(startX+1+len(titleRunes), y, ' ', nil, borderStyle)
	}
}

func drawBorderBottom(c Canvas, w, y int) {
	style := tcell.StyleDefault.Foreground(tcell.ColorDarkGray)
	c.SetContent(0, y, '└', nil, style)
	for x := 1; x < w-1; x++ {
		c.SetContent(x, y, '─', nil, style)
	}
	c.SetContent(w-1, y, '┘', nil, style)
}

func drawBorderSides(c Canvas, w, topY, bottomY int) {
	style := tcell.StyleDefault.Foreground(tcell.ColorDarkGray)
	for y := topY + 1; y < bottomY; y++ {
		c.SetContent(0, y, '│', nil, style)
		c.SetContent(w-1, y, '│', nil, style)
	}
}

func formatOutput(item model.Item, cfg Config) string {
	if len(cfg.AcceptNth) > 0 {
		// Use clean fields for output (ANSI stripped) so downstream consumers get plain text
		var parts []string
		for _, col := range cfg.AcceptNth {
			idx := col - 1
			if idx >= 0 && idx < len(item.Fields) {
				parts = append(parts, item.Fields[idx])
			}
		}
		return strings.Join(parts, "\t")
	}
	// No accept-nth: return the original line (preserves ANSI for piping)
	if item.Original != "" {
		return item.Original
	}
	return strings.Join(item.Fields, "\t")
}

// RunFilter runs in non-interactive mode (like fzf --filter).
func RunFilter(items []model.Item, query string, cfg Config) {
	searchCols := cfg.SearchCols
	if len(searchCols) == 0 {
		searchCols = cfg.Nth
	}

	var matched []model.Item
	for _, item := range items {
		ancestors := getAncestorNames(items, item)
		ts, indices := scorer.ScoreItem(item.Fields, query, searchCols, ancestors)
		if indices != nil {
			if cfg.Tiered {
				ts.Name -= item.Depth * cfg.DepthPenalty
			}
			m := item
			m.Score = ts
			m.MatchIndices = indices
			matched = append(matched, m)
		}
	}

	sort.SliceStable(matched, func(i, j int) bool {
		return matched[j].Score.Less(matched[i].Score)
	})

	for _, item := range matched {
		if cfg.ShowScores {
			fmt.Fprintf(os.Stdout, "[score=N:%d D:%d A:%d] %s\n", item.Score.Name, item.Score.Desc, item.Score.Ancestor, formatOutput(item, cfg))
		} else {
			fmt.Fprintln(os.Stdout, formatOutput(item, cfg))
		}
	}
}
