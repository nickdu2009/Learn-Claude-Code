package tasks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"sync"
)

var taskFilePattern = regexp.MustCompile(`^task_(\d+)\.json$`)

type FileRepository struct {
	dir string
	mu  sync.Mutex
}

func NewFileRepository(dir string) (*FileRepository, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create task dir: %w", err)
	}
	return &FileRepository{dir: dir}, nil
}

func (r *FileRepository) NextID() (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entries, err := os.ReadDir(r.dir)
	if err != nil {
		return 0, fmt.Errorf("read task dir: %w", err)
	}

	maxID := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		matches := taskFilePattern.FindStringSubmatch(entry.Name())
		if len(matches) != 2 {
			continue
		}

		id, err := strconv.Atoi(matches[1])
		if err != nil {
			return 0, fmt.Errorf("parse task id from %s: %w", entry.Name(), err)
		}
		if id > maxID {
			maxID = id
		}
	}

	return maxID + 1, nil
}

func (r *FileRepository) Save(task Task) error {
	if err := task.Validate(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	data, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal task: %w", err)
	}

	path := r.taskPath(task.ID)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0644); err != nil {
		return fmt.Errorf("write temp task file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename temp task file: %w", err)
	}

	return nil
}

func (r *FileRepository) Get(id int) (Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	data, err := os.ReadFile(r.taskPath(id))
	if err != nil {
		return Task{}, fmt.Errorf("read task %d: %w", id, err)
	}

	var task Task
	if err := json.Unmarshal(data, &task); err != nil {
		return Task{}, fmt.Errorf("unmarshal task %d: %w", id, err)
	}

	return task, nil
}

func (r *FileRepository) List() ([]Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entries, err := os.ReadDir(r.dir)
	if err != nil {
		return nil, fmt.Errorf("read task dir: %w", err)
	}

	taskList := make([]Task, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		matches := taskFilePattern.FindStringSubmatch(entry.Name())
		if len(matches) != 2 {
			continue
		}

		path := filepath.Join(r.dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read task file %s: %w", entry.Name(), err)
		}

		var task Task
		if err := json.Unmarshal(data, &task); err != nil {
			return nil, fmt.Errorf("unmarshal task file %s: %w", entry.Name(), err)
		}
		taskList = append(taskList, task)
	}

	sort.Slice(taskList, func(i, j int) bool {
		return taskList[i].ID < taskList[j].ID
	})

	return taskList, nil
}

func (r *FileRepository) taskPath(id int) string {
	return filepath.Join(r.dir, fmt.Sprintf("task_%d.json", id))
}
