// Package monitor provides lightweight consumer health tracking and
// error rate monitoring for the MediSync backend.
package monitor

import (
	"sync"
	"time"
)

// ConsumerState tracks the health of a single NATS consumer.
type ConsumerState struct {
	Name         string
	LastSuccess  time.Time
	LastError    time.Time
	LastErrorMsg string
	MsgCount     int64
	ErrorCount   int64
	mu           sync.RWMutex
}

// RecordSuccess marks a successfully processed message.
func (c *ConsumerState) RecordSuccess() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.LastSuccess = time.Now()
	c.MsgCount++
}

// RecordError marks a failed message with the error text.
func (c *ConsumerState) RecordError(errMsg string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.LastError = time.Now()
	c.LastErrorMsg = errMsg
	c.ErrorCount++
	c.MsgCount++
}

// IsHealthy returns true if the consumer has processed messages recently.
func (c *ConsumerState) IsHealthy(maxIdle time.Duration) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return time.Since(c.LastSuccess) < maxIdle
}

// Snapshot returns a read-only copy of the state.
func (c *ConsumerState) Snapshot() ConsumerSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return ConsumerSnapshot{
		Name:         c.Name,
		LastSuccess:  c.LastSuccess,
		LastError:    c.LastError,
		LastErrorMsg: c.LastErrorMsg,
		MsgCount:     c.MsgCount,
		ErrorCount:   c.ErrorCount,
	}
}

// ConsumerSnapshot is an immutable view of ConsumerState.
type ConsumerSnapshot struct {
	Name         string    `json:"name"`
	LastSuccess  time.Time `json:"last_success"`
	LastError    time.Time `json:"last_error"`
	LastErrorMsg string    `json:"last_error_msg,omitempty"`
	MsgCount     int64     `json:"msg_count"`
	ErrorCount   int64     `json:"error_count"`
}

// Tracker manages multiple consumer health states.
type Tracker struct {
	consumers map[string]*ConsumerState
	mu        sync.RWMutex
}

// NewTracker creates a consumer health tracker.
func NewTracker() *Tracker {
	return &Tracker{consumers: make(map[string]*ConsumerState)}
}

// Consumer returns the state for a named consumer, creating it if needed.
func (t *Tracker) Consumer(name string) *ConsumerState {
	t.mu.Lock()
	defer t.mu.Unlock()
	if c, ok := t.consumers[name]; ok {
		return c
	}
	c := &ConsumerState{Name: name}
	t.consumers[name] = c
	return c
}

// Snapshot returns all consumer states.
func (t *Tracker) Snapshot() []ConsumerSnapshot {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]ConsumerSnapshot, 0, len(t.consumers))
	for _, c := range t.consumers {
		out = append(out, c.Snapshot())
	}
	return out
}

// Healthy returns true if all consumers are healthy (processed within maxIdle).
func (t *Tracker) Healthy(maxIdle time.Duration) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	for _, c := range t.consumers {
		if !c.IsHealthy(maxIdle) {
			return false
		}
	}
	return true
}

// ErrorRate returns the overall error rate across all consumers.
// Returns 0 if no messages have been processed.
func (t *Tracker) ErrorRate() float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	var total, errors int64
	for _, c := range t.consumers {
		total += c.MsgCount
		errors += c.ErrorCount
	}
	if total == 0 {
		return 0
	}
	return float64(errors) / float64(total)
}
