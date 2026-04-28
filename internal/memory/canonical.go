package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

// CanonicalNode mirrors the canonical_nodes table.
type CanonicalNode struct {
	ID            string    `json:"id"`
	NodeType      string    `json:"node_type"`
	Name          string    `json:"name"`
	Path          string    `json:"path"`
	Body          string    `json:"body"`
	OutgoingEdges []string  `json:"outgoing_edges"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// SoulSheet mirrors the soul_sheets table.
type SoulSheetRec struct {
	ID         string    `json:"id"`
	SoulID     string    `json:"soul_id"`
	Version    string    `json:"version"`
	AgentRole  string    `json:"agent_role"`
	Body       string    `json:"body"`
	SourcePath string    `json:"source_path"`
	UpdatedAt  time.Time `json:"updated_at"`
	CreatedAt  time.Time `json:"created_at"`
}

// CanonicalStore manages the canonical model (nodes + soul sheets) in PostgreSQL.
type CanonicalStore struct {
	db *sql.DB
}

// NewCanonicalStore opens the rasa_memory database.
func NewCanonicalStore(dsn string) (*CanonicalStore, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("canonical store: open: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("canonical store: ping: %w", err)
	}
	return &CanonicalStore{db: db}, nil
}

// Close closes the database connection.
func (s *CanonicalStore) Close() error {
	return s.db.Close()
}

// UpsertNode inserts or updates a canonical node.
func (s *CanonicalStore) UpsertNode(ctx context.Context, n CanonicalNode) error {
	if n.ID == "" {
		return fmt.Errorf("upsert node: id required")
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO canonical_nodes (id, node_type, name, path, body, outgoing_edges)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (id) DO UPDATE SET
		   node_type = EXCLUDED.node_type,
		   name = EXCLUDED.name,
		   path = EXCLUDED.path,
		   body = EXCLUDED.body,
		   outgoing_edges = EXCLUDED.outgoing_edges,
		   updated_at = NOW()`,
		n.ID, n.NodeType, n.Name, nullableStr(n.Path), n.Body, pqArray(n.OutgoingEdges))
	if err != nil {
		return fmt.Errorf("upsert node: %w", err)
	}
	return nil
}

// GetNode fetches a single node by ID.
func (s *CanonicalStore) GetNode(ctx context.Context, id string) (*CanonicalNode, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, node_type, name, COALESCE(path, ''), body, outgoing_edges, created_at, updated_at
		 FROM canonical_nodes WHERE id = $1`, id)

	var n CanonicalNode
	var edges string // pq driver returns arrays as CSV-like strings
	var bodyBytes []byte
	err := row.Scan(&n.ID, &n.NodeType, &n.Name, &n.Path, &bodyBytes, &edges, &n.CreatedAt, &n.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get node: %w", err)
	}
	n.Body = string(bodyBytes)
	n.OutgoingEdges = parsePQArray(edges)
	return &n, nil
}

// GetNodeByPath finds a node by type and path.
func (s *CanonicalStore) GetNodeByPath(ctx context.Context, nodeType, path string) (*CanonicalNode, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, node_type, name, COALESCE(path, ''), body, outgoing_edges, created_at, updated_at
		 FROM canonical_nodes WHERE node_type = $1 AND path = $2 LIMIT 1`, nodeType, path)

	var n CanonicalNode
	var edges string
	var bodyBytes []byte
	err := row.Scan(&n.ID, &n.NodeType, &n.Name, &n.Path, &bodyBytes, &edges, &n.CreatedAt, &n.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get node by path: %w", err)
	}
	n.Body = string(bodyBytes)
	n.OutgoingEdges = parsePQArray(edges)
	return &n, nil
}

