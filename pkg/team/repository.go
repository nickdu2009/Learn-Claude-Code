package team

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
)

type MemberRepository interface {
	Load() ([]Member, error)
	SaveAll([]Member) error
}

type FileRepository struct {
	path string
	mu   sync.Mutex
}

func NewFileRepository(teamDir string) (*FileRepository, error) {
	if err := os.MkdirAll(teamDir, 0o755); err != nil {
		return nil, fmt.Errorf("create team dir: %w", err)
	}
	return &FileRepository{
		path: filepath.Join(teamDir, "config.json"),
	}, nil
}

func (r *FileRepository) Load() ([]Member, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	data, err := os.ReadFile(r.path)
	if err != nil {
		if os.IsNotExist(err) {
			return []Member{}, nil
		}
		return nil, fmt.Errorf("read team config: %w", err)
	}
	if len(data) == 0 {
		return []Member{}, nil
	}

	var cfg TeamConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal team config: %w", err)
	}

	members := make([]Member, len(cfg.Members))
	copy(members, cfg.Members)
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
	return members, nil
}

func (r *FileRepository) SaveAll(members []Member) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	sorted := append([]Member(nil), members...)
	slices.SortFunc(sorted, func(a, b Member) int {
		switch {
		case a.Name < b.Name:
			return -1
		case a.Name > b.Name:
			return 1
		default:
			return 0
		}
	})

	cfg := TeamConfig{
		TeamName: "default",
		Members:  sorted,
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal team config: %w", err)
	}

	tmpPath := r.path + ".tmp"
	if err := os.WriteFile(tmpPath, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write temp team config: %w", err)
	}
	if err := os.Rename(tmpPath, r.path); err != nil {
		return fmt.Errorf("rename temp team config: %w", err)
	}
	return nil
}

