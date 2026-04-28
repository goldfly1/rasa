package policy

import (
	"context"
	"testing"

	"github.com/goldf/rasa/internal/bus"
)

// setupTestEngine creates an engine with a real DB and a pre-seeded test soul.
func setupTestEngine(t *testing.T) *PolicyEngine {
	t.Helper()
	db := openTestDB(t)
	if db == nil {
		t.Skip("policy DB not available")
	}
	// Clean any leftover test rules
	db.Exec(`DELETE FROM policy_rules WHERE description LIKE 'test-%'`)
	db.Exec(`DELETE FROM audit_log WHERE task_id LIKE 'test-%' OR task_id LIKE 'task-%' or task_id = ''`)
	db.Exec(`DELETE FROM human_reviews WHERE task_id LIKE 'test-%'`)

	e := &PolicyEngine{
		db:         db,
		rules:      make([]PolicyRule, 0),
		soulSheets: make(map[string]*SoulSheet),
		audit:      NewAuditLogger(db),
		reviewer:   NewHumanReviewer(db),
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM policy_rules WHERE description LIKE 'test-%'`)
		e.Close()
	})

	if err := e.ReloadRules(t.Context()); err != nil {
		t.Fatalf("reload rules: %v", err)
	}
	return e
}

func seedRule(t *testing.T, e *PolicyEngine, scope RuleScope, matchField, matchOp, matchValue string, action RuleAction, desc string) {
	t.Helper()
	_, err := e.db.ExecContext(t.Context(),
		`INSERT INTO policy_rules (id, scope, priority, match_field, match_op, match_value, action, description)
		 VALUES (gen_random_uuid(), $1, 100, $2, $3, $4, $5, $6)`,
		scope, matchField, matchOp, matchValue, action, "test-"+desc)
	if err != nil {
		t.Fatalf("seed rule: %v", err)
	}
	if err := e.ReloadRules(t.Context()); err != nil {
		t.Fatalf("reload after seed: %v", err)
	}
}

func TestEvaluateOrgDeny(t *testing.T) {
	e := setupTestEngine(t)
	seedRule(t, e, ScopeOrg, "tool", "glob", "file_write:/etc/*", ActionDeny, "block-etc-writes")

	req := ToolCallRequest{
		ToolName:     "file_write",
		Args:         []string{"/etc/hosts"},
		EnvelopeMeta: bus.Metadata{SoulID: "s1", TaskID: "task-1"},
	}
	d := e.Evaluate(t.Context(), req)
	if d.Action != "deny" {
		t.Errorf("expected deny for /etc write, got %s: %s", d.Action, d.Reason)
	}
}

func TestEvaluateOrgAllow(t *testing.T) {
	e := setupTestEngine(t)
	seedRule(t, e, ScopeOrg, "tool", "glob", "file_write:/etc/*", ActionDeny, "block-etc")

	// Soul not loaded → should fail at soul layer. Override by registering.
	e.mu.Lock()
	e.soulSheets["s1"] = &SoulSheet{
		SoulID: "s1",
		Behavior: struct {
			ToolPolicy ToolPolicy `yaml:"tool_policy"`
		}{ToolPolicy: ToolPolicy{AllowedTools: []string{"file_write"}}},
	}
	e.mu.Unlock()

	req := ToolCallRequest{
		ToolName:     "file_write",
		Args:         []string{"/tmp/ok"},
		EnvelopeMeta: bus.Metadata{SoulID: "s1", TaskID: "task-2"},
	}
	d := e.Evaluate(t.Context(), req)
	if d.Action != "allow" {
		t.Errorf("expected allow for /tmp write, got %s: %s", d.Action, d.Reason)
	}
}

func TestEvaluateSoulDeniedTools(t *testing.T) {
	e := setupTestEngine(t)
	e.mu.Lock()
	e.soulSheets["s2"] = &SoulSheet{
		SoulID: "s2",
		Behavior: struct {
			ToolPolicy ToolPolicy `yaml:"tool_policy"`
		}{ToolPolicy: ToolPolicy{
			DeniedTools:  []string{"shell_exec:rm*"},
			AllowedTools: []string{"shell_exec", "file_read"},
		}},
	}
	e.mu.Unlock()

	req := ToolCallRequest{
		ToolName:     "shell_exec",
		Args:         []string{"rm", "-rf", "/tmp/x"},
		EnvelopeMeta: bus.Metadata{SoulID: "s2", TaskID: "task-3"},
	}
	d := e.Evaluate(t.Context(), req)
	if d.Action != "deny" {
		t.Errorf("expected deny for rm, got %s: %s", d.Action, d.Reason)
	}
}

func TestEvaluateSoulAllowedToolsDeny(t *testing.T) {
	e := setupTestEngine(t)
	e.mu.Lock()
	e.soulSheets["s3"] = &SoulSheet{
		SoulID: "s3",
		Behavior: struct {
			ToolPolicy ToolPolicy `yaml:"tool_policy"`
		}{ToolPolicy: ToolPolicy{AllowedTools: []string{"file_read"}}},
	}
	e.mu.Unlock()

	req := ToolCallRequest{
		ToolName:     "git_diff",
		Args:         nil,
		EnvelopeMeta: bus.Metadata{SoulID: "s3", TaskID: "task-4"},
	}
	d := e.Evaluate(t.Context(), req)
	if d.Action != "deny" {
		t.Errorf("expected deny for git_diff not in allowed, got %s", d.Action)
	}
}

func TestEvaluateTaskOverride(t *testing.T) {
	e := setupTestEngine(t)
	seedRule(t, e, ScopeTask, "task_id", "eq", "task-banned", ActionDeny, "task-ban")

	e.mu.Lock()
	e.soulSheets["s1"] = &SoulSheet{
		SoulID: "s1",
		Behavior: struct {
			ToolPolicy ToolPolicy `yaml:"tool_policy"`
		}{ToolPolicy: ToolPolicy{AllowedTools: []string{"file_read"}}},
	}
	e.mu.Unlock()

	req := ToolCallRequest{
		ToolName:     "file_read",
		Args:         []string{"/tmp/ok"},
		EnvelopeMeta: bus.Metadata{SoulID: "s1", TaskID: "task-banned"},
	}
	d := e.Evaluate(t.Context(), req)
	if d.Action != "deny" {
		t.Errorf("expected deny for banned task, got %s: %s", d.Action, d.Reason)
	}
}

func TestEvaluateFullAllow(t *testing.T) {
	e := setupTestEngine(t)
	e.mu.Lock()
	e.soulSheets["s4"] = &SoulSheet{
		SoulID: "s4",
		Behavior: struct {
			ToolPolicy ToolPolicy `yaml:"tool_policy"`
		}{ToolPolicy: ToolPolicy{AllowedTools: []string{"file_read", "shell_exec"}}},
	}
	e.mu.Unlock()

	req := ToolCallRequest{
		ToolName:     "file_read",
		Args:         []string{"/tmp/doc.txt"},
		EnvelopeMeta: bus.Metadata{SoulID: "s4", TaskID: "task-5"},
	}
	d := e.Evaluate(t.Context(), req)
	if d.Action != "allow" {
		t.Errorf("expected full allow, got %s: %s", d.Action, d.Reason)
	}
}

func TestEvaluateSoulNotLoaded(t *testing.T) {
	e := setupTestEngine(t)

	req := ToolCallRequest{
		ToolName:     "file_read",
		Args:         nil,
		EnvelopeMeta: bus.Metadata{SoulID: "nonexistent", TaskID: "task-6"},
	}
	d := e.Evaluate(t.Context(), req)
	if d.Action != "deny" {
		t.Errorf("expected deny for unknown soul, got %s", d.Action)
	}
}

func TestReloadRules(t *testing.T) {
	e := setupTestEngine(t)

	// Initially empty rules (test cleanup clears them)
	e.mu.RLock()
	initalCount := len(e.rules)
	e.mu.RUnlock()

	seedRule(t, e, ScopeOrg, "tool", "eq", "banned_tool", ActionDeny, "reload-test")

	e.mu.RLock()
	afterCount := len(e.rules)
	e.mu.RUnlock()

	if afterCount <= initalCount {
		t.Errorf("expected more rules after seed, had %d → %d", initalCount, afterCount)
	}
}

func TestFailClosedOnDBError(t *testing.T) {
	e := setupTestEngine(t)

	// Set a loadErr to simulate DB failure
	e.mu.Lock()
	e.loadErr = context.DeadlineExceeded
	e.mu.Unlock()

	req := ToolCallRequest{ToolName: "anything", EnvelopeMeta: bus.Metadata{SoulID: "s1"}}
	d := e.Evaluate(t.Context(), req)
	if d.Action != "deny" {
		t.Errorf("expected fail-closed deny, got %s", d.Action)
	}
}

func TestEvaluateEnvelope(t *testing.T) {
	e := setupTestEngine(t)
	e.mu.Lock()
	e.soulSheets["s-env"] = &SoulSheet{
		SoulID: "s-env",
		Behavior: struct {
			ToolPolicy ToolPolicy `yaml:"tool_policy"`
		}{ToolPolicy: ToolPolicy{AllowedTools: []string{"file_read"}}},
	}
	e.mu.Unlock()

	env, _ := bus.NewEnvelope("test", "policy-engine",
		map[string]any{"tool_name": "file_read", "args": []string{"/x"}},
		bus.Metadata{SoulID: "s-env", TaskID: "task-env"}, "")

	d, err := e.EvaluateEnvelope(t.Context(), env)
	if err != nil {
		t.Fatalf("EvaluateEnvelope: %v", err)
	}
	if d.Action != "allow" {
		t.Errorf("expected allow, got %s: %s", d.Action, d.Reason)
	}
}
