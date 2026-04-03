package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
)

// ToANSI serializes the MemScreen grid as an ANSI-escaped string.
// Each row becomes a line of text with SGR escape codes for styling.
// Palette colors (0-15) emit standard ANSI codes; RGB colors emit true color.
func (m *MemScreen) ToANSI() string {
	var b strings.Builder
	for y := 0; y < m.H; y++ {
		var lastFg, lastBg tcell.Color
		var lastAttrs tcell.AttrMask
		first := true

		for x := 0; x < m.W; x++ {
			style := m.Styles[y][x]
			fg, bg, attrs := style.Decompose()

			if first || fg != lastFg || bg != lastBg || attrs != lastAttrs {
				b.WriteString(sgrSequence(fg, bg, attrs))
				lastFg, lastBg, lastAttrs = fg, bg, attrs
				first = false
			}
			b.WriteRune(m.Grid[y][x])
		}
		b.WriteString("\x1b[0m") // reset at end of line
		if y < m.H-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// sgrSequence builds a full SGR escape sequence for the given style components.
func sgrSequence(fg, bg tcell.Color, attrs tcell.AttrMask) string {
	var params []string

	// Reset first, then apply
	params = append(params, "0")

	if attrs&tcell.AttrBold != 0 {
		params = append(params, "1")
	}
	if attrs&tcell.AttrDim != 0 {
		params = append(params, "2")
	}
	if attrs&tcell.AttrItalic != 0 {
		params = append(params, "3")
	}
	if attrs&tcell.AttrUnderline != 0 {
		params = append(params, "4")
	}
	if attrs&tcell.AttrReverse != 0 {
		params = append(params, "7")
	}
	if attrs&tcell.AttrStrikeThrough != 0 {
		params = append(params, "9")
	}

	if s := colorToSGR(fg, true); s != "" {
		params = append(params, s)
	}
	if s := colorToSGR(bg, false); s != "" {
		params = append(params, s)
	}

	return "\x1b[" + strings.Join(params, ";") + "m"
}

// colorToSGR converts a tcell.Color to its SGR parameter string.
func colorToSGR(c tcell.Color, isFg bool) string {
	if c == tcell.ColorDefault {
		return ""
	}

	if c.IsRGB() {
		r, g, b := c.RGB()
		if isFg {
			return fmt.Sprintf("38;2;%d;%d;%d", r, g, b)
		}
		return fmt.Sprintf("48;2;%d;%d;%d", r, g, b)
	}

	// Palette color: strip flag bits to get the index
	idx := int(c &^ (tcell.ColorValid | tcell.ColorIsRGB))
	if isFg {
		if idx < 8 {
			return strconv.Itoa(30 + idx)
		}
		if idx < 16 {
			return strconv.Itoa(90 + idx - 8)
		}
		return fmt.Sprintf("38;5;%d", idx)
	}
	if idx < 8 {
		return strconv.Itoa(40 + idx)
	}
	if idx < 16 {
		return strconv.Itoa(100 + idx - 8)
	}
	return fmt.Sprintf("48;5;%d", idx)
}
