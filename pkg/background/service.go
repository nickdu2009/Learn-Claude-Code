package background

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	defaultExecutionTimeout = 5 * time.Minute
	defaultNotificationSize = 32
	maxResultLength         = 50000
	maxSummaryLength        = 500
)

var dangerousPatterns = []string{
	"rm -rf /", "sudo", "shutdown", "reboot", "> /dev/",
}

type Option func(*Manager)

func WithExecutionTimeout(timeout time.Duration) Option {
	return func(m *Manager) {
		if timeout > 0 {
			m.executionTimeout = timeout
		}
	}
}

func WithNotificationBuffer(size int) Option {
	return func(m *Manager) {
		if size > 0 {
			m.notificationBuffer = size
		}
	}
}

type Manager struct {
	mu                 sync.RWMutex
	tasks              map[string]Task
	workdir            string
	executionTimeout   time.Duration
	notificationBuffer int
	done               chan Notification
	wakeup             chan struct{}
	nextID             uint64
}

func NewManager(workdir string, opts ...Option) (*Manager, error) {
	if strings.TrimSpace(workdir) == "" {
		return nil, fmt.Errorf("workdir is required")
	}

	absWorkdir, err := filepath.Abs(workdir)
	if err != nil {
		return nil, fmt.Errorf("resolve workdir: %w", err)
	}
	info, err := os.Stat(absWorkdir)
	if err != nil {
		return nil, fmt.Errorf("stat workdir: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("workdir is not a directory: %s", absWorkdir)
	}

	m := &Manager{
		tasks:              make(map[string]Task),
		workdir:            absWorkdir,
		executionTimeout:   defaultExecutionTimeout,
		notificationBuffer: defaultNotificationSize,
	}
	for _, opt := range opts {
		opt(m)
	}
	m.done = make(chan Notification, m.notificationBuffer)
	m.wakeup = make(chan struct{}, 1)
	return m, nil
}

func (m *Manager) Run(ctx context.Context, command string) (Task, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return Task{}, fmt.Errorf("command is required")
	}
	for _, pattern := range dangerousPatterns {
		if strings.Contains(command, pattern) {
			return Task{}, fmt.Errorf("dangerous command blocked")
		}
	}

	m.mu.Lock()
	m.nextID++
	taskID := fmt.Sprintf("bg-%d", m.nextID)
	task := Task{
		ID:        taskID,
		Command:   command,
		Status:    StatusRunning,
		StartedAt: time.Now().UTC(),
	}
	m.tasks[taskID] = task
	m.mu.Unlock()

	go m.execute(ctx, taskID, command)

	return task, nil
}

func (m *Manager) Check(taskID string) (Task, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	task, ok := m.tasks[strings.TrimSpace(taskID)]
	if !ok {
		return Task{}, fmt.Errorf("unknown background task %q", taskID)
	}
	return task, nil
}

func (m *Manager) List() ([]Task, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	taskList := make([]Task, 0, len(m.tasks))
	for _, task := range m.tasks {
		taskList = append(taskList, task)
	}
	return taskList, nil
}

func (m *Manager) DrainNotifications() []Notification {
	out := make([]Notification, 0)
	for {
		select {
		case notification := <-m.done:
			out = append(out, notification)
		default:
			return out
		}
	}
}

func (m *Manager) Wakeups() <-chan struct{} {
	return m.wakeup
}

func (m *Manager) execute(parentCtx context.Context, taskID string, command string) {
	runCtx := parentCtx
	cancel := func() {}
	if m.executionTimeout > 0 {
		runCtx, cancel = context.WithTimeout(parentCtx, m.executionTimeout)
	}
	defer cancel()

	cmd := exec.CommandContext(runCtx, "bash", "-c", command)
	cmd.Dir = m.workdir
	output, err := cmd.CombinedOutput()

	status := StatusCompleted
	result := trimResult(string(output), maxResultLength)
	if result == "" {
		result = "(no output)"
	}
	if err != nil {
		switch {
		case runCtx.Err() == context.DeadlineExceeded:
			status = StatusTimeout
			result = "Error: Timeout"
		default:
			status = StatusError
			if strings.TrimSpace(result) == "" {
				result = fmt.Sprintf("Error: %v", err)
			}
		}
	}

	finishedAt := time.Now().UTC()

	m.mu.Lock()
	task, ok := m.tasks[taskID]
	if ok {
		task.Status = status
		task.Result = result
		task.FinishedAt = &finishedAt
		m.tasks[taskID] = task
	}
	m.mu.Unlock()
	if !ok {
		return
	}

	m.done <- Notification{
		TaskID:  task.ID,
		Command: task.Command,
		Status:  status,
		Summary: trimResult(result, maxSummaryLength),
	}
	select {
	case m.wakeup <- struct{}{}:
	default:
	}
}

func trimResult(value string, maxLen int) string {
	value = strings.TrimSpace(value)
	if maxLen <= 0 || len(value) <= maxLen {
		return value
	}
	return value[:maxLen]
}
