# CLAUDE.md

## Overview

fzt (fuzzy tiered) is a pure scoring and state engine: depth-aware tiered scoring, tree state management, input handling, and pluggable data sources. Written in Go. No TUI, no terminal rendering, no frontend concerns -- those live in `nelsong6/fzt-terminal`.

### Package structure

- **`core/`** -- Public library. Data types (`Item`, `TieredScore`, `StyledRune`), fuzzy scoring (`FuzzyMatch`, `ScoreItem`), column parsing (`ParseLines`, `ComputeWidths`, `FormatRow`), YAML loading (`LoadYAML`, `LoadYAMLFromString`), ANSI parsing (`ParseANSI`, `StripANSI`), tree state management (`State`, `TreeContext`, `PushScope`, `PopScope`, `FilterItems`, `TreeVisibleItems`, `UpdateQueryExpansion`, `SyncTreeCursorToTopMatch`, `BuildScopePath`, `ExpandToPath`), key handlers (`HandleUnifiedKey`, `HandleKeyEvent`, `HandleTreeKey`, `HandleSearchKey`, `ClickUnifiedRow`), the `TreeProvider` interface for pluggable data sources, `DirProvider` for filesystem trees, `ListDriveRoots` (Windows), and `Config`.
- **`render/`** -- Headless rendering infrastructure. `Canvas` interface, `MemScreen` (in-memory grid with snapshot/styled-snapshot output), `Session`/`NewTreeSession` (headless wrapper for WASM/testing), ANSI serialization (`ToANSI`), structured data API (`GetVisibleRows`, `GetPromptState`, `GetUIState`), `Version` variable.
- **`cmd/fzt/`** -- Minimal scoring CLI: `echo lines | fzt "query"` -> ranked output. No TUI, no interaction.
- **`cmd/ansicheck/`**, **`cmd/debuginput/`**, **`cmd/icontest/`** -- Dev utilities.

### Ecosystem

Interactive tools import fzt alongside `nelsong6/fzt-terminal` which provides terminal/browser renderers, style (Catppuccin, DOS font, CRT), and frontend behavior (command palette, identity, actions). See the architecture diagrams at `docs.romaine.life/fzt/final`.

Repo: `D:\repos\fzt`

## Building

```
go build -o fzt.exe ./cmd/fzt
```

Tests:
```
go test ./core/...
```

## Scoring Architecture

Scoring uses a `TieredScore` struct with three levels compared lexicographically (name first, then desc, then ancestor). Any name match always outranks any description match, which always outranks any ancestor match. No magic multipliers -- tier ordering is enforced by `TieredScore.Less()` comparison logic.

### Match tiers (highest to lowest)

1. **Name** (field 0): Direct match against the item's name. Depth penalty applies here in tiered mode.
2. **Description** (fields 1+): Always searchable regardless of search columns. Search column restrictions only affect which fields qualify for the name tier.
3. **Ancestor**: Parent/grandparent folder names inherited via `ParentIdx` chain. Lets children be found by their parent's category name.

### Multi-term search

Queries are split on whitespace. Every term must match somewhere (AND logic). Each term independently finds its best match across the three tiers, preferring the highest tier available. Example: `git prune` -- "git" may match an ancestor name while "prune" matches the item's own name.

### Per-character scoring (FuzzyMatch)

Left-to-right character scan: +1 per match, +2 if consecutive, +3 bonus at position 0 or after a word boundary (space, `/`, `-`, `_`, `>`).

## Tree State

Tree state lives in `core/`. `State` holds a stack of `TreeContext` values, each containing dataset, query, tree navigation, scope levels, and context identity. All tree operations read/write the top context via `s.TopCtx()`.

### Key concepts

- **Scope**: Entering a folder pushes a `ScopeLevel` that saves query/cursor/offset. `PopScope` restores state and conditionally collapses the folder.
- **Context stack**: Multiple datasets (e.g., command palette pushed on top of normal tree). `PushContext`/`PopContext` manage the stack.
- **Provider**: `TreeProvider` interface enables lazy-loading. When `PushScope` encounters a folder with no loaded children and a provider is set, it calls `LoadChildren` to splice items dynamically.
- **Filtering**: `FilterItems` applies the current query to the search pool. In tiered mode, searches all descendants (optionally depth-limited via `SearchDepth`) with ancestor name inheritance. Results are sorted by `TieredScore.Less()`.
- **Auto-expansion**: `UpdateQueryExpansion` sets `QueryExpanded` flags to reveal the top match. `SyncTreeCursorToTopMatch` positions the cursor on it.

### Input handlers

All in `core/input.go`:

- `HandleUnifiedKey` -- Entry point for unified tree+search mode. Dispatches Shift+HJKL vim navigation, handles mode switching (typing activates search, arrows activate nav), delegates to `HandleTreeKey` or `HandleSearchKey`.
- `HandleKeyEvent` -- Flat (non-tree) mode key handling with mid-query cursor, scope via Enter/Right on folders.
- `HandleTreeKey` -- Pure tree navigation when no query is active. Up/Down move cursor, Enter pushes scope on folders, Left collapses/moves to parent.
- `HandleSearchKey` -- Search-active mode. Typing edits query and auto-positions cursor on top match. Tab autocompletes. Space on folder pushes scope.

## ANSI Parsing

`core/ansi.go` provides `ParseANSI` (string -> `[]StyledRune` with tcell styles) and `StripANSI` (removes all escape sequences). Supports SGR attributes (bold, dim, italic, underline, reverse, strikethrough), standard/bright 16 colors, 256-color palette, and true color RGB.

`render/ansi.go` provides `MemScreen.ToANSI()` which serializes the headless grid as ANSI-escaped text. Maps tcell styles to SGR codes (16-color, 256-color, true color). Used by WASM/web consumers.

## Structured Data API

`render/session.go` exposes three methods for DOM-based frontends that skip ANSI rendering entirely:

- `GetVisibleRows()` -- Returns `[]VisibleRow` with name, description, depth, folder/selected/topMatch flags, and match indices.
- `GetPromptState()` -- Returns mode (search/nav), scope breadcrumbs, query, cursor position, ghost autocomplete text, and hint.
- `GetUIState()` -- Returns title, version, label, border flag, tree offset, and total visible count.

## Dependencies

- `github.com/gdamore/tcell/v2` -- Used for `tcell.Style`, `tcell.Key`, and color types (core key handling and ANSI parsing). Not used for terminal I/O.
- `gopkg.in/yaml.v3` -- YAML tree loading.
