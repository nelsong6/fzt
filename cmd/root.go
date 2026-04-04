package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/nelsong6/fzt/internal/column"
	"github.com/nelsong6/fzt/internal/model"
	"github.com/nelsong6/fzt/internal/tui"
	"github.com/nelsong6/fzt/internal/yamlsrc"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "fzt",
	Short: "Fuzzy finder with hierarchical tiered scoring and native columns",
	Long:  "fzt is an fzf-compatible fuzzy finder that adds depth-aware tiered scoring and first-class column support.",
	RunE:  run,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Flags
var (
	flagLayout       string
	flagBorder       bool
	flagHeaderLines  int
	flagNth          string
	flagAcceptNth    string
	flagPrompt       string
	flagDelimiter    string
	flagTiered       bool
	flagDepthPenalty int
	flagSearchCols   string
	flagHeight       string
	flagFilter       string
	flagHeader       string
	flagShowScores   bool
	flagSimulate     bool
	flagSimWidth     int
	flagSimHeight    int
	flagSimQuery     string
	flagRecord       string
	flagStyled       bool
	flagANSI         bool
	flagYAML         string
	flagTitle        string
	flagTitlePos     string
	flagTree         bool
)

func init() {
	rootCmd.Version = tui.Version
	rootCmd.Flags().StringVar(&flagLayout, "layout", "default", "Layout: 'default' or 'reverse'")
	rootCmd.Flags().BoolVar(&flagBorder, "border", false, "Draw border around the finder")
	rootCmd.Flags().IntVar(&flagHeaderLines, "header-lines", 0, "Number of header lines to pin (not filtered)")
	rootCmd.Flags().StringVar(&flagNth, "nth", "", "Comma-separated field indices to search (1-based)")
	rootCmd.Flags().StringVar(&flagAcceptNth, "accept-nth", "", "Comma-separated field indices to output (1-based)")
	rootCmd.Flags().StringVar(&flagPrompt, "prompt", "> ", "Prompt string")
	rootCmd.Flags().StringVarP(&flagDelimiter, "delimiter", "d", "\t", "Field delimiter")
	rootCmd.Flags().BoolVar(&flagTiered, "tiered", false, "Enable tiered depth-aware scoring (first column is depth)")
	rootCmd.Flags().IntVar(&flagDepthPenalty, "depth-penalty", 5, "Score penalty per depth level (used with --tiered)")
	rootCmd.Flags().StringVar(&flagSearchCols, "search-cols", "", "Comma-separated field indices to score (1-based, overrides --nth for scoring)")
	rootCmd.Flags().StringVar(&flagHeight, "height", "", "Height as percentage (e.g. '40%') or absolute lines")
	rootCmd.Flags().StringVar(&flagFilter, "filter", "", "Non-interactive filter mode: print matches and exit")
	rootCmd.Flags().StringVar(&flagHeader, "header", "", "Header string (displayed above items, delimiter-separated for columns)")
	rootCmd.Flags().BoolVar(&flagShowScores, "show-scores", false, "Show scores in --filter output")
	rootCmd.Flags().BoolVar(&flagSimulate, "simulate", false, "Headless simulation mode (no terminal needed)")
	rootCmd.Flags().IntVar(&flagSimWidth, "width", 80, "Terminal width for --simulate")
	rootCmd.Flags().IntVar(&flagSimHeight, "height-lines", 15, "Terminal height in lines for --simulate")
	rootCmd.Flags().StringVar(&flagSimQuery, "sim-query", "", "Query to type character-by-character in --simulate mode")
	rootCmd.Flags().StringVar(&flagRecord, "record", "", "Write rendered frames to this file (used with --simulate)")
	rootCmd.Flags().BoolVar(&flagStyled, "styled", false, "Include style markers [H]=highlight [S]=selected in frame output")
	rootCmd.Flags().BoolVar(&flagANSI, "ansi", false, "Parse and preserve ANSI color codes from input")
	rootCmd.Flags().StringVar(&flagYAML, "yaml", "", "Load hierarchical data from a YAML file (enables --tiered automatically)")
	rootCmd.Flags().StringVar(&flagTitle, "title", "", "Title string displayed at the top of the finder")
	rootCmd.Flags().StringVar(&flagTitlePos, "title-pos", "left", "Title position: 'left', 'center', or 'right'")
	rootCmd.Flags().BoolVar(&flagTree, "tree", false, "Start in tree view mode (expand/collapse navigation)")
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(cmd *cobra.Command, args []string) error {
	var items []model.Item

	if flagYAML != "" {
		// YAML mode: load from file, auto-enable tiered scoring
		var err error
		items, err = yamlsrc.Load(flagYAML)
		if err != nil {
			return fmt.Errorf("loading yaml: %w", err)
		}
		flagTiered = true
		flagTree = true
		// Inject header if provided
		if flagHeader != "" {
			headerFields := strings.Split(flagHeader, flagDelimiter)
			headerItem := model.Item{Fields: headerFields, Depth: -1}
			items = append([]model.Item{headerItem}, items...)
			if flagHeaderLines == 0 {
				flagHeaderLines = 1
			}
		}
	} else {
		// Stdin mode
		lines, err := readStdin()
		if err != nil {
			return fmt.Errorf("reading stdin: %w", err)
		}
		if len(lines) == 0 {
			return nil
		}

		if flagHeader != "" {
			lines = append([]string{flagHeader}, lines...)
			if flagHeaderLines == 0 {
				flagHeaderLines = 1
			}
		}

		items = column.ParseLines(lines, flagDelimiter, flagTiered, flagANSI)
	}

	// Build config
	cfg := tui.Config{
		Layout:       flagLayout,
		Border:       flagBorder,
		HeaderLines:  flagHeaderLines,
		Nth:          parseIntList(flagNth),
		AcceptNth:    parseIntList(flagAcceptNth),
		Prompt:       flagPrompt,
		Delimiter:    flagDelimiter,
		Tiered:       flagTiered,
		DepthPenalty: flagDepthPenalty,
		SearchCols:   parseIntList(flagSearchCols),
		Height:       parseHeight(flagHeight),
		ShowScores:   flagShowScores,
		ANSI:         flagANSI,
		Title:        flagTitle,
		TitlePos:     flagTitlePos,
		TreeMode:     flagTree,
	}

	// Non-interactive filter mode
	if flagFilter != "" {
		data := items
		if cfg.HeaderLines > 0 && cfg.HeaderLines <= len(items) {
			data = items[cfg.HeaderLines:]
		}
		tui.RunFilter(data, flagFilter, cfg)
		return nil
	}

	// Headless simulation mode
	if flagSimulate {
		frames := tui.Simulate(items, cfg, flagSimQuery, flagSimWidth, flagSimHeight, flagStyled)
		output := tui.FormatFrames(frames)
		if flagRecord != "" {
			if err := os.WriteFile(flagRecord, []byte(output), 0644); err != nil {
				return fmt.Errorf("writing record file: %w", err)
			}
			fmt.Fprintf(os.Stderr, "Wrote %d frames to %s\n", len(frames), flagRecord)
		} else {
			fmt.Print(output)
		}
		return nil
	}

	// Interactive mode
	result, err := tui.Run(items, cfg)
	if err != nil {
		return err
	}
	if result == "" {
		os.Exit(130) // cancelled
	}
	fmt.Println(result)
	return nil
}

func readStdin() ([]string, error) {
	info, err := os.Stdin.Stat()
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeCharDevice != 0 {
		return nil, fmt.Errorf("no input (stdin is a terminal)")
	}

	var lines []string
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines, scanner.Err()
}

func parseIntList(s string) []int {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	var result []int
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if n, err := strconv.Atoi(p); err == nil {
			result = append(result, n)
		}
	}
	return result
}

func parseHeight(s string) int {
	if s == "" {
		return 0
	}
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "%") {
		s = strings.TrimSuffix(s, "%")
		n, err := strconv.Atoi(s)
		if err != nil {
			return 0
		}
		return n
	}
	// Absolute lines not implemented yet — treat as percentage
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}
