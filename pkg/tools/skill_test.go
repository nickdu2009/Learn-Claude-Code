package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

type fakeSkillProvider struct {
	content map[string]string
	err     error
}

func (f fakeSkillProvider) Content(name string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	content, ok := f.content[name]
	if !ok {
		return "", fmt.Errorf("unknown skill %q", name)
	}
	return content, nil
}

func TestNewLoadSkillHandler_LoadsSkillContent(t *testing.T) {
	handler := NewLoadSkillHandler(fakeSkillProvider{
		content: map[string]string{
			"pdf": "<skill name=\"pdf\">\nPDF instructions\n</skill>",
		},
	})

	result, err := handler(context.Background(), map[string]any{"name": "pdf"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "PDF instructions") {
		t.Fatalf("unexpected result: %q", result)
	}
}

func TestNewLoadSkillHandler_RequiresName(t *testing.T) {
	handler := NewLoadSkillHandler(fakeSkillProvider{})

	_, err := handler(context.Background(), map[string]any{"name": "   "})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "name") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewLoadSkillHandler_RejectsPathLikeName(t *testing.T) {
	handler := NewLoadSkillHandler(fakeSkillProvider{})

	_, err := handler(context.Background(), map[string]any{"name": "../pdf"})
	if err == nil {
		t.Fatal("expected invalid skill name error")
	}
	if !strings.Contains(err.Error(), "invalid skill name") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewLoadSkillHandler_PropagatesProviderError(t *testing.T) {
	expectedErr := fmt.Errorf("unknown skill")
	handler := NewLoadSkillHandler(fakeSkillProvider{err: expectedErr})

	_, err := handler(context.Background(), map[string]any{"name": "missing"})
	if err == nil {
		t.Fatal("expected provider error")
	}
	if err != expectedErr {
		t.Fatalf("got %v, want %v", err, expectedErr)
	}
}
