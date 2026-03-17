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
	baseCtx    context.Context
	cancelBase context.CancelFunc
	client     *openai.Client
	model      string

	repo    MemberRepository
	mailbox Mailbox

	mu      sync.RWMutex
	members map[string]Member
	factory AgentFactory

	wakeupMu sync.Mutex
	wakeups  map[string]chan struct{}

	traceMu           sync.Mutex
	teammateRunSeq    map[string]uint64
	pendingRunReasons map[string][]string

	nextMessageID atomic.Uint64
	nextRequestID atomic.Uint64
	runWG         sync.WaitGroup

	protocolMu       sync.RWMutex
	shutdownRequests map[string]ShutdownRequest
	planRequests     map[string]PlanRequest
	shutdownIntents  map[string]string
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
	serviceCtx, cancelBase := context.WithCancel(baseCtx)

	svc := &Service{
		baseCtx:    serviceCtx,
		cancelBase: cancelBase,
		client:     client,
		model:      strings.TrimSpace(model),
		repo:       repo,
		mailbox:    mailbox,
		members:    make(map[string]Member, len(memberList)),
		wakeups:    make(map[string]chan struct{}),

		teammateRunSeq:    make(map[string]uint64, len(memberList)),
		pendingRunReasons: make(map[string][]string, len(memberList)),
		shutdownRequests:  make(map[string]ShutdownRequest),
		planRequests:      make(map[string]PlanRequest),
		shutdownIntents:   make(map[string]string),
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

	s.runWG.Add(1)
	go func() {
		defer s.runWG.Done()
		s.runTeammate(ctx, member, prompt, systemPrompt, registry)
	}()
	return member, nil
}

func (s *Service) List() ([]Member, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := s.snapshotMembersLocked()
	return out, nil
}

func (s *Service) RequestShutdown(teammate string) (ShutdownRequest, error) {
	teammate, err := NormalizeName(teammate)
	if err != nil {
		return ShutdownRequest{}, err
	}

	s.mu.RLock()
	member, ok := s.members[teammate]
	s.mu.RUnlock()
	if !ok {
		return ShutdownRequest{}, fmt.Errorf("unknown teammate %q", teammate)
	}
	if member.Status == StatusShutdown {
		return ShutdownRequest{}, fmt.Errorf("%q is already shutdown", teammate)
	}

	req := ShutdownRequest{
		RequestID: s.newRequestID("shutdown"),
		Target:    teammate,
		Status:    RequestPending,
		UpdatedAt: time.Now().UTC(),
	}
	s.protocolMu.Lock()
	s.shutdownRequests[req.RequestID] = req
	s.protocolMu.Unlock()

	if err := s.sendWithExtra("lead", teammate, "Please shut down gracefully.", MessageTypeShutdownRequest, map[string]string{
		"request_id": req.RequestID,
	}); err != nil {
		return ShutdownRequest{}, err
	}
	return req, nil
}

func (s *Service) CheckShutdownRequest(requestID string) (ShutdownRequest, error) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return ShutdownRequest{}, fmt.Errorf("request id is required")
	}

	s.protocolMu.RLock()
	defer s.protocolMu.RUnlock()

	req, ok := s.shutdownRequests[requestID]
	if !ok {
		return ShutdownRequest{}, fmt.Errorf("unknown shutdown request %q", requestID)
	}
	return req, nil
}

