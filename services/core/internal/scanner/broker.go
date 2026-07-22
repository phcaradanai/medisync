package scanner

import (
	"context"
	"sync"
)

// Broker fans out Core-validated reads to the browser session for the same
// kiosk code. A scanner read must never be broadcast to another cabinet.
type Broker struct {
	mu          sync.RWMutex
	subscribers map[string]map[chan Event]struct{}
}

func NewBroker() *Broker {
	return &Broker{subscribers: make(map[string]map[chan Event]struct{})}
}

func (b *Broker) Subscribe(ctx context.Context, kioskCode string) (<-chan Event, func()) {
	ch := make(chan Event, 16)
	b.mu.Lock()
	if b.subscribers[kioskCode] == nil {
		b.subscribers[kioskCode] = make(map[chan Event]struct{})
	}
	b.subscribers[kioskCode][ch] = struct{}{}
	b.mu.Unlock()

	var once sync.Once
	stop := func() {
		once.Do(func() {
			b.mu.Lock()
			if group := b.subscribers[kioskCode]; group != nil {
				delete(group, ch)
				if len(group) == 0 {
					delete(b.subscribers, kioskCode)
				}
			}
			close(ch)
			b.mu.Unlock()
		})
	}
	go func() {
		<-ctx.Done()
		stop()
	}()
	return ch, stop
}

func (b *Broker) Publish(event Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.subscribers[event.KioskCode] {
		select {
		case ch <- event:
		default:
			// Keep the browser current if it is briefly rendering a slow frame.
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- event:
			default:
			}
		}
	}
}

func (b *Broker) SubscriberCount(kioskCode string) int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers[kioskCode])
}
