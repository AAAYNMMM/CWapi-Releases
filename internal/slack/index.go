package slack

import (
	"strings"
	"sync"
)

const defaultIndexCapacity = 500

type Index struct {
	mu       sync.RWMutex
	capacity int
	order    []string
	items    map[string]Message
}

func NewIndex(capacity int) *Index {
	if capacity < 1 {
		capacity = defaultIndexCapacity
	}
	return &Index{
		capacity: capacity,
		items:    make(map[string]Message, capacity),
	}
}

// Add returns true only for a newly observed message identity.
func (i *Index) Add(message Message) bool {
	message.MessageID = strings.TrimSpace(message.MessageID)
	message.Subject = strings.TrimSpace(message.Subject)
	if message.MessageID == "" || message.Subject == "" {
		return false
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if _, exists := i.items[message.MessageID]; exists {
		i.items[message.MessageID] = message
		return false
	}
	i.items[message.MessageID] = message
	i.order = append(i.order, message.MessageID)
	for len(i.order) > i.capacity {
		oldest := i.order[0]
		i.order = i.order[1:]
		delete(i.items, oldest)
	}
	return true
}

func (i *Index) List(prefix string, limit int) []Message {
	prefix = strings.TrimSpace(prefix)
	if limit < 1 {
		limit = 1
	}
	if limit > i.capacity {
		limit = i.capacity
	}
	i.mu.RLock()
	defer i.mu.RUnlock()
	result := make([]Message, 0, limit)
	for position := len(i.order) - 1; position >= 0 && len(result) < limit; position-- {
		message := i.items[i.order[position]]
		if prefix != "" && !strings.HasPrefix(message.Subject, prefix) {
			continue
		}
		result = append(result, message)
	}
	return result
}

func (i *Index) Len() int {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return len(i.order)
}
