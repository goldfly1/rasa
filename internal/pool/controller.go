package pool

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"math/rand"
	"time"

	"github.com/goldf/rasa/internal/bus"
)

// PoolController manages the agent pool: registry, heartbeat monitoring, task routing.
type PoolController struct {
	registry *AgentRegistry
	pgSub    *bus.PGSub
	redisSub *bus.RedisSub
	db       *sql.DB
	config   *PoolConfig
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewPoolController wires the pool controller to its dependencies.
func NewPoolController(
	ctx context.Context,
	cfg *PoolConfig,
	pgSub *bus.PGSub,
	redisSub *bus.RedisSub,
	db *sql.DB,
) *PoolController {
	ctx, cancel := context.WithCancel(ctx)
	return &PoolController{
		registry: NewAgentRegistry(),
		pgSub:    pgSub,
		redisSub: redisSub,
		db:       db,
		config:   cfg,
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Registry exposes the agent registry for health checks.
func (c *PoolController) Registry() *AgentRegistry {
	return c.registry
}

// Start activates subscriptions and begins the reap loop.
func (c *PoolController) Start() error {
	if err := c.pgSub.Subscribe(c.ctx, "tasks_assigned", c.HandleTaskAssigned); err != nil {
		return err
	}
	if err := c.redisSub.Subscribe(c.ctx, "agents.heartbeat.*", c.HandleHeartbeat); err != nil {
		return err
	}
	if err := c.redisSub.Start(c.ctx); err != nil {
		return err
	}
	go c.reapLoop()
	log.Println("pool-controller: subscriptions active")
	return nil
}

// HandleTaskAssigned receives a task_assigned envelope and routes to an agent.
func (c *PoolController) HandleTaskAssigned(env *bus.Envelope) {
	soulID := env.Metadata.SoulID
	taskID := env.Metadata.TaskID
	if taskID == "" {
		// extract from payload
		var p struct {
			TaskID string `json:"task_id"`
		}
		if err := json.Unmarshal(env.Payload, &p); err == nil && p.TaskID != "" {
			taskID = p.TaskID
		}
	}

	log.Printf("pool-controller: task assigned (soul=%s task=%s)", soulID, taskID)

	agents := c.registry.FindBySoul(soulID)
	if len(agents) == 0 {
		log.Printf("pool-controller: no agent for soul %s, keeping task %s PENDING", soulID, taskID)
		return
	}

	// Pick random agent (simple pilot routing)
	chosen := agents[rand.Intn(len(agents))]
	log.Printf("pool-controller: routing task %s → agent %s (soul=%s)", taskID, chosen, soulID)

	// Update task row to ASSIGNED with assigned_agent_id
	_, err := c.db.ExecContext(c.ctx,
		`UPDATE tasks SET status = 'ASSIGNED', assigned_agent_id = $1, started_at = NOW() WHERE id = $2`,
		chosen, taskID,
	)
	if err != nil {
		log.Printf("pool-controller: task assign update: %v", err)
	}
}

// HandleHeartbeat receives an agent heartbeat and updates the registry.
func (c *PoolController) HandleHeartbeat(env *bus.Envelope) {
	agentID := env.Metadata.AgentID
	soulID := env.Metadata.SoulID

	var payload struct {
		CurrentState string `json:"current_state"`
		SoulID       string `json:"soul_id"`
	}
	_ = json.Unmarshal(env.Payload, &payload)

	if payload.SoulID != "" {
		soulID = payload.SoulID
	}
	if payload.CurrentState == "" {
		payload.CurrentState = "IDLE"
	}

	c.registry.Upsert(agentID, soulID, payload.CurrentState)
}

// reapLoop periodically removes dead agents.
func (c *PoolController) reapLoop() {
	ticker := time.NewTicker(time.Duration(c.config.Pool.HeartbeatIntervalSeconds) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			deadline := time.Now().Add(-c.config.DeadAgentTimeout())
			dead := c.registry.RemoveDead(deadline)
			for _, id := range dead {
				log.Printf("pool-controller: agent %s declared dead (timeout)", id)
			}
		}
	}
}

// Shutdown gracefully stops the controller.
func (c *PoolController) Shutdown() {
	c.cancel()
	log.Println("pool-controller: shut down")
}
