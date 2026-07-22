package ratelimit

import (
	"sync"
	"testing"
	"time"
)

func TestAllowWithinLimit(t *testing.T) {
	l := New(3, 10*time.Minute)
	for i := 0; i < 3; i++ {
		if !l.Allow("key1") {
			t.Fatalf("Allow returned false on call %d within limit", i+1)
		}
	}
}

func TestAllowExceedsLimit(t *testing.T) {
	l := New(3, 10*time.Minute)
	for i := 0; i < 3; i++ {
		l.Allow("key1")
	}
	if l.Allow("key1") {
		t.Fatal("expected Allow to return false after exceeding limit")
	}
}

func TestAllowDifferentKeysIndependent(t *testing.T) {
	l := New(2, 10*time.Minute)
	// Exhaust key1
	l.Allow("key1")
	l.Allow("key1")
	if l.Allow("key1") {
		t.Fatal("key1 should be exhausted")
	}
	// key2 should still be allowed
	if !l.Allow("key2") {
		t.Fatal("key2 should not be affected by key1")
	}
	if !l.Allow("key2") {
		t.Fatal("key2 should still be within its own limit")
	}
}

func TestAllowWindowReset(t *testing.T) {
	l := New(2, 50*time.Millisecond)
	// Exhaust
	l.Allow("k")
	l.Allow("k")
	if l.Allow("k") {
		t.Fatal("should be exhausted")
	}
	// Wait for window to expire
	time.Sleep(60 * time.Millisecond)
	if !l.Allow("k") {
		t.Fatal("should be allowed after window reset")
	}
}

func TestAllowZeroMax(t *testing.T) {
	l := New(0, time.Minute)
	// Zero max means no limiting
	for i := 0; i < 100; i++ {
		if !l.Allow("key") {
			t.Fatalf("Allow returned false with zero max on call %d", i)
		}
	}
}

func TestAllowNegativeMax(t *testing.T) {
	l := New(-1, time.Minute)
	if !l.Allow("key") {
		t.Fatal("Allow should return true with negative max")
	}
}

func TestAllowZeroWindow(t *testing.T) {
	l := New(5, 0)
	if !l.Allow("key") {
		t.Fatal("Allow should return true with zero window")
	}
}

func TestReset(t *testing.T) {
	l := New(2, time.Hour)
	l.Allow("k")
	l.Allow("k")
	if l.Allow("k") {
		t.Fatal("should be exhausted")
	}
	l.Reset()
	if !l.Allow("k") {
		t.Fatal("should be allowed after Reset")
	}
}

func TestConcurrent(t *testing.T) {
	l := New(1000, time.Minute)
	var wg sync.WaitGroup
	n := 100
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				// Different keys per goroutine to avoid limit exhaustion
				_ = l.Allow("key-" + string(rune('A'+id%26)))
			}
		}(i)
	}
	wg.Wait()
	// No race = pass. The only assertion is that it doesn't panic.
}

func TestConcurrentSameKey(t *testing.T) {
	l := New(50, time.Minute)
	var wg sync.WaitGroup
	n := 20
	allowed := 0
	var mu sync.Mutex
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				if l.Allow("shared") {
					mu.Lock()
					allowed++
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()
	if allowed > 50 {
		t.Errorf("allowed %d requests, max is 50", allowed)
	}
}
