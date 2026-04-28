package policy

import (
	"strings"
	"testing"

	"github.com/goldf/rasa/internal/bus"
)

func TestBuildCandidates(t *testing.T) {
	cs := buildCandidates("shell_exec", []string{"sudo", "rm"})
	if len(cs) != 4 {
		t.Fatalf("expected 4 candidates, got %d: %v", len(cs), cs)
	}
	if cs[0] != "shell_exec" {
		t.Errorf("expected tool-only candidate, got %q", cs[0])
	}
	if cs[1] != "shell_exec:sudo" {
		t.Errorf("expected tool:arg0, got %q", cs[1])
	}
	if cs[2] != "shell_exec:rm" {
		t.Errorf("expected tool:arg1, got %q", cs[2])
	}
	if cs[3] != "shell_exec:sudo rm" {
		t.Errorf("expected tool:full_args, got %q", cs[3])
	}
}

func TestBuildCandidatesNoArgs(t *testing.T) {
	cs := buildCandidates("file_read", nil)
	if len(cs) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(cs))
	}
	if cs[0] != "file_read" {
		t.Errorf("expected 'file_read', got %q", cs[0])
	}
}

func TestApplyOpEq(t *testing.T) {
	if !applyOp("eq", "hello", "hello") {
		t.Error("expected eq match")
	}
	if applyOp("eq", "hello", "world") {
		t.Error("expected eq mismatch")
	}
}

func TestApplyOpGlob(t *testing.T) {
	if !applyOp("glob", "file_write:/etc/hosts", "file_write:/etc/*") {
		t.Error("expected glob match on /etc/*")
	}
	if applyOp("glob", "file_write:/usr/bin", "file_write:/etc/*") {
		t.Error("expected glob mismatch on /etc/* vs /usr/bin")
	}
	if !applyOp("glob", "shell_exec:sudo", "shell_exec:*") {
		t.Error("expected glob match on shell_exec:*")
	}
}

func TestApplyOpPrefix(t *testing.T) {
	if !applyOp("prefix", "file_write:/etc/hosts", "file_write:/etc") {
		t.Error("expected prefix match")
	}
	if applyOp("prefix", "shell_exec:ls", "file_write") {
		t.Error("expected prefix mismatch")
	}
}

func TestApplyOpContains(t *testing.T) {
	if !applyOp("contains", "shell_exec:rm -rf /tmp", "rm") {
		t.Error("expected contains match")
	}
	if applyOp("contains", "file_read:/tmp", "rm") {
		t.Error("expected contains mismatch")
	}
}

func TestApplyOpRegex(t *testing.T) {
	if !applyOp("regex", "shell_exec:sudo", "shell_exec:.*") {
		t.Error("expected regex match")
	}
	if applyOp("regex", "file_read", "shell_exec:.*") {
		t.Error("expected regex mismatch for wrong tool prefix")
	}
}

func TestApplyOpUnknownFailsClosed(t *testing.T) {
	if applyOp("bogus", "anything", "anything") {
		t.Error("unknown op should return false (fail closed)")
	}
}

func TestMatchRuleToolOnly(t *testing.T) {
	r := PolicyRule{MatchField: "tool", MatchOp: "eq", MatchValue: "file_write"}
	req := ToolCallRequest{ToolName: "file_write", Args: []string{"/tmp/ok"}}
	if !matchRule(r, req) {
		t.Error("expected tool-only eq to match")
	}
}

func TestMatchRuleToolArg(t *testing.T) {
	r := PolicyRule{MatchField: "tool", MatchOp: "eq", MatchValue: "shell_exec:sudo"}
	req := ToolCallRequest{ToolName: "shell_exec", Args: []string{"sudo"}}
	if !matchRule(r, req) {
		t.Error("expected tool:arg eq to match")
	}
}

func TestMatchRuleToolArgNoMatch(t *testing.T) {
	r := PolicyRule{MatchField: "tool", MatchOp: "eq", MatchValue: "shell_exec:sudo"}
	req := ToolCallRequest{ToolName: "shell_exec", Args: []string{"ls"}}
	if matchRule(r, req) {
		t.Error("expected tool:arg eq to NOT match wrong arg")
	}
}

func TestMatchRuleToolNameMismatch(t *testing.T) {
	r := PolicyRule{MatchField: "tool", MatchOp: "eq", MatchValue: "shell_exec:sudo"}
	req := ToolCallRequest{ToolName: "file_read", Args: []string{"sudo"}}
	if matchRule(r, req) {
		t.Error("expected mismatch when tool name differs")
	}
}

func TestMatchRuleGlobPathPattern(t *testing.T) {
	r := PolicyRule{MatchField: "tool", MatchOp: "glob", MatchValue: "file_write:/etc/*"}
	req := ToolCallRequest{ToolName: "file_write", Args: []string{"/etc/hosts"}}
	if !matchRule(r, req) {
		t.Error("expected glob pattern to match /etc/hosts")
	}
}

func TestMatchRuleGlobFullArgs(t *testing.T) {
	r := PolicyRule{MatchField: "tool", MatchOp: "glob", MatchValue: "shell_exec:*rm*"}
	req := ToolCallRequest{ToolName: "shell_exec", Args: []string{"rm", "-rf"}}
	if !matchRule(r, req) {
		t.Error("expected glob match on full args containing rm")
	}
}

func TestMatchRuleTaskID(t *testing.T) {
	r := PolicyRule{MatchField: "task_id", MatchOp: "eq", MatchValue: "task-123", Action: ActionDeny}
	req := ToolCallRequest{ToolName: "x", EnvelopeMeta: bus.Metadata{TaskID: "task-123"}}
	if !matchRule(r, req) {
		t.Error("expected task_id eq to match")
	}
}

func TestMatchRuleTaskIDMismatch(t *testing.T) {
	r := PolicyRule{MatchField: "task_id", MatchOp: "eq", MatchValue: "task-123", Action: ActionDeny}
	req := ToolCallRequest{ToolName: "x", EnvelopeMeta: bus.Metadata{TaskID: "task-456"}}
	if matchRule(r, req) {
		t.Error("expected task_id eq to NOT match")
	}
}

func TestMatchRuleUnknownField(t *testing.T) {
	r := PolicyRule{MatchField: "bogus", MatchOp: "eq", MatchValue: "anything"}
	req := ToolCallRequest{ToolName: "shell_exec", Args: []string{"ls"}}
	if matchRule(r, req) {
		t.Error("expected unknown match_field to return false")
	}
}

// --- benchmarks (useful when tuning rule evaluation) ---

func BenchmarkBuildCandidates(b *testing.B) {
	args := strings.Split("rm -rf /tmp/stale --no-preserve-root", " ")
	b.ResetTimer()
	for range b.N {
		buildCandidates("shell_exec", args)
	}
}

func BenchmarkApplyOpGlob(b *testing.B) {
	for range b.N {
		applyOp("glob", "file_write:/etc/hosts", "file_write:/etc/*")
	}
}
