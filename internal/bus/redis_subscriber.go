package bus

import (
	"context"
	"fmt"
	"sync"

	"github.com/redis/go-redis/v9"
)

// RedisSub subscribes to Redis channels (including glob patterns via PSubscribe) and dispatches to handlers.
type RedisSub struct {
	client   *redis.Client
	handlers map[string]func(*Envelope)
	mu       sync.RWMutex
	cancel   context.CancelFunc
}

// NewRedisSub connects to a Redis instance for subscribing.
func NewRedisSub(addr string) (*RedisSub, error) {
	client := redis.NewClient(&redis.Options{Addr: addr})
	if err := client.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("redis subscriber: ping: %w", err)
	}
	return &RedisSub{
		client:   client,
		handlers: make(map[string]func(*Envelope)),
	}, nil
}

// Subscribe registers a handler for a channel or glob pattern (e.g. "agents.heartbeat.*").
func (s *RedisSub) Subscribe(_ context.Context, channel string, handler func(*Envelope)) error {
	s.mu.Lock()
	s.handlers[channel] = handler
	s.mu.Unlock()
	return nil
}

// Start begins listening on all registered channels and patterns.
func (s *RedisSub) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	s.mu.RLock()
	exact := make([]string, 0)
	patterns := make([]string, 0)
	for ch := range s.handlers {
		if hasGlob(ch) {
			patterns = append(patterns, ch)
		} else {
			exact = append(exact, ch)
		}
	}
	s.mu.RUnlock()

	pubsub := s.client.PSubscribe(ctx, patterns...)
	if len(exact) > 0 {
		if err := pubsub.Subscribe(ctx, exact...); err != nil {
			pubsub.Close()
			return fmt.Errorf("redis subscriber: subscribe: %w", err)
		}
	}

	go s.drain(ctx, pubsub)
	return nil
}

// Close shuts down the subscriber.
func (s *RedisSub) Close() error {
	if s.cancel != nil {
		s.cancel()
	}
	return s.client.Close()
}

func (s *RedisSub) drain(ctx context.Context, pubsub *redis.PubSub) {
	defer pubsub.Close()
	ch := pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			env, err := EnvelopeFromJSON([]byte(msg.Payload))
			if err != nil {
				continue
			}
			matched := msg.Channel
			if msg.Pattern != "" {
				matched = msg.Pattern
			}
			s.mu.RLock()
			handler, ok := s.handlers[matched]
			s.mu.RUnlock()
			if ok {
				handler(env)
			}
		}
	}
}

func hasGlob(s string) bool {
	for _, c := range s {
		if c == '*' || c == '?' || c == '[' {
			return true
		}
	}
	return false
}
