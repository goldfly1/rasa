package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

// AssembleRequest is the JSON body for POST /assemble.
type AssembleRequest struct {
	SoulID     string            `json:"soul_id"`
	TaskID     string            `json:"task_id"`
	AgentID    string            `json:"agent_id"`
	Variables  []string          `json:"variables"`
	Resolution map[string]string `json:"resolution"`
}

// ContextPayload is the response for POST /assemble.
type ContextPayload struct {
	Variables map[string]any `json:"variables"`
	Hash      string         `json:"hash"`
}

// ContextAssembler resolves memory template variables for agent prompt injection.
type ContextAssembler struct {
	store     *SessionStore
	canonical *CanonicalStore
}

// NewContextAssembler creates an assembler backed by the given stores.
func NewContextAssembler(store *SessionStore, canonical *CanonicalStore) *ContextAssembler {
	return &ContextAssembler{store: store, canonical: canonical}
}

// Assemble resolves the requested variables and returns a ContextPayload.
func (a *ContextAssembler) Assemble(ctx context.Context, req AssembleRequest) (*ContextPayload, error) {
	payload := &ContextPayload{
		Variables: make(map[string]any),
	}

	for _, varName := range req.Variables {
		resolved, err := a.resolve(ctx, varName, req)
		if err != nil {
			log.Printf("[memory] resolve %s failed: %v", varName, err)
			// Fail-open: set empty, let agent work without memory
			resolved = nil
		}
		payload.Variables[varName] = resolved
	}

	payload.Hash = contextHash(req.SoulID, req.TaskID, req.AgentID, payload)
	return payload, nil
}

// AssembleHTTP is the HTTP handler for POST /assemble.
func (a *ContextAssembler) AssembleHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req AssembleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request: " + err.Error()})
		return
	}

	payload, err := a.Assemble(r.Context(), req)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, payload)
}

// --- resolution helpers ---

func (a *ContextAssembler) resolve(ctx context.Context, varName string, req AssembleRequest) (any, error) {
	switch varName {
	case "short_term_summary":
		return a.resolveShortTerm(ctx, req)
	case "semantic_matches":
		return a.resolveSemantic(ctx, req)
	case "graph_excerpt":
		return a.resolveGraph(ctx, req)
	case "archive_refs":
		return a.resolveArchive(ctx, req)
	default:
		return nil, fmt.Errorf("unknown variable %q", varName)
	}
}

func (a *ContextAssembler) resolveShortTerm(ctx context.Context, req AssembleRequest) (any, error) {
	window := 10 // default
	if soul, err := a.canonical.GetSoulSheet(ctx, req.SoulID); err == nil && soul != nil {
		// Parse short_term_window from soul body JSON
		var body struct {
			Memory struct {
				ShortTermWindow int `json:"short_term_window"`
			} `json:"memory"`
		}
		if json.Unmarshal([]byte(soul.Body), &body) == nil && body.Memory.ShortTermWindow > 0 {
			window = body.Memory.ShortTermWindow
		}
	}

	turns, err := a.store.GetRecentTurns(ctx, req.SoulID, req.TaskID, window)
	if err != nil {
		return nil, err
	}
	if turns == nil {
		return []ConversationTurn{}, nil
	}
	return turns, nil
}

func (a *ContextAssembler) resolveSemantic(ctx context.Context, req AssembleRequest) (any, error) {
	// Semantic search requires embedder — return empty for Phase 1
	return []any{}, nil
}

func (a *ContextAssembler) resolveGraph(ctx context.Context, req AssembleRequest) (any, error) {
	startID := req.Resolution["graph_excerpt"]
	if startID == "" {
		return []CanonicalNode{}, nil
	}

	depth := 2 // default
	if soul, err := a.canonical.GetSoulSheet(ctx, req.SoulID); err == nil && soul != nil {
		var body struct {
			Memory struct {
				GraphTraversalDepth int `json:"graph_traversal_depth"`
			} `json:"memory"`
		}
		if json.Unmarshal([]byte(soul.Body), &body) == nil && body.Memory.GraphTraversalDepth > 0 {
			depth = body.Memory.GraphTraversalDepth
		}
	}

	nodes, err := a.canonical.Traverse(ctx, startID, depth)
	if err != nil {
		return nil, err
	}
	if nodes == nil {
		return []CanonicalNode{}, nil
	}
	return nodes, nil
}

func (a *ContextAssembler) resolveArchive(ctx context.Context, req AssembleRequest) (any, error) {
	return []any{}, nil // deferred
}

// --- helpers ---

func contextHash(soulID, taskID, agentID string, payload *ContextPayload) string {
	h := sha256.New()
	h.Write([]byte(soulID + ":" + taskID + ":" + agentID + ":"))
	b, _ := json.Marshal(payload.Variables)
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
