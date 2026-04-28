package policy

import (
	"path"
	"regexp"
	"strings"
)

// buildCandidates generates all candidate strings for matching a tool_name + args
// against a tool:arg_pattern rule value.
//
//	e.g. tool_name="shell_exec", args=["sudo", "rm"] produces:
//	["shell_exec", "shell_exec:sudo", "shell_exec:rm", "shell_exec:sudo rm"]
func buildCandidates(toolName string, args []string) []string {
	candidates := []string{toolName}
	for _, arg := range args {
		candidates = append(candidates, toolName+":"+arg)
	}
	full := strings.Join(args, " ")
	if full != "" {
		candidates = append(candidates, toolName+":"+full)
	}
	return candidates
}

// applyOp checks a single actual value against an expected pattern using the given operator.
func applyOp(op, actual, expected string) bool {
	switch op {
	case "eq":
		return actual == expected
	case "glob":
		matched, _ := path.Match(expected, actual)
		return matched
	case "regex":
		matched, _ := regexp.MatchString(expected, actual)
		return matched
	case "prefix":
		return strings.HasPrefix(actual, expected)
	case "contains":
		return strings.Contains(actual, expected)
	default:
		return false
	}
}

// matchRule checks whether a PolicyRule matches a ToolCallRequest.
func matchRule(rule PolicyRule, req ToolCallRequest) bool {
	if rule.MatchField == "task_id" {
		return rule.MatchOp == "eq" && rule.MatchValue == req.EnvelopeMeta.TaskID
	}
	if rule.MatchField != "tool" {
		return false
	}
	candidates := buildCandidates(req.ToolName, req.Args)
	for _, cand := range candidates {
		if applyOp(rule.MatchOp, cand, rule.MatchValue) {
			return true
		}
	}
	return false
}
