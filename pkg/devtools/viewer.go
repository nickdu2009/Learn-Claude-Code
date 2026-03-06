package devtools

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

//go:embed viewer/dist/client
var viewerClientFS embed.FS

type TraceMeta struct {
	Supported   bool   `json:"supported"`
	Version     int    `json:"version"`
	GeneratedAt string `json:"generated_at,omitempty"`
	Message     string `json:"message,omitempty"`
}

type RunTreeNode struct {
	ID               string        `json:"id"`
	Kind             string        `json:"kind"`
	Title            string        `json:"title"`
	Status           string        `json:"status"`
	CompletionReason *string       `json:"completion_reason,omitempty"`
	StartedAt        string        `json:"started_at"`
	FinishedAt       *string       `json:"finished_at,omitempty"`
	StepCount        int           `json:"step_count"`
	Summary          *string       `json:"summary,omitempty"`
	InputPreview     *string       `json:"input_preview,omitempty"`
	ChildCount       int           `json:"child_count"`
	Children         []RunTreeNode `json:"children"`
}

type RunDetail struct {
	Run                   Run              `json:"run"`
	Steps                 []Step           `json:"steps"`
	LinkedChildRunsByStep map[string][]Run `json:"linked_child_runs_by_step"`
	Parent                *Run             `json:"parent,omitempty"`
}

type ViewerServer struct {
	tracePath string
	clientFS  fs.FS

	subscribersMu sync.Mutex
	subscribers   map[chan string]struct{}
}

func NewViewerServer(tracePath string) *ViewerServer {
	clientFS, _ := fs.Sub(viewerClientFS, "viewer/dist/client")
	return &ViewerServer{
		tracePath:   tracePath,
		clientFS:    clientFS,
		subscribers: make(map[chan string]struct{}),
	}
}

func (s *ViewerServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/trace/meta", s.handleTraceMeta)
	mux.HandleFunc("/api/runs", s.handleRuns)
	mux.HandleFunc("/api/runs/", s.handleRunDetail)
	mux.HandleFunc("/api/clear", s.handleClear)
	mux.HandleFunc("/api/notify", s.handleNotify)
	mux.HandleFunc("/api/events", s.handleEvents)
	mux.Handle("/assets/", s.staticAssetHandler())
	mux.HandleFunc("/", s.handleFrontend)
	return mux
}

func (s *ViewerServer) handleFrontend(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		http.NotFound(w, r)
		return
	}
	if s.clientFS == nil {
		http.Error(w, "Embedded DevTools client is unavailable. Rebuild the viewer assets.", http.StatusInternalServerError)
		return
	}
	data, err := fs.ReadFile(s.clientFS, "index.html")
	if err != nil {
		http.Error(w, "Embedded DevTools client is unavailable. Rebuild the viewer assets.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

func (s *ViewerServer) staticAssetHandler() http.Handler {
	if s.clientFS == nil {
		return http.NotFoundHandler()
	}
	assetsFS, err := fs.Sub(s.clientFS, "assets")
	if err != nil {
		return http.NotFoundHandler()
	}
	return http.StripPrefix("/assets/", http.FileServer(http.FS(assetsFS)))
}

func (s *ViewerServer) handleTraceMeta(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	_, meta := s.loadTrace()
	writeJSON(w, http.StatusOK, meta)
}

func (s *ViewerServer) handleRuns(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	trace, meta := s.loadTrace()
	if !meta.Supported {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "unsupported-trace-version",
			"message": meta.Message,
		})
		return
	}

	writeJSON(w, http.StatusOK, buildRunTree(trace))
}

func (s *ViewerServer) handleRunDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	trace, meta := s.loadTrace()
	if !meta.Supported {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "unsupported-trace-version",
			"message": meta.Message,
		})
		return
	}

	runID := strings.TrimPrefix(r.URL.Path, "/api/runs/")
	runID = strings.TrimSpace(runID)
	if runID == "" {
		http.NotFound(w, r)
		return
	}

	detail, ok := buildRunDetail(trace, runID)
	if !ok {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *ViewerServer) handleNotify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close()

	var payload struct {
		Event string `json:"event"`
	}
	_ = json.NewDecoder(r.Body).Decode(&payload)
	if strings.TrimSpace(payload.Event) == "" {
		payload.Event = "refresh"
	}
	s.broadcast(payload.Event)
	w.WriteHeader(http.StatusNoContent)
}

func (s *ViewerServer) handleClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	trace := newTraceFile()
	trace.GeneratedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := writeTraceFile(s.tracePath, trace); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error":   "clear-failed",
			"message": err.Error(),
		})
		return
	}

	s.broadcast("trace")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *ViewerServer) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := make(chan string, 8)
	s.subscribe(ch)
	defer s.unsubscribe(ch)

	fmt.Fprintf(w, "event: ready\ndata: %s\n\n", mustJSON(map[string]string{"event": "ready"}))
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-ch:
			fmt.Fprintf(w, "event: trace\ndata: %s\n\n", mustJSON(map[string]string{"event": event}))
			flusher.Flush()
		}
	}
}

