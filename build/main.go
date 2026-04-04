// Build script for fzt. Use "go run ./build" instead of "go build".
// Injects git describe version via ldflags so both native and WASM
// binaries carry the correct version without manual flags.
//
// Usage:
//
//	go run ./build              # native binary (fzt.exe on Windows)
//	go run ./build wasm         # WASM binary (fzt.wasm)
package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

func main() {
	version := "dev"
	out, err := exec.Command("git", "describe", "--tags", "--always", "--dirty").Output()
	if err == nil {
		if v := strings.TrimSpace(string(out)); v != "" {
			version = v
		}
	}

	wasm := len(os.Args) > 1 && os.Args[1] == "wasm"

	ext := ""
	pkg := "."
	output := "fzt"
	env := os.Environ()

	if wasm {
		ext = ".wasm"
		pkg = "./cmd/wasm"
		env = append(env, "GOOS=js", "GOARCH=wasm")
	} else if runtime.GOOS == "windows" {
		ext = ".exe"
	}

	output += ext
	ldflags := fmt.Sprintf("-X github.com/nelsong6/fzt/internal/tui.Version=%s", version)

	args := []string{"build", "-ldflags", ldflags, "-o", output, pkg}
	fmt.Printf("go %s\n", strings.Join(args, " "))

	cmd := exec.Command("go", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = env
	if err := cmd.Run(); err != nil {
		os.Exit(1)
	}
}
