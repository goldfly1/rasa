package policy

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// AuditLogger writes policy decisions to the audit_log table.
type AuditLogger struct {
	db *sql.DB
}

// NewAuditLogger creates an AuditLogger backed by the given DB.
func NewAuditLogger(db *sql.DB) *AuditLogger {
	return &AuditLogger{db: db}
}

// Log writes a single audit entry and returns its UUID.
func (a *AuditLogger) Log(ctx context.Context, taskID, agentID, ruleID string, decision string, contextJSON json.RawMessage) (string, error) {
	auditID := uuid.New().String()

	_, err := a.db.ExecContext(ctx,
		`INSERT INTO audit_log (id, task_id, agent_id, rule_id, decision, context)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		auditID, nullableStr(taskID), nullableStr(agentID), nullableStr(ruleID),
		decision, contextJSON)
	if err != nil {
		return "", fmt.Errorf("audit log: insert: %w", err)
	}
	return auditID, nil
}

func nullableStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
