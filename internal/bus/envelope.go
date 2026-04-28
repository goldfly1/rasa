package bus

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Metadata carries contextual fields for routing, policy, and tracing.
type Metadata struct {
	SoulID            string `json:"soul_id"`
	PromptVersionHash string `json:"prompt_version_hash"`
	AgentRole         string `json:"agent_role"`
	TaskID            string `json:"task_id"`
	AgentID           string `json:"agent_id"`
	TimestampMs       int64  `json:"timestamp_ms"`
}

// Envelope is the standard message wrapper for all transports.
type Envelope struct {
	MessageID          string          `json:"message_id"`
	CorrelationID      string          `json:"correlation_id"`
	SourceComponent    string          `json:"source_component"`
	DestinationComponent string        `json:"destination_component"`
	Payload            json.RawMessage `json:"payload"`
	Metadata           Metadata        `json:"metadata"`
}

// NewEnvelope creates a fully-populated envelope with generated IDs.
func NewEnvelope(source, destination string, payload any, meta Metadata, corrID string) (*Envelope, error) {
	if corrID == "" {
		corrID = uuid.New().String()
	}
	if meta.TimestampMs == 0 {
		meta.TimestampMs = time.Now().UnixMilli()
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return &Envelope{
		MessageID:          uuid.New().String(),
		CorrelationID:      corrID,
		SourceComponent:    source,
		DestinationComponent: destination,
		Payload:            raw,
		Metadata:           meta,
	}, nil
}

// ToJSON marshals the envelope to a JSON string.
func (e *Envelope) ToJSON() (string, error) {
	b, err := json.Marshal(e)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// EnvelopeFromJSON unmarshals a JSON string or bytes into an Envelope.
func EnvelopeFromJSON(raw []byte) (*Envelope, error) {
	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, err
	}
	return &env, nil
}
