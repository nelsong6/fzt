# CLAUDE.md

## Overview

fzh (fuzzy hierarchical) is an fzf-compatible fuzzy finder with two additions: depth-aware tiered scoring and first-class column support. Written in Go, uses tcell for the TUI.

Repo: `D:\repos\fuzzy-finder-tiers`
Binary: `fzh.exe` (built to repo root, on PATH via Profile 1's `profile.ps1`)

## Building

```
go build -o fzh.exe .
```

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
- `--show-scores` — annotates filter output with `[score=N]`
- `--simulate` — headless rendering (no terminal needed), generates frame-by-frame text snapshots
  - `--sim-query="text"` — types each character, one frame per keystroke
  - `--width=N --height-lines=N` — virtual terminal size
  - `--styled` — adds `[H]`=highlight `[S]`=selected `[*]`=both markers
  - `--record file.txt` — write frames to file instead of stdout

## Key Flags

fzf-compatible: `--layout`, `--border`, `--header-lines`, `--nth`, `--accept-nth`, `--prompt`, `--delimiter`, `--height`
New: `--tiered`, `--depth-penalty`, `--search-cols`, `--ansi`
