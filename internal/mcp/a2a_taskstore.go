package mcp

import (
	"errors"
	"sync"
	"time"
)

// ErrTaskNotFound is returned when a task does not exist in the store.
var ErrTaskNotFound = errors.New("task not found")

// ErrMaxTasksReached is returned when the store has reached its capacity.
var ErrMaxTasksReached = errors.New("maximum number of tasks reached")

// ErrDuplicateTask is returned when a task with the same ID already exists.
var ErrDuplicateTask = errors.New("task already exists")

// TaskListFilter defines filters for listing tasks.
type TaskListFilter struct {
	Status    string
	ContextID string
	Limit     int
	Offset    int
}

// TaskStore is the interface for A2A task persistence.
// Implementations: MemoryTaskStore (in-memory), PersistentTaskStore (Postgres).
type TaskStore interface {
	Create(task *A2ATask) error
	Get(id string) (*A2ATask, bool)
	Update(task *A2ATask) error
	List(filter TaskListFilter) ([]*A2ATask, int, error)
	Delete(id string) error
	RecoverStale() (int, error)
	Close()
}

// MemoryTaskStore stores A2A tasks in memory with TTL eviction.
type MemoryTaskStore struct {
	mu        sync.RWMutex
	tasks     map[string]*A2ATask
	maxTasks  int
	ttl       time.Duration
	stopCh    chan struct{}
	closeOnce sync.Once
}

// NewMemoryTaskStore creates a new in-memory task store.
func NewMemoryTaskStore(maxTasks int, ttl time.Duration) *MemoryTaskStore {
	if maxTasks <= 0 {
		maxTasks = 10000
	}
	if ttl <= 0 {
		ttl = 3600 * time.Second
	}
	ts := &MemoryTaskStore{
		tasks:    make(map[string]*A2ATask),
		maxTasks: maxTasks,
		ttl:      ttl,
		stopCh:   make(chan struct{}),
	}
	go ts.evictLoop()
	return ts
}

// Close stops the eviction goroutine.
func (ts *MemoryTaskStore) Close() {
	ts.closeOnce.Do(func() { close(ts.stopCh) })
}

// Create adds a new task. Returns ErrMaxTasksReached or ErrDuplicateTask on failure.
func (ts *MemoryTaskStore) Create(task *A2ATask) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if len(ts.tasks) >= ts.maxTasks {
		return ErrMaxTasksReached
	}
	if _, exists := ts.tasks[task.ID]; exists {
		return ErrDuplicateTask
	}
	ts.tasks[task.ID] = task
	return nil
}

// Get retrieves a task by ID.
func (ts *MemoryTaskStore) Get(id string) (*A2ATask, bool) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	task, ok := ts.tasks[id]
	return task, ok
}

// Update replaces a task in the store.
func (ts *MemoryTaskStore) Update(task *A2ATask) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	task.UpdatedAt = time.Now()
	ts.tasks[task.ID] = task
	return nil
}

// List returns filtered tasks with total count.
func (ts *MemoryTaskStore) List(filter TaskListFilter) ([]*A2ATask, int, error) {
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	if filter.Limit > 100 {
		filter.Limit = 100
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}

	ts.mu.RLock()
	defer ts.mu.RUnlock()

	// Collect matching tasks, sorted by updated_at desc (most recent first)
	var matched []*A2ATask
	for _, task := range ts.tasks {
		if filter.Status != "" && string(task.Status) != filter.Status {
			continue
		}
		if filter.ContextID != "" && task.ContextID != filter.ContextID {
			continue
		}
		matched = append(matched, task)
	}
	total := len(matched)

	// Apply offset and limit
	if filter.Offset >= total {
		return []*A2ATask{}, total, nil
	}
	end := filter.Offset + filter.Limit
	if end > total {
		end = total
	}

	return matched[filter.Offset:end], total, nil
}

// Delete removes a task by ID.
func (ts *MemoryTaskStore) Delete(id string) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if _, exists := ts.tasks[id]; !exists {
		return ErrTaskNotFound
	}
	delete(ts.tasks, id)
	return nil
}

// RecoverStale is a no-op for in-memory store (tasks are lost on restart).
func (ts *MemoryTaskStore) RecoverStale() (int, error) {
	return 0, nil
}

// Len returns the number of tasks in the store.
func (ts *MemoryTaskStore) Len() int {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return len(ts.tasks)
}

func (ts *MemoryTaskStore) evictLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ts.stopCh:
			return
		case <-ticker.C:
			ts.evictExpired()
		}
	}
}

func (ts *MemoryTaskStore) evictExpired() {
	now := time.Now()
	ts.mu.Lock()
	defer ts.mu.Unlock()
	for id, task := range ts.tasks {
		if now.Sub(task.UpdatedAt) > ts.ttl {
			delete(ts.tasks, id)
		}
	}
}
