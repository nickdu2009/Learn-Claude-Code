package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nickdu2009/learn-claude-code/pkg/devtools"
)

func main() {
	port := 4983
	if raw := strings.TrimSpace(os.Getenv("AI_SDK_DEVTOOLS_PORT")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed < 65536 {
			port = parsed
		}
	}

	root := resolveRepoRoot()
	tracePath := resolveTracePath(root)
	server := devtools.NewViewerServer(tracePath)

	fmt.Printf("Starting Trace V2 viewer on http://localhost:%d\n", port)
	fmt.Printf("Reading trace file: %s\n", tracePath)
	fmt.Println("Serving embedded frontend assets")

	if err := http.ListenAndServe(fmt.Sprintf(":%d", port), server.Handler()); err != nil {
		fmt.Fprintln(os.Stderr, "viewer error:", err)
		os.Exit(1)
	}
}

func resolveRepoRoot() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return findGitRoot(cwd)
}

func resolveTracePath(root string) string {
	if dir := strings.TrimSpace(os.Getenv("AI_SDK_DEVTOOLS_DIR")); dir != "" {
		if filepath.IsAbs(dir) {
			return filepath.Join(filepath.Clean(dir), "generations.json")
		}
		return filepath.Join(root, filepath.Clean(dir), "generations.json")
	}
	return filepath.Join(root, ".devtools", "generations.json")
}

func findGitRoot(start string) string {
	dir := start
	for {
		if dir == "" || dir == string(filepath.Separator) {
			return start
		}
		if fi, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			if fi.IsDir() || fi.Mode().IsRegular() {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return start
		}
		dir = parent
	}
}
