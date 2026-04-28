package bus

import "context"

// Publisher sends messages on a channel.
type Publisher interface {
	Publish(ctx context.Context, channel string, msg *Envelope) error
}

// Subscriber receives messages on a channel and dispatches to a handler.
type Subscriber interface {
	Subscribe(ctx context.Context, channel string, handler func(*Envelope)) error
	Close() error
}
