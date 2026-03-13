package team

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nickdu2009/learn-claude-code/pkg/devtools"
	"github.com/nickdu2009/learn-claude-code/pkg/loop"
	"github.com/nickdu2009/learn-claude-code/pkg/tools"
	"github.com/openai/openai-go"
)

type Service struct {
	baseCtx context.Context
	client  *openai.Client
	model   string

	repo    MemberRepository
	mailbox Mailbox

	mu      sync.RWMutex
	members map[string]Member
	factory AgentFactory

	wakeupMu sync.Mutex
	wakeups  map[string]chan struct{}

	nextMessageID atomic.Uint64
}

func NewService(
	baseCtx context.Context,
	client *openai.Client,
	model string,
	repo MemberRepository,
	mailbox Mailbox,
) (*Service, error) {
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	if repo == nil {
		return nil, fmt.Errorf("member repository is required")
	}
	if mailbox == nil {
		return nil, fmt.Errorf("mailbox is required")
	}

	memberList, err := repo.Load()
	if err != nil {
		return nil, err
	}

	svc := &Service{
		baseCtx: baseCtx,
		client:  client,
		model:   strings.TrimSpace(model),
		repo:    repo,
		mailbox: mailbox,
		members: make(map[string]Member, len(memberList)),
		wakeups: make(map[string]chan struct{}),
	}
	svc.ensureWakeupLocked("lead")

	needsSave := false
	for _, member := range memberList {
		if member.Status == StatusWorking {
			member.Status = StatusIdle
			member.UpdatedAt = time.Now().UTC()
			needsSave = true
		}
		svc.members[member.Name] = member
		svc.ensureWakeupLocked(member.Name)
	}
	if needsSave {
		if err := repo.SaveAll(svc.snapshotMembersLocked()); err != nil {
			return nil, err
		}
	}

	return svc, nil
}

func (s *Service) SetFactory(factory AgentFactory) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.factory = factory
}

func (s *Service) Spawn(ctx context.Context, name, role, prompt string) (Member, error) {
	return s.spawnWithTrace(ctx, name, role, prompt)
}

func (s *Service) spawnWithTrace(ctx context.Context, name, role, prompt string) (Member, error) {
	name, err := NormalizeName(name)
	if err != nil {
		return Member{}, err
	}
	role, err = NormalizeRole(role)
	if err != nil {
		return Member{}, err
	}
	prompt = NormalizePrompt(prompt)
	if prompt == "" {
		return Member{}, fmt.Errorf("prompt is required")
	}

	s.mu.Lock()
	factory := s.factory
	if factory == nil {
		s.mu.Unlock()
		return Member{}, fmt.Errorf("agent factory is not configured")
	}
	existing, exists := s.members[name]
	if exists && existing.Status != StatusIdle && existing.Status != StatusShutdown {
		s.mu.Unlock()
		return Member{}, fmt.Errorf("%q is currently %s", name, existing.Status)
	}

	member := Member{
		Name:      name,
		Role:      role,
		Status:    StatusWorking,
		Prompt:    prompt,
		UpdatedAt: time.Now().UTC(),
	}
	systemPrompt, registry, err := factory.Build(member)
	if err != nil {
		s.mu.Unlock()
		return Member{}, err
	}
	teammateRecorder, teammateRunMeta, err := s.newTeammateRecorder(ctx, member, prompt)
	if err != nil {
		s.mu.Unlock()
		return Member{}, err
	}

	s.members[name] = member
	s.ensureWakeupLocked(name)
	if err := s.repo.SaveAll(s.snapshotMembersLocked()); err != nil {
		if exists {
			s.members[name] = existing
		} else {
			delete(s.members, name)
		}
		s.mu.Unlock()
		return Member{}, err
	}
	s.mu.Unlock()

	go s.runTeammate(member, prompt, systemPrompt, registry, teammateRecorder, teammateRunMeta)
	return member, nil
}

func (s *Service) List() ([]Member, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := s.snapshotMembersLocked()
	return out, nil
}

