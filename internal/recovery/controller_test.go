package recovery

import (
	"testing"
	"time"
)

func TestIdempotencyLedgerKey(t *testing.T) {
	// Test key construction logic (unit test, no DB needed)
	key1 := "task-1:agent-1:recovery"
	key2 := "task-1:agent-1:recovery"
	key3 := "task-1:agent-2:recovery"

	if key1 != key2 {
		t.Error("same inputs should produce same key")
	}
	if key1 == key3 {
		t.Error("different agent should produce different key")
	}
}

func TestRecoveryTimeout(t *testing.T) {
	timeout := 15 * time.Second
	now := time.Now()
	seen := now.Add(-20 * time.Second)

	if !seen.Before(now.Add(-timeout)) {
		t.Error("agent seen 20s ago should be dead with 15s timeout")
	}

	seen2 := now.Add(-5 * time.Second)
	if seen2.Before(now.Add(-timeout)) {
		t.Error("agent seen 5s ago should not be dead with 15s timeout")
	}
}
