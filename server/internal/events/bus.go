package events

import (
	"log/slog"
	"sync"
)

type Event struct {
	Type        string
	WorkspaceID string
	ActorType   string
	ActorID     string
	Payload     any

	TaskID        string
	ChatSessionID string
}

type Handler func(Event)

type Bus struct {
	mu             sync.RWMutex
	listeners      map[string][]Handler
	globalHandlers []Handler
}

func New() *Bus {
	return &Bus{
		listeners: make(map[string][]Handler),
	}
}

func (b *Bus) Subscribe(eventType string, h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.listeners[eventType] = append(b.listeners[eventType], h)
}

func (b *Bus) SubscribeAll(h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.globalHandlers = append(b.globalHandlers, h)
}

func (b *Bus) Publish(e Event) {
	b.mu.RLock()
	handlers := b.listeners[e.Type]
	globals := b.globalHandlers
	b.mu.RUnlock()

	for _, h := range handlers {
		func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("panic in event listener", "event_type", e.Type, "recovered", r)
				}
			}()
			h(e)
		}()
	}

	for _, h := range globals {
		func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("panic in global event listener", "event_type", e.Type, "recovered", r)
				}
			}()
			h(e)
		}()
	}
}