func (s *Service) RespondShutdown(sender, requestID string, approve bool, reason string) (ShutdownRequest, error) {
	sender, err := NormalizeName(sender)
	if err != nil {
		return ShutdownRequest{}, err
	}
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return ShutdownRequest{}, fmt.Errorf("request id is required")
	}
	reason = strings.TrimSpace(reason)

	s.mu.RLock()
	_, ok := s.members[sender]
	s.mu.RUnlock()
	if !ok {
		return ShutdownRequest{}, fmt.Errorf("unknown teammate %q", sender)
	}

	s.protocolMu.Lock()
	req, ok := s.shutdownRequests[requestID]
	if !ok {
		s.protocolMu.Unlock()
		return ShutdownRequest{}, fmt.Errorf("unknown shutdown request %q", requestID)
	}
	if req.Target != sender {
		s.protocolMu.Unlock()
		return ShutdownRequest{}, fmt.Errorf("shutdown request %q targets %q, not %q", requestID, req.Target, sender)
	}
	req.Status = RequestRejected
	if approve {
		req.Status = RequestApproved
		s.shutdownIntents[sender] = requestID
	} else {
		delete(s.shutdownIntents, sender)
	}
	req.Reason = reason
	req.UpdatedAt = time.Now().UTC()
	s.shutdownRequests[requestID] = req
	s.protocolMu.Unlock()

	content := reason
	if content == "" {
		content = fmt.Sprintf("shutdown_response::%s::%t", requestID, approve)
	}
	if err := s.sendWithExtra(sender, "lead", content, MessageTypeShutdownResponse, map[string]string{
		"request_id": requestID,
		"approve":    fmt.Sprintf("%t", approve),
		"reason":     reason,
	}); err != nil {
		return ShutdownRequest{}, err
	}
	return req, nil
}

func (s *Service) SubmitPlan(sender, plan string) (PlanRequest, error) {
	sender, err := NormalizeName(sender)
	if err != nil {
		return PlanRequest{}, err
	}
	plan = strings.TrimSpace(plan)
	if plan == "" {
		return PlanRequest{}, fmt.Errorf("plan is required")
	}

	s.mu.RLock()
	_, ok := s.members[sender]
	s.mu.RUnlock()
	if !ok {
		return PlanRequest{}, fmt.Errorf("unknown teammate %q", sender)
	}

	req := PlanRequest{
		RequestID: s.newRequestID("plan"),
		From:      sender,
		Plan:      plan,
		Status:    RequestPending,
		UpdatedAt: time.Now().UTC(),
	}
	s.protocolMu.Lock()
	s.planRequests[req.RequestID] = req
	s.protocolMu.Unlock()

	if err := s.sendWithExtra(sender, "lead", plan, MessageTypePlanApprovalResponse, map[string]string{
		"request_id": req.RequestID,
		"plan":       plan,
	}); err != nil {
		return PlanRequest{}, err
	}
	return req, nil
}

func (s *Service) ReviewPlan(requestID string, approve bool, feedback string) (PlanRequest, error) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return PlanRequest{}, fmt.Errorf("request id is required")
	}
	feedback = strings.TrimSpace(feedback)

	s.protocolMu.Lock()
	req, ok := s.planRequests[requestID]
	if !ok {
		s.protocolMu.Unlock()
		return PlanRequest{}, fmt.Errorf("unknown plan request %q", requestID)
	}
	req.Status = RequestRejected
	if approve {
		req.Status = RequestApproved
	}
	req.Feedback = feedback
	req.UpdatedAt = time.Now().UTC()
	s.planRequests[requestID] = req
	s.protocolMu.Unlock()

	content := feedback
	if content == "" {
		content = fmt.Sprintf("plan_approval::%s::%t", requestID, approve)
	}
	if err := s.sendWithExtra("lead", req.From, content, MessageTypePlanApprovalResponse, map[string]string{
		"request_id": requestID,
		"approve":    fmt.Sprintf("%t", approve),
		"feedback":   feedback,
	}); err != nil {
		return PlanRequest{}, err
	}
	return req, nil
}

func (s *Service) ListShutdownRequests() []ShutdownRequest {
	s.protocolMu.RLock()
	defer s.protocolMu.RUnlock()

	requests := make([]ShutdownRequest, 0, len(s.shutdownRequests))
	for _, req := range s.shutdownRequests {
		requests = append(requests, req)
	}
	slices.SortFunc(requests, func(a, b ShutdownRequest) int {
		switch {
		case a.RequestID < b.RequestID:
			return -1
		case a.RequestID > b.RequestID:
			return 1
		default:
			return 0
		}
	})
	return requests
}

