package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/goldf/rasa/internal/bus"
	"github.com/goldf/rasa/internal/db"
	"github.com/goldf/rasa/internal/policy"
)

func main() {
	dsn := flag.String("db", "", "PostgreSQL DSN for policy database (default: env-based)")
	redisAddr := flag.String("redis", "localhost:6379", "Redis address")
	pollInterval := flag.Int("poll-interval", 30, "PG poll interval in seconds")
	soulDir := flag.String("soul-dir", "souls/", "Directory containing soul YAML sheets")
	flag.Parse()

	if *dsn == "" {
		*dsn = db.DSN("rasa_policy")
	}

	log.Printf("policy-engine starting db=%s redis=%s poll=%ds soul_dir=%s",
		*dsn, *redisAddr, *pollInterval, *soulDir)

	// Try Redis subscriber (non-fatal if unavailable)
	var redisSub *bus.RedisSub
	var redisPub *bus.RedisPub

	rpub, err := bus.NewRedisPub(*redisAddr)
	if err != nil {
		log.Printf("WARNING: Redis unavailable (%v), relying on PG polling only", err)
	} else {
		redisPub = rpub
		rsub, err := bus.NewRedisSub(*redisAddr)
		if err != nil {
			log.Printf("WARNING: Redis subscriber unavailable (%v)", err)
			redisPub.Close()
			redisPub = nil
		} else {
			redisSub = rsub
		}
	}

	ctx := context.Background()
	engine, err := policy.NewEngine(ctx, *dsn, *soulDir, time.Duration(*pollInterval)*time.Second)
	if err != nil {
		log.Fatalf("policy engine: %v", err)
	}
	defer engine.Close()

	// Subscribe to eval + hot-reload channels, then start listener
	if redisSub != nil && redisPub != nil {
		// Register handler for inbound tool validation requests
		if err := redisSub.Subscribe(ctx, "policy.validate", func(env *bus.Envelope) {
			decision, err := engine.EvaluateEnvelope(context.Background(), env)
			if err != nil {
				log.Printf("[policy] evaluate error: %v", err)
			}
			replyCh := "policy.decision." + env.CorrelationID
			replyEnv, _ := bus.NewEnvelope("policy-engine", env.SourceComponent, decision,
				bus.Metadata{TaskID: env.Metadata.TaskID}, env.CorrelationID)
			if err := redisPub.Publish(context.Background(), replyCh, replyEnv); err != nil {
				log.Printf("[policy] publish decision failed: %v", err)
			}
		}); err != nil {
			log.Fatalf("policy-engine: subscribe validate: %v", err)
		}

		// Register handler for hot-reload push
		if err := redisSub.Subscribe(ctx, "policy.update", func(_ *bus.Envelope) {
			log.Printf("[policy] redis reload triggered")
			if err := engine.ReloadRules(context.Background()); err != nil {
				log.Printf("[policy] redis reload failed: %v", err)
			}
		}); err != nil {
			log.Printf("WARNING: policy.update subscription failed: %v", err)
		}

		if err := redisSub.Start(ctx); err != nil {
			log.Fatalf("policy-engine: redis start: %v", err)
		}
		log.Printf("[policy] listening on Redis channels 'policy.validate' + 'policy.update'")
	}

	log.Println("policy-engine ready")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig

	if redisSub != nil {
		redisSub.Close()
	}
	if redisPub != nil {
		redisPub.Close()
	}
	log.Println("policy-engine shutting down")
}
