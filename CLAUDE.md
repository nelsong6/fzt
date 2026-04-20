# CLAUDE.md

## Overview

fzt (fuzzy tiered) is a pure scoring and state engine: depth-aware tiered scoring, tree state management, input handling, and pluggable data sources. Written in Go. No TUI, no terminal rendering, no frontend concerns -- those live in `nelsong6/fzt-terminal`.

### Package structure

- **`core/`** -- Public library. Data types (`Item`, `ItemAction`, `TieredScore`, `StyledRune`), fuzzy scoring (`FuzzyMatch`, `ScoreItem`), column parsing (`ParseLines`, `ComputeWidths`, `FormatRow`), YAML loading (`LoadYAML`, `LoadYAMLFromString`), ANSI parsing (`ParseANSI`, `StripANSI`), tree state management (`State`, `TreeContext`, `PushScope`, `PopScope`, `FilterItems`, `TreeVisibleItems`, `UpdateQueryExpansion`, `SyncTreeCursorToTopMatch`, `BuildScopePath`, `ExpandToPath`, `SetTitle`, `ClearTitle`), tree editing (`AddItemAfter`, `AddChildTo`, `DeleteItem`, `CanDelete`, `SerializeTree`), key handlers (`HandleUnifiedKey`, `HandleKeyEvent`, `HandleTreeKey`, `HandleSearchKey`, `ClickUnifiedRow`, `syncQueryToCursor`, `handleRenameKey`), the `TreeProvider` interface for pluggable data sources, `DirProvider` for filesystem trees, `ListDriveRoots` (Windows), and `Config`.
- **`render/`** -- Headless rendering infrastructure. `Canvas` interface, `MemScreen` (in-memory grid with snapshot/styled-snapshot output), `Session`/`NewTreeSession` (headless wrapper for WASM/testing), ANSI serialization (`ToANSI`), structured data API (`GetVisibleRows`, `GetPromptState`, `GetUIState`), `Version` variable.
- **`cmd/fzt/`** -- Minimal scoring CLI: `echo lines | fzt "query"` -> ranked output. No TUI, no interaction.
- **`cmd/ansicheck/`**, **`cmd/debuginput/`**, **`cmd/icontest/`** -- Dev utilities.

### Ecosystem

Interactive tools import fzt alongside `nelsong6/fzt-terminal` which provides terminal/browser renderers, style (Catppuccin, DOS font, CRT), and frontend behavior (command palette, identity, actions). See the architecture diagrams at `diagrams.romaine.life/fzt/final`.

Cross-repo references:

- Command palette injection and action routing: `fzt-terminal/command.go`
- WASM bridge exposing engine to browsers: `fzt-terminal/cmd/wasm/main.go`
- Bookmark data flow into fzt: `my-homepage/frontend/fzh-terminal.js` (bookmarksToYaml -> loadYAML -> init)
- Ancestor matching design implication: `core/scorer.go` ScoreItem comment

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

### Ancestor matching eliminates name collisions

Items with the same name in different folders are uniquely searchable. Ancestor names are inherited via `ParentIdx` chain and scored at the ancestor tier. Example: the command palette has "on"/"off" leaves under both `version` and `whoami` -- "whoami on" finds only whoami's "on" because "whoami" must match an ancestor, and only whoami's children have it. Do NOT rename items to avoid apparent collisions -- the scoring system handles disambiguation. This is the intended design. See `core/scorer.go` ScoreItem comment.

### Config field relationships

