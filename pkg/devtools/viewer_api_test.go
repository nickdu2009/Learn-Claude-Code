package devtools

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestViewerAPI_TraceMetaSupportedAndUnsupported(t *testing.T) {
	dir := t.TempDir()
	tracePath := filepath.Join(dir, "generations.json")
	server := NewViewerServer(tracePath)

	writeViewerTrace(t, tracePath, database{
		Version: traceVersion,
		Runs:    []Run{},
		Steps:   []Step{},
	})

	resp := httptest.NewRecorder()
	server.Handler().ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/api/trace/meta", nil))
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
	}
	var meta TraceMeta
	if err := json.Unmarshal(resp.Body.Bytes(), &meta); err != nil {
		t.Fatalf("decode meta: %v", err)
	}
	if !meta.Supported || meta.Version != traceVersion {
		t.Fatalf("unexpected meta: %+v", meta)
	}

	if err := os.WriteFile(tracePath, []byte(`{"version":1,"runs":[],"steps":[]}`), 0o644); err != nil {
		t.Fatalf("write unsupported trace: %v", err)
	}

	resp = httptest.NewRecorder()
	server.Handler().ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/api/trace/meta", nil))
	if err := json.Unmarshal(resp.Body.Bytes(), &meta); err != nil {
		t.Fatalf("decode unsupported meta: %v", err)
	}
	if meta.Supported {
		t.Fatalf("expected unsupported meta, got %+v", meta)
	}
	if meta.Version != 1 {
		t.Fatalf("version = %d, want 1", meta.Version)
	}
}

func TestViewerAPI_RunTreeAndRunDetail(t *testing.T) {
	dir := t.TempDir()
	tracePath := filepath.Join(dir, "generations.json")
	server := NewViewerServer(tracePath)

	parentRunID := "root-run"
	childRunID := "child-run"
	parentStepID := "step-1"
	trace := database{
		Version:     traceVersion,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Runs: []Run{
			{
				ID:        parentRunID,
				Kind:      "main",
				Title:     "Root",
				Status:    "completed",
				StartedAt: "2026-03-06T12:00:00Z",
				FinishedAt: func() *string {
					s := "2026-03-06T12:00:05Z"
					return &s
				}(),
				StepCount: 1,
			},
			{
				ID:               childRunID,
				Kind:             "subagent",
				Title:            "Inspect repo",
				Status:           "completed",
				CompletionReason: stringPtr("normal"),
				StartedAt:        "2026-03-06T12:00:01Z",
				FinishedAt:       stringPtr("2026-03-06T12:00:04Z"),
				ParentRunID:      &parentRunID,
				ParentStepID:     &parentStepID,
				Summary:          stringPtr("Found the relevant files."),
				StepCount:        1,
			},
		},
		Steps: []Step{
			{
				ID:                parentStepID,
				RunID:             parentRunID,
				StepNumber:        1,
				Type:              "generate",
				ModelID:           "mock-model",
				StartedAt:         "2026-03-06T12:00:00Z",
				Input:             "{}",
				LinkedChildRunIDs: []string{childRunID},
			},
			{
				ID:         "child-step-1",
				RunID:      childRunID,
				StepNumber: 1,
				Type:       "generate",
				ModelID:    "mock-model",
				StartedAt:  "2026-03-06T12:00:02Z",
				Input:      "{}",
			},
		},
	}
	writeViewerTrace(t, tracePath, trace)

	resp := httptest.NewRecorder()
	server.Handler().ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/api/runs", nil))
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
	}
	var tree []RunTreeNode
	if err := json.Unmarshal(resp.Body.Bytes(), &tree); err != nil {
		t.Fatalf("decode runs tree: %v", err)
	}
	if len(tree) != 1 {
		t.Fatalf("roots = %d, want 1", len(tree))
	}
	if len(tree[0].Children) != 1 || tree[0].Children[0].ID != childRunID {
		t.Fatalf("unexpected child tree: %+v", tree[0].Children)
	}

	resp = httptest.NewRecorder()
	server.Handler().ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/api/runs/"+parentRunID, nil))
	if resp.Code != http.StatusOK {
		t.Fatalf("detail status = %d, want %d", resp.Code, http.StatusOK)
	}
	var detail RunDetail
	if err := json.Unmarshal(resp.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode run detail: %v", err)
	}
	if detail.Run.ID != parentRunID {
		t.Fatalf("run id = %q, want %q", detail.Run.ID, parentRunID)
	}
	children := detail.LinkedChildRunsByStep[parentStepID]
	if len(children) != 1 || children[0].ID != childRunID {
		t.Fatalf("unexpected linked child runs: %+v", children)
	}
}

func TestViewerAPI_MalformedTraceV2FailsFast(t *testing.T) {
	dir := t.TempDir()
	tracePath := filepath.Join(dir, "generations.json")
	server := NewViewerServer(tracePath)

	parentRunID := "root-run"
	parentStepID := "step-1"
	writeViewerTrace(t, tracePath, database{
		Version: traceVersion,
		Runs: []Run{
			{
				ID:        parentRunID,
				Kind:      "main",
				Title:     "",
				Status:    "completed",
				StartedAt: "2026-03-06T12:00:00Z",
				FinishedAt: func() *string {
					s := "2026-03-06T12:00:05Z"
					return &s
				}(),
				StepCount: 1,
			},
		},
		Steps: []Step{
			{
				ID:         parentStepID,
				RunID:      parentRunID,
				StepNumber: 1,
				Type:       "generate",
				ModelID:    "mock-model",
				StartedAt:  "2026-03-06T12:00:00Z",
				Input:      "{}",
			},
		},
	})

	resp := httptest.NewRecorder()
	server.Handler().ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/api/trace/meta", nil))
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
	}
	var meta TraceMeta
	if err := json.Unmarshal(resp.Body.Bytes(), &meta); err != nil {
		t.Fatalf("decode trace meta: %v", err)
	}
	if meta.Supported {
		t.Fatalf("expected unsupported meta, got %+v", meta)
	}
	if meta.Message == "" {
		t.Fatalf("expected error message, got %+v", meta)
	}

	resp = httptest.NewRecorder()
	server.Handler().ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/api/runs", nil))
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusBadRequest)
	}
}

func TestViewerAPI_Clear(t *testing.T) {
	dir := t.TempDir()
	tracePath := filepath.Join(dir, "generations.json")
	server := NewViewerServer(tracePath)

	writeViewerTrace(t, tracePath, database{
		Version: traceVersion,
		Runs: []Run{
			{
				ID:        "root-run",
				Kind:      "main",
				Title:     "Root",
				Status:    "completed",
				StartedAt: "2026-03-06T12:00:00Z",
				FinishedAt: func() *string {
					s := "2026-03-06T12:00:05Z"
					return &s
				}(),
				StepCount: 0,
			},
		},
	})

	resp := httptest.NewRecorder()
	server.Handler().ServeHTTP(resp, httptest.NewRequest(http.MethodPost, "/api/clear", nil))
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
	}

	var trace database
	data, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatalf("read trace: %v", err)
	}
	if err := json.Unmarshal(data, &trace); err != nil {
		t.Fatalf("decode trace: %v", err)
	}
	if len(trace.Runs) != 0 || len(trace.Steps) != 0 {
		t.Fatalf("expected cleared trace, got %+v", trace)
	}
}

func writeViewerTrace(t *testing.T, path string, trace database) {
	t.Helper()
	trace = normalizeTraceFile(trace)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir trace dir: %v", err)
	}
	data, err := json.Marshal(trace)
	if err != nil {
		t.Fatalf("marshal trace: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write trace: %v", err)
	}
}