func (s *Service) ListPlanRequests() []PlanRequest {
	s.protocolMu.RLock()
	defer s.protocolMu.RUnlock()

	requests := make([]PlanRequest, 0, len(s.planRequests))
	for _, req := range s.planRequests {
		requests = append(requests, req)
	}
	slices.SortFunc(requests, func(a, b PlanRequest) int {
		switch {
		case a.RequestID < b.RequestID:
			return -1
		case a.RequestID > b.RequestID:
			return 1
		default:
			return 0
		}
	})
	return requests
}

func (s *Service) Send(sender, recipient, content, msgType string) error {
	return s.sendWithExtra(sender, recipient, content, msgType, nil)
}

func (s *Service) sendWithExtra(sender, recipient, content, msgType string, extra map[string]string) error {
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
	if len(extra) > 0 {
		msg.Extra = mapsClone(extra)
	}
	if err := s.mailbox.Send(msg); err != nil {
		return err
	}
	s.noteRunWakeup(recipient, sender, msgType, content)
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

func (s *Service) Wait() {
	s.runWG.Wait()
}

func (s *Service) Shutdown(ctx context.Context) error {
	if s.cancelBase != nil {
		s.cancelBase()
	}
	if ctx == nil {
		s.Wait()
		return nil
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Wait()
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) runTeammate(
	traceCtx context.Context,
	member Member,
	prompt string,
	systemPrompt string,
	registry *tools.Registry,
) {
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

	for {
		runMeta := s.nextTeammateRunMeta(member, prompt)
		runResult := devtools.RunResult{
			Status:           "completed",
			CompletionReason: "normal",
			Summary:          fmt.Sprintf("teammate %s finished", member.Name),
		}
		runCtx, recorder, err := s.beginTeammateRun(traceCtx, member, prompt, runMeta)
		if err != nil {
			_ = s.Send(member.Name, "lead", fmt.Sprintf("teammate %s failed to begin trace: %v", member.Name, err), MessageTypeMessage)
			_ = s.setStatus(member.Name, StatusIdle)
			return
		}

		nextHistory, err := runner(runCtx, s.client, s.model, history, registry)
		if err != nil {
			if s.baseCtx.Err() != nil {
				runResult.CompletionReason = "signal"
				runResult.Summary = fmt.Sprintf("teammate %s stopped with context cancellation", member.Name)
				s.finishTeammateRun(runCtx, recorder, member, runResult)
				_ = s.setStatus(member.Name, StatusShutdown)
				return
			}
			runResult.Status = "failed"
			runResult.CompletionReason = "error"
			runResult.Summary = fmt.Sprintf("teammate %s stopped with error", member.Name)
			runResult.Error = err.Error()
			_ = s.Send(member.Name, "lead", fmt.Sprintf("teammate %s stopped with error: %v", member.Name, err), MessageTypeMessage)
			s.finishTeammateRun(runCtx, recorder, member, runResult)
			_ = s.setStatus(member.Name, StatusIdle)
			return
		}
		history = nextHistory
		s.finishTeammateRun(runCtx, recorder, member, runResult)
		if _, shouldShutdown := s.consumeShutdownIntent(member.Name); shouldShutdown {
			if err := s.setStatus(member.Name, StatusShutdown); err != nil {
				return
			}
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

func (s *Service) beginTeammateRun(
	traceCtx context.Context,
	member Member,
	prompt string,
	runMeta devtools.RunMeta,
) (context.Context, devtools.Recorder, error) {
	recorder, _, err := s.newTeammateRecorder(traceCtx, member, prompt)
	if err != nil {
		return nil, nil, err
	}

	runCtx := s.baseCtx
	if recorder != nil {
		runCtx = devtools.WithRecorder(runCtx, recorder)
		if err := recorder.BeginRun(runCtx, runMeta); err != nil {
			return nil, nil, err
		}
	}
	return runCtx, recorder, nil
}

func (s *Service) finishTeammateRun(
	runCtx context.Context,
	recorder devtools.Recorder,
	member Member,
	runResult devtools.RunResult,
) {
	if recorder == nil {
		return
	}
	if s.baseCtx.Err() != nil {
		runResult.CompletionReason = "signal"
		runResult.Summary = fmt.Sprintf("teammate %s stopped with context cancellation", member.Name)
	}
	_ = recorder.FinishRun(runCtx, runResult)
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

func (s *Service) newRequestID(prefix string) string {
	id := s.nextRequestID.Add(1)
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "request"
	}
	return fmt.Sprintf("%s-%d-%d", prefix, time.Now().UTC().UnixNano(), id)
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

func FormatShutdownRequest(req ShutdownRequest) (string, error) {
	data, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal shutdown request: %w", err)
	}
	return string(data), nil
}

func FormatPlanRequest(req PlanRequest) (string, error) {
	data, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal plan request: %w", err)
	}
	return string(data), nil
}

func (s *Service) newTeammateRecorder(
	ctx context.Context,
	member Member,
	prompt string,
) (devtools.Recorder, devtools.RunMeta, error) {
	meta := newTeammateRunMeta(member, prompt, "", 0)

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

func newTeammateRunMeta(member Member, prompt string, reason string, seq uint64) devtools.RunMeta {
	reason = previewPrompt(reason, 72)
	title := fmt.Sprintf("teammate %s (%s)", member.Name, member.Role)
	if seq > 0 {
		title = fmt.Sprintf("%s #%d", title, seq)
	}
	if reason != "" {
		title = title + " - " + reason
	}

	inputPreview := reason
	if inputPreview == "" {
		inputPreview = previewPrompt(prompt, 160)
	}

	return devtools.RunMeta{
		Kind:         "teammate",
		Title:        title,
		InputPreview: inputPreview,
	}
}

func (s *Service) nextTeammateRunMeta(member Member, prompt string) devtools.RunMeta {
	s.traceMu.Lock()
	defer s.traceMu.Unlock()

	s.teammateRunSeq[member.Name]++
	seq := s.teammateRunSeq[member.Name]
	reason := summarizeRunReasons(s.pendingRunReasons[member.Name])
	delete(s.pendingRunReasons, member.Name)

	if reason == "" {
		reason = "initial task: " + previewPrompt(prompt, 56)
	}
	return newTeammateRunMeta(member, prompt, reason, seq)
}

func (s *Service) noteRunWakeup(recipient, sender, msgType, content string) {
	if recipient == "lead" {
		return
	}

	reason := formatRunWakeupReason(sender, msgType, content)
	if reason == "" {
		return
	}

	s.traceMu.Lock()
	defer s.traceMu.Unlock()
	s.pendingRunReasons[recipient] = append(s.pendingRunReasons[recipient], reason)
}

func summarizeRunReasons(reasons []string) string {
	switch len(reasons) {
	case 0:
		return ""
	case 1:
		return reasons[0]
	case 2:
		return previewPrompt(reasons[0]+"; "+reasons[1], 72)
	default:
		return previewPrompt(fmt.Sprintf("%s (+%d more)", reasons[0], len(reasons)-1), 72)
	}
}

func (s *Service) consumeShutdownIntent(name string) (string, bool) {
	s.protocolMu.Lock()
	defer s.protocolMu.Unlock()

	requestID, ok := s.shutdownIntents[name]
	if ok {
		delete(s.shutdownIntents, name)
	}
	return requestID, ok
}

func mapsClone(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		if strings.TrimSpace(value) == "" {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func formatRunWakeupReason(sender, msgType, content string) string {
	sender = strings.TrimSpace(sender)
	msgType = strings.TrimSpace(msgType)
	content = previewPrompt(content, 48)
	if sender == "" && content == "" {
		return ""
	}

	if msgType == "" || msgType == MessageTypeMessage {
		if sender == "" {
			return content
		}
		if content == "" {
			return sender + " message"
		}
		return fmt.Sprintf("%s message: %s", sender, content)
	}

	if sender == "" {
		if content == "" {
			return msgType
		}
		return fmt.Sprintf("%s: %s", msgType, content)
	}
	if content == "" {
		return fmt.Sprintf("%s %s", sender, msgType)
	}
	return fmt.Sprintf("%s %s: %s", sender, msgType, content)
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
