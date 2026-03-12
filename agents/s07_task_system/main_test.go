package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nickdu2009/learn-claude-code/pkg/tasks"
)

func TestFormatInProgressTaskWarning_EmptyWhenNoTaskInProgress(t *testing.T) {
	warning := formatInProgressTaskWarning([]tasks.Task{
		{ID: 1, Subject: "Setup project", Status: tasks.StatusPending},
		{ID: 2, Subject: "Write code", Status: tasks.StatusCompleted},
	})

	if warning != "" {
		t.Fatalf("warning = %q, want empty", warning)
	}
}

func TestFormatInProgressTaskWarning_ListsOnlyInProgressTasksSortedByID(t *testing.T) {
	warning := formatInProgressTaskWarning([]tasks.Task{
		{ID: 3, Subject: "Write tests", Status: tasks.StatusInProgress},
		{ID: 1, Subject: "Setup project", Status: tasks.StatusPending},
		{ID: 2, Subject: "Write code", Status: tasks.StatusInProgress},
	})

	if !strings.Contains(warning, "still in_progress") {
		t.Fatalf("expected warning header, got %q", warning)
	}
	if !strings.Contains(warning, "- #2 Write code") {
		t.Fatalf("expected task #2 in warning, got %q", warning)
	}
	if !strings.Contains(warning, "- #3 Write tests") {
		t.Fatalf("expected task #3 in warning, got %q", warning)
	}
	if strings.Contains(warning, "Setup project") {
		t.Fatalf("warning should not mention non-in_progress task, got %q", warning)
	}
	if strings.Index(warning, "#2 Write code") > strings.Index(warning, "#3 Write tests") {
		t.Fatalf("expected sorted warning output, got %q", warning)
	}
}

func TestWarnInProgressTasks_WritesWarningMessage(t *testing.T) {
	var out bytes.Buffer

	warnInProgressTasks(&out, staticTaskLister{
		tasks: []tasks.Task{
			{ID: 4, Subject: "Review code", Status: tasks.StatusInProgress},
		},
	})

	output := out.String()
	if !strings.Contains(output, "still in_progress") {
		t.Fatalf("expected warning header, got %q", output)
	}
	if !strings.Contains(output, "#4 Review code") {
		t.Fatalf("expected task entry, got %q", output)
	}
}

func TestWarnInProgressTasks_ReportsListError(t *testing.T) {
	var out bytes.Buffer

	warnInProgressTasks(&out, staticTaskLister{err: assertiveError("boom")})

	output := out.String()
	if !strings.Contains(output, "failed to inspect task board") {
		t.Fatalf("expected list error warning, got %q", output)
	}
	if !strings.Contains(output, "boom") {
		t.Fatalf("expected original error in output, got %q", output)
	}
}

type staticTaskLister struct {
	tasks []tasks.Task
	err   error
}

func (s staticTaskLister) ListTasks() ([]tasks.Task, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.tasks, nil
}

type assertiveError string

func (e assertiveError) Error() string {
	return string(e)
}
