// fzt-terminal.js — Shared terminal renderer for fzt WASM consumers.
// Parses ANSI output from the fzt Go TUI, renders styled HTML spans
// into a container element, and forwards keyboard/mouse events.
//
// Usage:
//   import { createFztTerminal } from './fzt-terminal.js';
//   const term = createFztTerminal(container, { palette, fontFamily, ... });
//   await term.initWasm();
//   term.loadYAML(yaml);
//   term.initSession();

// ── ANSI parser ────────────────────────────────────────────────
// Parses ANSI-escaped text into a 2D grid of styled cells.
function parseANSI(ansi, palette, palette256Fn) {
  const rows = ansi.split("\n");
  const grid = [];

  for (const row of rows) {
    const cells = [];
    let fg = null;
    let bg = null;
    let bold = false;
    let italic = false;
    let dim = false;
    let underline = false;

    let i = 0;
    while (i < row.length) {
      if (row[i] === "\x1b" && row[i + 1] === "[") {
        let j = i + 2;
        while (j < row.length && row[j] !== "m") j++;
        if (j < row.length) {
          const params = row.slice(i + 2, j).split(";").map(Number);
          let p = 0;
          while (p < params.length) {
            const n = params[p];
            if (n === 0) {
              fg = null; bg = null;
              bold = false; italic = false; dim = false; underline = false;
            } else if (n === 1) { bold = true; }
            else if (n === 2) { dim = true; }
            else if (n === 3) { italic = true; }
            else if (n === 4) { underline = true; }
            else if (n === 22) { bold = false; dim = false; }
            else if (n === 23) { italic = false; }
            else if (n === 24) { underline = false; }
            else if (n >= 30 && n <= 37) { fg = palette[n - 30]; }
            else if (n === 39) { fg = null; }
            else if (n >= 40 && n <= 47) { bg = palette[n - 40]; }
            else if (n === 49) { bg = null; }
            else if (n >= 90 && n <= 97) { fg = palette[n - 90 + 8]; }
            else if (n >= 100 && n <= 107) { bg = palette[n - 100 + 8]; }
            else if (n === 38) {
              if (params[p + 1] === 5) {
                fg = palette256Fn(params[p + 2]);
                p += 2;
              } else if (params[p + 1] === 2) {
                fg = `rgb(${params[p + 2]},${params[p + 3]},${params[p + 4]})`;
                p += 4;
              }
            } else if (n === 48) {
              if (params[p + 1] === 5) {
                bg = palette256Fn(params[p + 2]);
                p += 2;
              } else if (params[p + 1] === 2) {
                bg = `rgb(${params[p + 2]},${params[p + 3]},${params[p + 4]})`;
                p += 4;
              }
            }
            p++;
          }
          i = j + 1;
          continue;
        }
      }
      const cp = row.codePointAt(i);
      const char = String.fromCodePoint(cp);
      // Detect wide characters: supplementary plane + BMP Private Use Area (nerd font icons)
      const wide = cp > 0xFFFF || (cp >= 0xE000 && cp <= 0xF8FF);
      cells.push({ char, fg, bg, bold, italic, dim, underline, wide });
      i += char.length;
    }
    // Mark padding cells after double-width characters
    for (let j = 0; j < cells.length; j++) {
      if (cells[j].wide && j + 1 < cells.length && cells[j + 1].char === " ") {
        cells[j + 1].widePad = true;
      }
    }
    grid.push(cells);
  }
  return grid;
}

// 256-color palette helper
function makePalette256(palette) {
  return function palette256(n) {
    if (n < 16) return palette[n];
    if (n < 232) {
      n -= 16;
      const r = Math.floor(n / 36) * 51;
      const g = Math.floor((n % 36) / 6) * 51;
      const b = (n % 6) * 51;
      return `rgb(${r},${g},${b})`;
    }
    const v = 8 + (n - 232) * 10;
    return `rgb(${v},${v},${v})`;
  };
}

