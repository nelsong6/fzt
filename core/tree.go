package core

import (
	"path/filepath"
	"sort"
	"strings"
)

// ContextKind distinguishes the root context from a command palette context.
type ContextKind int

const (
	ContextNormal  ContextKind = iota
	ContextCommand
)

// ScopeLevel saves navigation state when entering a folder.
type ScopeLevel struct {
	ParentIdx   int // index into AllItems (-1 for root)
	Query       []rune
	Cursor      int
	Index       int
	Offset      int
	WasExpanded bool // true if folder was already expanded before pushScope
}

// TreeContext holds all dataset, query, and tree navigation state for one
// level of the context stack.
type TreeContext struct {
	// Dataset
	AllItems     []Item
	Items        []Item
	Filtered     []Item
	Headers      []Item
	Widths       []int
	NameColWidth int
	ColGap       int

	// Query
	Query  []rune
	Cursor int

	// Flat-mode selection
	Index  int // selected item index in filtered list
	Offset int // scroll offset

	// Tree navigation
	TreeExpanded  map[int]bool
	TreeCursor    int
	TreeOffset    int
	SearchActive  bool
	NavMode       bool
	QueryExpanded map[int]bool

	// Scope (within this context)
	Scope []ScopeLevel

	// Context identity
	Kind         ContextKind
	OnLeafSelect func(item Item) string
	PromptIcon   rune // 0 = default (search/nav), ':' for commands
}

// CommandItem describes a frontend-registered command for the `:` palette.
type CommandItem struct {
	Name        string // display name (e.g. "edit", "copy yaml")
	Description string // short description shown beside the name
	Action      string // action string returned on selection (e.g. "edit", "copy-yaml")
}

// State holds the context stack and global flags.
type State struct {
	Contexts         []TreeContext
	Cancelled        bool
	ShowVersion      bool
	Provider         TreeProvider  // optional: loads children on demand for lazy tree modes
	FrontendCommands []CommandItem // registered by the frontend; shown at top level of `:` palette
}

// TopCtx returns a pointer to the top of the context stack.
func (s *State) TopCtx() *TreeContext { return &s.Contexts[len(s.Contexts)-1] }

// PushContext pushes a new context onto the stack.
func (s *State) PushContext(ctx TreeContext) {
	s.Contexts = append(s.Contexts, ctx)
}

// PopContext pops the top context, keeping at least one.
func (s *State) PopContext() {
	if len(s.Contexts) <= 1 {
		return
	}
	s.Contexts = s.Contexts[:len(s.Contexts)-1]
}

// TreeRow represents a single visible row in the tree view.
type TreeRow struct {
	Item    Item
	ItemIdx int // index in AllItems
}

// NewState creates the initial state from items and config.
// Returns the state and the resolved search columns.
func NewState(items []Item, cfg Config) (*State, []int) {
	var headers []Item
	data := items
	if cfg.HeaderLines > 0 && cfg.HeaderLines <= len(items) {
		headers = items[:cfg.HeaderLines]
		data = items[cfg.HeaderLines:]
	}

	allWidths := ComputeWidths(items)

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

	rootItems := data
	if cfg.Tiered {
		rootItems = RootItemsOf(data)
	}

	rootCtx := TreeContext{
		AllItems:     data,
		Items:        rootItems,
		Headers:      headers,
		Widths:       allWidths,
		NameColWidth: nameColW,
		ColGap:       2,
		Index:        -1,
		Scope:        []ScopeLevel{{ParentIdx: -1}},
		Kind:         ContextNormal,
	}

	s := &State{
		Contexts: []TreeContext{rootCtx},
	}

	searchCols := cfg.SearchCols
	if len(searchCols) == 0 {
		searchCols = cfg.Nth
	}
	FilterItems(s, cfg, searchCols)

	return s, searchCols
}

// FindInAll finds the index of an item in allItems by matching Fields[0] and Depth.
func FindInAll(allItems []Item, item Item) int {
	for i, ai := range allItems {
		if ai.Depth == item.Depth && len(ai.Fields) > 0 && len(item.Fields) > 0 && ai.Fields[0] == item.Fields[0] {
			return i
		}
	}
	return -1
}

