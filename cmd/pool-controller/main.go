package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/lib/pq"

	"github.com/goldf/rasa/internal/bus"
	"github.com/goldf/rasa/internal/db"
	"github.com/goldf/rasa/internal/pool"
)

func main() {
	cfgPath := flag.String("config", "config/pool.yaml", "Pool config path")
	dsnFlag := flag.String("db", "", "PostgreSQL DSN (default: env-based rasa_orch)")
	redisAddr := flag.String("redis", "localhost:6379", "Redis address")
	httpAddr := flag.String("http", "127.0.0.1:8301", "HTTP listen address")
	flag.Parse()

	dsn := *dsnFlag
	if dsn == "" {
		dsn = db.DSN("rasa_orch")
	}

	cfg, err := pool.LoadConfig(*cfgPath)
	if err != nil {
		log.Fatalf("pool-controller: config: %v", err)
	}

	log.Printf("pool-controller: config=%s db=%s redis=%s http=%s",
		*cfgPath, dsn, *redisAddr, *httpAddr)

	// PG subscriber for tasks_assigned
	pgSub, err := bus.NewPGSub(dsn)
	if err != nil {
		log.Fatalf("pool-controller: pg sub: %v", err)
	}
	defer pgSub.Close()

	// Redis subscriber for heartbeats
	redisSub, err := bus.NewRedisSub(*redisAddr)
	if err != nil {
		log.Fatalf("pool-controller: redis sub: %v", err)
	}
	defer redisSub.Close()

	// DB for task status updates
	pgDB, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("pool-controller: db open: %v", err)
	}
	defer pgDB.Close()
	if err := pgDB.Ping(); err != nil {
		log.Fatalf("pool-controller: db ping: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ctrl := pool.NewPoolController(ctx, cfg, pgSub, redisSub, pgDB)
	if err := ctrl.Start(); err != nil {
		log.Fatalf("pool-controller: start: %v", err)
	}

	// HTTP health endpoint
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status":"ok","agents":%d}`, ctrl.Registry().Count())
	})

	srv := &http.Server{
		Addr:         *httpAddr,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  30 * time.Second,
	}

	go func() {
		log.Printf("pool-controller: HTTP listening on %s", *httpAddr)
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("pool-controller: http: %v", err)
		}
	}()

	log.Println("pool-controller ready")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig

	log.Println("pool-controller shutting down")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	srv.Shutdown(shutdownCtx)
	ctrl.Shutdown()
}
