package dispensing

import (
	"strings"
	"testing"
)

func TestStateMachineValidTransitions(t *testing.T) {
	tests := []struct {
		from   State
		to     State
		expect string // empty = valid transition
	}{
		// RECEIVED → READY, CANCELLED, EXPIRED
		{StateReceived, StateReady, ""},
		{StateReceived, StateCancelled, ""},
		{StateReceived, StateExpired, ""},
		{StateReceived, StateDispensing, "cannot transition from"},

		// READY → DISPENSING, CANCELLED, EXPIRED
		{StateReady, StateDispensing, ""},
		{StateReady, StateCancelled, ""},
		{StateReady, StateExpired, ""},
		{StateReady, StateReceived, "cannot transition from"},
		{StateReady, StateDispensed, "cannot transition from"},

		// DISPENSING → DISPENSED, FAILED
		{StateDispensing, StateDispensed, ""},
		{StateDispensing, StateFailed, ""},
		{StateDispensing, StateReady, "cannot transition from"},
		{StateDispensing, StateCancelled, "cannot transition from"},

		// Terminal states (no outgoing edges)
		{StateDispensed, StateReady, "terminal"},
		{StateFailed, StateReady, "terminal"},
		{StateCancelled, StateReady, "terminal"},
		{StateExpired, StateReady, "terminal"},
	}

	for _, tt := range tests {
		t.Run(string(tt.from)+"->"+string(tt.to), func(t *testing.T) {
			err := tt.from.CanTransitionTo(tt.to)
			if tt.expect == "" {
				if err != nil {
					t.Errorf("expected valid transition, got: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.expect)
				} else if !strings.Contains(err.Error(), tt.expect) {
					t.Errorf("expected error containing %q, got: %v", tt.expect, err)
				}
			}
		})
	}
}

func TestStateMachineValidTransitionsMapCoverage(t *testing.T) {
	// Pin that every non-terminal state has entries.
	expected := map[State]int{
		StateReceived:   3,
		StateReady:      3,
		StateDispensing: 2,
	}
	for state, count := range expected {
		allowed, ok := ValidTransitions[state]
		if !ok {
			t.Errorf("state %q missing from ValidTransitions map", state)
			continue
		}
		if len(allowed) != count {
			t.Errorf("state %q has %d transitions, want %d", state, len(allowed), count)
		}
	}
}

func TestTerminalStatesNotInTransitionsMap(t *testing.T) {
	terminals := []State{StateDispensed, StateFailed, StateCancelled, StateExpired}
	for _, s := range terminals {
		if _, ok := ValidTransitions[s]; ok {
			t.Errorf("terminal state %q should not have entries in ValidTransitions", s)
		}
	}
}
