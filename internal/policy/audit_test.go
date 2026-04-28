package policy

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// humanReviewerWithMockIO creates a HumanReviewer with fake stdin/stdout.
// Uses a nil db — only works for the IO logic path (RequestReview will crash on
// the DB insert). Use TestHumanReviewIO* tests for pure IO; integration tests
// below use a real DB.
func humanReviewerWithMockIO(t *testing.T, input string) (*HumanReviewer, *bytes.Buffer) {
	t.Helper()
	buf := &bytes.Buffer{}
	return &HumanReviewer{in: strings.NewReader(input), out: buf}, buf
}

func TestHumanReviewDenyIO(t *testing.T) {
	// Test the prompt + decision logic with a nil DB.
	// RequestReview WILL PANIC on DB insert — these are smoke tests for the IO layer.
	// Real flow tests are in engine_test.go with a real DB.
	t.Skip("needs real DB; tested via engine integration test")
}

func TestAuditLoggerLog(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	if db == nil {
		return
	}

	al := NewAuditLogger(db)
	id, err := al.Log(t.Context(), "task-1", "agent-1", "rule-1", "deny", json.RawMessage(`{"tool":"rm"}`))
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if id == "" {
		t.Error("expected non-empty audit ID")
	}

	var found string
	err = db.QueryRow(`SELECT id FROM audit_log WHERE id = $1`, id).Scan(&found)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if found != id {
		t.Errorf("expected %s, got %s", id, found)
	}
}

func TestAuditLoggerNullableFields(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	if db == nil {
		return
	}

	al := NewAuditLogger(db)
	id, err := al.Log(t.Context(), "", "", "", "allow", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	if id == "" {
		t.Error("expected non-empty audit ID")
	}
}
