package monitor

import (
	"testing"
	"time"
)

func TestConsumerRecordSuccess(t *testing.T) {
	c := &ConsumerState{Name: "test"}
	c.RecordSuccess()

	if c.MsgCount != 1 {
		t.Errorf("MsgCount = %d, want 1", c.MsgCount)
	}
	if c.LastSuccess.IsZero() {
		t.Error("LastSuccess should not be zero")
	}
}

func TestConsumerRecordError(t *testing.T) {
	c := &ConsumerState{Name: "test"}
	c.RecordError("something went wrong")

	if c.MsgCount != 1 {
		t.Errorf("MsgCount = %d, want 1", c.MsgCount)
	}
	if c.ErrorCount != 1 {
		t.Errorf("ErrorCount = %d, want 1", c.ErrorCount)
	}
	if c.LastErrorMsg != "something went wrong" {
		t.Errorf("LastErrorMsg = %q, want %q", c.LastErrorMsg, "something went wrong")
	}
}

func TestConsumerIsHealthy(t *testing.T) {
	c := &ConsumerState{Name: "test"}
	if c.IsHealthy(time.Second) {
		t.Error("should not be healthy with no messages")
	}

	c.RecordSuccess()
	if !c.IsHealthy(time.Second) {
		t.Error("should be healthy after recent success")
	}
}

func TestConsumerIsHealthyExpired(t *testing.T) {
	c := &ConsumerState{Name: "test"}
	c.RecordSuccess()
	c.LastSuccess = time.Now().Add(-2 * time.Second)

	if c.IsHealthy(time.Second) {
		t.Error("should not be healthy after maxIdle exceeded")
	}
}

func TestConsumerSnapshot(t *testing.T) {
	c := &ConsumerState{Name: "my-consumer"}
	c.RecordSuccess()
	c.RecordError("timeout")

	s := c.Snapshot()
	if s.Name != "my-consumer" {
		t.Errorf("Name = %q", s.Name)
	}
	if s.MsgCount != 2 {
		t.Errorf("MsgCount = %d", s.MsgCount)
	}
	if s.ErrorCount != 1 {
		t.Errorf("ErrorCount = %d", s.ErrorCount)
	}
}

func TestTrackerConsumer(t *testing.T) {
	tr := NewTracker()
	c1 := tr.Consumer("c1")
	c2 := tr.Consumer("c1") // same name

	if c1 != c2 {
		t.Error("same name should return same ConsumerState")
	}
	_ = tr.Consumer("c2")
	if len(tr.Snapshot()) != 2 {
		t.Errorf("Snapshot count = %d, want 2", len(tr.Snapshot()))
	}
}

func TestTrackerHealthy(t *testing.T) {
	tr := NewTracker()
	tr.Consumer("a").RecordSuccess()

	if !tr.Healthy(time.Second) {
		t.Error("should be healthy")
	}
}

func TestTrackerErrorRate(t *testing.T) {
	tr := NewTracker()
	ca := tr.Consumer("a")
	cb := tr.Consumer("b")

	ca.RecordSuccess()
	cb.RecordError("fail")

	rate := tr.ErrorRate()
	if rate != 0.5 {
		t.Errorf("ErrorRate = %f, want 0.5", rate)
	}
}

func TestTrackerErrorRateZero(t *testing.T) {
	tr := NewTracker()
	if tr.ErrorRate() != 0 {
		t.Errorf("ErrorRate = %f, want 0 (no messages)", tr.ErrorRate())
	}
}

func TestConsumerConcurrency(t *testing.T) {
	c := &ConsumerState{Name: "concurrent"}
	done := make(chan bool)
	for i := 0; i < 100; i++ {
		go func() {
			c.RecordSuccess()
			done <- true
		}()
	}
	for i := 0; i < 100; i++ {
		<-done
	}
	if c.MsgCount != 100 {
		t.Errorf("MsgCount = %d, want 100 (concurrent safety)", c.MsgCount)
	}
}

func TestConsumerSnapshotThreadSafe(t *testing.T) {
	c := &ConsumerState{Name: "safe"}
	go func() {
		for i := 0; i < 50; i++ {
			c.RecordSuccess()
		}
	}()
	for i := 0; i < 50; i++ {
		_ = c.Snapshot()
	}
}