// RootItemsOf returns only depth-0 items.
func RootItemsOf(items []Item) []Item {
	var out []Item
	for _, item := range items {
		if item.Depth == 0 {
			out = append(out, item)
		}
	}
	return out
}

// DescendantsOf returns all items under a given parent (or all items if parentIdx is -1).
func DescendantsOf(allItems []Item, parentIdx int) []Item {
	if parentIdx < 0 {
		return allItems
	}
	var out []Item
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

// ChildrenOf returns the direct children of the item at parentIdx in allItems.
func ChildrenOf(allItems []Item, parentIdx int) []Item {
	parent := allItems[parentIdx]
	var out []Item
	for _, childIdx := range parent.Children {
		if childIdx < len(allItems) {
			out = append(out, allItems[childIdx])
		}
	}
	return out
}

// TreeVisibleItems builds the list of currently visible tree rows.
func TreeVisibleItems(s *State) []TreeRow {
	var rows []TreeRow
	buildVisibleTree(s, -1, &rows)
	return rows
}

func buildVisibleTree(s *State, parentIdx int, rows *[]TreeRow) {
	ctx := s.TopCtx()
	var children []int
	if parentIdx < 0 {
		for i, item := range ctx.AllItems {
			if item.Depth == 0 {
				children = append(children, i)
			}
		}
	} else {
		children = ctx.AllItems[parentIdx].Children
	}

	for _, idx := range children {
		if idx >= len(ctx.AllItems) {
			continue
		}
		*rows = append(*rows, TreeRow{Item: ctx.AllItems[idx], ItemIdx: idx})
		expanded := ctx.TreeExpanded[idx] || ctx.QueryExpanded[idx]
		if ctx.AllItems[idx].HasChildren && expanded {
			buildVisibleTree(s, idx, rows)
		}
	}
}

// UpdateQueryExpansion sets auto-expansion to reveal the top match in the tree.
func UpdateQueryExpansion(s *State) {
	ctx := s.TopCtx()
	ctx.QueryExpanded = make(map[int]bool)
	if len(ctx.Filtered) == 0 {
		return
	}
	topMatch := ctx.Filtered[0]
	idx := FindInAll(ctx.AllItems, topMatch)
	if idx < 0 {
		return
	}
	for {
		parentIdx := ctx.AllItems[idx].ParentIdx
		if parentIdx < 0 {
			break
		}
		ctx.QueryExpanded[parentIdx] = true
		idx = parentIdx
	}
}

// SyncTreeCursorToTopMatch moves the tree cursor to the top match position.
func SyncTreeCursorToTopMatch(s *State) {
	ctx := s.TopCtx()
	if len(ctx.Filtered) == 0 {
		return
	}
	topIdx := FindInAll(ctx.AllItems, ctx.Filtered[0])
	if topIdx < 0 {
		return
	}
	visible := TreeVisibleItems(s)
	for vi, row := range visible {
		if row.ItemIdx == topIdx {
			ctx.TreeCursor = vi
			return
		}
	}
}

// PushScope enters a folder, expanding it in the tree.
// If the folder has no children and a Provider is set, children are loaded dynamically.
func PushScope(s *State, itemIdx int, cfg Config, searchCols []int) {
	ctx := s.TopCtx()
	cur := &ctx.Scope[len(ctx.Scope)-1]
	cur.Query = ctx.Query
	cur.Cursor = ctx.Cursor
	cur.Index = ctx.TreeCursor
	cur.Offset = ctx.TreeOffset

	// Lazy load: if folder has no children yet and a provider exists, load them
	if len(ctx.AllItems[itemIdx].Children) == 0 && s.Provider != nil {
		parentPath := itemFullPath(ctx, itemIdx)
		newItems := s.Provider.LoadChildren(parentPath)
		spliceChildren(ctx, itemIdx, newItems)
	}

	ctx.Scope = append(ctx.Scope, ScopeLevel{
		ParentIdx:   itemIdx,
		WasExpanded: ctx.TreeExpanded[itemIdx],
	})
	ctx.Items = ChildrenOf(ctx.AllItems, itemIdx)

	ctx.TreeExpanded[itemIdx] = true

	ctx.SearchActive = true
	ctx.Query = nil
	ctx.Cursor = 0
	ctx.QueryExpanded = make(map[int]bool)
	FilterItems(s, cfg, searchCols)
}

// itemFullPath builds the filesystem path for a tree item by walking up the ParentIdx chain.
func itemFullPath(ctx *TreeContext, itemIdx int) string {
	var parts []string
	idx := itemIdx
	for idx >= 0 && idx < len(ctx.AllItems) {
		item := ctx.AllItems[idx]
		if len(item.Fields) > 0 {
			parts = append([]string{item.Fields[0]}, parts...)
		}
		idx = item.ParentIdx
	}
	if len(parts) == 0 {
		return ""
	}
	// Reconstruct path: first part is drive (e.g. "C:"), rest are folders
	if len(parts) == 1 {
		return parts[0] + string(filepath.Separator)
	}
	return filepath.Join(parts...)
}

// spliceChildren adds provider-loaded items as children of the given parent.
func spliceChildren(ctx *TreeContext, parentIdx int, newItems []Item) {
	parentDepth := ctx.AllItems[parentIdx].Depth
	baseIdx := len(ctx.AllItems)
	for i := range newItems {
		newItems[i].ParentIdx = parentIdx
		newItems[i].Depth = parentDepth + 1
		ctx.AllItems[parentIdx].Children = append(ctx.AllItems[parentIdx].Children, baseIdx+i)
	}
	ctx.AllItems = append(ctx.AllItems, newItems...)
}

// PopScope exits the current folder scope, returning to the parent.
func PopScope(s *State, cfg Config, searchCols []int) {
	ctx := s.TopCtx()
	if len(ctx.Scope) <= 1 {
		return
	}
	popped := ctx.Scope[len(ctx.Scope)-1]
	ctx.Scope = ctx.Scope[:len(ctx.Scope)-1]
	prev := ctx.Scope[len(ctx.Scope)-1]

	if !popped.WasExpanded && popped.ParentIdx >= 0 {
		delete(ctx.TreeExpanded, popped.ParentIdx)
	}

	if prev.ParentIdx < 0 {
		ctx.Items = RootItemsOf(ctx.AllItems)
	} else {
		ctx.Items = ChildrenOf(ctx.AllItems, prev.ParentIdx)
	}

	ctx.Query = prev.Query
	ctx.Cursor = prev.Cursor
	ctx.TreeCursor = prev.Index
	ctx.TreeOffset = prev.Offset

	if len(ctx.Scope) <= 1 && len(ctx.Query) == 0 {
		ctx.SearchActive = false
		ctx.Filtered = nil
		ctx.TreeCursor = -1
		ctx.QueryExpanded = make(map[int]bool)
	} else {
		FilterItems(s, cfg, searchCols)
		UpdateQueryExpansion(s)
	}
}

// GetAncestorNames returns the names of all ancestors via the ParentIdx chain.
func GetAncestorNames(allItems []Item, item Item) []string {
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

// FilterItems applies the current query to the state's items.
func FilterItems(s *State, cfg Config, searchCols []int) {
	ctx := s.TopCtx()
	query := string(ctx.Query)
	if query == "" {
		ctx.Filtered = make([]Item, len(ctx.Items))
		copy(ctx.Filtered, ctx.Items)
		return
	}

	searchPool := ctx.Items
	if cfg.Tiered {
		searchPool = DescendantsOf(ctx.AllItems, ctx.Scope[len(ctx.Scope)-1].ParentIdx)
	}

	var matched []Item
	for _, item := range searchPool {
		ancestors := GetAncestorNames(ctx.AllItems, item)
		ts, indices := ScoreItem(item.Fields, query, searchCols, ancestors)
		if indices != nil {
			if cfg.Tiered {
				relativeDepth := item.Depth
				if len(ctx.Scope) > 1 {
					scopeDepth := ctx.AllItems[ctx.Scope[len(ctx.Scope)-1].ParentIdx].Depth + 1
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
	ctx.Filtered = matched
}

// BuildScopePath returns the breadcrumb path for the current scope.
func BuildScopePath(s *State) string {
	ctx := s.TopCtx()
	if len(ctx.Scope) <= 1 {
		return ""
	}
	var parts []string
	for _, level := range ctx.Scope[1:] {
		if level.ParentIdx >= 0 && level.ParentIdx < len(ctx.AllItems) {
			parts = append(parts, ctx.AllItems[level.ParentIdx].Fields[0])
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " › ")
}
