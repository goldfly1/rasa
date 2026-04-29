package recovery

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/goldf/rasa/internal/bus"
)

// RecoveryController monitors heartbeat liveness and re-queues lost tasks.
type RecoveryController struct {
	mu       sync.Mutex
	lastSeen map[string]time.Time // agent_id → last heartbeat

	orchDB    *sql.DB // rasa_orch for task queries
	recoveryDB *sql.DB // rasa_recovery for recovery_log
	ledger    *IdempotencyLedger
	pgSub     *bus.PGSub
	redisSub  *bus.RedisSub
	pgPub     *bus.PGPub

	timeout time.Duration
	ctx     context.Context
	cancel  context.CancelFunc
}

// NewRecoveryController creates a new recovery controller.
func NewRecoveryController(
	ctx context.Context,
	orchDB *sql.DB,
	recoveryDB *sql.DB,
	pgSub *bus.PGSub,
	redisSub *bus.RedisSub,
	pgPub *bus.PGPub,
	timeout time.Duration,
) *RecoveryController {
	ctx, cancel := context.WithCancel(ctx)
	return &RecoveryController{
		lastSeen:   make(map[string]time.Time),
		orchDB:     orchDB,
		recoveryDB: recoveryDB,
		ledger:     NewIdempotencyLedger(recoveryDB),
		pgSub:      pgSub,
		redisSub:   redisSub,
		pgPub:      pgPub,
		timeout:    timeout,
		ctx:        ctx,
		cancel:     cancel,
	}
}

// LastSeenCount returns the number of agents tracked.
func (c *RecoveryController) LastSeenCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.lastSeen)
}

// Start activates subscriptions and begins the reap loop.
func (c *RecoveryController) Start() error {
	if err := c.redisSub.Subscribe(c.ctx, "agents.heartbeat.*", c.HandleHeartbeat); err != nil {
		return err
	}
	if err := c.redisSub.Start(c.ctx); err != nil {
		return err
	}
	go c.reapLoop()
	log.Println("recovery-controller: subscriptions active")
	return nil
}

// HandleHeartbeat records agent liveness.
func (c *RecoveryController) HandleHeartbeat(env *bus.Envelope) {
	agentID := env.Metadata.AgentID
	c.mu.Lock()
	c.lastSeen[agentID] = time.Now()
	c.mu.Unlock()
}

// reapLoop periodically checks for dead agents.
func (c *RecoveryController) reapLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.reapDeadAgents()
		}
	}
}

func (c *RecoveryController) reapDeadAgents() {
	deadline := time.Now().Add(-c.timeout)

	c.mu.Lock()
	var dead []string
	for id, seen := range c.lastSeen {
		if seen.Before(deadline) {
			dead = append(dead, id)
		}
	}
	for _, id := range dead {
		delete(c.lastSeen, id)
	}
	c.mu.Unlock()

	for _, agentID := range dead {
		c.handleDeadAgent(agentID)
	}
}

func (c *RecoveryController) handleDeadAgent(agentID string) {
	log.Printf("recovery: agent %s declared dead", agentID)

	// Find running task assigned to this agent
	var taskID, soulID, status string
	err := c.orchDB.QueryRowContext(c.ctx,
		"SELECT id, soul_id, status FROM tasks WHERE assigned_agent_id=$1 AND status IN ('ASSIGNED','RUNNING') ORDER BY created_at DESC LIMIT 1",
		agentID,
	).Scan(&taskID, &soulID, &status)
	if err != nil {
		if err == sql.ErrNoRows {
			log.Printf("recovery: agent %s dead but no active task found", agentID)
			c.writeRecoveryLog(taskID, agentID, "", "noop", "no_active_task")
			return
		}
		log.Printf("recovery: task lookup error for agent %s: %v", agentID, err)
		return
	}

	// Check for checkpoint
	var ckptID string
	ckptErr := c.orchDB.QueryRowContext(c.ctx,
		"SELECT id FROM checkpoint_refs WHERE task_id=$1 ORDER BY created_at DESC LIMIT 1",
		taskID,
	).Scan(&ckptID)

	if ckptErr == sql.ErrNoRows {
		// No checkpoint — re-queue the task
		_, err := c.orchDB.ExecContext(c.ctx,
			"UPDATE tasks SET status='PENDING', assigned_agent_id=NULL, retry_count=retry_count+1, retry_after=NOW() + INTERVAL '30 seconds' WHERE id=$1",
			taskID,
		)
		if err != nil {
			log.Printf("recovery: re-queue update error for task %s: %v", taskID, err)
			return
		}

		// Re-publish to tasks_assigned
		env, err := bus.NewEnvelope("recovery-controller", "pool-controller",
			map[string]string{"task_id": taskID, "soul_id": soulID},
			bus.Metadata{SoulID: soulID, TaskID: taskID},
			"",
		)
		if err == nil {
			if err := c.pgPub.Publish(c.ctx, "tasks_assigned", env); err != nil {
				log.Printf("recovery: re-publish error for task %s: %v", taskID, err)
			}
		}

		// Write ledger entry
		action := map[string]string{"action": "re-queued", "reason": "agent_dead_no_checkpoint"}
		actionJSON, _ := json.Marshal(action)
		c.ledger.Insert(c.ctx, taskID, agentID, "recovery", string(actionJSON))

		// Write recovery log
		c.writeRecoveryLog(taskID, agentID, "", "re-queued", "agent_dead_no_checkpoint")

		log.Printf("recovery: task %s re-queued (soul=%s, agent=%s dead)", taskID[:8], soulID, agentID)
	} else if ckptErr == nil {
		// Has checkpoint — full replay deferred until agent-side checkpointing exists
		log.Printf("recovery: agent %s dead, task %s has checkpoint %s — replay deferred", agentID, taskID[:8], ckptID[:8])

		action := map[string]string{"action": "checkpoint_found", "reason": "replay_deferred"}
		actionJSON, _ := json.Marshal(action)
		c.ledger.Insert(c.ctx, taskID, agentID, "recovery", string(actionJSON))

		// Write recovery log
		c.writeRecoveryLog(taskID, agentID, ckptID, "checkpoint_found", "replay_deferred")
	} else {
		log.Printf("recovery: checkpoint lookup error for task %s: %v", taskID, ckptErr)
	}
}

// writeRecoveryLog inserts a structured entry into the recovery_log table.
func (c *RecoveryController) writeRecoveryLog(taskID, agentID, checkpointID, action, reason string) {
	meta := map[string]string{"reason": reason}
	metaJSON, _ := json.Marshal(meta)

	var ckptID interface{}
	if checkpointID != "" {
		ckptID = checkpointID
	}

	_, err := c.recoveryDB.ExecContext(c.ctx,
		`INSERT INTO recovery_log (task_id, agent_id, checkpoint_id, action, metadata, created_at)
		 VALUES ($1, $2, $3, $4, $5, NOW())`,
		taskID, agentID, ckptID, action, string(metaJSON),
	)
	if err != nil {
		log.Printf("recovery: recovery_log insert error: %v", err)
	}
}

// Shutdown gracefully stops the controller.
func (c *RecoveryController) Shutdown() {
	c.cancel()
	log.Println("recovery-controller: shut down")
}