// Traverse returns the subgraph reachable from startID up to maxDepth hops via recursive CTE.
func (s *CanonicalStore) Traverse(ctx context.Context, startID string, maxDepth int) ([]CanonicalNode, error) {
	if maxDepth <= 0 {
		maxDepth = 1
	}

	rows, err := s.db.QueryContext(ctx,
		`WITH RECURSIVE subgraph AS (
		    SELECT id, node_type, name, COALESCE(path, '') AS path, body,
		           outgoing_edges, created_at, updated_at, 0 AS depth
		    FROM canonical_nodes WHERE id = $1
		    UNION
		    SELECT cn.id, cn.node_type, cn.name, COALESCE(cn.path, '') AS path, cn.body,
		           cn.outgoing_edges, cn.created_at, cn.updated_at, sg.depth + 1
		    FROM canonical_nodes cn
		    JOIN subgraph sg ON cn.id = ANY(sg.outgoing_edges)
		    WHERE sg.depth < $2
		)
		SELECT DISTINCT ON (id) id, node_type, name, path, body, outgoing_edges, created_at, updated_at
		FROM subgraph ORDER BY id, depth`, startID, maxDepth)
	if err != nil {
		return nil, fmt.Errorf("traverse: %w", err)
	}
	defer rows.Close()

	var nodes []CanonicalNode
	for rows.Next() {
		var n CanonicalNode
		var edges string
		var bodyBytes []byte
		if err := rows.Scan(&n.ID, &n.NodeType, &n.Name, &n.Path, &bodyBytes, &edges, &n.CreatedAt, &n.UpdatedAt); err != nil {
			return nil, fmt.Errorf("traverse scan: %w", err)
		}
		n.Body = string(bodyBytes)
		n.OutgoingEdges = parsePQArray(edges)
		nodes = append(nodes, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("traverse rows: %w", err)
	}
	return nodes, nil
}

// UpsertSoulSheet inserts or updates a soul sheet record.
func (s *CanonicalStore) UpsertSoulSheet(ctx context.Context, r SoulSheetRec) error {
	if r.SoulID == "" {
		return fmt.Errorf("upsert soul sheet: soul_id required")
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO soul_sheets (id, soul_id, version, agent_role, body, source_path)
		 VALUES (COALESCE($1, gen_random_uuid()), $2, $3, $4, $5, $6)
		 ON CONFLICT (soul_id) DO UPDATE SET
		   version = EXCLUDED.version,
		   agent_role = EXCLUDED.agent_role,
		   body = EXCLUDED.body,
		   updated_at = NOW()`,
		nullableStr(r.ID), r.SoulID, r.Version, r.AgentRole, r.Body, nullableStr(r.SourcePath))
	if err != nil {
		return fmt.Errorf("upsert soul sheet: %w", err)
	}
	return nil
}

// GetSoulSheet fetches a soul sheet by soul_id.
func (s *CanonicalStore) GetSoulSheet(ctx context.Context, soulID string) (*SoulSheetRec, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, soul_id, version, agent_role, body, COALESCE(source_path, ''), updated_at, created_at
		 FROM soul_sheets WHERE soul_id = $1`, soulID)

	var r SoulSheetRec
	var bodyBytes []byte
	err := row.Scan(&r.ID, &r.SoulID, &r.Version, &r.AgentRole, &bodyBytes, &r.SourcePath, &r.UpdatedAt, &r.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get soul sheet: %w", err)
	}
	r.Body = string(bodyBytes)
	return &r, nil
}

// --- helpers ---

func nullableStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// pqArray formats a []string as a PostgreSQL array literal.
func pqArray(vs []string) any {
	if len(vs) == 0 {
		return "{}"
	}
	quoted := make([]string, len(vs))
	for i, v := range vs {
		quoted[i] = `"` + strings.NewReplacer(`"`, `\"`, `\`, `\\`).Replace(v) + `"`
	}
	return "{" + strings.Join(quoted, ",") + "}"
}

// parsePQArray converts a PostgreSQL array literal string into []string.
func parsePQArray(raw string) []string {
	if raw == "" || raw == "{}" {
		return nil
	}

	raw = strings.Trim(raw, "{}")
	if raw == "" {
		return nil
	}

	var result []string
	var current strings.Builder
	inQuotes := false
	escaped := false

	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if escaped {
			current.WriteByte(c)
			escaped = false
			continue
		}
		if c == '\\' && inQuotes {
			escaped = true
			continue
		}
		if c == '"' {
			inQuotes = !inQuotes
			continue
		}
		if c == ',' && !inQuotes {
			result = append(result, current.String())
			current.Reset()
			continue
		}
		current.WriteByte(c)
	}
	if current.Len() > 0 {
		result = append(result, current.String())
	}
	return result
}

// canonicalToJSON marshals a Go value to JSON string for Body storage.
func canonicalToJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
