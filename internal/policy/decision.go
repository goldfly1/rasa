package policy

import (
	"encoding/json"

	"github.com/goldf/rasa/internal/bus"
)

// ToolCallRequest is the input to PolicyEngine.Evaluate.
type ToolCallRequest struct {
	ToolName     string       `json:"tool_name"`
	Args         []string     `json:"args"`
	EnvelopeMeta bus.Metadata `json:"envelope_meta"`
}

// Decision is the output of Evaluate.
type Decision struct {
	Action string `json:"action"` // "allow", "deny", "escalate"
	RuleID string `json:"rule_id"`
	Reason string `json:"reason"`
	AuditID string `json:"audit_id"`
}

// RuleScope and RuleAction match the PG enum types.
type RuleScope string

const (
	ScopeOrg   RuleScope = "org"
	ScopeSoul  RuleScope = "soul"
	ScopeTask  RuleScope = "task"
	ScopeHuman RuleScope = "human"
)

type RuleAction string

const (
	ActionAllow     RuleAction = "allow"
	ActionDeny      RuleAction = "deny"
	ActionEscalate  RuleAction = "escalate"
	ActionRateLimit RuleAction = "rate_limit"
)

// RuleActionString returns the string form of a RuleAction for the Decision struct.
func RuleActionString(a RuleAction) string {
	return string(a)
}

// PolicyRule is a single rule from the policy_rules table.
type PolicyRule struct {
	ID           string          `json:"id"`
	Scope        RuleScope       `json:"scope"`
	Priority     int             `json:"priority"`
	MatchField   string          `json:"match_field"`
	MatchOp      string          `json:"match_op"`
	MatchValue   string          `json:"match_value"`
	Action       RuleAction      `json:"action"`
	ActionParams json.RawMessage `json:"action_params"`
	Description  string          `json:"description"`
}
