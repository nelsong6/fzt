package main

import (
	"fmt"

	"github.com/nelsong6/fzh/internal/column"
)

func main() {
	// Simulate lsd output with ANSI + nerd font icon (U+F115 = ef 84 95)
	input := "\x1b[38;5;4m\x1b[1m\xef\x84\x95 ComfyUI\x1b[0m"

	fmt.Printf("Input bytes: %x\n", []byte(input))

	stripped := column.StripANSI(input)
	fmt.Printf("Stripped: %q\n", stripped)
	fmt.Printf("Stripped bytes: %x\n", []byte(stripped))

	runes := []rune(stripped)
	fmt.Printf("Runes: ")
	for _, r := range runes {
		fmt.Printf("U+%04X ", r)
	}
	fmt.Println()

	styled := column.ParseANSI(input)
	fmt.Printf("ParseANSI runes: ")
	for _, sr := range styled {
		fmt.Printf("U+%04X ", sr.Char)
	}
	fmt.Println()
}