// ── Grid renderer ──────────────────────────────────────────────
function renderGrid(grid, cursorX, cursorY, container, opts) {
  const { cursorClass, nerdFontFamily, onRowClick } = opts;
  const frag = document.createDocumentFragment();

  for (let y = 0; y < grid.length; y++) {
    const row = grid[y];
    const rowDiv = document.createElement("div");
    if (onRowClick) {
      rowDiv.style.cursor = "pointer";
      const rowIdx = y;
      rowDiv.addEventListener("click", () => onRowClick(rowIdx));
    }
    let lastBg = null;
    let i = 0;
    while (i < row.length) {
      const start = i;
      const cell = row[i];
      const isCursorCell = (y === cursorY && i === cursorX);

      i++;

      if (cell.wide && i < row.length && row[i].widePad) {
        i++;
      } else if (!isCursorCell && !cell.wide) {
        while (
          i < row.length &&
          !row[i].wide &&
          !row[i].widePad &&
          row[i].fg === cell.fg &&
          row[i].bg === cell.bg &&
          row[i].bold === cell.bold &&
          row[i].italic === cell.italic &&
          row[i].dim === cell.dim &&
          row[i].underline === cell.underline &&
          !(y === cursorY && i === cursorX)
        ) {
          i++;
        }
      }

      const span = document.createElement("span");
      let text = "";
      for (let j = start; j < i; j++) {
        text += row[j].char;
      }
      span.textContent = text;

      const styles = [];
      if (cell.fg) styles.push(`color:${cell.fg}`);
      if (cell.bg) styles.push(`background:${cell.bg}`);
      if (cell.bold) styles.push("font-weight:bold");
      if (cell.italic) styles.push("font-style:italic");
      if (cell.dim) styles.push("opacity:0.6");
      if (cell.underline) styles.push("text-decoration:underline");
      if (cell.wide) {
        const font = nerdFontFamily || "'Symbols Nerd Font Mono',monospace";
        styles.push(`display:inline-block;width:calc(2 * var(--char-w));overflow:hidden;text-align:center;font-family:${font};vertical-align:bottom;line-height:1.2`);
      }

      if (isCursorCell) {
        span.className = cursorClass;
      }
      lastBg = cell.bg;

      if (styles.length > 0) {
        span.setAttribute("style", styles.join(";"));
      }
      rowDiv.appendChild(span);
    }
    if (lastBg) {
      rowDiv.style.background = lastBg;
    }
    frag.appendChild(rowDiv);
  }

  container.innerHTML = "";
  container.appendChild(frag);
}

// ── Font metrics ───────────────────────────────────────────────
function measureChar(fontFamily) {
  const probe = document.createElement("pre");
  probe.style.cssText =
    "position:absolute;left:-9999px;top:-9999px;white-space:pre;" +
    `font-family:${fontFamily};font-size:16px;line-height:1.2;` +
    "padding:0;margin:0;border:0";
  probe.textContent = "MMMMMMMMMM";
  document.body.appendChild(probe);
  const rect = probe.getBoundingClientRect();
  document.body.removeChild(probe);

  const w = rect.width / 10;
  const h = rect.height;

  if (w >= 4 && h >= 8) {
    document.documentElement.style.setProperty("--char-w", w + "px");
    return { w, h };
  }
  return { w: 9.6, h: 19.2 };
}

// ── Key forwarding ─────────────────────────────────────────────
const FZT_FORWARD_KEYS = [
  "ArrowUp", "ArrowDown", "ArrowLeft", "ArrowRight",
  "Enter", "Escape", "Backspace", "Delete", "Tab", "Home", "End",
];
const FZT_CTRL_KEYS = "aAeEuUwWpPnNcC";

