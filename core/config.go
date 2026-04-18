package core

// Config holds all options derived from CLI flags.
//
// Key relationships:
//   - TreeMode is a renderer flag (consumed by fzt-terminal tui package, not the engine).
//     Tiered enables hierarchical scoring (depth penalty, ancestor matching) in the engine.
//     TreeMode implies Tiered in practice — all tree-mode callers also set Tiered.
//   - SearchCols overrides Nth for scoring. Nth restricts which fields are searched in flat mode.
//     Description fields (index 1+) are always searchable at the desc tier regardless of either.
//   - FrontendName/Version/Commands are set by the ecosystem layer (fzt-terminal ApplyConfig).
//     They drive the two-level `:` command palette structure in InjectCommandFolder.
type Config struct {
	Layout       string // "reverse" (prompt at top) or "default" (prompt at bottom). Renderer-only.
	Border       bool   // draw box border around the TUI. Renderer-only.
	HeaderLines  int    // number of leading items treated as column headers (not scored)
	Nth          []int  // 1-based field indices for search scope in flat mode. Fallback for SearchCols.
	AcceptNth    []int  // 1-based field indices to include in the output string on selection
	Prompt       string // custom prompt prefix string
	Delimiter    string // field delimiter for ParseLines (e.g. "\t")
	Tiered       bool   // enable hierarchical scoring: depth penalty, ancestor matching, scope-based search pools
	DepthPenalty int    // per-level penalty subtracted from Name tier score (relative to current scope). All callers use 5.
	SearchCols   []int  // 1-based: restricts which fields qualify for the Name tier. Overrides Nth for scoring.
	Height       int    // percentage of terminal height (0 = full screen, 1-99 = inline mode)
	ShowScores   bool   // annotate filter output with raw scores (debugging)
	ANSI         bool   // preserve ANSI escape codes from input in StyledFields
	Title        string // border title text
	TitlePos     string // title position within border
	TreeMode     bool   // renderer flag: use drawUnified (tree view). Not read by the engine.
	Label        string // secondary label shown in the border (top-left, e.g. user name)
	SearchDepth  int    // 0 = unlimited, N = search only N levels deep from current scope
	// Frontend identity — populated by ecosystem layer (fzt-terminal ApplyConfig), not the engine.
	FrontendName     string        // e.g. "automate", "homepage" — shown as "X ctl" scope title
	FrontendVersion  string        // displayed via "version > on" in the : palette
	FrontendCommands []CommandItem // registered commands for the first level of the : palette
	InitialDisplay   string        // mapped to State.IdentityLabel — shown via "whoami > on" in : palette
	FoldersOnly      bool // folders are the selectable items — Enter on an already-scoped folder returns "select:" instead of no-op
	EnvTags []string // environment capabilities (e.g. "terminal", "wasm", "browser") — items with DisplayCondition are shown only if their tag is in this set
	// Provider for lazy tree loading (e.g. DirProvider for file picker mode).
	// PushScope calls Provider.LoadChildren when entering a folder with no loaded children.
	Provider   TreeProvider
	FocusedDir string // path to pre-expand on startup when using Provider (e.g. current working directory)
	ConfigDir          string // directory containing sync state files (.last-sync-check, .identity, identities.json, cache)
	InitialMenuVersion int    // persisted menu version for conflict detection on save
	// Self-update target — consumed by fzt-terminal's RunUpdate when the
	// user triggers ::update in the core palette. Each consumer binary
	// (fzt-automate, fzt-picker, fzt) points at its own GitHub releases
	// so the update pulls the right asset. Empty strings fall back to
	// the fzt defaults for backward compat.
	UpdateRepo        string // "owner/name", e.g. "nelsong6/fzt-automate"
	UpdateAssetPrefix string // release asset prefix, e.g. "fzt-automate" (builds "-<os>-<arch>[.exe]")
	UpdateBinaryName  string // on-disk final name, e.g. "fzt-automate"
}
