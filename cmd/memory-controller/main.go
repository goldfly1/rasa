package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/goldf/rasa/internal/db"
	"github.com/goldf/rasa/internal/memory"
)

func main() {
	dsn := flag.String("db", "", "PostgreSQL DSN for rasa_memory (default: env-based)")
	redisAddr := flag.String("redis", "localhost:6379", "Redis address")
	httpAddr := flag.String("http", "127.0.0.1:8300", "HTTP listen address")
	flag.Parse()

	if *dsn == "" {
		*dsn = db.DSN("rasa_memory")
	}

	log.Printf("memory-controller starting db=%s redis=%s http=%s", *dsn, *redisAddr, *httpAddr)

	// Session store (Redis) — fatal if unavailable
	store, err := memory.NewSessionStore(*redisAddr)
	if err != nil {
		log.Fatalf("memory-controller: redis: %v", err)
	}
	defer store.Close()

	// Canonical store (PostgreSQL) — fatal if unavailable
	canonical, err := memory.NewCanonicalStore(*dsn)
	if err != nil {
		log.Fatalf("memory-controller: canonical: %v", err)
	}
	defer canonical.Close()

	// Context assembler — the HTTP API
	assembler := memory.NewContextAssembler(store, canonical)

	mux := http.NewServeMux()
	mux.HandleFunc("/assemble", assembler.AssembleHTTP)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	srv := &http.Server{
		Addr:         *httpAddr,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  30 * time.Second,
	}

	go func() {
		log.Printf("memory-controller HTTP listening on %s", *httpAddr)
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("memory-controller: http: %v", err)
		}
	}()

	log.Println("memory-controller ready")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig

	log.Println("memory-controller shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}