func (s *Service) Send(sender, recipient, content, msgType string) error {
	sender, err := NormalizeName(sender)
	if err != nil {
		return err
	}
	recipient, err = NormalizeName(recipient)
	if err != nil {
		return err
	}
	msgType, err = NormalizeMessageType(msgType)
	if err != nil {
		return err
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return fmt.Errorf("content is required")
	}

	if recipient != "lead" {
		s.mu.RLock()
		_, ok := s.members[recipient]
		s.mu.RUnlock()
		if !ok {
			return fmt.Errorf("unknown teammate %q", recipient)
		}
	}

	msg := Message{
		ID:        s.newMessageID(),
		Type:      msgType,
		From:      sender,
		To:        recipient,
		Content:   content,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.mailbox.Send(msg); err != nil {
		return err
	}
	s.signalWakeup(recipient)
	return nil
}

func (s *Service) Broadcast(sender, content string) (int, error) {
	sender, err := NormalizeName(sender)
	if err != nil {
		return 0, err
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return 0, fmt.Errorf("content is required")
	}

	s.mu.RLock()
	recipients := make([]string, 0, len(s.members))
	for name := range s.members {
		if name == sender {
			continue
		}
		recipients = append(recipients, name)
	}
	s.mu.RUnlock()
	slices.Sort(recipients)

	for _, recipient := range recipients {
		if err := s.Send(sender, recipient, content, MessageTypeBroadcast); err != nil {
			return 0, err
		}
	}
	return len(recipients), nil
}

func (s *Service) DrainInbox(recipient string) ([]Message, error) {
	recipient, err := NormalizeName(recipient)
	if err != nil {
		return nil, err
	}
	return s.mailbox.Drain(recipient)
}

func (s *Service) DrainInboxJSON(recipient string) (string, error) {
	messages, err := s.DrainInbox(recipient)
	if err != nil {
		return "", err
	}
	return formatMessages(messages)
}

func (s *Service) DrainInboxNotifications(recipient string) (string, error) {
	messages, err := s.DrainInbox(recipient)
	if err != nil {
		return "", err
	}
	if len(messages) == 0 {
		return "", nil
	}
	return formatMessages(messages)
}

func (s *Service) Wakeups(recipient string) <-chan struct{} {
	recipient, err := NormalizeName(recipient)
	if err != nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}

	s.wakeupMu.Lock()
	defer s.wakeupMu.Unlock()
	return s.ensureWakeupLockedNoMutex(recipient)
}

func (s *Service) runTeammate(
	member Member,
	prompt string,
	systemPrompt string,
	registry *tools.Registry,
	recorder devtools.Recorder,
	runMeta devtools.RunMeta,
) {
	runCtx := s.baseCtx
	runResult := devtools.RunResult{
		Status:           "completed",
		CompletionReason: "normal",
		Summary:          fmt.Sprintf("teammate %s finished", member.Name),
	}
	if recorder != nil {
		runCtx = devtools.WithRecorder(runCtx, recorder)
		if err := recorder.BeginRun(runCtx, runMeta); err != nil {
			_ = s.Send(member.Name, "lead", fmt.Sprintf("teammate %s failed to begin trace: %v", member.Name, err), MessageTypeMessage)
			_ = s.setStatus(member.Name, StatusIdle)
			return
		}
		defer func() {
			if s.baseCtx.Err() != nil {
				runResult.CompletionReason = "signal"
				runResult.Summary = fmt.Sprintf("teammate %s stopped with context cancellation", member.Name)
			}
			_ = recorder.FinishRun(runCtx, runResult)
		}()
	}

	if s.client == nil {
		_ = s.Send(member.Name, "lead", fmt.Sprintf("teammate %s has no configured client", member.Name), MessageTypeMessage)
		_ = s.setStatus(member.Name, StatusIdle)
		return
	}
	if strings.TrimSpace(s.model) == "" {
		_ = s.Send(member.Name, "lead", fmt.Sprintf("teammate %s has no configured model", member.Name), MessageTypeMessage)
		_ = s.setStatus(member.Name, StatusIdle)
		return
	}

	runner := loop.RunWithTeamInboxNotifications(member.Name, s)
	history := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(systemPrompt),
		openai.UserMessage(prompt),
	}
	var err error

	for {
		history, err = runner(runCtx, s.client, s.model, history, registry)
		if err != nil {
			runResult.Status = "failed"
			runResult.CompletionReason = "error"
			runResult.Summary = fmt.Sprintf("teammate %s stopped with error", member.Name)
			runResult.Error = err.Error()
			_ = s.Send(member.Name, "lead", fmt.Sprintf("teammate %s stopped with error: %v", member.Name, err), MessageTypeMessage)
			_ = s.setStatus(member.Name, StatusIdle)
			return
		}
		if err := s.setStatus(member.Name, StatusIdle); err != nil {
			return
		}

		select {
		case <-s.baseCtx.Done():
			_ = s.setStatus(member.Name, StatusShutdown)
			return
		case <-s.Wakeups(member.Name):
			if s.baseCtx.Err() != nil {
				_ = s.setStatus(member.Name, StatusShutdown)
				return
			}
			if err := s.setStatus(member.Name, StatusWorking); err != nil {
				return
			}
		}
	}
}

