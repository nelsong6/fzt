package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	if scanner.Scan() {
		line := scanner.Text()
		bytes := []byte(line)
		hex := fmt.Sprintf("%X", bytes)
		if strings.Contains(hex, "EF8495") {
			fmt.Println("true - icon U+F115 found")
		} else {
			fmt.Println("false - icon not found")
			fmt.Printf("First 20 non-ANSI bytes after date: ")
			// Find the icon region (after "2026" in the lsd output)
			idx := strings.Index(hex, "32303236")
			if idx > 0 {
				start := idx + 8 // past "2026"
				end := start + 40
				if end > len(hex) {
					end = len(hex)
				}
				fmt.Println(hex[start:end])
			} else {
				fmt.Println("couldn't locate date marker")
			}
		}
	}
}
