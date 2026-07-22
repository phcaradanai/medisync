package vending

import "sync"

// kioskLocker serializes complete Sticker transactions per physical kiosk.
// Different cabinets can still dispense concurrently; requests for one code
// wait until its previous pickup has been confirmed and fully acknowledged.
type kioskLocker struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func newKioskLocker() *kioskLocker {
	return &kioskLocker{locks: make(map[string]*sync.Mutex)}
}

func (l *kioskLocker) lock(kioskCode string) func() {
	l.mu.Lock()
	lock := l.locks[kioskCode]
	if lock == nil {
		lock = &sync.Mutex{}
		l.locks[kioskCode] = lock
	}
	l.mu.Unlock()

	lock.Lock()
	return lock.Unlock
}
