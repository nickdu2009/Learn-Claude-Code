package tasks

import (
	"fmt"
	"slices"
	"strings"
)

type Status string

const (
	StatusPending    Status = "pending"
	StatusInProgress Status = "in_progress"
	StatusCompleted  Status = "completed"
)

type Task struct {
	ID          int    `json:"id"`
	Subject     string `json:"subject"`
	Description string `json:"description,omitempty"`
	Status      Status `json:"status"`
	BlockedBy   []int  `json:"blockedBy,omitempty"`
	Blocks      []int  `json:"blocks,omitempty"`
	Owner       string `json:"owner,omitempty"`
}

func (t Task) Validate() error {
	if t.ID <= 0 {
		return fmt.Errorf("task id must be positive")
	}
	if strings.TrimSpace(t.Subject) == "" {
		return fmt.Errorf("task subject is required")
	}

	switch t.Status {
	case StatusPending, StatusInProgress, StatusCompleted:
	default:
		return fmt.Errorf("invalid task status %q", t.Status)
	}

	if hasDuplicateInts(t.BlockedBy) {
		return fmt.Errorf("blockedBy contains duplicate ids")
	}
	if hasDuplicateInts(t.Blocks) {
		return fmt.Errorf("blocks contains duplicate ids")
	}
	if slices.Contains(t.BlockedBy, t.ID) {
		return fmt.Errorf("task cannot be blocked by itself")
	}
	if slices.Contains(t.Blocks, t.ID) {
		return fmt.Errorf("task cannot block itself")
	}

	return nil
}

func (t Task) IsReady() bool {
	return t.Status == StatusPending && len(t.BlockedBy) == 0
}

func (t Task) IsBlocked() bool {
	return t.Status == StatusPending && len(t.BlockedBy) > 0
}

func AppendUnique(ids []int, id int) []int {
	if slices.Contains(ids, id) {
		return ids
	}
	return append(ids, id)
}

func RemoveID(ids []int, id int) []int {
	out := make([]int, 0, len(ids))
	for _, current := range ids {
		if current != id {
			out = append(out, current)
		}
	}
	return out
}

func hasDuplicateInts(ids []int) bool {
	seen := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			return true
		}
		seen[id] = struct{}{}
	}
	return false
}
