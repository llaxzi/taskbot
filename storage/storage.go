// Package storage - in memory хранилище задач
package storage

import (
	"errors"
	"sync"
	"sync/atomic"
)

// Ошибка передается как текст пользователю в Telegram с заглавной буквы
var (
	errNotFound = errors.New("Задача не найдена") //nolint:staticcheck
	errNotYours = errors.New("Задача не на вас")  //nolint:staticcheck
)

type Task struct {
	id               int64
	name             string
	ownerID          int64
	ownerUsername    string
	assigneeID       int64
	assigneeUsername string
	assigned         bool
}

func NewTask(id int64, name string, ownerID int64, ownerUsername string) *Task {
	return &Task{id: id, name: name, ownerID: ownerID, ownerUsername: ownerUsername}
}

// Инкапсулируем поля для функций build

func (t *Task) ID() int64 {
	return t.id
}

func (t *Task) Info() TaskData {
	return TaskData{
		ID:               t.id,
		Name:             t.name,
		OwnerID:          t.ownerID,
		OwnerUsername:    t.ownerUsername,
		AssigneeID:       t.assigneeID,
		AssigneeUsername: t.assigneeUsername,
		Assigned:         t.assigned,
	}
}

// TaskData - Возвращаемая модель для команд /tasks, /my, /owner, /assign, /unassign, /resolve
type TaskData struct {
	ID               int64
	Name             string
	OwnerID          int64
	OwnerUsername    string
	AssigneeID       int64
	AssigneeUsername string
	Assigned         bool

	Err error

	// Опционально: для Assign
	OldAssigneeID       *int64
	OldAssigneeUsername *string
}

type Storage struct {
	nextID  atomic.Int64
	tasks   map[int64]*Task
	tasksMu sync.RWMutex
}

func NewStorage() *Storage {
	s := &Storage{tasks: make(map[int64]*Task)}
	s.nextID.Store(0)
	return s
}

func (s *Storage) NewTask(name string, ownerID int64, ownerUsername string) int64 {
	id := s.nextID.Add(1)

	s.tasksMu.Lock()
	defer s.tasksMu.Unlock()

	s.tasks[id] = NewTask(id, name, ownerID, ownerUsername)
	return id
}

func (s *Storage) AssignTask(taskID, assigneeID int64, assigneeUsername string) TaskData {
	s.tasksMu.Lock()
	defer s.tasksMu.Unlock()

	task, ok := s.tasks[taskID]
	if !ok {
		return TaskData{Err: errNotFound}
	}

	oldAssigneeID := task.assigneeID
	oldAssigneeUsername := task.assigneeUsername

	task.assigneeID = assigneeID
	task.assigneeUsername = assigneeUsername
	task.assigned = true

	return TaskData{
		Name:                task.name,
		OwnerID:             task.ownerID,
		OwnerUsername:       task.ownerUsername,
		OldAssigneeID:       &oldAssigneeID,
		OldAssigneeUsername: &oldAssigneeUsername,
	}
}

func (s *Storage) UnassignTask(taskID, assigneeID int64) TaskData {
	s.tasksMu.Lock()
	defer s.tasksMu.Unlock()

	task, ok := s.tasks[taskID]
	if !ok {
		return TaskData{Err: errNotFound}
	}

	if task.assigneeID != assigneeID {
		return TaskData{Err: errNotYours}
	}

	task.assigned = false
	task.assigneeID = 0
	task.assigneeUsername = ""

	return TaskData{
		Name:          task.name,
		OwnerID:       task.ownerID,
		OwnerUsername: task.ownerUsername,
	}
}

func (s *Storage) ResolveTask(taskID, assigneeID int64) TaskData {
	s.tasksMu.Lock()
	defer s.tasksMu.Unlock()

	task, ok := s.tasks[taskID]
	if !ok {
		return TaskData{Err: errNotFound}
	}

	if task.assigneeID != assigneeID {
		return TaskData{Err: errNotYours}
	}

	delete(s.tasks, taskID)

	return TaskData{
		Name:          task.name,
		OwnerID:       task.ownerID,
		OwnerUsername: task.ownerUsername,
	}
}

func (s *Storage) AllTasks() []*Task {
	s.tasksMu.RLock()
	defer s.tasksMu.RUnlock()

	result := make([]*Task, 0)
	for _, task := range s.tasks {
		result = append(result, task)
	}

	return result
}

func (s *Storage) OwnedTasks(ownerID int64) []*Task {
	s.tasksMu.RLock()
	defer s.tasksMu.RUnlock()

	var result []*Task
	for _, task := range s.tasks {
		if task.ownerID == ownerID {
			result = append(result, task)
		}
	}

	return result
}

func (s *Storage) AssignedTasks(assigneeID int64) []*Task {
	s.tasksMu.RLock()
	defer s.tasksMu.RUnlock()

	var result []*Task
	for _, task := range s.tasks {
		if task.assigneeID == assigneeID {
			result = append(result, task)
		}
	}

	return result
}
