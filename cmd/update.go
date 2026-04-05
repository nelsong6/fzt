package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"

	"github.com/nelsong6/fzt/internal/tui"
	"github.com/spf13/cobra"
)

const ghRepo = "nelsong6/fzt"

func init() {
	rootCmd.AddCommand(updateCmd)
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update fzt to the latest release",
	RunE:  runUpdate,
}

type ghRelease struct {
	TagName string    `json:"tag_name"`
	Assets  []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func runUpdate(cmd *cobra.Command, args []string) error {
	fmt.Printf("Current version: %s\n", tui.Version)

	// Fetch latest release
	resp, err := http.Get(fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", ghRepo))
	if err != nil {
		return fmt.Errorf("checking for updates: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var release ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return fmt.Errorf("parsing release: %w", err)
	}

	latest := strings.TrimPrefix(release.TagName, "v")
	current := strings.TrimPrefix(tui.Version, "v")
	// Strip dirty/dev suffixes for comparison
	if i := strings.IndexByte(current, '-'); i > 0 {
		current = current[:i]
	}

	if latest == current {
		fmt.Printf("Already up to date (%s)\n", release.TagName)
		return nil
	}

	fmt.Printf("Updating to %s...\n", release.TagName)

	// Find the right asset for this OS/arch
	assetName := fmt.Sprintf("fzt-%s-%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		assetName += ".exe"
	}

	var downloadURL string
	for _, asset := range release.Assets {
		if asset.Name == assetName {
			downloadURL = asset.BrowserDownloadURL
			break
		}
	}

	if downloadURL == "" {
		return fmt.Errorf("no release asset for %s/%s (looking for %s)", runtime.GOOS, runtime.GOARCH, assetName)
	}

	// Download the new binary
	dlResp, err := http.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("downloading: %w", err)
	}
	defer dlResp.Body.Close()

	if dlResp.StatusCode != 200 {
		return fmt.Errorf("download returned %d", dlResp.StatusCode)
	}

	// Get current executable path
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding executable path: %w", err)
	}

	// Write to a temp file next to the executable, then rename (atomic on most OSes)
	tmpPath := execPath + ".update"
	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}

	if _, err := io.Copy(tmpFile, dlResp.Body); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("writing update: %w", err)
	}
	tmpFile.Close()

	// Make executable (no-op on Windows)
	if runtime.GOOS != "windows" {
		if err := os.Chmod(tmpPath, 0755); err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("chmod: %w", err)
		}
	}

	// On Windows, rename the old binary first (can't overwrite a running exe)
	if runtime.GOOS == "windows" {
		oldPath := execPath + ".old"
		os.Remove(oldPath) // clean up previous .old if exists
		if err := os.Rename(execPath, oldPath); err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("renaming old binary: %w", err)
		}
	}

	if err := os.Rename(tmpPath, execPath); err != nil {
		return fmt.Errorf("replacing binary: %w", err)
	}

	fmt.Printf("Updated to %s\n", release.TagName)
	return nil
}
