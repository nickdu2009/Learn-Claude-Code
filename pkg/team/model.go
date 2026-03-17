package team

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

type Status string

const (
	StatusWorking  Status = "working"
	StatusIdle     Status = "idle"
	StatusShutdown Status = "shutdown"
)

type RequestStatus string

const (
	RequestPending  RequestStatus = "pending"
	RequestApproved RequestStatus = "approved"
	RequestRejected RequestStatus = "rejected"
)

const (
	MessageTypeMessage              = "message"
	MessageTypeBroadcast            = "broadcast"
	MessageTypeShutdownRequest      = "shutdown_request"
	MessageTypeShutdownResponse     = "shutdown_response"
	MessageTypePlanApprovalResponse = "plan_approval_response"
)

var teammateNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

type Member struct {
	Name      string    `json:"name"`
	Role      string    `json:"role"`
	Status    Status    `json:"status"`
	Prompt    string    `json:"prompt,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type Message struct {
	ID        string            `json:"id"`
	Type      string            `json:"type"`
	From      string            `json:"from"`
	To        string            `json:"to"`
	Content   string            `json:"content"`
	Extra     map[string]string `json:"extra,omitempty"`
	CreatedAt time.Time         `json:"createdAt"`
}

type TeamConfig struct {
	TeamName string   `json:"team_name"`
	Members  []Member `json:"members"`
}

type ShutdownRequest struct {
	RequestID string        `json:"request_id"`
	Target    string        `json:"target"`
	Status    RequestStatus `json:"status"`
	Reason    string        `json:"reason,omitempty"`
	UpdatedAt time.Time     `json:"updated_at"`
}

type PlanRequest struct {
	RequestID string        `json:"request_id"`
	From      string        `json:"from"`
	Plan      string        `json:"plan"`
	Status    RequestStatus `json:"status"`
	Feedback  string        `json:"feedback,omitempty"`
	UpdatedAt time.Time     `json:"updated_at"`
}

func NormalizeName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if !teammateNamePattern.MatchString(name) {
		return "", fmt.Errorf("invalid teammate name %q", name)
	}
	return name, nil
}

func NormalizeRole(role string) (string, error) {
	role = strings.TrimSpace(role)
	if role == "" {
		return "", fmt.Errorf("role is required")
	}
	return role, nil
}

func NormalizePrompt(prompt string) string {
	return strings.TrimSpace(prompt)
}

func NormalizeStatus(status Status) (Status, error) {
	switch status {
	case StatusWorking, StatusIdle, StatusShutdown:
		return status, nil
	default:
		return "", fmt.Errorf("invalid teammate status %q", status)
	}
}

func NormalizeRequestStatus(status RequestStatus) (RequestStatus, error) {
	switch status {
	case RequestPending, RequestApproved, RequestRejected:
		return status, nil
	default:
		return "", fmt.Errorf("invalid request status %q", status)
	}
}

func NormalizeMessageType(msgType string) (string, error) {
	msgType = strings.TrimSpace(msgType)
	switch msgType {
	case MessageTypeMessage,
		MessageTypeBroadcast,
		MessageTypeShutdownRequest,
		MessageTypeShutdownResponse,
		MessageTypePlanApprovalResponse:
		return msgType, nil
	default:
		return "", fmt.Errorf("invalid message type %q", msgType)
	}
}
