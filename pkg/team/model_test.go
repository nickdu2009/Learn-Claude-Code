package team

import "testing"

func TestNormalizeName(t *testing.T) {
	name, err := NormalizeName("alice_1")
	if err != nil {
		t.Fatalf("NormalizeName returned error: %v", err)
	}
	if name != "alice_1" {
		t.Fatalf("name = %q, want %q", name, "alice_1")
	}
}

func TestNormalizeName_RejectsInvalidValue(t *testing.T) {
	if _, err := NormalizeName("../alice"); err == nil {
		t.Fatal("expected invalid teammate name error")
	}
}

func TestNormalizeMessageType(t *testing.T) {
	for _, msgType := range []string{
		MessageTypeMessage,
		MessageTypeBroadcast,
		MessageTypeShutdownRequest,
		MessageTypeShutdownResponse,
		MessageTypePlanApprovalResponse,
	} {
		if _, err := NormalizeMessageType(msgType); err != nil {
			t.Fatalf("NormalizeMessageType(%q) returned error: %v", msgType, err)
		}
	}
}

