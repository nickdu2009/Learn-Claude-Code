package background

import "time"

type Status string

const (
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusTimeout   Status = "timeout"
	StatusError     Status = "error"
)

type Task struct {
	ID         string     `json:"id"`
	Command    string     `json:"command"`
	Status     Status     `json:"status"`
	Result     string     `json:"result,omitempty"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

type Notification struct {
	TaskID  string `json:"taskId"`
	Command string `json:"command"`
	Status  Status `json:"status"`
	Summary string `json:"summary"`
}
