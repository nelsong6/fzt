package main

import (
	"bufio"
	"fmt"
	"os"
	"sort"

	"github.com/nelsong6/fzt/core"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] == "--help" || os.Args[1] == "-h" {
		fmt.Fprintln(os.Stderr, "fzt — fuzzy tiered scorer")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Usage: <input> | fzt <query>")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Reads lines from stdin, scores each against <query> using")
		fmt.Fprintln(os.Stderr, "tiered fuzzy matching, and prints matches ranked best-first.")
		os.Exit(0)
	}
	query := os.Args[1]

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	var lines []string
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" {
			lines = append(lines, line)
		}
	}

	type scored struct {
		line  string
		score core.TieredScore
	}
	var results []scored
	for _, line := range lines {
		ts, _ := core.ScoreItem([]string{line}, query, nil, nil)
		if ts.Total() > 0 {
			results = append(results, scored{line, ts})
		}
	}

	sort.SliceStable(results, func(i, j int) bool {
		return results[j].score.Less(results[i].score)
	})

	for _, r := range results {
		fmt.Println(r.line)
	}
}