- `TreeMode` is a renderer flag consumed by fzt-terminal's tui package. The engine does not read it.
- `Tiered` enables hierarchical scoring in the engine: depth penalty, ancestor matching, scope-based search pools in FilterItems. TreeMode implies Tiered in practice (all tree-mode callers set both), but they are separate fields.
- `SearchCols` (1-based) restricts which fields qualify for the Name tier. If empty, falls back to `Nth`. Description fields (index 1+) are always searchable at the Desc tier regardless.
- `DepthPenalty` is subtracted from Name tier score as `relativeDepth * DepthPenalty`. Relative depth is measured from current scope, not absolute. All callers use 5.
- `FrontendName`/`FrontendVersion`/`FrontendCommands` are set by the ecosystem (fzt-terminal ApplyConfig), not the engine. They drive the two-level `:` palette via InjectCommandFolder.
- `HidePalette` suppresses the `:` palette entirely. When set, `InjectCommandFolder` early-returns — no `:` root row, no palette items in `AllItems`, so typing `:` can't reach them via search either. Set by consumers where the palette is meaningless (my-homepage's unauthenticated playground visitors, for whom edit/logout make no sense). Default false; fzt-automate / fzt-picker / authenticated homepage keep the palette visible.
- `InitialDisplay` maps to `State.IdentityLabel` -- shown via "whoami > on" in the command palette.
- `FoldersOnly` changes Enter key behavior: Enter on an already-scoped folder returns `"select:"` instead of no-op. Used by picker's folder-pick mode.

### Per-character scoring (FuzzyMatch)

Left-to-right character scan: +1 per match, +2 if consecutive, +3 bonus at position 0 or after a word boundary (space, `/`, `-`, `_`, `>`).

## Tree State

Tree state lives in `core/`. `State` holds a stack of `TreeContext` values, each containing dataset, query, tree navigation, scope levels, and context identity. All tree operations read/write the top context via `s.TopCtx()`.

### Key concepts

- **Scope**: Entering a folder pushes a `ScopeLevel` that saves query/cursor/offset. `PopScope` restores state and conditionally collapses the folder.
- **Context stack**: `State.Contexts` is a stack of `TreeContext` values. Index 0 = primary dataset. The command palette pushes a second context on top with its own items, query, and scope. `PushContext`/`PopContext` manage the stack. `TopCtx()` always returns the active context.
- **Provider**: `TreeProvider` interface enables lazy-loading. When `PushScope` encounters a folder with no loaded children and a provider is set, it calls `LoadChildren` to splice items dynamically.
- **Filtering**: `FilterItems` applies the current query to the search pool. In tiered mode, searches all descendants (optionally depth-limited via `SearchDepth`) with ancestor name inheritance. Results are sorted by `TieredScore.Less()`.
- **Auto-expansion**: `UpdateQueryExpansion` sets `QueryExpanded` flags to reveal the top match. `SyncTreeCursorToTopMatch` positions the cursor on it.
- **Hidden folders**: Items with `Hidden: true` are excluded from the visible tree but participate in search. `TreeVisibleItems` starts from a hidden folder's children (exclusive "takeover" view) when the user is scoped inside it. The `:` palette folder used to be Hidden (2026-04-19: flipped to visible so it shows as a regular row at root — discoverability over stealth). No current consumer marks any item Hidden — the field is retained for future use.

### Input handlers

All in `core/input.go`:

- `HandleUnifiedKey(s, key, ch, shift, cfg, searchCols)` -- Entry point for unified tree+search mode. Takes a `shift bool` so event sources can report modifier state (picker CGo via `GetKeyState`, WASM via browser events, tui.Run via `ev.Modifiers()`). Handles rename-mode dispatch, Shift+Enter universal confirm-select, normal-mode lowercase `hjkl` + `/` + `` ` `` + Backspace routing, and delegates other keys to `HandleTreeKey` / `HandleSearchKey`. Ctrl bindings are gone — no Ctrl+C/P/N/A/E/U/W anywhere.
- `HandleKeyEvent(s, key, ch, shift, cfg, searchCols)` -- Flat (non-tree) mode. Same shift plumb. Shift+Enter commits the highlighted filtered item; Home/End move the query cursor; Escape cascades (clear query → pop scope → pop context → quit).
- `HandleTreeKey` -- Pure tree navigation when no query is active. Up/Down move cursor, Enter pushes scope on folders (selects the folder if `FoldersOnly` and already scoped, otherwise no-op), Left collapses/moves to parent. No shift param needed — the caller (`HandleUnifiedKey`) has already consumed Shift+Enter before delegating.
- `HandleSearchKey` -- Search-active mode. Typing edits query (capitals + shifted symbols literal — Shift is Shift); lowercase `hjkl` recursively routes to nav when NavMode is set; `/` exits normal mode back to search; `` ` `` enters normal mode from search. Tab autocompletes. Space on folder pushes scope. Shift+Enter commits cursor's item.
- `syncQueryToCursor` -- Called during Up/Down navigation in search mode. Updates the search query to match the highlighted item's name and clears stale Filtered results so the search bar follows the cursor.
- `normalModeNavBinding(ch)` -- Maps `'h'` / `'j'` / `'k'` / `'l'` to their tcell arrow keys + title-glyph strings. Used by both `HandleUnifiedKey`'s SA=false branch and `HandleSearchKey`'s NavMode branch so the hjkl vocabulary is centralized.

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

