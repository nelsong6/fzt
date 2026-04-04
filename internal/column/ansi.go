package column

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/nelsong6/fzt/internal/model"
)

// ansiPattern matches ANSI CSI sequences like \x1b[38;5;229m or \x1b[0m.
var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[()][0-9A-Za-z]`)

// StripANSI removes all ANSI escape sequences from a string.
func StripANSI(s string) string {
	return ansiPattern.ReplaceAllString(s, "")
}

// ParseANSI parses a string containing ANSI escape codes into a slice of styled runes.
// Each rune carries the tcell style that was active at its position.
func ParseANSI(s string) []model.StyledRune {
	var result []model.StyledRune
	style := tcell.StyleDefault

	runes := []rune(s)
	i := 0
	for i < len(runes) {
		if runes[i] == '\x1b' && i+1 < len(runes) && runes[i+1] == '[' {
			// Parse CSI sequence: \x1b[ ... m
			j := i + 2
			for j < len(runes) && ((runes[j] >= '0' && runes[j] <= '9') || runes[j] == ';') {
				j++
			}
			if j < len(runes) && runes[j] == 'm' {
				// SGR sequence — parse the parameters
				params := string(runes[i+2 : j])
				style = applySGR(style, params)
				i = j + 1
				continue
			}
			// Non-SGR CSI sequence — skip it
			if j < len(runes) {
				i = j + 1
			} else {
				i = j
			}
			continue
		} else if runes[i] == '\x1b' {
			// Other escape sequence — skip until we find a letter or run out
			j := i + 1
			for j < len(runes) && !((runes[j] >= 'A' && runes[j] <= 'Z') || (runes[j] >= 'a' && runes[j] <= 'z')) {
				j++
			}
			if j < len(runes) {
				i = j + 1
			} else {
				i = j
			}
			continue
		}

		result = append(result, model.StyledRune{Char: runes[i], Style: style})
		i++
	}
	return result
}

// applySGR updates a tcell.Style based on SGR (Select Graphic Rendition) parameters.
func applySGR(style tcell.Style, params string) tcell.Style {
	if params == "" || params == "0" {
		return tcell.StyleDefault
	}

	parts := strings.Split(params, ";")
	for i := 0; i < len(parts); i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			continue
		}

		switch {
		case n == 0:
			style = tcell.StyleDefault
		case n == 1:
			style = style.Bold(true)
		case n == 2:
			style = style.Dim(true)
		case n == 3:
			style = style.Italic(true)
		case n == 4:
			style = style.Underline(true)
		case n == 7:
			style = style.Reverse(true)
		case n == 22:
			style = style.Bold(false).Dim(false)
		case n == 23:
			style = style.Italic(false)
		case n == 24:
			style = style.Underline(false)
		case n == 27:
			style = style.Reverse(false)

		// Standard foreground colors (30-37)
		case n >= 30 && n <= 37:
			style = style.Foreground(tcell.PaletteColor(n - 30))
		case n == 39:
			style = style.Foreground(tcell.ColorDefault)

		// Standard background colors (40-47)
		case n >= 40 && n <= 47:
			style = style.Background(tcell.PaletteColor(n - 40))
		case n == 49:
			style = style.Background(tcell.ColorDefault)

		// Bright foreground colors (90-97)
		case n >= 90 && n <= 97:
			style = style.Foreground(tcell.PaletteColor(n - 90 + 8))
		// Bright background colors (100-107)
		case n >= 100 && n <= 107:
			style = style.Background(tcell.PaletteColor(n - 100 + 8))

		// 256-color: 38;5;N (foreground) or 48;5;N (background)
		case n == 38:
			if i+1 < len(parts) {
				mode, _ := strconv.Atoi(parts[i+1])
				if mode == 5 && i+2 < len(parts) {
					color, _ := strconv.Atoi(parts[i+2])
					style = style.Foreground(tcell.PaletteColor(color))
					i += 2
				} else if mode == 2 && i+4 < len(parts) {
					r, _ := strconv.Atoi(parts[i+2])
					g, _ := strconv.Atoi(parts[i+3])
					b, _ := strconv.Atoi(parts[i+4])
					style = style.Foreground(tcell.NewRGBColor(int32(r), int32(g), int32(b)))
					i += 4
				}
			}
		case n == 48:
			if i+1 < len(parts) {
				mode, _ := strconv.Atoi(parts[i+1])
				if mode == 5 && i+2 < len(parts) {
					color, _ := strconv.Atoi(parts[i+2])
					style = style.Background(tcell.PaletteColor(color))
					i += 2
				} else if mode == 2 && i+4 < len(parts) {
					r, _ := strconv.Atoi(parts[i+2])
					g, _ := strconv.Atoi(parts[i+3])
					b, _ := strconv.Atoi(parts[i+4])
					style = style.Background(tcell.NewRGBColor(int32(r), int32(g), int32(b)))
					i += 4
				}
			}
		}
	}
	return style
}
