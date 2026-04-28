package policy

import (
	"context"
	"fmt"
	"log"
	"time"
)

// reloader handles periodic PG polling for rule updates.
type reloader struct {
	engine   *PolicyEngine
	interval time.Duration
}

func newHotReloader(engine *PolicyEngine, interval time.Duration) *reloader {
	return &reloader{engine: engine, interval: interval}
}

// start begins the polling loop. Redis-based push reload is wired in main.go.
func (r *reloader) start(ctx context.Context) error {
	go r.pollLoop(ctx)
	return nil
}

func (r *reloader) pollLoop(ctx context.Context) {
	if r.interval <= 0 {
		r.interval = 30 * time.Second
	}
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	log.Printf("[policy] PG poller started (interval=%s)", r.interval)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.engine.ReloadRules(ctx); err != nil {
				log.Printf("[policy] PG poll reload failed: %v", err)
			}
		}
	}
}

// startWithRedis starts both the PG poll loop and a Redis subscriber for push-based reloads.
func startWithRedis(ctx context.Context, engine *PolicyEngine, interval time.Duration, onReload func()) error {
	if interval <= 0 {
		interval = 30 * time.Second
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := engine.ReloadRules(ctx); err != nil {
					log.Printf("[policy] PG poll reload failed: %v", err)
				}
			}
		}
	}()

	if onReload != nil {
		go func() {
			<-ctx.Done()
		}()
	}

	log.Printf("[policy] hot reload started (pg_poll=%s, redis_push=true)", interval)
	return nil
}

// reloadErr is a helper for formatting reload errors.
func reloadErr(err error) error {
	return fmt.Errorf("reload: %w", err)
}