## Change Log

### 2026-04-09

**core/input.go**
- Enter-on-folder fix: Enter on a folder the user is already scoped into is now a no-op (or triggers the folder's link URL if it has one). Prevents repeated `PushScope` stacking scope levels. Guard added in both `HandleTreeKey` and `HandleSearchKey` Enter handlers.
- `syncQueryToCursor`: New function. When navigating Up/Down in search mode, the search query updates to match the highlighted item's name and stale `Filtered` results are cleared. Makes the search bar follow the cursor in nav mode.

**core/tree.go**
- `TitleOverride` + `TitleStyle` fields on `State`: `TitleOverride` replaces the default console title when set. `TitleStyle` controls color (0=default cyan, 1=green success, 2=red error). Added `SetTitle`/`ClearTitle` methods that evict all ambient displays (`SyncTimerShown` etc.).
- `SyncIcon`, `SyncNextCheck`, `SyncTimerShown` fields on `State`: Infrastructure for background sync check. `SyncIcon` shows in top-right corner when sync is available. `SyncNextCheck` tracks next check timestamp. `SyncTimerShown` enables live countdown display.
- `JWTSecret`, `ConfigDir` fields on `State`: JWT secret sourced from OS credential store. `ConfigDir` for sync state files.
- `RecalcNameColWidth` removed: Column width is no longer recalculated globally after command injection. The description column now flows naturally with visible content.

**core/config.go**
- `Config.ConfigDir` added: Plumbed from automate `main.go` through to `State` for sync operations.
- `Config.InitialMenuVersion` added: Persisted menu version for conflict detection on save.

### 2026-04-10

**core/model.go**
- `ItemAction` struct: `{Type, Target}` replaces the old `Item.URL` string and `Item.Action` string with a single structured pointer (`*ItemAction`). Type is "url", "command", or "function"; Target is the value. nil = informational or folder.
- `IsProperty`, `PropertyOf`, `PropertyKey` fields on `Item`: Support temporary property items for inspect mode. `PropertyKey` values: "name", "description", "url", "action".

**core/tree.go**
- `EditMode`, `EditBuffer`, `EditTargetIdx`, `EditOrigName` fields on `State`: Edit mode infrastructure for rename/property editing.
- `Dirty`, `MenuVersion` fields on `State`: Track unsaved changes and current menu version for conflict detection.
- `InspectTargetIdx`, `InspectItemIdxs` fields on `State`: Inspect mode state — which item is being inspected and the indices of its temporary property items.
- `AddItemAfter`, `AddChildTo`: Insert items into the flat tree, updating parent Children arrays.
- `DeleteItem`: Soft-delete (hide) items and mark tree dirty.
- `CanDelete`: Prevents deleting items in the active scope chain.
- `SerializeTree`: Converts AllItems back to nested `[]interface{}` for API persistence. Decomposes `*ItemAction` into separate "url"/"action" keys for backwards-compatible JSON.

**core/input.go**
- `handleRenameKey`: New key handler for rename/property edit mode. Processes typing, backspace, Enter (confirm), Escape (cancel). Property edits update the parent item's fields or `*ItemAction`.
- Folder-URL checks updated: `item.URL != ""` → `item.Action != nil && item.Action.Type == "url"`.

**core/yaml.go**
- `entryToAction`: Converts YAML `url`/`action` strings into `*ItemAction`. URL takes precedence (type "url"). Keeps YAML format backwards-compatible.

**render/session.go**
- `SelectedURL` updated: Reads from `Action.Target` when `Action.Type == "url"` instead of the removed `Item.URL` field.

