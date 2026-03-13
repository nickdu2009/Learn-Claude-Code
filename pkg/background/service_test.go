package background

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestManagerRunReturnsImmediately(t *testing.T) {
	manager := newTestManager(t)

	start := time.Now()
	task, err := manager.Run(context.Background(), "sleep 0.2; echo done")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if task.Status != StatusRunning {
		t.Fatalf("task status = %q, want %q", task.Status, StatusRunning)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("Run blocked for %v, expected immediate return", elapsed)
	}
}

func TestManagerCompletedTaskEmitsNotificationOnce(t *testing.T) {
	manager := newTestManager(t)

	task, err := manager.Run(context.Background(), "echo done")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	notification := waitForNotification(t, manager, task.ID)
	if notification.Status != StatusCompleted {
		t.Fatalf("notification status = %q, want %q", notification.Status, StatusCompleted)
	}
	if notification.Summary != "done" {
		t.Fatalf("notification summary = %q, want %q", notification.Summary, "done")
	}

	drained := manager.DrainNotifications()
	if len(drained) != 0 {
		t.Fatalf("expected notifications to be drained once, got %d", len(drained))
	}
}

func TestManagerFailedTaskEmitsErrorNotification(t *testing.T) {
	manager := newTestManager(t)

	task, err := manager.Run(context.Background(), "echo boom >&2; exit 3")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	notification := waitForNotification(t, manager, task.ID)
	if notification.Status != StatusError {
		t.Fatalf("notification status = %q, want %q", notification.Status, StatusError)
	}
	if !strings.Contains(notification.Summary, "boom") {
		t.Fatalf("expected stderr in notification summary, got %q", notification.Summary)
	}
}

func TestManagerMultipleTasksCompleteOutOfOrder(t *testing.T) {
	manager := newTestManager(t)

	slowTask, err := manager.Run(context.Background(), "sleep 0.2; echo slow")
	if err != nil {
		t.Fatalf("Run slow: %v", err)
	}
	fastTask, err := manager.Run(context.Background(), "sleep 0.05; echo fast")
	if err != nil {
		t.Fatalf("Run fast: %v", err)
	}

	first := waitForAnyNotification(t, manager)
	second := waitForAnyNotification(t, manager)
	if first.TaskID != fastTask.ID {
		t.Fatalf("first notification task = %q, want %q", first.TaskID, fastTask.ID)
	}
	if second.TaskID != slowTask.ID {
		t.Fatalf("second notification task = %q, want %q", second.TaskID, slowTask.ID)
	}
}

func TestManagerRunRejectsDangerousCommand(t *testing.T) {
	manager := newTestManager(t)

	_, err := manager.Run(context.Background(), "sudo rm -rf /")
	if err == nil {
		t.Fatal("expected dangerous command error")
	}
	if !strings.Contains(err.Error(), "dangerous command") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestManagerCheckReturnsUpdatedTask(t *testing.T) {
	manager := newTestManager(t)

	task, err := manager.Run(context.Background(), "echo inspect")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	_ = waitForNotification(t, manager, task.ID)

	got, err := manager.Check(task.ID)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if got.Status != StatusCompleted {
		t.Fatalf("task status = %q, want %q", got.Status, StatusCompleted)
	}
	if got.Result != "inspect" {
		t.Fatalf("task result = %q, want %q", got.Result, "inspect")
	}
	if got.FinishedAt == nil {
		t.Fatal("expected finished timestamp")
	}
}

func newTestManager(t *testing.T) *Manager {
	t.Helper()

	manager, err := NewManager(
		t.TempDir(),
		WithExecutionTimeout(2*time.Second),
		WithNotificationBuffer(8),
	)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return manager
}

func waitForNotification(t *testing.T, manager *Manager, taskID string) Notification {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, notification := range manager.DrainNotifications() {
			if notification.TaskID == taskID {
				return notification
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for notification for %s", taskID)
	return Notification{}
}

func waitForAnyNotification(t *testing.T, manager *Manager) Notification {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		notifications := manager.DrainNotifications()
		if len(notifications) > 0 {
			return notifications[0]
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for notification")
	return Notification{}
}