function shouldForwardKey(e, isActive, isEditMode, extraCheck) {
  if (isEditMode) return false;
  if (!isActive) return false;
  if (extraCheck && !extraCheck(e)) return false;
  if (document.activeElement && document.activeElement.matches("input, textarea, select")) return false;
  if (e.key.length === 1 && !e.ctrlKey && !e.altKey && !e.metaKey) return true;
  if (e.ctrlKey && FZT_CTRL_KEYS.includes(e.key)) return true;
  return FZT_FORWARD_KEYS.includes(e.key);
}

// ── Factory ────────────────────────────────────────────────────
// Creates an fzt terminal instance bound to a container element.
//
// Options:
//   palette          - 16-color array (required)
//   fontFamily       - CSS font-family for metrics (default: monospace)
//   nerdFontFamily   - font-family for wide/icon spans (default: 'Symbols Nerd Font Mono',monospace)
//   cursorClass      - CSS class for cursor span (default: "fzt-cursor")
//   containerPadding - px to subtract from each side for grid sizing (default: 0)
//   onAction         - callback(action, url) for leaf selection / cancel
//   onRender         - callback(frame) after each render
//   shouldForwardKey - extra check(e) returning bool, called before forwarding
//   defaultCursorPos - {x, y} fallback when Go hides cursor (default: {x:3, y:1})
//   wasmUrl          - URL to fetch fzt.wasm (default: "fzt.wasm")
//
export function createFztTerminal(container, options = {}) {
  const palette = options.palette;
  if (!palette || palette.length < 16) {
    throw new Error("fzt-terminal: palette (16-color array) is required");
  }

  const fontFamily = options.fontFamily || "monospace";
  const nerdFontFamily = options.nerdFontFamily || "'Symbols Nerd Font Mono',monospace";
  const cursorClass = options.cursorClass || "fzt-cursor";
  const containerPadding = options.containerPadding || 0;
  const defaultCursorPos = options.defaultCursorPos || { x: 3, y: 1 };
  const wasmUrl = options.wasmUrl || "fzt.wasm";
  const extraForwardCheck = options.shouldForwardKey || null;

  const palette256 = makePalette256(palette);
  let charSize = null;
  let ready = false;
  let sessionActive = false;
  let active = true;
  let editMode = false;
  let rendering = false;
  let lastGridSize = null;
  let onAction = options.onAction || null;
  let onRender = options.onRender || null;
  let keydownHandler = null;
  let resizeObserver = null;

  function render(result) {
    if (!result || result instanceof Error) return;
    if (rendering) return;
    rendering = true;
    try {
      const grid = parseANSI(result.ansi, palette, palette256);
      let cx = result.cursorX;
      let cy = result.cursorY;
      if (cx < 0 || cy < 0) {
        cx = defaultCursorPos.x;
        cy = defaultCursorPos.y;
      }
      renderGrid(grid, cx, cy, container, {
        cursorClass,
        nerdFontFamily,
        onRowClick: handleRowClick,
      });
      if (onRender) onRender(result);
    } finally {
      rendering = false;
    }
  }

  function computeGridSize() {
    const rect = container.getBoundingClientRect();
    if (rect.width < 10 || rect.height < 10) {
      return { cols: 80, rows: 24 };
    }
    if (!charSize) charSize = measureChar(fontFamily);
    const pad = containerPadding * 2;
    const cols = Math.min(Math.max(Math.floor((rect.width - pad) / charSize.w), 20), 250);
    const rows = Math.min(Math.max(Math.floor((rect.height - pad) / charSize.h), 5), 80);
    return { cols, rows };
  }

  function sendKey(key, ctrlKey, shiftKey) {
    try {
      const result = fzt.handleKey(key, ctrlKey, shiftKey);
      if (result instanceof Error) {
        console.error("fzt.handleKey error:", result.message);
        return;
      }
      render(result);
      if (result.action && onAction) {
        onAction(result.action, result.url);
      }
    } catch (err) {
      console.error("handleKey threw:", err);
    }
  }

  function handleRowClick(row) {
    if (!sessionActive || editMode) return;
    try {
      const result = fzt.clickRow(row);
      if (result instanceof Error) {
        console.error("fzt.clickRow error:", result.message);
        return;
      }
      render(result);
      if (result.action && onAction) {
        onAction(result.action, result.url);
      }
    } catch (err) {
      console.error("clickRow threw:", err);
    }
  }

  // Public API
  return {
    async initWasm() {
      container.textContent = "Loading fzt...";
      await document.fonts.ready;
      charSize = measureChar(fontFamily);

      const go = new Go();
      const result = await WebAssembly.instantiateStreaming(
        fetch(wasmUrl),
        go.importObject
      );
      go.run(result.instance);
      ready = true;

      // Wire keyboard listener
      keydownHandler = (e) => {
        if (!sessionActive) return;
        if (!shouldForwardKey(e, active, editMode, extraForwardCheck)) return;
        e.preventDefault();
        sendKey(e.key, e.ctrlKey, e.shiftKey);
      };
      document.addEventListener("keydown", keydownHandler);

      // Wire resize observer
      resizeObserver = new ResizeObserver(() => {
        if (!sessionActive) return;
        try {
          const { cols, rows } = computeGridSize();
          const key = cols + "x" + rows;
          if (key === lastGridSize) return;
          lastGridSize = key;
          const result = fzt.resize(cols, rows);
          if (result instanceof Error) {
            console.error("fzt.resize error:", result.message);
            return;
          }
          render(result);
        } catch (err) {
          console.error("resize threw:", err);
        }
      });
      resizeObserver.observe(container);
    },

    loadYAML(yaml) {
      if (!ready) return;
      const result = fzt.loadYAML(yaml);
      if (result instanceof Error) {
        console.error("fzt.loadYAML error:", result.message);
        container.textContent = "Error loading YAML: " + result.message;
        return false;
      }
      return true;
    },

    initSession() {
      if (!ready) return;
      const { cols, rows } = computeGridSize();
      lastGridSize = cols + "x" + rows;
      const result = fzt.init(cols, rows);
      if (result instanceof Error) {
        console.error("fzt.init error:", result.message);
        container.textContent = "Error: " + result.message;
        return false;
      }
      sessionActive = true;
      active = true;
      render(result);
      return true;
    },

    sendKey(key, ctrlKey, shiftKey) {
      sendKey(key, ctrlKey, shiftKey);
    },

    clickRow(row) {
      handleRowClick(row);
    },

    resize() {
      if (!sessionActive) return;
      const { cols, rows } = computeGridSize();
      const key = cols + "x" + rows;
      if (key === lastGridSize) return;
      lastGridSize = key;
      const result = fzt.resize(cols, rows);
      if (!(result instanceof Error)) render(result);
    },

    render() {
      if (!sessionActive) return;
      const { cols, rows } = computeGridSize();
      const result = fzt.resize(cols, rows);
      if (!(result instanceof Error)) render(result);
    },

    setActive(val) {
      active = val;
      if (val && sessionActive) {
        requestAnimationFrame(() => {
          const { cols, rows } = computeGridSize();
          const key = cols + "x" + rows;
          if (key !== lastGridSize) {
            lastGridSize = key;
            const result = fzt.resize(cols, rows);
            if (!(result instanceof Error)) render(result);
          }
        });
      }
    },

    setEditMode(val) {
      editMode = val;
    },

    onAction(callback) {
      onAction = callback;
    },

    isReady() {
      return ready;
    },

    isSessionActive() {
      return sessionActive;
    },

    computeGridSize,

    destroy() {
      if (keydownHandler) {
        document.removeEventListener("keydown", keydownHandler);
        keydownHandler = null;
      }
      if (resizeObserver) {
        resizeObserver.disconnect();
        resizeObserver = null;
      }
      sessionActive = false;
      ready = false;
    },
  };
}
