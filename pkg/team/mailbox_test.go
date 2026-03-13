package team

import (
	"sync"
	"testing"
	"time"
)

func TestFileMailbox_SendAndDrain(t *testing.T) {
	mailbox, err := NewFileMailbox(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileMailbox: %v", err)
	}

	if err := mailbox.Send(Message{
		ID:        "msg-1",
		Type:      MessageTypeMessage,
		From:      "lead",
		To:        "alice",
		Content:   "hello",
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	messages, err := mailbox.Drain("alice")
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("message count = %d, want 1", len(messages))
	}
	if messages[0].Content != "hello" {
		t.Fatalf("content = %q, want %q", messages[0].Content, "hello")
	}

	messages, err = mailbox.Drain("alice")
	if err != nil {
		t.Fatalf("Drain second time: %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("expected inbox to be empty, got %d messages", len(messages))
	}
}

func TestFileMailbox_ConcurrentSendAndDrain(t *testing.T) {
	mailbox, err := NewFileMailbox(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileMailbox: %v", err)
	}

	const senders = 24
	var wg sync.WaitGroup
	wg.Add(senders)
	for i := 0; i < senders; i++ {
		go func(i int) {
			defer wg.Done()
			if err := mailbox.Send(Message{
				ID:        time.Now().UTC().Format("20060102150405.000000000") + "-" + string(rune('a'+i)),
				Type:      MessageTypeMessage,
				From:      "lead",
				To:        "alice",
				Content:   "payload",
				CreatedAt: time.Now().UTC(),
			}); err != nil {
				t.Errorf("Send(%d): %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	messages, err := mailbox.Drain("alice")
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if len(messages) != senders {
		t.Fatalf("message count = %d, want %d", len(messages), senders)
	}
}

