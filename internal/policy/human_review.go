package policy

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/google/uuid"
)

// HumanReviewer handles operator approval via stdin prompt (pilot).
type HumanReviewer struct {
	db  *sql.DB
	in  io.Reader
	out io.Writer
}

// NewHumanReviewer creates a reviewer using the real stdin/stdout.
func NewHumanReviewer(db *sql.DB) *HumanReviewer {
	return &HumanReviewer{
		db:  db,
		in:  os.Stdin,
		out: os.Stdout,
	}
}

// RequestReview blocks on stdin prompting the operator, then returns the decision.
func (h *HumanReviewer) RequestReview(ctx context.Context, taskID, agentID, reason string, payload json.RawMessage) (Decision, error) {
	reviewID := uuid.New().String()

	_, err := h.db.ExecContext(ctx,
		`INSERT INTO human_reviews (id, task_id, agent_id, reason, payload, status)
		 VALUES ($1, $2, $3, $4, $5, 'pending')`,
		reviewID, taskID, agentID, reason, payload)
	if err != nil {
		return Decision{}, fmt.Errorf("human review: insert: %w", err)
	}

	fmt.Fprintf(h.out, "\n=== HUMAN REVIEW REQUIRED ===\n")
	fmt.Fprintf(h.out, "Task:    %s\n", taskID)
	fmt.Fprintf(h.out, "Agent:   %s\n", agentID)
	fmt.Fprintf(h.out, "Reason:  %s\n", reason)
	fmt.Fprintf(h.out, "Payload: %s\n", string(payload))
	fmt.Fprint(h.out, "Approve? (y/N/D=deny permanently): ")

	reader := bufio.NewReader(h.in)
	line, err := reader.ReadString('\n')
	if err != nil {
		h.db.ExecContext(ctx,
			`UPDATE human_reviews SET status='denied_by_error', resolved_at=NOW() WHERE id=$1`, reviewID)
		return Decision{Action: "deny", Reason: "stdin read error: " + err.Error()}, nil
	}

	line = strings.TrimSpace(strings.ToLower(line))

	switch {
	case line == "y":
		h.db.ExecContext(ctx,
			`UPDATE human_reviews SET status='approved', reviewer='cli', resolved_at=NOW() WHERE id=$1`, reviewID)
		return Decision{Action: "allow", Reason: "human approved via CLI"}, nil

	case line == "d":
		h.db.ExecContext(ctx,
			`UPDATE human_reviews SET status='denied_permanently', reviewer='cli', resolved_at=NOW() WHERE id=$1`, reviewID)
		h.db.ExecContext(ctx,
			`INSERT INTO policy_rules (id, scope, priority, match_field, match_op, match_value, action, description)
			 VALUES ($1, 'task', 1000, 'task_id', 'eq', $2, 'deny', $3)`,
			uuid.New().String(), taskID, "human-permanent-deny: "+reason)
		return Decision{Action: "deny", Reason: "human permanently denied via CLI"}, nil

	default:
		h.db.ExecContext(ctx,
			`UPDATE human_reviews SET status='denied', reviewer='cli', resolved_at=NOW() WHERE id=$1`, reviewID)
		return Decision{Action: "deny", Reason: "human denied via CLI"}, nil
	}
}
