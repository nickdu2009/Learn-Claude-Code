package team

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Mailbox interface {
	Send(msg Message) error
	Drain(recipient string) ([]Message, error)
}

type FileMailbox struct {
	dir   string
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func NewFileMailbox(inboxDir string) (*FileMailbox, error) {
	if err := os.MkdirAll(inboxDir, 0o755); err != nil {
		return nil, fmt.Errorf("create inbox dir: %w", err)
	}
	return &FileMailbox{
		dir:   inboxDir,
		locks: make(map[string]*sync.Mutex),
	}, nil
}

func (m *FileMailbox) Send(msg Message) error {
	recipient, err := NormalizeName(msg.To)
	if err != nil {
		return err
	}
	if _, err := NormalizeMessageType(msg.Type); err != nil {
		return err
	}
	if strings.TrimSpace(msg.ID) == "" {
		return fmt.Errorf("message id is required")
	}
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now().UTC()
	}

	lock := m.lockFor(recipient)
	lock.Lock()
	defer lock.Unlock()

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	path := m.inboxPath(recipient)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open inbox: %w", err)
	}
	defer file.Close()

	if _, err := file.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("append inbox: %w", err)
	}
	return nil
}

func (m *FileMailbox) Drain(recipient string) ([]Message, error) {
	recipient, err := NormalizeName(recipient)
	if err != nil {
		return nil, err
	}

	lock := m.lockFor(recipient)
	lock.Lock()
	defer lock.Unlock()

	path := m.inboxPath(recipient)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open inbox: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	messages := make([]Message, 0)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var msg Message
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			return nil, fmt.Errorf("decode inbox message: %w", err)
		}
		messages = append(messages, msg)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan inbox: %w", err)
	}

	if err := file.Truncate(0); err != nil {
		return nil, fmt.Errorf("truncate inbox: %w", err)
	}
	if _, err := file.Seek(0, 0); err != nil {
		return nil, fmt.Errorf("rewind inbox: %w", err)
	}
	return messages, nil
}

func (m *FileMailbox) lockFor(recipient string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()

	lock, ok := m.locks[recipient]
	if !ok {
		lock = &sync.Mutex{}
		m.locks[recipient] = lock
	}
	return lock
}

func (m *FileMailbox) inboxPath(recipient string) string {
	return filepath.Join(m.dir, recipient+".jsonl")
}

