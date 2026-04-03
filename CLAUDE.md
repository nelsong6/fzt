# CLAUDE.md

## Overview

fzh (fuzzy hierarchical) is an fzf-compatible fuzzy finder with two additions: depth-aware tiered scoring and first-class column support. Written in Go, uses tcell for the TUI.

Repo: `D:\repos\fuzzy-tiered`
Binary: `fzh.exe` (built to repo root, on PATH via Profile 1's `profile.ps1`)

## Building

```
go build -o fzh.exe .
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
lsd --icon always --color always | fzh --ansi

# GOOD — icons preserved
cmd /c "lsd --icon always --color always | fzh --ansi"
```

This applies to any rich input (nerd font icons, ANSI colors from tools like lsd). The `--ansi` flag tells fzh to parse and preserve ANSI color codes, and `--icon always --color always` forces lsd to emit them when piped.

## Testing

- `--filter="query"` — non-interactive mode, prints matches to stdout
- `--show-scores` — annotates filter output with `[score=N:X D:Y A:Z]` (name/desc/ancestor tiers)
- `--simulate` — headless rendering (no terminal needed), generates frame-by-frame text snapshots
  - `--sim-query="text"` — types each character, one frame per keystroke
  - `--width=N --height-lines=N` — virtual terminal size
  - `--styled` — adds `[H]`=highlight `[S]`=selected `[*]`=both markers
  - `--record file.txt` — write frames to file instead of stdout

## Key Flags

fzf-compatible: `--layout`, `--border`, `--header-lines`, `--nth`, `--accept-nth`, `--prompt`, `--delimiter`, `--height`
New: `--tiered`, `--depth-penalty`, `--search-cols`, `--ansi`

## Change Log

### 2026-04-02

- **Multi-term search**: Query split on spaces with AND logic — every term must match somewhere. Enables queries like `git prune` to match children by combining parent category + item name.
- **Ancestor name inheritance**: Children inherit parent folder names for searching at the lowest tier. Walks `ParentIdx` chain via `getAncestorNames()`. `ParentIdx` initialized to -1 for piped data to avoid false references.
- **TieredScore struct**: Replaced flat int scoring with `TieredScore{Name, Desc, Ancestor}` and lexicographic `Less()` comparison. Designed to avoid magic multiplier constants — tier ordering is pure control flow. Depth penalty applies to `Name` field only.
- **Descriptions always searchable**: `--nth` no longer blocks description matching. `shouldSearch()` gates the name tier; descriptions (field 1+) are always eligible at the desc tier. Fixes the `at` menu where `--nth=1` was silently preventing description search.
- **Auto-highlight top result while typing**: Top match always selected (blue highlight) as user types. Enter immediately confirms. Prompt shows query text with cursor even while an item is highlighted.
- **Description text color**: Changed from `tcell.ColorGray` to `tcell.StyleDefault` (normal terminal white).
- **WASM build target**: Added `cmd/wasm/main.go` — compiles fzh's internal scorer, YAML loader, and filtering logic to WebAssembly (`GOOS=js GOARCH=wasm`). Exposes `fzh.loadYAML()`, `fzh.filter()`, and `fzh.getChildren()` to JavaScript via `syscall/js`. Enables running the actual Go scoring engine in the browser for the fuzzy-tiers-showcase frontend.
- **`LoadFromString`**: Added to `internal/yamlsrc/yamlsrc.go` — parses YAML content from a string without filesystem I/O. File-reference children are not supported (errors). Used by the WASM bridge since browsers have no filesystem.
