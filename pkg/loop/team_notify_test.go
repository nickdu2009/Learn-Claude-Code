package loop

import (
	"context"
	"strings"
	"testing"

	"github.com/nickdu2009/learn-claude-code/pkg/tools"
)

type stubInboxNotificationSource struct {
	payload string
	err     error
}

func (s stubInboxNotificationSource) DrainInboxNotifications(string) (string, error) {
	return s.payload, s.err
}

func TestBuildTeamInboxMessages(t *testing.T) {
	messages := buildTeamInboxMessages(`[{"from":"lead","content":"hello"}]`)
	if len(messages) != 2 {
		t.Fatalf("message count = %d, want 2", len(messages))
	}
	if messages[0].OfUser == nil || !strings.Contains(messages[0].OfUser.Content.OfString.Value, "<inbox>") {
		t.Fatalf("expected first message to contain inbox payload: %#v", messages[0])
	}
	if messages[1].OfAssistant == nil || messages[1].OfAssistant.Content.OfString.Value != "Noted inbox messages." {
		t.Fatalf("unexpected assistant ack: %#v", messages[1])
	}
}

func TestRunWithTeamInboxNotifications_PropagatesDrainError(t *testing.T) {
	runner := RunWithTeamInboxNotifications("lead", stubInboxNotificationSource{err: context.Canceled})
	_, err := runner(context.Background(), nil, "mock-model", nil, tools.New())
	if err == nil || !strings.Contains(err.Error(), "drain inbox notifications") {
		t.Fatalf("expected drain error, got %v", err)
	}
}

