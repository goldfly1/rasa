package bus

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// RedisPub publishes ephemeral messages via Redis Pub/Sub.
type RedisPub struct {
	client *redis.Client
}

// NewRedisPub connects to a Redis instance for publishing.
func NewRedisPub(addr string) (*RedisPub, error) {
	client := redis.NewClient(&redis.Options{Addr: addr})
	if err := client.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("redis publisher: ping: %w", err)
	}
	return &RedisPub{client: client}, nil
}

// Publish sends an envelope as JSON to a Redis channel.
func (p *RedisPub) Publish(ctx context.Context, channel string, msg *Envelope) error {
	envJSON, err := msg.ToJSON()
	if err != nil {
		return fmt.Errorf("redis publish: marshal: %w", err)
	}
	if err := p.client.Publish(ctx, channel, envJSON).Err(); err != nil {
		return fmt.Errorf("redis publish: %w", err)
	}
	return nil
}

// Close closes the Redis connection.
func (p *RedisPub) Close() error {
	return p.client.Close()
}
