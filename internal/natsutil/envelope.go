package natsutil

import (
	"encoding/json"
	"fmt"
	"time"
)

// Envelope is the standard NATS message envelope.
type Envelope struct {
	MessageID     string          `json:"message_id"`
	CorrelationID string          `json:"correlation_id"`
	Source        string          `json:"source"`
	Destination   string          `json:"destination"`
	Timestamp     time.Time       `json:"timestamp"`
	SoulMeta      SoulMetadata    `json:"soul_meta"`
	Payload       json.RawMessage `json:"payload"`
}

// SoulMetadata carries identity for routing and policy.
type SoulMetadata struct {
	SoulID    string `json:"soul_id"`
	AgentRole string `json:"agent_role"`
	Version   string `json:"version"`
}

// Publish wraps a payload into an Envelope and returns the marshaled JSON.
func Publish(source, dest, msgID, corrID string, soul SoulMetadata, payload any) ([]byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}
	env := Envelope{
		MessageID:     msgID,
		CorrelationID: corrID,
		Source:        source,
		Destination:   dest,
		Timestamp:     time.Now().UTC(),
		SoulMeta:      soul,
		Payload:       raw,
	}
	return json.Marshal(env)
}
