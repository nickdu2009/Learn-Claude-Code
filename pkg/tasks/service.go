package tasks

import (
	"fmt"
	"slices"
	"strings"
	"sync"
)

type Service struct {
	repo Repository
	mu   sync.Mutex
}

type UpdateTaskInput struct {
	ID           int
	Status       *Status
	AddBlockedBy []int
	AddBlocks    []int
	Owner        *string
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) CreateTask(subject, description string) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id, err := s.repo.NextID()
	if err != nil {
		return Task{}, err
	}

	task := Task{
		ID:          id,
		Subject:     strings.TrimSpace(subject),
		Description: strings.TrimSpace(description),
		Status:      StatusPending,
		BlockedBy:   []int{},
		Blocks:      []int{},
		Owner:       "",
	}
	if err := s.repo.Save(task); err != nil {
		return Task{}, err
	}

	return task, nil
}

func (s *Service) GetTask(id int) (Task, error) {
	return s.repo.Get(id)
}

func (s *Service) ListTasks() ([]Task, error) {
	return s.repo.List()
}

func (s *Service) UpdateTask(input UpdateTaskInput) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, err := s.repo.Get(input.ID)
	if err != nil {
		return Task{}, err
	}

	all, err := s.repo.List()
	if err != nil {
		return Task{}, err
	}
	byID := indexTasks(all)

	if input.Status != nil {
		task.Status = *input.Status
	}
	if input.Owner != nil {
		task.Owner = strings.TrimSpace(*input.Owner)
	}

	for _, depID := range input.AddBlockedBy {
		if depID == task.ID {
			return Task{}, fmt.Errorf("task cannot be blocked by itself")
		}

		dep, ok := byID[depID]
		if !ok {
			return Task{}, fmt.Errorf("blockedBy task %d not found", depID)
		}

		task.BlockedBy = AppendUnique(task.BlockedBy, depID)
		dep.Blocks = AppendUnique(dep.Blocks, task.ID)
		byID[depID] = dep
	}

	for _, blockedID := range input.AddBlocks {
		if blockedID == task.ID {
			return Task{}, fmt.Errorf("task cannot block itself")
		}

		blockedTask, ok := byID[blockedID]
		if !ok {
			return Task{}, fmt.Errorf("blocks task %d not found", blockedID)
		}

		task.Blocks = AppendUnique(task.Blocks, blockedID)
		blockedTask.BlockedBy = AppendUnique(blockedTask.BlockedBy, task.ID)
		byID[blockedID] = blockedTask
	}

	byID[task.ID] = task

	if err := validateGraph(byID); err != nil {
		return Task{}, err
	}
	if err := detectCycle(mapValues(byID)); err != nil {
		return Task{}, err
	}

	if err := s.repo.Save(task); err != nil {
		return Task{}, err
	}
	for id, updated := range byID {
		if id == task.ID {
			continue
		}
		if err := s.repo.Save(updated); err != nil {
			return Task{}, err
		}
	}

	if input.Status != nil && *input.Status == StatusCompleted {
		if err := s.clearDependency(task.ID); err != nil {
			return Task{}, err
		}
		task, err = s.repo.Get(task.ID)
		if err != nil {
			return Task{}, err
		}
	}

	return task, nil
}

func (s *Service) clearDependency(completedID int) error {
	all, err := s.repo.List()
	if err != nil {
		return err
	}

	for _, task := range all {
		if !slices.Contains(task.BlockedBy, completedID) {
			continue
		}
		task.BlockedBy = RemoveID(task.BlockedBy, completedID)
		if err := s.repo.Save(task); err != nil {
			return err
		}
	}

	return nil
}

func indexTasks(taskList []Task) map[int]Task {
	byID := make(map[int]Task, len(taskList))
	for _, task := range taskList {
		byID[task.ID] = task
	}
	return byID
}

func mapValues(byID map[int]Task) []Task {
	taskList := make([]Task, 0, len(byID))
	for _, task := range byID {
		taskList = append(taskList, task)
	}
	return taskList
}

func validateGraph(byID map[int]Task) error {
	for _, task := range byID {
		if err := task.Validate(); err != nil {
			return fmt.Errorf("task %d invalid: %w", task.ID, err)
		}

		for _, depID := range task.BlockedBy {
			dep, ok := byID[depID]
			if !ok {
				return fmt.Errorf("task %d blockedBy task %d not found", task.ID, depID)
			}
			if !slices.Contains(dep.Blocks, task.ID) {
				return fmt.Errorf("task %d blockedBy %d but reverse blocks edge is missing", task.ID, depID)
			}
		}

		for _, blockedID := range task.Blocks {
			blockedTask, ok := byID[blockedID]
			if !ok {
				return fmt.Errorf("task %d blocks task %d not found", task.ID, blockedID)
			}
			if !slices.Contains(blockedTask.BlockedBy, task.ID) {
				return fmt.Errorf("task %d blocks %d but reverse blockedBy edge is missing", task.ID, blockedID)
			}
		}
	}

	return nil
}

func detectCycle(taskList []Task) error {
	graph := make(map[int][]int, len(taskList))
	for _, task := range taskList {
		graph[task.ID] = append([]int(nil), task.BlockedBy...)
	}

	const (
		unvisited = 0
		visiting  = 1
		visited   = 2
	)
	state := make(map[int]int, len(graph))

	var visit func(int) error
	visit = func(id int) error {
		switch state[id] {
		case visiting:
			return fmt.Errorf("cycle detected involving task %d", id)
		case visited:
			return nil
		}

		state[id] = visiting
		for _, depID := range graph[id] {
			if _, ok := graph[depID]; !ok {
				return fmt.Errorf("task %d depends on missing task %d", id, depID)
			}
			if err := visit(depID); err != nil {
				return err
			}
		}
		state[id] = visited
		return nil
	}

	for id := range graph {
		if state[id] == unvisited {
			if err := visit(id); err != nil {
				return err
			}
		}
	}

	return nil
}
