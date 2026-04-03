//go:build js && wasm

package main

import (
	"syscall/js"

	"github.com/gdamore/tcell/v2"
	"github.com/nelsong6/fzh/internal/model"
	"github.com/nelsong6/fzh/internal/tui"
	"github.com/nelsong6/fzh/internal/yamlsrc"
)

var (
	currentItems []model.Item
	session      *tui.Session
)

func main() {
	js.Global().Set("fzh", js.ValueOf(map[string]interface{}{
		"init":      js.FuncOf(initSession),
		"handleKey": js.FuncOf(handleKey),
		"resize":    js.FuncOf(resize),
		"loadYAML":  js.FuncOf(loadYAML),
	}))
	select {}
}

// loadYAML parses YAML and stores items, but does not create a session.
func loadYAML(this js.Value, args []js.Value) interface{} {
	if len(args) < 1 {
		return jsError("loadYAML requires a YAML string argument")
	}
	items, err := yamlsrc.LoadFromString(args[0].String())
	if err != nil {
		return jsError(err.Error())
	}
	currentItems = items
	return js.Null()
}

// initSession creates a new headless TUI session.
// Args: cols (int), rows (int)
// Returns: {ansi: string, cursorX: int, cursorY: int}
func initSession(this js.Value, args []js.Value) interface{} {
	if len(args) < 2 {
		return jsError("init requires (cols, rows)")
	}
	cols := args[0].Int()
	rows := args[1].Int()

	if len(currentItems) == 0 {
		return jsError("no items loaded — call loadYAML first")
	}

	cfg := tui.Config{
		Layout:       "reverse",
		Border:       true,
		Tiered:       true,
		DepthPenalty: 5,
	}

	session = tui.NewSession(currentItems, cfg, cols, rows)
	frame := session.Render()
	return frameToJS(frame)
}

// handleKey processes a keyboard event.
// Args: key (string, e.g. "ArrowUp", "Enter", "a"), ctrl (bool), shift (bool)
// Returns: {ansi: string, cursorX: int, cursorY: int, action: string}
func handleKey(this js.Value, args []js.Value) interface{} {
	if session == nil {
		return jsError("session not initialized")
	}
	if len(args) < 3 {
		return jsError("handleKey requires (key, ctrl, shift)")
	}

	keyStr := args[0].String()
	ctrl := args[1].Bool()
	shift := args[2].Bool()

	key, ch := translateKey(keyStr, ctrl, shift)
	if key == tcell.KeyRune && ch == 0 {
		// Unrecognized key — ignore
		return js.Null()
	}

	frame, action := session.HandleKey(key, ch)

	obj := js.Global().Get("Object").New()
	obj.Set("ansi", frame.ANSI)
	obj.Set("cursorX", frame.CursorX)
	obj.Set("cursorY", frame.CursorY)
	obj.Set("action", action)
	return obj
}

// resize changes the terminal dimensions.
// Args: cols (int), rows (int)
// Returns: {ansi: string, cursorX: int, cursorY: int}
func resize(this js.Value, args []js.Value) interface{} {
	if session == nil {
		return jsError("session not initialized")
	}
	if len(args) < 2 {
		return jsError("resize requires (cols, rows)")
	}
	cols := args[0].Int()
	rows := args[1].Int()

	frame := session.Resize(cols, rows)
	return frameToJS(frame)
}

// translateKey maps browser key event properties to tcell key + rune.
func translateKey(key string, ctrl, shift bool) (tcell.Key, rune) {
	switch key {
	case "ArrowUp":
		return tcell.KeyUp, 0
	case "ArrowDown":
		return tcell.KeyDown, 0
	case "ArrowLeft":
		return tcell.KeyLeft, 0
	case "ArrowRight":
		return tcell.KeyRight, 0
	case "Enter":
		return tcell.KeyEnter, 0
	case "Escape":
		return tcell.KeyEscape, 0
	case "Backspace":
		return tcell.KeyBackspace2, 0
	case "Delete":
		return tcell.KeyDelete, 0
	case "Tab":
		if shift {
			return tcell.KeyBacktab, 0
		}
		return tcell.KeyTab, 0
	case "Home":
		return tcell.KeyCtrlA, 0
	case "End":
		return tcell.KeyCtrlE, 0
	}

	// Single character keys
	if len(key) == 1 {
		r := rune(key[0])
		if ctrl {
			switch r {
			case 'a', 'A':
				return tcell.KeyCtrlA, 0
			case 'e', 'E':
				return tcell.KeyCtrlE, 0
			case 'u', 'U':
				return tcell.KeyCtrlU, 0
			case 'w', 'W':
				return tcell.KeyCtrlW, 0
			case 'p', 'P':
				return tcell.KeyCtrlP, 0
			case 'n', 'N':
				return tcell.KeyCtrlN, 0
			case 'c', 'C':
				return tcell.KeyCtrlC, 0
			}
			return tcell.KeyRune, 0 // unknown ctrl combo — ignore
		}
		return tcell.KeyRune, r
	}

	// Multi-character key name we don't handle (Shift, Control, etc.)
	return tcell.KeyRune, 0
}

func frameToJS(frame tui.SessionFrame) interface{} {
	obj := js.Global().Get("Object").New()
	obj.Set("ansi", frame.ANSI)
	obj.Set("cursorX", frame.CursorX)
	obj.Set("cursorY", frame.CursorY)
	return obj
}

func jsError(msg string) interface{} {
	return js.Global().Get("Error").New(msg)
}
