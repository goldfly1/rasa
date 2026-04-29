package eval

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"sync"

	"github.com/goldf/rasa/internal/bus"
)

// EvalRecord represents a completed task evaluation.
type EvalRecord struct {
	TaskID    string  `json:"task_id"`
	AgentID   string  `json:"agent_id"`
	SoulID    string  `json:"soul_id"`
	Score     float64 `json:"score"`
	Passed    bool    `json:"passed"`
	DurationMs int   `json:"duration_ms"`
}

// DriftWindow tracks rolling score windows per soul_id.
type DriftWindow struct {
	mu      sync.Mutex
	buffers map[string][]float64 // soul_id → last N scores
	maxSize int
}

// NewDriftWindow creates a drift window with fixed capacity per soul.
func NewDriftWindow(maxSize int) *DriftWindow {
	return &DriftWindow{
		buffers: make(map[string][]float64),
		maxSize: maxSize,
	}
}

// Push adds a score to a soul's window.
func (w *DriftWindow) Push(soulID string, score float64) {
	w.mu.Lock()
	defer w.mu.Unlock()

	buf := w.buffers[soulID]
	buf = append(buf, score)
	if len(buf) > w.maxSize {
		buf = buf[len(buf)-w.maxSize:]
	}
	w.buffers[soulID] = buf
}

// ShouldAlert checks whether a soul's rolling pass rate is below threshold.
func (w *DriftWindow) ShouldAlert(soulID string, threshold float64) bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	buf := w.buffers[soulID]
	if len(buf) < w.maxSize {
		return false
	}

	sum := 0.0
	for _, s := range buf {
		sum += s
	}
	avg := sum / float64(len(buf))
	return avg < threshold
}

// Stats returns count and mean for a soul_id.
func (w *DriftWindow) Stats(soulID string) (int, float64) {
	w.mu.Lock()
	defer w.mu.Unlock()

	buf := w.buffers[soulID]
	if len(buf) == 0 {
		return 0, 0
	}
	sum := 0.0
	for _, s := range buf {
		sum += s
	}
	return len(buf), sum / float64(len(buf))
}

// EvalAggregator consumes eval_record and maintains the drift window.
type EvalAggregator struct {
	db      *sql.DB
	pgSub   *bus.PGSub
	window  *DriftWindow
	ctx     context.Context
	cancel  context.CancelFunc
}

// NewEvalAggregator creates a new evaluation aggregator.
func NewEvalAggregator(ctx context.Context, db *sql.DB, pgSub *bus.PGSub) *EvalAggregator {
	ctx, cancel := context.WithCancel(ctx)
	return &EvalAggregator{
		db:     db,
		pgSub:  pgSub,
		window: NewDriftWindow(20),
		ctx:    ctx,
		cancel: cancel,
	}
}

// Start activates the PG subscription.
func (a *EvalAggregator) Start() error {
	return a.pgSub.Subscribe(a.ctx, "eval_record", a.HandleEvalRecord)
}

// HandleEvalRecord receives a completed evaluation and updates state.
func (a *EvalAggregator) HandleEvalRecord(env *bus.Envelope) {
	var record EvalRecord
	if err := json.Unmarshal(env.Payload, &record); err != nil {
		log.Printf("eval: parse error: %v", err)
		return
	}

	// Insert into evaluation_records
	meta := map[string]interface{}{
		"duration_ms": record.DurationMs,
		"passed":      record.Passed,
	}
	metaJSON, _ := json.Marshal(meta)

	_, err := a.db.ExecContext(a.ctx,
		`INSERT INTO evaluation_records (task_id, agent_id, soul_id, benchmark, score, metadata)
		 VALUES ($1, $2, $3, 'auto', $4, $5)`,
		record.TaskID, record.AgentID, record.SoulID, record.Score, string(metaJSON),
	)
	if err != nil {
		log.Printf("eval: insert error: %v", err)
	}

	// Update drift window
	score := record.Score
	if record.Passed && record.Score == 0 {
		score = 1.0 // passed without explicit scoring → 1.0
	}
	a.window.Push(record.SoulID, score)

	if a.window.ShouldAlert(record.SoulID, 0.6) {
		n, avg := a.window.Stats(record.SoulID)
		log.Printf("eval: DRIFT ALERT soul=%s (n=%d avg=%.2f below threshold)", record.SoulID, n, avg)
	}
}

// WindowStats returns stats for a soul.
func (a *EvalAggregator) WindowStats(soulID string) (int, float64) {
	return a.window.Stats(soulID)
}

// Shutdown gracefully stops the aggregator.
func (a *EvalAggregator) Shutdown() {
	a.cancel()
	log.Println("eval-aggregator: shut down")
}
