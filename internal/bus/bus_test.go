package bus

import (
	"encoding/json"
	"testing"
)

func TestNewEnvelope(t *testing.T) {
	meta := Metadata{
		SoulID:    "coder-v2-dev",
		AgentRole: "CODER",
		TaskID:    "0195f...",
	}
	env, err := NewEnvelope("orchestrator", "pool_controller", map[string]string{"cmd": "build"}, meta, "")
	if err != nil {
		t.Fatalf("NewEnvelope: %v", err)
	}
	if env.MessageID == "" {
		t.Error("expected non-empty MessageID")
	}
	if env.CorrelationID == "" {
		t.Error("expected non-empty CorrelationID")
	}
	if env.SourceComponent != "orchestrator" {
		t.Errorf("expected 'orchestrator', got %q", env.SourceComponent)
	}
	if env.Metadata.SoulID != "coder-v2-dev" {
		t.Errorf("expected 'coder-v2-dev', got %q", env.Metadata.SoulID)
	}

	var payload map[string]string
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatalf("payload unmarshal: %v", err)
	}
	if payload["cmd"] != "build" {
		t.Errorf("expected 'build', got %q", payload["cmd"])
	}
}

func TestNewEnvelopePreservesCorrelationID(t *testing.T) {
	env, err := NewEnvelope("a", "b", nil, Metadata{}, "my-corr-id")
	if err != nil {
		t.Fatalf("NewEnvelope: %v", err)
	}
	if env.CorrelationID != "my-corr-id" {
		t.Errorf("expected 'my-corr-id', got %q", env.CorrelationID)
	}
}

func TestEnvelopeToJSON(t *testing.T) {
	meta := Metadata{SoulID: "s1", TimestampMs: 12345}
	env, err := NewEnvelope("src", "dst", map[string]int{"x": 1}, meta, "")
	if err != nil {
		t.Fatalf("NewEnvelope: %v", err)
	}
	raw, err := env.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON: %v", err)
	}
	var d map[string]any
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if d["source_component"] != "src" {
		t.Errorf("expected 'src', got %v", d["source_component"])
	}
	meta2, ok := d["metadata"].(map[string]any)
	if !ok {
		t.Fatal("metadata not a map")
	}
	if meta2["soul_id"] != "s1" {
		t.Errorf("expected 's1', got %v", meta2["soul_id"])
	}
}

func TestEnvelopeRoundTrip(t *testing.T) {
	meta := Metadata{
		SoulID:            "reviewer-v1",
		PromptVersionHash: "abc123",
		AgentRole:         "REVIEWER",
		TaskID:            "t1",
		AgentID:           "agent-1",
		TimestampMs:       999,
	}
	env1, err := NewEnvelope("a", "b", map[string]any{"f": 1.5}, meta, "")
	if err != nil {
		t.Fatalf("NewEnvelope: %v", err)
	}
	raw, err := env1.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON: %v", err)
	}
	env2, err := EnvelopeFromJSON([]byte(raw))
	if err != nil {
		t.Fatalf("EnvelopeFromJSON: %v", err)
	}
	if env2.MessageID != env1.MessageID {
		t.Error("MessageID mismatch")
	}
	if env2.CorrelationID != env1.CorrelationID {
		t.Error("CorrelationID mismatch")
	}
	if env2.Metadata.SoulID != "reviewer-v1" {
		t.Errorf("SoulID mismatch: got %q", env2.Metadata.SoulID)
	}
	if env2.Metadata.TimestampMs != 999 {
		t.Errorf("TimestampMs mismatch: got %d", env2.Metadata.TimestampMs)
	}
}

func TestChannelValidation(t *testing.T) {
	valid := []string{"tasks_assigned", "checkpoint_saved", "a", "A1_b2"}
	for _, ch := range valid {
		if !validChannel.MatchString(ch) {
			t.Errorf("expected %q to be valid", ch)
		}
	}
	invalid := []string{"1bad", "has-dash", "has.dot", ""}
	for _, ch := range invalid {
		if validChannel.MatchString(ch) {
			t.Errorf("expected %q to be invalid", ch)
		}
	}
}
