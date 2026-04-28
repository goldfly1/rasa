package pool

import (
	"testing"
	"time"
)

func TestNewAgentRegistry(t *testing.T) {
	r := NewAgentRegistry()
	if r.Count() != 0 {
		t.Errorf("empty registry count: want 0, got %d", r.Count())
	}
}

func TestRegistryUpsertNew(t *testing.T) {
	r := NewAgentRegistry()
	info := r.Upsert("agent-1", "coder-v2-dev", "IDLE")

	if info.AgentID != "agent-1" {
		t.Errorf("agent ID: want agent-1, got %s", info.AgentID)
	}
	if info.SoulID != "coder-v2-dev" {
		t.Errorf("soul ID: want coder-v2-dev, got %s", info.SoulID)
	}
	if info.State != "IDLE" {
		t.Errorf("state: want IDLE, got %s", info.State)
	}
	if r.Count() != 1 {
		t.Errorf("count: want 1, got %d", r.Count())
	}
}

func TestRegistryUpsertUpdate(t *testing.T) {
	r := NewAgentRegistry()
	r.Upsert("agent-1", "coder-v2-dev", "IDLE")
	info := r.Upsert("agent-1", "coder-v2-dev", "ACTIVE")

	if info.State != "ACTIVE" {
		t.Errorf("state after update: want ACTIVE, got %s", info.State)
	}
	if r.Count() != 1 {
		t.Errorf("count after update: want 1, got %d", r.Count())
	}
}

func TestRegistryUpsertSoulChange(t *testing.T) {
	r := NewAgentRegistry()
	r.Upsert("agent-1", "coder-v2-dev", "IDLE")
	r.Upsert("agent-1", "reviewer-v1", "IDLE")

	// old soul index should be empty
	coders := r.FindBySoul("coder-v2-dev")
	if len(coders) != 0 {
		t.Errorf("old soul: want 0 agents, got %d", len(coders))
	}
	// new soul index should have it
	reviewers := r.FindBySoul("reviewer-v1")
	if len(reviewers) != 1 {
		t.Errorf("new soul: want 1 agent, got %d", len(reviewers))
	}
}

func TestRegistryFindBySoul(t *testing.T) {
	r := NewAgentRegistry()
	r.Upsert("agent-1", "coder-v2-dev", "IDLE")
	r.Upsert("agent-2", "coder-v2-dev", "IDLE")
	r.Upsert("agent-3", "reviewer-v1", "IDLE")

	coders := r.FindBySoul("coder-v2-dev")
	if len(coders) != 2 {
		t.Errorf("coders: want 2, got %d", len(coders))
	}

	missing := r.FindBySoul("nonexistent")
	if len(missing) != 0 {
		t.Errorf("missing: want 0, got %d", len(missing))
	}
}

func TestRegistryRemoveDead(t *testing.T) {
	r := NewAgentRegistry()
	r.Upsert("agent-1", "coder-v2-dev", "IDLE")
	r.Upsert("agent-2", "coder-v2-dev", "IDLE")

	// agent-2 was registered now... simulate old heartbeat
	info := r.Get("agent-2")
	info.LastHeartbeat = time.Now().Add(-30 * time.Second)

	// deadline 15s ago -> agent-2 should be dead, agent-1 alive
	deadline := time.Now().Add(-15 * time.Second)
	dead := r.RemoveDead(deadline)

	if len(dead) != 1 {
		t.Errorf("dead count: want 1, got %d", len(dead))
	}
	if dead[0] != "agent-2" {
		t.Errorf("dead agent: want agent-2, got %s", dead[0])
	}
	if r.Count() != 1 {
		t.Errorf("remaining: want 1, got %d", r.Count())
	}
}

func TestRegistryGet(t *testing.T) {
	r := NewAgentRegistry()
	r.Upsert("agent-1", "coder-v2-dev", "ACTIVE")

	info := r.Get("agent-1")
	if info == nil {
		t.Fatal("Get returned nil for known agent")
	}
	if info.State != "ACTIVE" {
		t.Errorf("state: want ACTIVE, got %s", info.State)
	}

	if r.Get("nonexistent") != nil {
		t.Error("Get should return nil for unknown agent")
	}
}

func TestRegistryList(t *testing.T) {
	r := NewAgentRegistry()
	r.Upsert("agent-1", "coder-v2-dev", "IDLE")
	r.Upsert("agent-2", "reviewer-v1", "IDLE")

	list := r.List()
	if len(list) != 2 {
		t.Errorf("list len: want 2, got %d", len(list))
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	// Test default values are applied
	cfg := &PoolConfig{}
	if cfg.Pool.HeartbeatIntervalSeconds = 5; cfg.Pool.HeartbeatIntervalSeconds != 5 {
		t.Error("default heartbeat interval not applied")
	}
	if cfg.Pool.HeartbeatTimeoutFactor = 3; cfg.Pool.HeartbeatTimeoutFactor != 3 {
		t.Error("default timeout factor not applied")
	}

	cfg.Pool.HeartbeatIntervalSeconds = 5
	cfg.Pool.HeartbeatTimeoutFactor = 3
	timeout := cfg.DeadAgentTimeout()
	if timeout != 15*time.Second {
		t.Errorf("dead agent timeout: want 15s, got %v", timeout)
	}
}

func TestMaxConcurrent(t *testing.T) {
	cfg := &PoolConfig{}
	cfg.Souls = []struct {
		ID       string `yaml:"id"`
		Replicas int    `yaml:"replicas"`
	}{
		{ID: "coder-v2-dev", Replicas: 2},
		{ID: "reviewer-v1", Replicas: 1},
	}

	if n := cfg.MaxConcurrent(); n != 3 {
		t.Errorf("max concurrent: want 3, got %d", n)
	}
}
