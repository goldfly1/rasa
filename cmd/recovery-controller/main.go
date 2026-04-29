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
	"github.com/goldf/rasa/internal/recovery"
)

func main() {
	dsnFlag := flag.String("db", "", "PostgreSQL DSN (default: env-based rasa_recovery)")
	redisAddr := flag.String("redis", "localhost:6379", "Redis address")
	httpAddr := flag.String("http", "127.0.0.1:8302", "HTTP listen address")
	flag.Parse()

	dsn := *dsnFlag
	if dsn == "" {
		dsn = db.DSN("rasa_recovery")
	}

	orchDSN := db.DSN("rasa_orch")

	log.Printf("recovery-controller: db=%s redis=%s http=%s", dsn, *redisAddr, *httpAddr)

	// PG subscriber for recovery notifications
	pgSub, err := bus.NewPGSub(dsn)
	if err != nil {
		log.Fatalf("recovery-controller: pg sub: %v", err)
	}
	defer pgSub.Close()

	// PG publisher
	pgPub, err := bus.NewPGPub(orchDSN)
	if err != nil {
		log.Fatalf("recovery-controller: pg pub: %v", err)
	}
	defer pgPub.Close()

	// Redis subscriber for heartbeats
	redisSub, err := bus.NewRedisSub(*redisAddr)
	if err != nil {
		log.Fatalf("recovery-controller: redis sub: %v", err)
	}
	defer redisSub.Close()

	// DB connections: orch for task queries, recovery for ledger
	orchDB, err := sql.Open("postgres", orchDSN)
	if err != nil {
		log.Fatalf("recovery-controller: orch db: %v", err)
	}
	defer orchDB.Close()

	recoveryDB, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("recovery-controller: recovery db: %v", err)
	}
	defer recoveryDB.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ctrl := recovery.NewRecoveryController(ctx, orchDB, recoveryDB, pgSub, redisSub, pgPub, 15*time.Second)
	if err := ctrl.Start(); err != nil {
		log.Fatalf("recovery-controller: start: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status":"ok","agents_tracked":%d}`, ctrl.LastSeenCount())
	})

	srv := &http.Server{
		Addr:         *httpAddr,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  30 * time.Second,
	}

	go func() {
		log.Printf("recovery-controller: HTTP listening on %s", *httpAddr)
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("recovery-controller: http: %v", err)
		}
	}()

	log.Println("recovery-controller ready")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig

	log.Println("recovery-controller shutting down")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	srv.Shutdown(shutdownCtx)
	ctrl.Shutdown()
}
