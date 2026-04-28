package policy

import (
	"fmt"
	"os"
	"path"
	"strings"

	"gopkg.in/yaml.v3"
)

// SoulSheet is the minimal subset of a soul YAML needed by the policy engine.
type SoulSheet struct {
	SoulID    string `yaml:"soul_id"`
	AgentRole string `yaml:"agent_role"`
	Behavior  struct {
		ToolPolicy ToolPolicy `yaml:"tool_policy"`
	} `yaml:"behavior"`
}

// ToolPolicy mirrors behavior.tool_policy from a soul YAML file.
type ToolPolicy struct {
	AutoInvoke          bool     `yaml:"auto_invoke"`
	AllowedTools        []string `yaml:"allowed_tools"`
	DeniedTools         []string `yaml:"denied_tools"`
	RequireHumanConfirm []string `yaml:"require_human_confirm"`
}

// LoadSoulSheet reads and parses a single soul YAML file.
func LoadSoulSheet(path string) (*SoulSheet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("soul sheet: read %s: %w", path, err)
	}
	var s SoulSheet
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("soul sheet: parse %s: %w", path, err)
	}
	return &s, nil
}

// toolPatternMatches checks whether a tool:arg_pattern (e.g. "shell_exec:sudo" or "file_write")
// matches the given tool name and args.
//
// Patterns:
//   - "file_write"          — tool-only, matches any arg
//   - "shell_exec:sudo"     — tool + exact arg match
//   - "file_write:/etc/*"   — tool + glob on arg
//   - "shell_exec:rm -rf"   — tool + glob on full arg string
func toolPatternMatches(pattern, toolName string, args []string) bool {
	idx := strings.Index(pattern, ":")
	if idx == -1 {
		return pattern == toolName
	}
	patternTool := pattern[:idx]
	patternArg := pattern[idx+1:]

	if patternTool != toolName {
		return false
	}

	fullArgs := strings.Join(args, " ")
	for _, arg := range args {
		if argMatches(patternArg, arg) {
			return true
		}
	}
	return argMatches(patternArg, fullArgs)
}

// argMatches checks a single arg or arg string against a pattern.
// Uses glob when wildcards are present, otherwise does substring matching.
func argMatches(pattern, actual string) bool {
	if hasGlobMeta(pattern) {
		matched, _ := path.Match(pattern, actual)
		return matched
	}
	return strings.Contains(actual, pattern)
}

// hasGlobMeta returns true if the string contains glob metacharacters.
func hasGlobMeta(s string) bool {
	return strings.ContainsAny(s, "*?[")
}

// checkSoulPolicy evaluates a soul sheet's tool_policy against a request.
// Returns deny if the tool is denied or not in the allowlist; allow otherwise.
func checkSoulPolicy(soul *SoulSheet, req ToolCallRequest) Decision {
	tp := soul.Behavior.ToolPolicy

	// Blacklist first (denied_tools overrides everything)
	for _, pattern := range tp.DeniedTools {
		if toolPatternMatches(pattern, req.ToolName, req.Args) {
			return Decision{
				Action: "deny",
				Reason: "soul sheet denied_tools matched pattern " + pattern,
			}
		}
	}

	// Allowlist second (if populated)
	if len(tp.AllowedTools) > 0 {
		for _, pattern := range tp.AllowedTools {
			if toolPatternMatches(pattern, req.ToolName, req.Args) {
				return Decision{Action: "allow"}
			}
		}
		return Decision{
			Action: "deny",
			Reason: "tool " + req.ToolName + " not in soul sheet allowed_tools",
		}
	}

	return Decision{Action: "allow"}
}

// needsHumanConfirm checks whether the request matches any require_human_confirm pattern.
func needsHumanConfirm(soul *SoulSheet, req ToolCallRequest) (bool, string) {
	for _, pattern := range soul.Behavior.ToolPolicy.RequireHumanConfirm {
		if toolPatternMatches(pattern, req.ToolName, req.Args) {
			return true, pattern
		}
	}
	return false, ""
}
