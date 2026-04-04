# CLAUDE.md

## Overview

fzt (fuzzy tiered) is an fzf-compatible fuzzy finder with two additions: depth-aware tiered scoring and first-class column support. Written in Go. Full-screen mode uses tcell; inline mode (`--height`) renders directly with ANSI escapes.

Repo: `D:\repos\fzt`
Binary: `fzt.exe` (built to repo root, on PATH via Profile 1's `profile.ps1`)

## Building

```
go build -ldflags="-X github.com/nelsong6/fzt/internal/tui.Version=$(git describe --tags --always --dirty)" -o fzt.exe .
```

## Scoring Architecture

Scoring uses a `TieredScore` struct with three levels compared lexicographically (name first, then desc, then ancestor). Any name match always outranks any description match, which always outranks any ancestor match. No magic multipliers — tier ordering is enforced by `TieredScore.Less()` comparison logic.

### Match tiers (highest to lowest)

1. **Name** (field 0): Direct match against the item's name. Depth penalty applies here in tiered mode.
2. **Description** (fields 1+): Always searchable regardless of `--nth`. `--nth` only restricts which fields qualify for the name tier.
3. **Ancestor**: Parent/grandparent folder names inherited via `ParentIdx` chain. Lets children be found by their parent's category name.

### Multi-term search

Queries are split on whitespace. Every term must match somewhere (AND logic). Each term independently finds its best match across the three tiers, preferring the highest tier available. Example: `git prune` — "git" may match an ancestor name while "prune" matches the item's own name.

### Per-character scoring (FuzzyMatch)

Left-to-right character scan: +1 per match, +2 if consecutive, +3 bonus at position 0 or after a word boundary (space, `/`, `-`, `_`, `>`).

## PowerShell Pipe Encoding (Profile 1)

PowerShell 5.1 mangles Private Use Area Unicode codepoints (nerd font icons) when piping between native commands. The icons get converted to `?` (U+003F) because PowerShell transcodes through .NET's UTF-16 strings and drops unmappable codepoints.

**Workaround:** Wrap the pipe in `cmd /c` so bytes flow directly between processes without touching PowerShell's pipeline:

```powershell
# BAD — icons become ???
lsd --icon always --color always | fzt --ansi

# GOOD — icons preserved
cmd /c "lsd --icon always --color always | fzt --ansi"
```

This applies to any rich input (nerd font icons, ANSI colors from tools like lsd). The `--ansi` flag tells fzt to parse and preserve ANSI color codes, and `--icon always --color always` forces lsd to emit them when piped.

## Testing

- `--filter="query"` — non-interactive mode, prints matches to stdout
- `--show-scores` — annotates filter output with `[score=N:X D:Y A:Z]` (name/desc/ancestor tiers)
- `--simulate` — headless rendering (no terminal needed), generates frame-by-frame text snapshots
  - `--sim-query="text"` — key events, one frame per keystroke. Plain characters are literal. Special keys: `{up}`, `{down}`, `{left}`, `{right}`, `{enter}`, `{tab}`, `{esc}`, `{bs}`, `{space}`, `{ctrl+u}`, `{ctrl+w}`
  - `--width=N --height-lines=N` — virtual terminal size
  - `--styled` — adds `[H]`=highlight `[S]`=selected `[*]`=both markers
  - `--record file.txt` — write frames to file instead of stdout

## Tree Mode

Tree mode is the primary interaction model for hierarchical data. Auto-enabled with `--yaml`. The tree is the single navigation surface — there is no separate results panel.

### Interaction model

Two modes that switch automatically based on user action:

- **Search mode** ( icon, yellow): typing drives navigation. The tree auto-expands to reveal the top match, and the cursor follows it. Ghost autocomplete text shows the remaining characters of the top match name when the query is a prefix.
- **Nav mode** ( icon, cyan): arrow keys drive. The selected item's name echoes in the prompt (italic gray, like autocomplete — speculative, not committed). Ancestor breadcrumbs show the path to the current item.

Switching: typing any character → search mode. Pressing Up/Down/Left/Right → nav mode. The transition is automatic.

### Key bindings

- **Typing**: appends to query (always at end — no mid-query cursor), filters tree, auto-expands to top match
- **Up/Down**: move tree cursor (switches to nav mode)
- **Left**: collapse folder or move to parent. At root in nav mode → exits nav mode back to search
- **Right**: expand folder or move to first child
- **Tab**: autocomplete query to top match name. If already a perfect match, no-op (TODO: undecided behavior for repeated Tab after perfect match)
- **Space**: on a folder → pushScope (enter folder). On a leaf → inserts space in query
- **Enter**: on a folder → toggle expand/collapse. On a leaf → select (return output)
- **Backspace**: in search mode → delete last query char. In nav mode → takes displayed item name, removes last char, switches to search mode. On empty query in scope → popScope
- **Ctrl+W**: delete last word from query
- **Ctrl+U**: clean slate — exit nav mode, clear query, deselect, collapse auto-expansions
- **Escape**: clear query → deactivate search → pop scope → cancel (progressive)
- **Ctrl+C**: hard cancel

### Scope

Space on a folder pushes a scope level. The folder name appears as a locked breadcrumb (dark gray, non-italic) in the prompt. Backspace on empty query pops the scope and collapses the folder (if it wasn't expanded before entering). Scope state is saved/restored across push/pop (query, cursor position, tree offset).

### Prompt bar anatomy

`[mode icon] [scope breadcrumbs] [context breadcrumbs] [query + ghost | nav preview]`

- **Mode icon**: search () or nav () — switches automatically
- **Scope breadcrumbs**: locked folders entered via Space (dark gray, non-italic)
- **Context breadcrumbs**: ancestor path of the focused item (dark gray, italic) — transient, updates as the match/cursor changes. Stops at the scope boundary
- **Query + ghost**: typed text in white, ghost autocomplete in dark gray
- **Nav preview**: selected item name in italic dark gray (only in nav mode)

### Tree rendering

- No selection on start (`treeCursor = -1`). First arrow or search highlights an item
- Emptying the query resets selection to none
- Icons: 󰉋 (yellow, bold) for folders, (white) for files
- Selected items have blue background with `▸` indicator
- Top match highlight (blue background) only shows in search mode, not nav mode

### Inline mode

`--height N%` renders the tree inline in the terminal buffer (no alternate screen). Uses raw terminal I/O (`CONIN$`/`CONOUT$` on Windows, `/dev/tty` on Unix) with ANSI escape sequences. The cursor position is tracked between renders to avoid the visible cursor jumping to wrong positions between frames.

## Key Flags

fzf-compatible: `--layout`, `--border`, `--header-lines`, `--nth`, `--accept-nth`, `--prompt`, `--delimiter`, `--height`
New: `--tiered`, `--depth-penalty`, `--search-cols`, `--ansi`, `--title`, `--title-pos`, `--tree` (auto-enabled by `--yaml`)

## Change Log

### 2026-04-02

- **Multi-term search**: Query split on spaces with AND logic — every term must match somewhere. Enables queries like `git prune` to match children by combining parent category + item name.
- **Ancestor name inheritance**: Children inherit parent folder names for searching at the lowest tier. Walks `ParentIdx` chain via `getAncestorNames()`. `ParentIdx` initialized to -1 for piped data to avoid false references.
- **TieredScore struct**: Replaced flat int scoring with `TieredScore{Name, Desc, Ancestor}` and lexicographic `Less()` comparison. Designed to avoid magic multiplier constants — tier ordering is pure control flow. Depth penalty applies to `Name` field only.
- **Descriptions always searchable**: `--nth` no longer blocks description matching. `shouldSearch()` gates the name tier; descriptions (field 1+) are always eligible at the desc tier. Fixes the `at` menu where `--nth=1` was silently preventing description search.
- **Auto-highlight top result while typing**: Top match always selected (blue highlight) as user types. Enter immediately confirms. Prompt shows query text with cursor even while an item is highlighted.
- **Description text color**: Changed from `tcell.ColorGray` to `tcell.StyleDefault` (normal terminal white).
- **WASM build target**: Added `cmd/wasm/main.go` — compiles fzt's internal scorer, YAML loader, and filtering logic to WebAssembly (`GOOS=js GOARCH=wasm`). Exposes `fzt.loadYAML()`, `fzt.filter()`, and `fzt.getChildren()` to JavaScript via `syscall/js`. Enables running the actual Go scoring engine in the browser for the fuzzy-tiers-showcase frontend.
- **`LoadFromString`**: Added to `internal/yamlsrc/yamlsrc.go` — parses YAML content from a string without filesystem I/O. File-reference children are not supported (errors). Used by the WASM bridge since browsers have no filesystem.
- **ANSI serialization** (`internal/tui/ansi.go`): `MemScreen.ToANSI()` serializes the headless grid as ANSI-escaped text. Maps tcell palette colors to standard ANSI SGR codes (30-37/90-97 for 16-color, 38;5;N for 256, 38;2;R;G;B for true color). Emits a full SGR reset+set on each style change, reset at end of each line. Designed for web terminal rendering — the JS side parses these codes and maps palette indices to a Tokyo Night theme.
- **Headless TUI Session** (`internal/tui/session.go`): `Session` type wraps state + MemScreen + Config for WASM/headless use. Methods: `NewSession(items, cfg, w, h)`, `Render() SessionFrame`, `HandleKey(key, ch) (SessionFrame, action)`, `Resize(w, h) SessionFrame`. `SessionFrame` returns ANSI string + cursor position.
- **Extracted key handling** (`internal/tui/tui.go`): Moved the 160-line key event switch from `Run()` into standalone `handleKeyEvent(s, key, ch, cfg, searchCols) string`. Shared by both `Run()` (terminal) and `Session.HandleKey()` (WASM). Returns action strings: `""` (continue), `"cancel"`, `"select:<output>"`. Also made Escape context-sensitive: clears query first, then pops scope, then cancels — while Ctrl+C remains a hard cancel.
- **WASM rewrite** (`cmd/wasm/main.go`): Replaced stateless filter/score API with full stateful TUI session. New API: `fzt.loadYAML(yaml)`, `fzt.init(cols, rows)`, `fzt.handleKey(key, ctrl, shift)`, `fzt.resize(cols, rows)`. Returns `{ansi, cursorX, cursorY}` JS objects. Includes `translateKey()` mapping browser `event.key` strings to tcell key constants. The web version now runs the exact same rendering pipeline as the terminal.
- **MemScreen dimension clamping** (`internal/tui/canvas.go`): `NewMemScreen` clamps width to 1-500 and height to 1-200 to prevent catastrophic memory allocation from bad input (e.g., WASM receiving oversized grid dimensions from a browser measurement error).
- **URL field on items** (`internal/model/model.go`, `internal/yamlsrc/yamlsrc.go`): YAML entries can include an optional `url` field. Stored on `model.Item.URL`. `Session.SelectedURL()` returns it on leaf selection so the WASM bridge can pass it to JS for opening in a new tab.

### 2026-04-03

- **WASM header injection** (`cmd/wasm/main.go`): `initSession` now prepends a header item (`Fields: ["Name", "Description"]`, `Depth: -1`) and sets `HeaderLines: 1` in the TUI config — matching the CLI's `--header` behavior in `cmd/root.go`. Previously the WASM bridge skipped header injection entirely, so the fuzzy-tiers-showcase was missing the column headers that the terminal version displayed.
- **WASM session starts with no selection** (`internal/tui/session.go`): `NewSession` now sets `s.index = -1` instead of auto-selecting index 0. The prompt starts empty and ready for typing — no item is highlighted until the user navigates or types a query. Requested for my-homepage integration where the terminal is always visible and auto-selecting felt wrong.
- **`--title` and `--title-pos` flags** (`cmd/root.go`, `internal/tui/tui.go`): Title text rendered on the top border edge (like fzf's `--border-label`). `drawBorderTopWithTitle()` overlays the title text onto the `─` characters of the top border with space padding. `--title-pos` controls alignment: `left` (default), `center`, or `right`. Added `Title` and `TitlePos` fields to `Config`.
- **Inline rendering mode** (`internal/tui/inline.go`): When `--height N%` is specified (N < 100), fzt now renders inline in the main terminal buffer instead of entering tcell's alternate screen buffer. Preserves scrollback above the picker. `RunInline()` bypasses tcell entirely — opens the TTY directly (`CONIN$`/`CONOUT$` on Windows, `/dev/tty` on Unix), puts it in raw mode via `golang.org/x/term`, reserves vertical space with newlines, and renders frames using `MemScreen.ToANSI()` with ANSI cursor movement. `Run()` dispatches to `RunInline()` when `cfg.Height > 0 && < 100`.
- **Unified tree+search mode** (`--tree` flag, `internal/tui/tree.go`): New rendering mode that combines a navigable tree view with fuzzy search. The tree is always visible; typing activates search with auto-expansion of the top match's parent folder. Flat ranked results appear below the tree. Motivated by my-homepage needing a single terminal interface for both browsing and searching bookmarks, replacing the old two-panel (fzt terminal + HTML tree) design.
- **Three-layer focus model**: Tree (arrow nav, Enter expand/select) → Prompt (typing edits query, tree auto-expands) → Results (Tab to enter, arrows navigate). Escape walks back up. Replaces the previous two-mode (treeMode + searchMode) approach.
- **Scope as breadcrumb**: Enter/Tab on folder or typing exact folder name + Space pushes scope. Folder name rendered as greyed-out text in the prompt — looks like the user typed it. Backspace on empty query pops scope. Tree expands the scoped folder in place (full hierarchy stays visible). Escape clears query first, then pops scope.
- **Bordered prompt bar**: Search input rendered inside a `┌─┐│ │└─┘` box with a subtle background (256-color #303030), visually separated from tree content below. Cursor always visible in prompt.
- **Click row WASM API** (`fzt.clickRow(row)`, `fzt.initTree(cols, rows)`): Maps visual row to tree item or result. Folders toggle expand/collapse, leaves return selection. `NewTreeSession` creates a unified session; `initTree` exposes it to JS.
- **`--tree` CLI flag** (`cmd/root.go`): Activates unified tree+search mode in interactive terminal. `runWithSession` renders directly to tcell canvas (avoids wide-character corruption from MemScreen→tcell cell copy). Uses `screen.Sync()` for full redraws on layout changes.
- **MemScreen.GetContent** (`internal/tui/canvas.go`): Added getter for reading back rune + style at a cell position.
- **Test bookmarks** (`test-bookmarks.yaml`): Sample hierarchical bookmark data with descriptions for testing tree mode. `fzttest` script updated to use `--tree` mode.
- **Raw terminal abstraction** (`internal/tui/rawreader.go`, `rawreader_windows.go`, `rawreader_unix.go`): `rawTerminal` type wraps platform-specific TTY open, raw mode, and key reading. Windows path enables `ENABLE_VIRTUAL_TERMINAL_INPUT` on `CONIN$` so arrow keys arrive as ANSI escape sequences. `ReadKey()` handles the escape key ambiguity with a 50ms `SetReadDeadline` timeout.
- **Key parser** (`internal/tui/keyparse.go`): `parseKey()` translates raw terminal bytes into `(tcell.Key, rune)` pairs compatible with `handleKeyEvent()`. Handles single-byte control chars, CSI sequences (arrows, delete, backtab), SS3 sequences, and UTF-8 multi-byte runes. Skips unrecognized CSI sequences gracefully.
- **Tree navigation stays in tree** (`internal/tui/tree.go`): `handleTreeKey` no longer calls `pushScope` on Enter/Right. Enter toggles folder expand/collapse, Right expands (or moves to first child if already expanded), Left collapses (or moves to parent). Escape cancels the picker. Motivation: entering a folder previously activated search mode via `pushScope` which set `searchActive = true`, routing keys to `handlePromptKey` where Up/Down arrows were ignored — arrow navigation was lost after opening any folder.
- **Test bookmarks updated** (`test-bookmarks.yaml`): Replaced synthetic test data with real bookmark hierarchy matching the live my-homepage deployment (Nelsonhub, Home, Bills, Dev, google, romaine.life).

### 2026-04-04

- **Unified tree interaction model**: Replaced the three-layer focus model (tree → prompt → results) with a two-mode model (search mode + nav mode). The tree is now the single navigation surface — no separate results panel. Motivated by the realization that a tree has meaningful spatial order (like fzf's `--no-sort`) and shouldn't be split into two lists. The cursor jumps to the top match in-place via auto-expansion rather than showing matches in a separate ranked list.
- **Search/nav mode toggle**: Typing activates search mode ( icon, yellow); arrow keys activate nav mode ( icon, cyan). The transition is automatic. In search mode, the query drives the cursor (`syncTreeCursorToTopMatch`). In nav mode, the cursor drives the prompt display (item name echoed as italic gray ghost text). `navMode` bool on state tracks the current mode.
- **Ghost autocomplete**: When the query is a case-insensitive prefix of the top match's name, the remaining characters appear as dark gray ghost text after the cursor. Tab accepts the autocomplete. Repeated Tab on a perfect match is intentionally a no-op (behavior TBD).
- **Context breadcrumbs**: Italic dark gray ancestor path in the prompt bar showing where the focused item lives in the hierarchy. Distinct from scope breadcrumbs (non-italic, pushed via Space). The context path is purely derived from the current match/cursor — no state, updates live as you type or navigate. Walks `ParentIdx` chain, stops at the current scope boundary.
- **Scope collapse on pop**: `scopeLevel` now tracks `wasExpanded` — whether the folder was already expanded before `pushScope`. On `popScope`, the folder collapses back only if `pushScope` was the one that expanded it. Prevents Backspace from collapsing folders the user manually opened.
- **Simplified query editing**: Removed mid-query cursor movement (Left/Right now navigate the tree, not the text). Query is always appended at end. Backspace deletes from end. Ctrl+W deletes last word. Ctrl+U is "clean slate" (exit nav, clear query, deselect). No Delete key in tree mode.
- **Backspace from nav mode**: Takes the displayed item name, removes the last character, and switches to search mode with that as the query. Makes the nav preview feel editable.
- **Left exits nav mode**: Pressing Left when already at a root-level item (nothing to collapse, no parent) exits nav mode and returns to search mode.
- **Emptying query deselects**: When the query becomes empty (Backspace, Escape, Ctrl+U, Ctrl+W), `treeCursor` resets to -1 — no item highlighted. Works regardless of scope depth.
- **`--yaml` auto-enables tree mode**: `--yaml` now implies `--tree` (and `--tiered`). The `--tree` flag is still accepted but redundant. Tree mode is the primary interaction model for hierarchical data, not an optional toggle.
- **Inline tree mode**: `RunInline` (`--height`) now supports tree mode. Checks `cfg.TreeMode` to use `drawUnified`/`handleUnifiedKey` instead of the flat rendering path. The `at` menu (`automate.ps1`) and `fzttest` now behave identically.
- **Simulate supports special keys**: `--sim-query` now accepts `{up}`, `{down}`, `{left}`, `{right}`, `{enter}`, `{tab}`, `{esc}`, `{bs}`, `{space}`, `{ctrl+u}`, `{ctrl+w}` in addition to plain characters. `parseSimQuery` tokenizes the string. Simulate uses `handleUnifiedKey` for tree mode instead of raw state manipulation.
- **Inline cursor fix**: Fixed bug where the visible cursor sat on the title bar between renders. The old code showed the cursor at the correct position then immediately moved it to the top of the region for the next redraw (while still visible). Now tracks `cursorRow` and moves to top at the start of the next render (while hidden).
- **`drawText` convention fix**: Changed `drawText` to use character-index-based limiting (like `drawHighlightedText`) instead of absolute screen position. All callers already passed relative widths — the function was the mismatch. Fixed names being truncated or invisible in tree rows where x-offset exceeded the old absolute limit.
- **File icon**: Changed from `\uF016` (nf-fa-file_o, outline) to `\uF15B` (nf-fa-file, solid white page) with white foreground.
- **Command mode** (`internal/tui/commands.go`, `internal/tui/version.go`): Typing `:` opens a command palette. Two contexts: **global** (no selection — takes over the full prompt area) and **contextual** (item selected — renders as a bottom panel below the tree). Commands: `version` (shows build version), `name` (prints selected item name), `desc` (prints selected item description). The command list is filterable by typing. Enter executes, Escape exits. After execution, output replaces the command list and "press any key" dismisses. Ghost autocomplete works in the command prompt the same as the search prompt.
- **Build version injection**: `Version` variable in `internal/tui/version.go` defaults to `"dev"` and is overridden via `-ldflags="-X ...Version=<tag>"` in CI. The build matrix computes the next version tag before building so all artifacts (including WASM) carry the release version. The `:version` command displays it.
- **Repo rename**: Module path changed from `github.com/nelsong6/fuzzy-tiered` to `github.com/nelsong6/fzt`. All import paths updated across the codebase.
- **Cross-repo deploy pipeline**: CI now publishes `fzt.wasm` as a release asset alongside native binaries. After release, the workflow authenticates via OIDC to Azure Key Vault, retrieves the `romaine-life-app` GitHub App credentials, generates a short-lived installation token, and fires `repository_dispatch` to `fzt-showcase` and `my-homepage` so they automatically redeploy with the new WASM. No static PAT — credentials are ephemeral.
