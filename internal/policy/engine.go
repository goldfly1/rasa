package policy

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/goldf/rasa/internal/bus"
)

// PolicyEngine evaluates tool call requests against the layered permission matrix.
type PolicyEngine struct {
	db         *sql.DB
	rules      []PolicyRule
	soulSheets map[string]*SoulSheet
	audit      *AuditLogger
	reviewer   *HumanReviewer
	mu         sync.RWMutex
	loadErr    error
	cancel     context.CancelFunc
}

// NewEngine creates a PolicyEngine, loads initial rules from DB, and preloads
// soul sheets from the given directory. It also starts the hot reload poller.
func NewEngine(ctx context.Context, dsn, soulDir string, pollInterval time.Duration) (*PolicyEngine, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("policy engine: open: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("policy engine: ping: %w", err)
	}

	e := &PolicyEngine{
		db:         db,
		rules:      make([]PolicyRule, 0),
		soulSheets: make(map[string]*SoulSheet),
		audit:      NewAuditLogger(db),
		reviewer:   NewHumanReviewer(db),
	}

	if err := e.ReloadRules(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("policy engine: initial rule load: %w", err)
	}

	if err := e.loadSoulSheets(soulDir); err != nil {
		log.Printf("[policy] WARNING: soul sheet loading errors: %v", err)
	}

	// Start PG poll reloader
	reloader := newHotReloader(e, pollInterval)
	ctx2, cancel := context.WithCancel(context.Background())
	e.cancel = cancel
	if err := reloader.start(ctx2); err != nil {
		db.Close()
		cancel()
		return nil, fmt.Errorf("policy engine: reloader: %w", err)
	}

	return e, nil
}

// Evaluate runs the full layered rule check.
func (e *PolicyEngine) Evaluate(ctx context.Context, req ToolCallRequest) Decision {
	// Fail-closed: DB errors lock all decisions
	e.mu.RLock()
	ld := e.loadErr
	e.mu.RUnlock()
	if ld != nil {
		d := Decision{Action: "deny", Reason: "policy engine error: " + ld.Error()}
		e.auditFireAndForget(req, d, "")
		return d
	}

	// 1. Org guardrails
	d := e.evaluateScope(ScopeOrg, req)
	if d.Action == "deny" {
		e.auditFireAndForget(req, d, d.RuleID)
		return d
	}

	// 2. Soul sheet tool_policy
	soul := e.getSoulSheet(req.EnvelopeMeta.SoulID)
	if soul == nil {
		d := Decision{Action: "deny", Reason: "soul sheet " + req.EnvelopeMeta.SoulID + " not loaded"}
		e.auditFireAndForget(req, d, "")
		return d
	}
	d = checkSoulPolicy(soul, req)
	if d.Action == "deny" {
		e.auditFireAndForget(req, d, "")
		return d
	}

	// 3. Task override
	d = e.evaluateScope(ScopeTask, req)
	if d.Action == "deny" {
		e.auditFireAndForget(req, d, d.RuleID)
		return d
	}

	// 4. Human confirm
	yes, pat := needsHumanConfirm(soul, req)
	if yes {
		reason := "tool " + req.ToolName + " matched require_human_confirm pattern " + pat
		payload, _ := json.Marshal(req)
		rd, err := e.reviewer.RequestReview(ctx,
			req.EnvelopeMeta.TaskID, req.EnvelopeMeta.AgentID, reason, payload)
		if err != nil {
			d := Decision{Action: "deny", Reason: "human review error: " + err.Error()}
			e.auditFireAndForget(req, d, "")
			return d
		}
		e.auditFireAndForget(req, rd, "")
		return rd
	}

	// 5. Default allow
	d = Decision{Action: "allow", Reason: "all layers passed"}
	e.auditFireAndForget(req, d, "")
	return d
}

// EvaluateEnvelope unmarshals a bus.Envelope payload into a ToolCallRequest and evaluates it.
func (e *PolicyEngine) EvaluateEnvelope(ctx context.Context, env *bus.Envelope) (Decision, error) {
	var req ToolCallRequest
	if err := json.Unmarshal(env.Payload, &req); err != nil {
		return Decision{Action: "deny", Reason: "malformed request: " + err.Error()}, nil
	}
	req.EnvelopeMeta = env.Metadata
	return e.Evaluate(ctx, req), nil
}

