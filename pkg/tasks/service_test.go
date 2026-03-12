package tasks

import "testing"

func TestService_CreateTaskDefaultsToPending(t *testing.T) {
	svc := newTempService(t)

	task, err := svc.CreateTask("parse input", "read user prompt")
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	if task.ID != 1 {
		t.Fatalf("task id = %d, want 1", task.ID)
	}
	if task.Status != StatusPending {
		t.Fatalf("status = %q, want pending", task.Status)
	}
	if len(task.BlockedBy) != 0 || len(task.Blocks) != 0 {
		t.Fatalf("expected empty edges, got blockedBy=%v blocks=%v", task.BlockedBy, task.Blocks)
	}
}

func TestService_UpdateTaskAddsMirroredDependencies(t *testing.T) {
	svc := newTempService(t)

	parseTask := mustCreateTask(t, svc, "parse")
	transformTask := mustCreateTask(t, svc, "transform")

	updated, err := svc.UpdateTask(UpdateTaskInput{
		ID:           transformTask.ID,
		AddBlockedBy: []int{parseTask.ID},
	})
	if err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}

	if len(updated.BlockedBy) != 1 || updated.BlockedBy[0] != parseTask.ID {
		t.Fatalf("blockedBy = %v, want [%d]", updated.BlockedBy, parseTask.ID)
	}

	parseTask, err = svc.GetTask(parseTask.ID)
	if err != nil {
		t.Fatalf("GetTask(parse): %v", err)
	}
	if len(parseTask.Blocks) != 1 || parseTask.Blocks[0] != transformTask.ID {
		t.Fatalf("blocks = %v, want [%d]", parseTask.Blocks, transformTask.ID)
	}
}

func TestService_UpdateTaskCompletedClearsBlockedBy(t *testing.T) {
	svc := newTempService(t)

	parseTask := mustCreateTask(t, svc, "parse")
	transformTask := mustCreateTask(t, svc, "transform")
	emitTask := mustCreateTask(t, svc, "emit")

	if _, err := svc.UpdateTask(UpdateTaskInput{ID: transformTask.ID, AddBlockedBy: []int{parseTask.ID}}); err != nil {
		t.Fatalf("wire transform: %v", err)
	}
	if _, err := svc.UpdateTask(UpdateTaskInput{ID: emitTask.ID, AddBlockedBy: []int{parseTask.ID}}); err != nil {
		t.Fatalf("wire emit: %v", err)
	}

	completed := StatusCompleted
	if _, err := svc.UpdateTask(UpdateTaskInput{ID: parseTask.ID, Status: &completed}); err != nil {
		t.Fatalf("complete parse: %v", err)
	}

	transformTask, err := svc.GetTask(transformTask.ID)
	if err != nil {
		t.Fatalf("GetTask(transform): %v", err)
	}
	emitTask, err = svc.GetTask(emitTask.ID)
	if err != nil {
		t.Fatalf("GetTask(emit): %v", err)
	}

	if len(transformTask.BlockedBy) != 0 {
		t.Fatalf("transform blockedBy = %v, want []", transformTask.BlockedBy)
	}
	if len(emitTask.BlockedBy) != 0 {
		t.Fatalf("emit blockedBy = %v, want []", emitTask.BlockedBy)
	}
	if !transformTask.IsReady() || !emitTask.IsReady() {
		t.Fatalf("expected transform and emit to be ready after parse completion")
	}
}

func TestService_UpdateTaskRejectsDependencyCycle(t *testing.T) {
	svc := newTempService(t)

	task1 := mustCreateTask(t, svc, "task-1")
	task2 := mustCreateTask(t, svc, "task-2")

	if _, err := svc.UpdateTask(UpdateTaskInput{ID: task2.ID, AddBlockedBy: []int{task1.ID}}); err != nil {
		t.Fatalf("wire task2: %v", err)
	}

	_, err := svc.UpdateTask(UpdateTaskInput{ID: task1.ID, AddBlockedBy: []int{task2.ID}})
	if err == nil {
		t.Fatal("expected cycle detection error")
	}
}

func newTempService(t *testing.T) *Service {
	t.Helper()

	repo, err := NewFileRepository(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileRepository: %v", err)
	}
	return NewService(repo)
}

func mustCreateTask(t *testing.T, svc *Service, subject string) Task {
	t.Helper()

	task, err := svc.CreateTask(subject, "")
	if err != nil {
		t.Fatalf("CreateTask(%q): %v", subject, err)
	}
	return task
}
