package policy

import (
	"os"
	"path/filepath"
	"testing"
)

const testSoulYAML = `
soul_version: "1.0.0"
soul_id: "coder-v2-dev"
agent_role: CODER

behavior:
  tool_policy:
    auto_invoke: false
    allowed_tools:
      - "file_read"
      - "file_write"
      - "shell_exec"
    denied_tools:
      - "shell_exec:sudo"
      - "file_write:/etc/*"
    require_human_confirm:
      - "shell_exec:rm -rf"
      - "shell_exec:git push"
`

func writeTempSoul(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "test.yaml")
	if err := os.WriteFile(p, []byte(testSoulYAML), 0644); err != nil {
		t.Fatalf("write temp soul: %v", err)
	}
	return p
}

func TestLoadSoulSheet(t *testing.T) {
	p := writeTempSoul(t)
	s, err := LoadSoulSheet(p)
	if err != nil {
		t.Fatalf("LoadSoulSheet: %v", err)
	}
	if s.SoulID != "coder-v2-dev" {
		t.Errorf("expected 'coder-v2-dev', got %q", s.SoulID)
	}
	if s.AgentRole != "CODER" {
		t.Errorf("expected CODER, got %q", s.AgentRole)
	}
	tp := s.Behavior.ToolPolicy
	if tp.AutoInvoke {
		t.Error("expected auto_invoke=false")
	}
	if len(tp.AllowedTools) != 3 {
		t.Errorf("expected 3 allowed_tools, got %d", len(tp.AllowedTools))
	}
	if len(tp.DeniedTools) != 2 {
		t.Errorf("expected 2 denied_tools, got %d", len(tp.DeniedTools))
	}
	if len(tp.RequireHumanConfirm) != 2 {
		t.Errorf("expected 2 require_human_confirm, got %d", len(tp.RequireHumanConfirm))
	}
}

func TestLoadSoulSheetNotExist(t *testing.T) {
	_, err := LoadSoulSheet("/nonexistent/soul.yaml")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestToolPatternMatchesToolOnly(t *testing.T) {
	if !toolPatternMatches("file_write", "file_write", []string{"/any/path"}) {
		t.Error("expected tool-only pattern to match")
	}
	if toolPatternMatches("file_write", "file_read", nil) {
		t.Error("expected tool-only pattern mismatch for wrong tool")
	}
}

func TestToolPatternMatchesToolArgExact(t *testing.T) {
	if !toolPatternMatches("shell_exec:sudo", "shell_exec", []string{"sudo"}) {
		t.Error("expected tool:arg to match exact arg")
	}
	if toolPatternMatches("shell_exec:sudo", "shell_exec", []string{"ls"}) {
		t.Error("expected tool:arg to not match wrong arg")
	}
}

func TestToolPatternMatchesGlobArg(t *testing.T) {
	if !toolPatternMatches("file_write:/etc/*", "file_write", []string{"/etc/hosts"}) {
		t.Error("expected glob to match /etc/hosts")
	}
	if toolPatternMatches("file_write:/etc/*", "file_write", []string{"/usr/bin"}) {
		t.Error("expected glob to not match /usr/bin")
	}
}

func TestToolPatternMatchesFullArgs(t *testing.T) {
	if !toolPatternMatches("shell_exec:rm*", "shell_exec", []string{"rm", "-rf"}) {
		t.Error("expected glob on full args ('rm -rf') to match 'rm*'")
	}
}

func TestToolPatternMatchesToolNameMismatch(t *testing.T) {
	if toolPatternMatches("shell_exec:sudo", "file_write", []string{"sudo"}) {
		t.Error("expected mismatch when tool name differs")
	}
}

func TestCheckSoulPolicyDeniedTools(t *testing.T) {
	s, err := LoadSoulSheet(writeTempSoul(t))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	req := ToolCallRequest{ToolName: "shell_exec", Args: []string{"sudo", "reboot"}}
	d := checkSoulPolicy(s, req)
	if d.Action != "deny" {
		t.Errorf("expected deny for denied_tools match, got %s", d.Action)
	}

	// Sanity: not denied without matching args
	req2 := ToolCallRequest{ToolName: "shell_exec", Args: []string{"ls"}}
	d2 := checkSoulPolicy(s, req2)
	if d2.Action != "allow" {
		t.Errorf("expected allow for non-matching args, got %s", d2.Action)
	}
}

func TestCheckSoulPolicyAllowedTools(t *testing.T) {
	s, err := LoadSoulSheet(writeTempSoul(t))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// git_diff is not in allowed_tools
	req := ToolCallRequest{ToolName: "git_diff", Args: nil}
	d := checkSoulPolicy(s, req)
	if d.Action != "deny" {
		t.Errorf("expected deny for tool not in allowed_tools, got %s", d.Action)
	}

	// shell_exec IS in allowed_tools (and not denied since no sudo)
	req2 := ToolCallRequest{ToolName: "shell_exec", Args: []string{"ls"}}
	d2 := checkSoulPolicy(s, req2)
	if d2.Action != "allow" {
		t.Errorf("expected allow for tool in allowed_tools, got %s: %s", d2.Action, d2.Reason)
	}
}

func TestNeedsHumanConfirm(t *testing.T) {
	s, err := LoadSoulSheet(writeTempSoul(t))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	req := ToolCallRequest{ToolName: "shell_exec", Args: []string{"rm", "-rf", "/tmp/stale"}}
	yes, pat := needsHumanConfirm(s, req)
	if !yes {
		t.Error("expected trigger for rm -rf")
	}
	if pat == "" {
		t.Error("expected matching pattern back")
	}

	req2 := ToolCallRequest{ToolName: "shell_exec", Args: []string{"ls"}}
	yes2, _ := needsHumanConfirm(s, req2)
	if yes2 {
		t.Error("expected no trigger for ls")
	}
}
