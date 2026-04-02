package main

import (
	"fmt"
	"os"
	"time"

	"github.com/gdamore/tcell/v2"
)

func main() {
	screen, err := tcell.NewScreen()
	if err != nil {
		fmt.Fprintln(os.Stderr, "NewScreen error:", err)
		os.Exit(1)
	}
	if err := screen.Init(); err != nil {
		fmt.Fprintln(os.Stderr, "Init error:", err)
		os.Exit(1)
	}
	defer screen.Fini()

	screen.SetStyle(tcell.StyleDefault.Background(tcell.ColorBlack).Foreground(tcell.ColorWhite))
	screen.Clear()

	style := tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tcell.ColorBlack)
	hlStyle := tcell.StyleDefault.Foreground(tcell.ColorYellow).Background(tcell.ColorBlack)

	// Nerd font icons (PUA)
	icons := []struct {
		r    rune
		name string
	}{
		{0xF115, "folder-open"},
		{0xF07B, "folder"},
		{0xF01C, "inbox"},
		{0xF120, "terminal"},
		{0xE612, "go-logo"},
		{'A', "plain-A (control)"},
	}

	y := 1
	drawStr(screen, 1, y, "Nerd Font Icon Test", style)
	y += 2

	for _, ic := range icons {
		screen.SetContent(1, y, ic.r, nil, hlStyle)
		drawStr(screen, 3, y, fmt.Sprintf("U+%04X  %s", ic.r, ic.name), style)
		y++
	}

	y += 1
	drawStr(screen, 1, y, "Press ESC or q to exit", style)

	screen.Show()

	for {
		ev := screen.PollEvent()
		switch ev := ev.(type) {
		case *tcell.EventKey:
			if ev.Key() == tcell.KeyEscape || ev.Rune() == 'q' {
				return
			}
		}
		// Add a small sleep to prevent busy loop
		time.Sleep(10 * time.Millisecond)
	}
}

func drawStr(screen tcell.Screen, x, y int, s string, style tcell.Style) {
	for _, r := range s {
		screen.SetContent(x, y, r, nil, style)
		x++
	}
}
