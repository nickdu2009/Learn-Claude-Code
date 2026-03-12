package tasks

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileRepository_NextIDUsesLargestExistingID(t *testing.T) {
	repo, dir := newTempFileRepository(t)

	if err := repo.Save(Task{ID: 2, Subject: "second", Status: StatusPending}); err != nil {
		t.Fatalf("save task 2: %v", err)
	}
	if err := repo.Save(Task{ID: 5, Subject: "fifth", Status: StatusPending}); err != nil {
		t.Fatalf("save task 5: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ignore.txt"), []byte("noop"), 0644); err != nil {
		t.Fatalf("write noise file: %v", err)
	}

	nextID, err := repo.NextID()
	if err != nil {
		t.Fatalf("NextID: %v", err)
	}
	if nextID != 6 {
		t.Fatalf("next id = %d, want 6", nextID)
	}
}

func TestFileRepository_ListReturnsTasksSortedByID(t *testing.T) {
	repo, _ := newTempFileRepository(t)

	for _, task := range []Task{
		{ID: 3, Subject: "third", Status: StatusPending},
		{ID: 1, Subject: "first", Status: StatusPending},
		{ID: 2, Subject: "second", Status: StatusPending},
	} {
		if err := repo.Save(task); err != nil {
			t.Fatalf("save task %d: %v", task.ID, err)
		}
	}

	taskList, err := repo.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(taskList) != 3 {
		t.Fatalf("list length = %d, want 3", len(taskList))
	}
	for i, wantID := range []int{1, 2, 3} {
		if taskList[i].ID != wantID {
			t.Fatalf("task[%d].ID = %d, want %d", i, taskList[i].ID, wantID)
		}
	}
}

func TestFileRepository_SaveAndGetRoundTrip(t *testing.T) {
	repo, _ := newTempFileRepository(t)

	original := Task{
		ID:          1,
		Subject:     "parse docs",
		Description: "read the prompt and build tasks",
		Status:      StatusInProgress,
		BlockedBy:   []int{2},
		Blocks:      []int{3},
		Owner:       "agent-a",
	}
	if err := repo.Save(original); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := repo.Get(1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Subject != original.Subject || got.Description != original.Description {
		t.Fatalf("round trip mismatch: got %+v want %+v", got, original)
	}
	if got.Status != original.Status {
		t.Fatalf("status = %q, want %q", got.Status, original.Status)
	}
	if len(got.BlockedBy) != 1 || got.BlockedBy[0] != 2 {
		t.Fatalf("blockedBy = %v, want [2]", got.BlockedBy)
	}
	if len(got.Blocks) != 1 || got.Blocks[0] != 3 {
		t.Fatalf("blocks = %v, want [3]", got.Blocks)
	}
	if got.Owner != original.Owner {
		t.Fatalf("owner = %q, want %q", got.Owner, original.Owner)
	}
}

func newTempFileRepository(t *testing.T) (*FileRepository, string) {
	t.Helper()

	dir := t.TempDir()
	repo, err := NewFileRepository(dir)
	if err != nil {
		t.Fatalf("NewFileRepository: %v", err)
	}
	return repo, dir
}