func (s *Service) setStatus(name string, status Status) error {
	status, err := NormalizeStatus(status)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	member, ok := s.members[name]
	if !ok {
		return fmt.Errorf("unknown teammate %q", name)
	}
	member.Status = status
	member.UpdatedAt = time.Now().UTC()
	s.members[name] = member
	return s.repo.SaveAll(s.snapshotMembersLocked())
}

func (s *Service) snapshotMembersLocked() []Member {
	members := make([]Member, 0, len(s.members))
	for _, member := range s.members {
		members = append(members, member)
	}
	slices.SortFunc(members, func(a, b Member) int {
		switch {
		case a.Name < b.Name:
			return -1
		case a.Name > b.Name:
			return 1
		default:
			return 0
		}
	})
	return members
}

func (s *Service) ensureWakeupLocked(recipient string) chan struct{} {
	s.wakeupMu.Lock()
	defer s.wakeupMu.Unlock()
	return s.ensureWakeupLockedNoMutex(recipient)
}

func (s *Service) ensureWakeupLockedNoMutex(recipient string) chan struct{} {
	ch, ok := s.wakeups[recipient]
	if !ok {
		ch = make(chan struct{}, 1)
		s.wakeups[recipient] = ch
	}
	return ch
}

func (s *Service) signalWakeup(recipient string) {
	s.wakeupMu.Lock()
	ch := s.ensureWakeupLockedNoMutex(recipient)
	s.wakeupMu.Unlock()

	select {
	case ch <- struct{}{}:
	default:
	}
}

func (s *Service) newMessageID() string {
	id := s.nextMessageID.Add(1)
	return fmt.Sprintf("msg-%d-%d", time.Now().UTC().UnixNano(), id)
}

func formatMessages(messages []Message) (string, error) {
	if len(messages) == 0 {
		return "[]", nil
	}
	data, err := json.MarshalIndent(messages, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal messages: %w", err)
	}
	return string(data), nil
}

func (s *Service) newTeammateRecorder(
	ctx context.Context,
	member Member,
	prompt string,
) (devtools.Recorder, devtools.RunMeta, error) {
	meta := devtools.RunMeta{
		Kind:         "teammate",
		Title:        fmt.Sprintf("teammate %s (%s)", member.Name, member.Role),
		InputPreview: previewPrompt(prompt, 160),
	}

	parentStepID := devtools.ParentStepFrom(ctx)
	parentRecorder := devtools.RecorderFrom(ctx)
	if parentStepID != "" && parentRecorder.RunID() != "" {
		recorder, err := parentRecorder.SpawnChild(ctx, parentStepID, devtools.ChildRunMeta{
			Kind:         meta.Kind,
			Title:        meta.Title,
			InputPreview: meta.InputPreview,
		})
		if err != nil {
			return nil, devtools.RunMeta{}, err
		}
		return recorder, meta, nil
	}

	return devtools.NewRecorderFromEnv(), meta, nil
}

func previewPrompt(prompt string, limit int) string {
	prompt = strings.Join(strings.Fields(strings.TrimSpace(prompt)), " ")
	if prompt == "" {
		return ""
	}
	if limit <= 0 || len(prompt) <= limit {
		return prompt
	}
	if limit <= 3 {
		return prompt[:limit]
	}
	return prompt[:limit-3] + "..."
}

