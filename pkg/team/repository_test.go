package team

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileRepository_LoadMissingConfig(t *testing.T) {
	repo, err := NewFileRepository(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileRepository: %v", err)
	}

	members, err := repo.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(members) != 0 {
		t.Fatalf("member count = %d, want 0", len(members))
	}
}

func TestFileRepository_SaveAll_WritesConfigAtomically(t *testing.T) {
	dir := t.TempDir()
	repo, err := NewFileRepository(dir)
	if err != nil {
		t.Fatalf("NewFileRepository: %v", err)
	}

	member := Member{
		Name:      "alice",
		Role:      "coder",
		Status:    StatusIdle,
		UpdatedAt: time.Now().UTC(),
	}
	if err := repo.SaveAll([]Member{member}); err != nil {
		t.Fatalf("SaveAll: %v", err)
	}

	path := filepath.Join(dir, "config.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected config.json: %v", err)
	}

	members, err := repo.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(members) != 1 || members[0].Name != "alice" {
		t.Fatalf("members = %#v, want alice", members)
	}
}

