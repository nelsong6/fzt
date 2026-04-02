package column

import (
	"strings"

	"github.com/nelsong6/fzh/internal/model"
)

// ParseLines splits raw input lines by delimiter into Items.
// If tiered is true, the first field is parsed as the depth integer.
// If ansi is true, ANSI codes are parsed into StyledFields for colored rendering.
func ParseLines(lines []string, delimiter string, tiered bool, ansi bool) []model.Item {
	items := make([]model.Item, 0, len(lines))
	for _, line := range lines {
		item := parseLine(line, delimiter, tiered, ansi)
		items = append(items, item)
	}
	return items
}

func parseLine(line, delimiter string, tiered bool, ansi bool) model.Item {
	cleanLine := StripANSI(line)
	cleanParts := strings.Split(cleanLine, delimiter)
	displayParts := strings.Split(line, delimiter)

	depth := 0

	if tiered && len(cleanParts) > 1 {
		d := 0
		for _, c := range cleanParts[0] {
			if c >= '0' && c <= '9' {
				d = d*10 + int(c-'0')
			}
		}
		depth = d
		cleanParts = cleanParts[1:]
		displayParts = displayParts[1:]
	}

	item := model.Item{
		Fields:        cleanParts,
		DisplayFields: displayParts,
		Depth:         depth,
		Original:      line,
	}

	if ansi {
		item.StyledFields = make([][]model.StyledRune, len(displayParts))
		for i, dp := range displayParts {
			item.StyledFields[i] = ParseANSI(dp)
		}
	}

	return item
}

// ComputeWidths calculates the max width per column across all items.
// Uses the clean (ANSI-stripped) Fields for accurate width calculation.
func ComputeWidths(items []model.Item) []int {
	maxCols := 0
	for _, item := range items {
		if len(item.Fields) > maxCols {
			maxCols = len(item.Fields)
		}
	}

	widths := make([]int, maxCols)
	for _, item := range items {
		for i, field := range item.Fields {
			runeLen := len([]rune(field))
			if runeLen > widths[i] {
				widths[i] = runeLen
			}
		}
	}
	return widths
}

// FormatRow renders a single item's fields as a padded, aligned string.
func FormatRow(fields []string, widths []int, gap int) string {
	if gap <= 0 {
		gap = 2
	}
	var b strings.Builder
	for i, field := range fields {
		if i > 0 {
			b.WriteString(strings.Repeat(" ", gap))
		}
		b.WriteString(field)
		if i < len(fields)-1 && i < len(widths) {
			pad := widths[i] - len([]rune(field))
			if pad > 0 {
				b.WriteString(strings.Repeat(" ", pad))
			}
		}
	}
	return b.String()
}

// FormatHeader renders a header row using the given column names.
func FormatHeader(names []string, widths []int, gap int) string {
	return FormatRow(names, widths, gap)
}
