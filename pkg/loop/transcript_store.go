package loop

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/openai/openai-go"
)

// TranscriptStore persists full conversation history before compaction.
type TranscriptStore struct {
	Dir string
}

// Save writes the full message history as JSONL and returns the absolute path.
func (s TranscriptStore) Save(messages []openai.ChatCompletionMessageParamUnion) (string, error) {
	dir, err := resolveTranscriptDir(s.Dir)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create transcript dir: %w", err)
	}

	path := filepath.Join(dir, fmt.Sprintf("transcript_%d.jsonl", time.Now().UnixNano()))
	file, err := os.Create(path)
	if err != nil {
		return "", fmt.Errorf("failed to create transcript file: %w", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	for _, msg := range messages {
		if err := encoder.Encode(msg); err != nil {
			return "", fmt.Errorf("failed to write transcript: %w", err)
		}
	}
	return path, nil
}

func resolveTranscriptDir(configured string) (string, error) {
	if filepath.IsAbs(configured) {
		return filepath.Clean(configured), nil
	}

	repoRoot, err := repoRootFromWorkingDir()
	if err != nil {
		return "", err
	}
	if configured == "" {
		return filepath.Join(repoRoot, ".transcripts"), nil
	}
	return filepath.Join(repoRoot, configured), nil
}

func repoRootFromWorkingDir() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	dir := filepath.Clean(cwd)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("failed to locate repository root from %s", cwd)
}
