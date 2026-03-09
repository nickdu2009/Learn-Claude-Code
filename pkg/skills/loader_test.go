package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewLoader_LoadsSkillsAndDescriptions(t *testing.T) {
	dir := t.TempDir()
	writeSkillFile(t, dir, "code-review", `---
name: code-review
description: Review code carefully.
tags: review,security
---

# Review
Check for bugs.`)
	writeSkillFile(t, dir, "pdf", `---
name: pdf
description: Work with PDF files.
---

# PDF
Use pdftotext.`)

	loader, err := NewLoader(dir)
	if err != nil {
		t.Fatalf("NewLoader returned error: %v", err)
	}

	if got, want := loader.Names(), []string{"code-review", "pdf"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("Names() = %v, want %v", got, want)
	}

	descriptions := loader.Descriptions()
	if !strings.Contains(descriptions, "code-review: Review code carefully. [review,security]") {
		t.Fatalf("Descriptions() missing code-review line:\n%s", descriptions)
	}
	if !strings.Contains(descriptions, "pdf: Work with PDF files.") {
		t.Fatalf("Descriptions() missing pdf line:\n%s", descriptions)
	}

	content, err := loader.Content("pdf")
	if err != nil {
		t.Fatalf("Content returned error: %v", err)
	}
	if !strings.Contains(content, "<skill name=\"pdf\">") || !strings.Contains(content, "Use pdftotext.") {
		t.Fatalf("unexpected Content output:\n%s", content)
	}
}

func TestNewLoader_FallsBackToDirectoryNameWithoutFrontmatter(t *testing.T) {
	dir := t.TempDir()
	writeSkillFile(t, dir, "agent-builder", "# Agent Builder\nUse the loop.")

	loader, err := NewLoader(dir)
	if err != nil {
		t.Fatalf("NewLoader returned error: %v", err)
	}

	if got, want := loader.Names(), []string{"agent-builder"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("Names() = %v, want %v", got, want)
	}

	descriptions := loader.Descriptions()
	if !strings.Contains(descriptions, "agent-builder: No description") {
		t.Fatalf("Descriptions() = %q, want fallback description", descriptions)
	}
}

func TestNewLoader_RejectsDuplicateSkillNames(t *testing.T) {
	dir := t.TempDir()
	writeSkillFile(t, dir, "skill-a", `---
name: duplicate
description: First.
---

body`)
	writeSkillFile(t, dir, "skill-b", `---
name: duplicate
description: Second.
---

body`)

	_, err := NewLoader(dir)
	if err == nil {
		t.Fatal("expected duplicate skill name error")
	}
	if !strings.Contains(err.Error(), "duplicate skill name") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoader_ContentUnknownSkill(t *testing.T) {
	loader, err := NewLoader(t.TempDir())
	if err != nil {
		t.Fatalf("NewLoader returned error: %v", err)
	}

	_, err = loader.Content("missing")
	if err == nil {
		t.Fatal("expected unknown skill error")
	}
	if !strings.Contains(err.Error(), "unknown skill") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func writeSkillFile(t *testing.T, root string, name string, content string) {
	t.Helper()
	path := filepath.Join(root, name, "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
}
