package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// ConversationTurn represents a single turn in an agent conversation session.
type ConversationTurn struct {
	Role        string `json:"role"`
	Content     string `json:"content"`
	TimestampMs int64  `json:"timestamp_ms"`
	TurnIndex   int    `json:"turn_index"`
}

// SessionMeta holds metadata for a session.
type SessionMeta struct {
	SoulID       string `json:"soul_id"`
	TaskID       string `json:"task_id"`
	AgentID      string `json:"agent_id"`
	CreatedAt    int64  `json:"created_at"`
	LastActiveAt int64  `json:"last_active_at"`
}

// SessionStore manages ephemeral conversation turns in Redis.
// Key schema:
//   - session:{soul_id}:{task_id}:{turn_index}  — Hash with role, content, timestamp_ms
//   - session_turns:{soul_id}:{task_id}          — ZSET: turn_index scored by timestamp_ms
//   - session_meta:{soul_id}:{task_id}           — String: JSON SessionMeta
type SessionStore struct {
	client *redis.Client
}

// NewSessionStore connects to Redis.
func NewSessionStore(addr string) (*SessionStore, error) {
	client := redis.NewClient(&redis.Options{Addr: addr})
	if err := client.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("session store: ping: %w", err)
	}
	return &SessionStore{client: client}, nil
}

// Close releases the Redis connection.
func (s *SessionStore) Close() error {
	return s.client.Close()
}

// InitSession creates session metadata and sets TTL.
func (s *SessionStore) InitSession(ctx context.Context, soulID, taskID, agentID string, ttl time.Duration) error {
	now := time.Now().UnixMilli()
	meta := SessionMeta{
		SoulID:       soulID,
		TaskID:       taskID,
		AgentID:      agentID,
		CreatedAt:    now,
		LastActiveAt: now,
	}
	metaJSON, _ := json.Marshal(meta)
	metaKey := "session_meta:" + soulID + ":" + taskID

	pipe := s.client.Pipeline()
	pipe.Set(ctx, metaKey, string(metaJSON), ttl)
	pipe.Set(ctx, "session_counter:"+soulID+":"+taskID, "0", ttl)
	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("init session: %w", err)
	}
	return nil
}

// AppendTurn adds a conversation turn and refreshes session TTL.
func (s *SessionStore) AppendTurn(ctx context.Context, soulID, taskID, role, content string) error {
	turnKey := "session:" + soulID + ":" + taskID
	metaKey := "session_meta:" + soulID + ":" + taskID
	counterKey := "session_counter:" + soulID + ":" + taskID

	// Get current TTL to reuse
	ttl, err := s.client.TTL(ctx, metaKey).Result()
	if err != nil || ttl <= 0 {
		ttl = 20 * time.Minute
	}

	idx, err := s.client.Incr(ctx, counterKey).Result()
	if err != nil {
		return fmt.Errorf("append turn: incr: %w", err)
	}
	turnIdx := int(idx)
	now := time.Now().UnixMilli()

	pipe := s.client.Pipeline()
	turnHashKey := fmt.Sprintf("%s:%d", turnKey, turnIdx)
	pipe.HSet(ctx, turnHashKey, "role", role)
	pipe.HSet(ctx, turnHashKey, "content", content)
	pipe.HSet(ctx, turnHashKey, "timestamp_ms", strconv.FormatInt(now, 10))
	pipe.Expire(ctx, fmt.Sprintf("%s:%d", turnKey, turnIdx), ttl)
	pipe.ZAdd(ctx, "session_turns:"+soulID+":"+taskID, redis.Z{
		Score:  float64(now),
		Member: turnIdx,
	})
	pipe.Expire(ctx, "session_turns:"+soulID+":"+taskID, ttl)
	pipe.Expire(ctx, metaKey, ttl)

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("append turn: %w", err)
	}
	return nil
}

// GetRecentTurns returns the most recent N turns for a session.
func (s *SessionStore) GetRecentTurns(ctx context.Context, soulID, taskID string, window int) ([]ConversationTurn, error) {
	if window <= 0 {
		return nil, nil
	}

	turnsKey := "session_turns:" + soulID + ":" + taskID
	// Get most recent N turn indices
	members, err := s.client.ZRevRangeWithScores(ctx, turnsKey, 0, int64(window-1)).Result()
	if err != nil {
		return nil, fmt.Errorf("get recent turns: %w", err)
	}
	if len(members) == 0 {
		return nil, nil
	}

	// Fetch each turn hash
	var turns []ConversationTurn
	for _, m := range members {
		idx := m.Member.(string) // stored as string in ZSET
		turnKey := "session:" + soulID + ":" + taskID + ":" + idx
		fields, err := s.client.HGetAll(ctx, turnKey).Result()
		if err != nil || len(fields) == 0 {
			continue // key expired between ZREVRANGE and HGETALL
		}
		turnIdx, _ := strconv.Atoi(idx)
		ts, _ := strconv.ParseInt(fields["timestamp_ms"], 10, 64)
		turns = append(turns, ConversationTurn{
			Role:        fields["role"],
			Content:     fields["content"],
			TimestampMs: ts,
			TurnIndex:   turnIdx,
		})
	}
	return turns, nil
}

// GetSessionMeta returns session metadata or nil if expired.
func (s *SessionStore) GetSessionMeta(ctx context.Context, soulID, taskID string) (*SessionMeta, error) {
	metaKey := "session_meta:" + soulID + ":" + taskID
	raw, err := s.client.Get(ctx, metaKey).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get session meta: %w", err)
	}
	var meta SessionMeta
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		return nil, fmt.Errorf("get session meta unmarshal: %w", err)
	}
	return &meta, nil
}

// ExtendSession refreshes the TTL on an active session.
func (s *SessionStore) ExtendSession(ctx context.Context, soulID, taskID string, ttl time.Duration) error {
	metaKey := "session_meta:" + soulID + ":" + taskID
	turnsKey := "session_turns:" + soulID + ":" + taskID
	counterKey := "session_counter:" + soulID + ":" + taskID

	pipe := s.client.Pipeline()
	pipe.Expire(ctx, metaKey, ttl)
	pipe.Expire(ctx, turnsKey, ttl)
	pipe.Expire(ctx, counterKey, ttl)
	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("extend session: %w", err)
	}
	return nil
}

// CloseSession deletes all Redis keys for a session.
func (s *SessionStore) CloseSession(ctx context.Context, soulID, taskID string) error {
	metaKey := "session_meta:" + soulID + ":" + taskID
	turnsKey := "session_turns:" + soulID + ":" + taskID
	counterKey := "session_counter:" + soulID + ":" + taskID

	// Get turn indices to delete individual hash keys
	members, _ := s.client.ZRange(ctx, turnsKey, 0, -1).Result()

	pipe := s.client.Pipeline()
	pipe.Del(ctx, metaKey, turnsKey, counterKey)
	for _, m := range members {
		pipe.Del(ctx, "session:"+soulID+":"+taskID+":"+m)
	}
	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("close session: %w", err)
	}
	return nil
}
