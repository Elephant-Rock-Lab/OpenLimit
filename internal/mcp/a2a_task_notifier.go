package mcp

import "sync"

// TaskNotifier provides pub/sub for SSE task updates.
type TaskNotifier struct {
	mu       sync.Mutex
	watchers map[string][]chan *A2ATask
	closed   bool
}

// NewTaskNotifier creates a new task notifier.
func NewTaskNotifier() *TaskNotifier {
	return &TaskNotifier{
		watchers: make(map[string][]chan *A2ATask),
	}
}

// Subscribe registers a channel for updates about a specific task.
func (n *TaskNotifier) Subscribe(taskID string) chan *A2ATask {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.closed {
		ch := make(chan *A2ATask)
		close(ch)
		return ch
	}
	ch := make(chan *A2ATask, 8)
	n.watchers[taskID] = append(n.watchers[taskID], ch)
	return ch
}

// Notify sends a task update to all subscribers.
func (n *TaskNotifier) Notify(task *A2ATask) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.closed {
		return
	}
	for _, ch := range n.watchers[task.ID] {
		select {
		case ch <- task:
		default:
			// Drop oldest: drain one, then send
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- task:
			default:
			}
		}
	}
}

// Unsubscribe removes a channel from the notification list.
func (n *TaskNotifier) Unsubscribe(taskID string, ch chan *A2ATask) {
	n.mu.Lock()
	defer n.mu.Unlock()
	subs := n.watchers[taskID]
	for i, sub := range subs {
		if sub == ch {
			n.watchers[taskID] = append(subs[:i], subs[i+1:]...)
			close(ch)
			break
		}
	}
	if len(n.watchers[taskID]) == 0 {
		delete(n.watchers, taskID)
	}
}

// Close shuts down the notifier and closes all subscriber channels.
func (n *TaskNotifier) Close() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.closed = true
	for id, subs := range n.watchers {
		for _, ch := range subs {
			close(ch)
		}
		delete(n.watchers, id)
	}
}
