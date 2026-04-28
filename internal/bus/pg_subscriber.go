package bus

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/lib/pq"
)

// PGSub subscribes to PostgreSQL LISTEN/NOTIFY channels and dispatches to handlers.
type PGSub struct {
	db           *sql.DB
	listener     *pq.Listener
	handlers     map[string]func(*Envelope)
	mu           sync.RWMutex
	ctx          context.Context
	cancel       context.CancelFunc
}

// NewPGSub creates a subscriber using a dual-connection: one for LISTEN, one for row queries.
func NewPGSub(dsn string) (_ *PGSub, err error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("pg subscriber: open: %w", err)
	}
	defer func() {
		if err != nil {
			db.Close()
		}
	}()

	if _, err := db.Exec(busTableDDL); err != nil {
		return nil, fmt.Errorf("pg subscriber: ddl: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	sub := &PGSub{
		db:       db,
		listener: pq.NewListener(dsn, 1*time.Second, 10*time.Second, nil),
		handlers: make(map[string]func(*Envelope)),
		ctx:      ctx,
		cancel:   cancel,
	}

	go sub.dispatchLoop()
	return sub, nil
}

// Subscribe registers a handler for a channel. Must be called before Start.
func (s *PGSub) Subscribe(_ context.Context, channel string, handler func(*Envelope)) error {
	if !validChannel.MatchString(channel) {
		return fmt.Errorf("pg subscribe: invalid channel %q", channel)
	}
	s.mu.Lock()
	s.handlers[channel] = handler
	s.mu.Unlock()

	if err := s.listener.Listen(channel); err != nil {
		return fmt.Errorf("pg subscribe: listen %q: %w", channel, err)
	}
	return nil
}

// Close shuts down the listener and DB connection.
func (s *PGSub) Close() error {
	s.cancel()
	s.listener.Close()
	return s.db.Close()
}

func (s *PGSub) dispatchLoop() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case n := <-s.listener.Notify:
			if n == nil {
				continue
			}
			s.mu.RLock()
			handler, ok := s.handlers[n.Channel]
			s.mu.RUnlock()
			if !ok {
				continue
			}
			env, err := s.fetchAndMark(n.Channel)
			if err != nil || env == nil {
				continue
			}
			handler(env)
		}
	}
}

func (s *PGSub) fetchAndMark(channel string) (*Envelope, error) {
	ctx := context.Background()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	row := tx.QueryRowContext(ctx,
		"SELECT id, envelope FROM bus_messages WHERE channel = $1 AND status = 'pending' ORDER BY id LIMIT 1 FOR UPDATE",
		channel,
	)
	var id int64
	var envRaw json.RawMessage
	if err := row.Scan(&id, &envRaw); err != nil {
		return nil, nil // no pending message — not an error
	}

	env, err := EnvelopeFromJSON(envRaw)
	if err != nil {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx,
		"UPDATE bus_messages SET status = 'consumed' WHERE id = $1", id,
	); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return env, nil
}
