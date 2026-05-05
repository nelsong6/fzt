package core

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ContextKind distinguishes the root context from a command palette context.
type ContextKind int

const (
	ContextNormal ContextKind = iota
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
// Populated by the ecosystem layer (fzt-terminal), not the engine.
type CommandItem struct {
	Name        string
	Description string
	Action      string
	Children    []CommandItem
}

// State holds the context stack and global flags.
type State struct {
	Contexts         []TreeContext // context stack. Index 0 = primary dataset. Command palette pushes on top. TopCtx() returns active (top) context.
	Cancelled        bool          // set by Escape from root — signals render loop to exit
	VersionRegistry  []string      // ordered version strings (frontend + engine). Consumed by the `version` palette leaves. Built by InjectCommandFolder.
	TitleOverride    string        // when non-empty, replaces the default title in the border. Used as a console output line.
	TitleStyle       int           // 0=default (cyan/bold), 1=success (green), 2=error (red), 3=neutral (slate), 4=nav-mode (matches prompt NavModeFg), 5=search-mode (matches prompt SearchModeFg). Controls TitleOverride color.
	Provider         TreeProvider  // optional lazy-loader. PushScope calls LoadChildren when entering a folder with no children.
	FrontendCommands []CommandItem // commands for the first level of the : palette. Set by ApplyConfig from Config.FrontendCommands.
	FrontendName     string        // frontend identifier (e.g. "automate"). Drives scope title ("automate ctl" vs "fzt ctl").
	FrontendVersion  string        // frontend version string. Registered at index 0 of VersionRegistry.
	HidePalette      bool          // suppress the visible `:` root row (commands stay reachable by typing `:`). Set from Config.HidePalette.
	IdentityLabel    string        // loaded identity (e.g. "nelson"). Emitted as a one-shot status by the :whoami palette leaf.
	SyncIcon         string        // non-empty = show icon in top-right corner of border (e.g. "⟳" when sync available)
	SyncNextCheck    int64         // unix timestamp — when the next background sync check fires (0 = disabled)
	SyncTimerShown   bool          // true = show countdown to next sync check in the title bar
	JWTSecret        string        // JWT signing secret from OS credential store, set by validate command
	ConfigDir        string        // directory containing sync state files (.identity, identities.json, cache)
	EditMode         string        // active edit action: "add-after", "add-folder", "rename", "delete", "inspect", "" = none
	EditBuffer       []rune        // text buffer for rename mode
	EditTargetIdx    int           // item index being renamed (for restoring on cancel)
	EditOrigName     string        // original name before rename (for restoring on cancel)
	Dirty            bool          // unsaved changes exist
	MenuVersion      int           // last known version from API, used for conflict detection on save
	InspectTargetIdx int           // item being inspected (-1 = none)
	InspectItemIdxs  []int         // indices of temporary property items in AllItems
	EnvTags          []string      // environment capabilities for display condition filtering

	// PromptMode temporarily takes over the prompt text box for a frontend-owned
	// action without disturbing tree query/scope/cursor state.
	PromptMode        string
	PromptAction      string
	PromptIcon        rune
	PromptPlaceholder string
	PromptQuery       []rune
	PromptCursor      int

	// State inspector — passive-explorer mode for discovering reachable states.
	// When StatesBannerOn is true, frontends render Describe() as a banner and
	// suppress exit-on-select for items with actions (the action is stashed in
	// LastActionPreview instead). Navigation, scope, and mode transitions are
	// unaffected so every state remains reachable.
	StatesBannerOn    bool   // true = show state banner, suppress action execution
	LastActionPreview string // most recent "would execute: ..." snapshot

	// PulseUntil is a unix-millis deadline during which the title bar should
	// render with reverse-video styling. Bumped on every SetTitle so repeated
	// identical status messages still produce visible feedback (a pulse the
	// user can see even when content is unchanged).
	PulseUntil int64

	// Self-update target (mirrored from Config by ApplyConfig). fzt-terminal's
	// RunUpdate reads these. Empty = fzt defaults.
	UpdateRepo        string
	UpdateAssetPrefix string
	UpdateBinaryName  string
}

// PulseDurationMs is how long each title pulse lasts.
const PulseDurationMs = 350

// TopCtx returns a pointer to the top of the context stack.
func (s *State) TopCtx() *TreeContext { return &s.Contexts[len(s.Contexts)-1] }

// SetTitle sets a title bar message, evicting any ambient display (timer, etc).
// Pulses the title only when the new (msg, style) pair is a pure repeat of
// what's already showing — distinct messages are self-evidently new and don't
// need the visual flash. Repeat-case pulse is the important one since it's
// the only time there'd be no other visible change.
func (s *State) SetTitle(msg string, style int) {
	isRepeat := s.TitleOverride == msg && s.TitleStyle == style
	s.TitleOverride = msg
	s.TitleStyle = style
	s.SyncTimerShown = false
	if isRepeat {
		s.PulseUntil = time.Now().UnixMilli() + PulseDurationMs
	}
}

// IsPulsing reports whether the title is currently in its post-SetTitle pulse
// window. Frontends call this each frame and render reverse video if true.
func (s *State) IsPulsing() bool {
	return time.Now().UnixMilli() < s.PulseUntil
}

func (s *State) EnterPromptMode(mode, action string, icon rune, placeholder string) {
	s.PromptMode = mode
	s.PromptAction = action
	s.PromptIcon = icon
	s.PromptPlaceholder = placeholder
	s.PromptQuery = nil
	s.PromptCursor = 0
}

func (s *State) ExitPromptMode() {
	s.PromptMode = ""
	s.PromptAction = ""
	s.PromptIcon = 0
	s.PromptPlaceholder = ""
	s.PromptQuery = nil
	s.PromptCursor = 0
}

// ClearTitle removes the title override and any ambient display.
func (s *State) ClearTitle() {
	s.TitleOverride = ""
	s.TitleStyle = 0
	s.SyncTimerShown = false
}

// Describe returns a one-line snapshot of the user-visible state. Used by
// the states-inspector banner — frontends render this verbatim. Fields are
// space-separated key=value; unset/zero fields are omitted so the line stays
// short.
func (s *State) Describe() string {
	ctx := s.TopCtx()
	parts := []string{}
	kind := "normal"
	if ctx.Kind == ContextCommand {
		kind = "palette"
	}
	parts = append(parts, "ctx="+kind)
	if d := len(ctx.Scope); d > 0 {
		parts = append(parts, "scope="+strconv.Itoa(d))
	}
	mode := "tree"
	if ctx.SearchActive {
		mode = "search"
	} else if ctx.NavMode {
		mode = "nav"
	}
	parts = append(parts, "mode="+mode)
	if len(ctx.Query) > 0 {
		parts = append(parts, "query=\""+string(ctx.Query)+"\"")
	}
	if s.EditMode != "" {
		parts = append(parts, "edit="+s.EditMode)
	}
	if s.InspectTargetIdx >= 0 {
		parts = append(parts, "inspect="+strconv.Itoa(s.InspectTargetIdx))
	}
	if s.Dirty {
		parts = append(parts, "dirty")
	}
	if s.LastActionPreview != "" {
		parts = append(parts, "last="+s.LastActionPreview)
	}
	return strings.Join(parts, " ")
}

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
		Contexts:         []TreeContext{rootCtx},
		InspectTargetIdx: -1,
		EditTargetIdx:    -1,
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

// DescendantsOfDepth returns items under parentIdx, limited to maxDepth levels.
func DescendantsOfDepth(allItems []Item, parentIdx int, maxDepth int) []Item {
	if parentIdx < 0 {
		// From root: collect items within maxDepth levels
		var out []Item
		for _, item := range allItems {
			if item.Depth < maxDepth {
				out = append(out, item)
			}
		}
		return out
	}
	parentDepth := allItems[parentIdx].Depth
	var out []Item
	var collect func(idx int, depth int)
	collect = func(idx int, depth int) {
		if depth > maxDepth {
			return
		}
		for _, childIdx := range allItems[idx].Children {
			if childIdx < len(allItems) {
				out = append(out, allItems[childIdx])
				if depth < maxDepth {
					collect(childIdx, depth+1)
				}
			}
		}
	}
	_ = parentDepth
	collect(parentIdx, 1)
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
// If scoped into a hidden folder, starts from that folder's children
// instead of root (exclusive "takeover" view).
func TreeVisibleItems(s *State) []TreeRow {
	ctx := s.TopCtx()
	// Check if any scope level is a hidden folder — if so, render from there
	startIdx := -1
	for _, level := range ctx.Scope[1:] {
		if level.ParentIdx >= 0 && level.ParentIdx < len(ctx.AllItems) {
			if ctx.AllItems[level.ParentIdx].Hidden {
				startIdx = level.ParentIdx
				break // use the outermost hidden scope
			}
		}
	}
	var rows []TreeRow
	if startIdx >= 0 {
		// Exclusive view: only show children of the hidden folder
		buildVisibleTree(s, startIdx, &rows)
	} else {
		buildVisibleTree(s, -1, &rows)
	}
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
		item := ctx.AllItems[idx]
		if item.Hidden {
			// Never render hidden items as rows, but recurse into children
			// if the folder is expanded or in the scope chain
			inScope := false
			for _, level := range ctx.Scope {
				if level.ParentIdx == idx {
					inScope = true
					break
				}
			}
			expanded := ctx.TreeExpanded[idx] || ctx.QueryExpanded[idx] || inScope
			if item.HasChildren && expanded {
				buildVisibleTree(s, idx, rows)
			}
			continue
		}
		*rows = append(*rows, TreeRow{Item: item, ItemIdx: idx})
		expanded := ctx.TreeExpanded[idx] || ctx.QueryExpanded[idx]
		if item.HasChildren && expanded {
			buildVisibleTree(s, idx, rows)
		}
	}
}

// isVisibleHidden returns true if a hidden item should still be shown —
// either because it matches the current search or it's in the active scope chain.
func isVisibleHidden(ctx *TreeContext, idx int) bool {
	// Check if in scope chain (folder was entered)
	for _, level := range ctx.Scope {
		if level.ParentIdx == idx {
			return true
		}
	}
	// Check if it matches the current filtered results
	for _, item := range ctx.Filtered {
		if FindInAll(ctx.AllItems, item) == idx {
			return true
		}
	}
	return false
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
// If the top match is hidden (not in visible rows), resets cursor to -1
// so the "act on top match" fallback fires on Enter.
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
	// Top match not in visible tree (hidden item) — clear cursor
	ctx.TreeCursor = -1
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
		parentPath := ItemFullPath(ctx, itemIdx)
		newItems := s.Provider.LoadChildren(parentPath)
		SpliceChildren(ctx, itemIdx, newItems)
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

// ItemFullPath builds the filesystem path for a tree item by walking up the ParentIdx chain.
func ItemFullPath(ctx *TreeContext, itemIdx int) string {
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
	// Ensure drive letter has trailing separator (filepath.Join treats "C:" as relative)
	if len(parts[0]) == 2 && parts[0][1] == ':' {
		parts[0] += string(filepath.Separator)
	}
	return filepath.Join(parts...)
}

// SpliceChildren adds provider-loaded items as children of the given parent.
func SpliceChildren(ctx *TreeContext, parentIdx int, newItems []Item) {
	parentDepth := ctx.AllItems[parentIdx].Depth
	baseIdx := len(ctx.AllItems)
	for i := range newItems {
		newItems[i].ParentIdx = parentIdx
		newItems[i].Depth = parentDepth + 1
		ctx.AllItems[parentIdx].Children = append(ctx.AllItems[parentIdx].Children, baseIdx+i)
	}
	ctx.AllItems = append(ctx.AllItems, newItems...)
}

// AddItemAfter appends a new item to AllItems as a sibling after cursorItemIdx.
// Returns the index of the new item in AllItems.
func AddItemAfter(ctx *TreeContext, cursorItemIdx int, newItem Item) int {
	if cursorItemIdx < 0 || cursorItemIdx >= len(ctx.AllItems) {
		return -1
	}
	cursor := ctx.AllItems[cursorItemIdx]
	parentIdx := cursor.ParentIdx
	newItem.ParentIdx = parentIdx
	newItem.Depth = cursor.Depth

	newIdx := len(ctx.AllItems)
	ctx.AllItems = append(ctx.AllItems, newItem)

	// Insert in parent's Children after cursor's position
	if parentIdx >= 0 {
		parent := &ctx.AllItems[parentIdx]
		insertAt := len(parent.Children) // default: end
		for i, childIdx := range parent.Children {
			if childIdx == cursorItemIdx {
				insertAt = i + 1
				break
			}
		}
		parent.Children = append(parent.Children, 0)
		copy(parent.Children[insertAt+1:], parent.Children[insertAt:])
		parent.Children[insertAt] = newIdx
	}

	return newIdx
}

// AddChildTo appends a new item as the first child of parentIdx.
// Sets HasChildren on the parent. Returns the index of the new item.
func AddChildTo(ctx *TreeContext, parentIdx int, newItem Item) int {
	if parentIdx < 0 || parentIdx >= len(ctx.AllItems) {
		return -1
	}
	parent := &ctx.AllItems[parentIdx]
	newItem.ParentIdx = parentIdx
	newItem.Depth = parent.Depth + 1

	newIdx := len(ctx.AllItems)
	ctx.AllItems = append(ctx.AllItems, newItem)

	parent.Children = append([]int{newIdx}, parent.Children...)
	parent.HasChildren = true

	return newIdx
}

// DeleteItem soft-deletes an item and its children by hiding them
// and removing from the parent's Children slice.
func DeleteItem(ctx *TreeContext, itemIdx int) {
	if itemIdx < 0 || itemIdx >= len(ctx.AllItems) {
		return
	}
	// Recursively hide children
	var hideRecursive func(idx int)
	hideRecursive = func(idx int) {
		ctx.AllItems[idx].Hidden = true
		for _, childIdx := range ctx.AllItems[idx].Children {
			hideRecursive(childIdx)
		}
	}
	hideRecursive(itemIdx)

	// Remove from parent's Children
	parentIdx := ctx.AllItems[itemIdx].ParentIdx
	if parentIdx >= 0 {
		parent := &ctx.AllItems[parentIdx]
		for i, childIdx := range parent.Children {
			if childIdx == itemIdx {
				parent.Children = append(parent.Children[:i], parent.Children[i+1:]...)
				break
			}
		}
		if len(parent.Children) == 0 {
			parent.HasChildren = false
		}
	}
}

// CanDelete returns false if the item is in the active scope chain.
func CanDelete(s *State, itemIdx int) bool {
	ctx := s.TopCtx()
	for _, level := range ctx.Scope {
		if level.ParentIdx == itemIdx {
			return false
		}
	}
	return true
}

// SerializeTree converts AllItems to nested JSON-compatible structures.
// Skips property items (ephemeral), soft-deleted items, and client-injected
// items (e.g. the `:` command palette) so those never get round-tripped back
// to the cloud menu on save.
func SerializeTree(ctx *TreeContext) []interface{} {
	var serializeItem func(idx int) map[string]interface{}
	serializeItem = func(idx int) map[string]interface{} {
		item := ctx.AllItems[idx]
		if item.IsProperty || item.Injected {
			return nil
		}

		m := map[string]interface{}{}
		if len(item.Fields) > 0 {
			m["name"] = item.Fields[0]
		}
		if len(item.Fields) > 1 && item.Fields[1] != "" {
			m["description"] = item.Fields[1]
		}
		if item.Action != nil {
			if item.Action.Type == "url" {
				m["url"] = item.Action.Target
			} else if item.Action.Target != "" {
				m["action"] = item.Action.Target
			}
		}
		if item.Hidden {
			m["hidden"] = true
		}

		if len(item.Children) > 0 {
			var children []interface{}
			for _, childIdx := range item.Children {
				child := serializeItem(childIdx)
				if child != nil {
					children = append(children, child)
				}
			}
			if len(children) > 0 {
				m["children"] = children
			}
		}
		return m
	}

	var result []interface{}
	for i, item := range ctx.AllItems {
		if item.Depth == 0 && !item.IsProperty && !item.Injected {
			if m := serializeItem(i); m != nil {
				result = append(result, m)
			}
		}
	}
	return result
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

// hasHiddenAncestor returns true if the item has a hidden ancestor that
// is NOT in the current scope chain (i.e. we haven't scoped into it).
func hasHiddenAncestor(allItems []Item, item Item, scope []ScopeLevel) bool {
	idx := item.ParentIdx
	for idx >= 0 && idx < len(allItems) {
		if allItems[idx].Hidden {
			// Check if this hidden folder is in our scope chain
			for _, level := range scope {
				if level.ParentIdx == idx {
					return false // we're scoped into it, so its children are fair game
				}
			}
			return true // hidden ancestor, not in scope
		}
		idx = allItems[idx].ParentIdx
	}
	return false
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
		parentIdx := ctx.Scope[len(ctx.Scope)-1].ParentIdx
		if cfg.SearchDepth > 0 {
			searchPool = DescendantsOfDepth(ctx.AllItems, parentIdx, cfg.SearchDepth)
		} else {
			searchPool = DescendantsOf(ctx.AllItems, parentIdx)
		}
	}

	var matched []Item
	for _, item := range searchPool {
		// Skip items inside hidden folders (e.g. command palette)
		// unless we're scoped into that hidden folder
		if hasHiddenAncestor(ctx.AllItems, item, ctx.Scope) {
			continue
		}
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
