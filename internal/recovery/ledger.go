package recovery

import (
	"context"
	"database/sql"
	"log"
	"time"
)

// IdempotencyLedger ensures recovery actions are not duplicated.
type IdempotencyLedger struct {
	db *sql.DB
}

// NewIdempotencyLedger creates a new ledger backed by the recovery database.
func NewIdempotencyLedger(db *sql.DB) *IdempotencyLedger {
	return &IdempotencyLedger{db: db}
}

// Insert records a recovery action in the ledger.
func (l *IdempotencyLedger) Insert(ctx context.Context, taskID, agentID, operation, resultJSON string) error {
	key := taskID + ":" + agentID + ":" + operation
	_, err := l.db.ExecContext(ctx,
		`INSERT INTO idempotency_ledger (key_hash, key_plain, operation, status, result, completed_at)
		 VALUES (md5($1), $1, $2, 'completed', $3, $4)
		 ON CONFLICT (key_hash) DO UPDATE SET status='completed', result=$3, completed_at=$4`,
		key, operation, resultJSON, time.Now(),
	)
	if err != nil {
		log.Printf("recovery: ledger insert error: %v", err)
		return err
	}
	return nil
}

// Exists returns true if an action has already been recorded.
func (l *IdempotencyLedger) Exists(ctx context.Context, taskID, agentID, operation string) bool {
	key := taskID + ":" + agentID + ":" + operation
	var status string
	err := l.db.QueryRowContext(ctx,
		"SELECT status FROM idempotency_ledger WHERE key_hash=md5($1) AND status='completed'",
		key,
	).Scan(&status)
	return err == nil
}
