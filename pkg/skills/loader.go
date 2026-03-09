package skills

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Skill is a single loadable knowledge bundle parsed from SKILL.md.
type Skill struct {
	Name        string
	Description string
	Tags        string
	Body        string
	Path        string
}

// Loader scans a skills directory and exposes skill summaries and bodies.
type Loader struct {
	skills map[string]Skill
	order  []string
}

// NewLoader loads all SKILL.md files beneath skillsDir.
func NewLoader(skillsDir string) (*Loader, error) {
	loader := &Loader{
		skills: make(map[string]Skill),
	}

	info, err := os.Stat(skillsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return loader, nil
		}
		return nil, fmt.Errorf("stat skills dir: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("skills path is not a directory: %s", skillsDir)
	}

	var paths []string
	err = filepath.WalkDir(skillsDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if d.Name() == "SKILL.md" {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk skills dir: %w", err)
	}

	slices.Sort(paths)
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		meta, body := parseFrontmatter(string(content))
		name := strings.TrimSpace(meta["name"])
		if name == "" {
			name = filepath.Base(filepath.Dir(path))
		}
		if _, exists := loader.skills[name]; exists {
			return nil, fmt.Errorf("duplicate skill name: %s", name)
		}
		loader.skills[name] = Skill{
			Name:        name,
			Description: strings.TrimSpace(meta["description"]),
			Tags:        strings.TrimSpace(meta["tags"]),
			Body:        body,
			Path:        path,
		}
		loader.order = append(loader.order, name)
	}

	slices.Sort(loader.order)
	return loader, nil
}

// Names returns the known skill names in stable order.
func (l *Loader) Names() []string {
	names := make([]string, len(l.order))
	copy(names, l.order)
	return names
}

// Descriptions returns Layer 1 metadata for the system prompt.
func (l *Loader) Descriptions() string {
	if len(l.order) == 0 {
		return "(no skills available)"
	}

	lines := make([]string, 0, len(l.order))
	for _, name := range l.order {
		skill := l.skills[name]
		desc := skill.Description
		if desc == "" {
			desc = "No description"
		}
		line := fmt.Sprintf("  - %s: %s", skill.Name, desc)
		if skill.Tags != "" {
			line += fmt.Sprintf(" [%s]", skill.Tags)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// Content returns Layer 2 full skill content wrapped for tool_result injection.
func (l *Loader) Content(name string) (string, error) {
	skill, ok := l.skills[name]
	if !ok {
		available := strings.Join(l.order, ", ")
		if available == "" {
			available = "(none)"
		}
		return "", fmt.Errorf("unknown skill %q. available: %s", name, available)
	}
	return fmt.Sprintf("<skill name=\"%s\">\n%s\n</skill>", skill.Name, skill.Body), nil
}

func parseFrontmatter(text string) (map[string]string, string) {
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		return map[string]string{}, strings.TrimSpace(normalized)
	}

	rest := strings.TrimPrefix(normalized, "---\n")
	idx := strings.Index(rest, "\n---\n")
	if idx < 0 {
		return map[string]string{}, strings.TrimSpace(normalized)
	}

	metaText := rest[:idx]
	body := strings.TrimSpace(rest[idx+len("\n---\n"):])
	meta := make(map[string]string)
	for _, line := range strings.Split(metaText, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, ":") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		meta[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	return meta, body
}