func (s *ViewerServer) subscribe(ch chan string) {
	s.subscribersMu.Lock()
	defer s.subscribersMu.Unlock()
	s.subscribers[ch] = struct{}{}
}

func (s *ViewerServer) unsubscribe(ch chan string) {
	s.subscribersMu.Lock()
	defer s.subscribersMu.Unlock()
	delete(s.subscribers, ch)
	close(ch)
}

func (s *ViewerServer) broadcast(event string) {
	s.subscribersMu.Lock()
	defer s.subscribersMu.Unlock()

	for ch := range s.subscribers {
		select {
		case ch <- event:
		default:
		}
	}
}

func (s *ViewerServer) loadTrace() (database, TraceMeta) {
	if s == nil || strings.TrimSpace(s.tracePath) == "" {
		return newTraceFile(), TraceMeta{
			Supported: true,
			Version:   traceVersion,
			Message:   "Trace path is not configured.",
		}
	}

	data, err := os.ReadFile(s.tracePath)
	if err != nil {
		if os.IsNotExist(err) {
			return newTraceFile(), TraceMeta{
				Supported: true,
				Version:   traceVersion,
				Message:   "No trace file found yet.",
			}
		}
		return newTraceFile(), TraceMeta{
			Supported: false,
			Message:   fmt.Sprintf("Failed to read trace file: %v", err),
		}
	}

	var envelope struct {
		Version     int    `json:"version"`
		GeneratedAt string `json:"generated_at"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return newTraceFile(), TraceMeta{
			Supported: false,
			Message:   fmt.Sprintf("Trace file is not valid JSON: %v", err),
		}
	}
	if envelope.Version != traceVersion {
		return newTraceFile(), TraceMeta{
			Supported: false,
			Version:   envelope.Version,
			Message:   fmt.Sprintf("Unsupported trace version %d. Viewer requires version %d.", envelope.Version, traceVersion),
		}
	}

	var trace database
	if err := json.Unmarshal(data, &trace); err != nil {
		return newTraceFile(), TraceMeta{
			Supported: false,
			Version:   envelope.Version,
			Message:   fmt.Sprintf("Failed to decode Trace V2 file: %v", err),
		}
	}
	trace = normalizeTraceFile(trace)
	if err := validateTraceFile(trace); err != nil {
		return newTraceFile(), TraceMeta{
			Supported: false,
			Version:   trace.Version,
			Message:   fmt.Sprintf("Trace V2 invariants failed: %v", err),
		}
	}
	if err := validateViewerTraceData(trace); err != nil {
		return newTraceFile(), TraceMeta{
			Supported: false,
			Version:   trace.Version,
			Message:   fmt.Sprintf("Trace V2 viewer validation failed: %v", err),
		}
	}

	return trace, TraceMeta{
		Supported:   true,
		Version:     trace.Version,
		GeneratedAt: trace.GeneratedAt,
	}
}

func buildRunTree(trace database) []RunTreeNode {
	runByID := make(map[string]Run, len(trace.Runs))
	for _, run := range trace.Runs {
		runByID[run.ID] = run
	}

	childrenByParent := make(map[string][]string, len(trace.Runs))
	var roots []string
	for _, run := range trace.Runs {
		parentRunID := derefString(run.ParentRunID)
		if parentRunID == "" {
			roots = append(roots, run.ID)
			continue
		}
		if _, exists := runByID[parentRunID]; !exists {
			roots = append(roots, run.ID)
			continue
		}
		childrenByParent[parentRunID] = append(childrenByParent[parentRunID], run.ID)
	}

	sortRunIDs(roots, runByID)
	for parentRunID := range childrenByParent {
		sortRunIDs(childrenByParent[parentRunID], runByID)
	}

	nodes := make([]RunTreeNode, 0, len(roots))
	for _, runID := range roots {
		nodes = append(nodes, buildRunTreeNode(runByID, childrenByParent, runID))
	}
	return nodes
}

func buildRunTreeNode(runByID map[string]Run, childrenByParent map[string][]string, runID string) RunTreeNode {
	run := runByID[runID]
	childIDs := childrenByParent[runID]
	children := make([]RunTreeNode, 0, len(childIDs))
	for _, childID := range childIDs {
		children = append(children, buildRunTreeNode(runByID, childrenByParent, childID))
	}

	return RunTreeNode{
		ID:               run.ID,
		Kind:             run.Kind,
		Title:            run.Title,
		Status:           run.Status,
		CompletionReason: run.CompletionReason,
		StartedAt:        run.StartedAt,
		FinishedAt:       run.FinishedAt,
		StepCount:        run.StepCount,
		Summary:          run.Summary,
		InputPreview:     run.InputPreview,
		ChildCount:       len(children),
		Children:         children,
	}
}

func buildRunDetail(trace database, runID string) (RunDetail, bool) {
	runByID := make(map[string]Run, len(trace.Runs))
	for _, run := range trace.Runs {
		runByID[run.ID] = run
	}

	selectedRun, ok := runByID[runID]
	if !ok {
		return RunDetail{}, false
	}

	steps := make([]Step, 0)
	stepIDs := make(map[string]struct{})
	for _, step := range trace.Steps {
		if step.RunID != runID {
			continue
		}
		step.LinkedChildRunIDs = uniqueStrings(step.LinkedChildRunIDs)
		steps = append(steps, step)
		stepIDs[step.ID] = struct{}{}
	}
	sort.Slice(steps, func(i, j int) bool {
		if steps[i].StepNumber == steps[j].StepNumber {
			return steps[i].StartedAt < steps[j].StartedAt
		}
		return steps[i].StepNumber < steps[j].StepNumber
	})

	linked := make(map[string][]Run, len(steps))
	for _, run := range trace.Runs {
		parentStepID := derefString(run.ParentStepID)
		parentRunID := derefString(run.ParentRunID)
		if parentRunID != runID {
			continue
		}
		if _, exists := stepIDs[parentStepID]; !exists {
			continue
		}
		linked[parentStepID] = append(linked[parentStepID], run)
	}
	for stepID := range linked {
		sortRuns(linked[stepID])
	}

	var parent *Run
	if parentRunID := derefString(selectedRun.ParentRunID); parentRunID != "" {
		if parentRun, exists := runByID[parentRunID]; exists {
			parent = &parentRun
		}
	}

	return RunDetail{
		Run:                   selectedRun,
		Steps:                 steps,
		LinkedChildRunsByStep: linked,
		Parent:                parent,
	}, true
}

func sortRunIDs(runIDs []string, runByID map[string]Run) {
	sort.Slice(runIDs, func(i, j int) bool {
		left := runByID[runIDs[i]]
		right := runByID[runIDs[j]]
		if left.StartedAt == right.StartedAt {
			return left.ID < right.ID
		}
		return left.StartedAt < right.StartedAt
	})
}

func sortRuns(runs []Run) {
	sort.Slice(runs, func(i, j int) bool {
		if runs[i].StartedAt == runs[j].StartedAt {
			return runs[i].ID < runs[j].ID
		}
		return runs[i].StartedAt < runs[j].StartedAt
	})
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func validateViewerTraceData(trace database) error {
	for _, run := range trace.Runs {
		if strings.TrimSpace(run.Kind) == "" {
			return fmt.Errorf("run %q kind is required", run.ID)
		}
		if strings.TrimSpace(run.Title) == "" {
			return fmt.Errorf("run %q title is required", run.ID)
		}
		if strings.TrimSpace(run.Status) == "" {
			return fmt.Errorf("run %q status is required", run.ID)
		}
		if strings.TrimSpace(run.StartedAt) == "" {
			return fmt.Errorf("run %q started_at is required", run.ID)
		}
	}

	for _, step := range trace.Steps {
		if strings.TrimSpace(step.Type) == "" {
			return fmt.Errorf("step %q type is required", step.ID)
		}
		if strings.TrimSpace(step.ModelID) == "" {
			return fmt.Errorf("step %q model_id is required", step.ID)
		}
		if strings.TrimSpace(step.StartedAt) == "" {
			return fmt.Errorf("step %q started_at is required", step.ID)
		}
		if !json.Valid([]byte(step.Input)) {
			return fmt.Errorf("step %q input must be valid JSON", step.ID)
		}
		if step.Output != nil && !json.Valid([]byte(*step.Output)) {
			return fmt.Errorf("step %q output must be valid JSON", step.ID)
		}
		if step.Usage != nil && !json.Valid([]byte(*step.Usage)) {
			return fmt.Errorf("step %q usage must be valid JSON", step.ID)
		}
		if step.ProviderOption != nil && !json.Valid([]byte(*step.ProviderOption)) {
			return fmt.Errorf("step %q provider_options must be valid JSON", step.ID)
		}
		if step.RawRequest != nil && !json.Valid([]byte(*step.RawRequest)) {
			return fmt.Errorf("step %q raw_request must be valid JSON", step.ID)
		}
		if step.RawResponse != nil && !json.Valid([]byte(*step.RawResponse)) {
			return fmt.Errorf("step %q raw_response must be valid JSON", step.ID)
		}
		if step.RawChunks != nil && !json.Valid([]byte(*step.RawChunks)) {
			return fmt.Errorf("step %q raw_chunks must be valid JSON", step.ID)
		}
	}

	return nil
}

func writeTraceFile(tracePath string, trace database) error {
	if strings.TrimSpace(tracePath) == "" {
		return fmt.Errorf("trace path is not configured")
	}
	trace = normalizeTraceFile(trace)
	if err := validateTraceFile(trace); err != nil {
		return err
	}
	if err := validateViewerTraceData(trace); err != nil {
		return err
	}
	if err := os.MkdirAll(filepathDir(tracePath), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(trace, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(tracePath, data, 0o644)
}

func filepathDir(path string) string {
	lastSlash := strings.LastIndex(path, string(os.PathSeparator))
	if lastSlash <= 0 {
		return "."
	}
	return path[:lastSlash]
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}