// ReloadRules queries all rules from PG and atomically swaps the cache.
func (e *PolicyEngine) ReloadRules(ctx context.Context) error {
	rows, err := e.db.QueryContext(ctx,
		`SELECT id, scope, priority, match_field, match_op, match_value,
		        action, action_params, COALESCE(description, '')
		 FROM policy_rules ORDER BY scope, priority DESC`)
	if err != nil {
		e.mu.Lock()
		e.loadErr = fmt.Errorf("reload: query: %w", err)
		e.mu.Unlock()
		return e.loadErr
	}
	defer rows.Close()

	var rules []PolicyRule
	for rows.Next() {
		var r PolicyRule
		if err := rows.Scan(&r.ID, &r.Scope, &r.Priority,
			&r.MatchField, &r.MatchOp, &r.MatchValue,
			&r.Action, &r.ActionParams, &r.Description); err != nil {
			e.mu.Lock()
			e.loadErr = fmt.Errorf("reload: scan: %w", err)
			e.mu.Unlock()
			return e.loadErr
		}
		rules = append(rules, r)
	}
	if err := rows.Err(); err != nil {
		e.mu.Lock()
		e.loadErr = fmt.Errorf("reload: rows: %w", err)
		e.mu.Unlock()
		return e.loadErr
	}

	e.mu.Lock()
	e.rules = rules
	e.loadErr = nil
	e.mu.Unlock()

	log.Printf("[policy] reloaded %d rules", len(rules))
	return nil
}

// Close shuts down the engine.
func (e *PolicyEngine) Close() error {
	if e.cancel != nil {
		e.cancel()
	}
	return e.db.Close()
}

// --- internal helpers ---

func (e *PolicyEngine) evaluateScope(scope RuleScope, req ToolCallRequest) Decision {
	e.mu.RLock()
	defer e.mu.RUnlock()

	for _, r := range e.rules {
		if r.Scope != scope {
			continue
		}
		if matchRule(r, req) {
			return Decision{
				Action: string(r.Action),
				RuleID: r.ID,
				Reason: fmt.Sprintf("rule %s (%s): %s", r.ID, r.Scope, r.Description),
			}
		}
	}
	return Decision{Action: "allow"}
}

func (e *PolicyEngine) getSoulSheet(soulID string) *SoulSheet {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.soulSheets[soulID]
}

func (e *PolicyEngine) auditFireAndForget(req ToolCallRequest, d Decision, ruleID string) {
	ctxJSON, _ := json.Marshal(map[string]any{
		"tool_name": req.ToolName,
		"args":      req.Args,
		"soul_id":   req.EnvelopeMeta.SoulID,
	})
	go func() {
		id, err := e.audit.Log(context.Background(),
			req.EnvelopeMeta.TaskID, req.EnvelopeMeta.AgentID,
			ruleID, d.Action, ctxJSON)
		if err != nil {
			log.Printf("[policy] audit log failed: %v", err)
		} else {
			log.Printf("[policy] audit %s: %s on %s for task %s",
				id, d.Action, req.ToolName, req.EnvelopeMeta.TaskID)
		}
	}()
}

func (e *PolicyEngine) loadSoulSheets(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("soul dir %s: %w", dir, err)
	}

	var errs []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		p := filepath.Join(dir, entry.Name())
		s, loadErr := LoadSoulSheet(p)
		if loadErr != nil {
			errs = append(errs, entry.Name()+": "+loadErr.Error())
			continue
		}
		e.mu.Lock()
		e.soulSheets[s.SoulID] = s
		e.mu.Unlock()
		log.Printf("[policy] loaded soul %s from %s", s.SoulID, entry.Name())
	}

	if len(errs) > 0 {
		return fmt.Errorf("soul sheet errors: %s", strings.Join(errs, "; "))
	}
	return nil
}
