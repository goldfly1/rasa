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
	"github.com/goldf/rasa/internal/eval"
)

func main() {
	mode := flag.String("mode", "aggregator", "Mode: aggregator | scorer")
	dsnFlag := flag.String("db", "", "PostgreSQL DSN (default: env-based rasa_eval)")
	httpAddr := flag.String("http", "127.0.0.1:8303", "HTTP listen address")
	flag.Parse()

	dsn := *dsnFlag
	if dsn == "" {
		dsn = db.DSN("rasa_eval")
	}

	if *mode == "scorer" {
		log.Println("eval-scorer: Python-based scoring — use python -m rasa.eval.scorer instead")
		os.Exit(0)
	}

	log.Printf("eval-aggregator: db=%s http=%s", dsn, *httpAddr)

	pgSub, err := bus.NewPGSub(dsn)
	if err != nil {
		log.Fatalf("eval-aggregator: pg sub: %v", err)
	}
	defer pgSub.Close()

	evalDB, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("eval-aggregator: db open: %v", err)
	}
	defer evalDB.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	agg := eval.NewEvalAggregator(ctx, evalDB, pgSub)
	if err := agg.Start(); err != nil {
		log.Fatalf("eval-aggregator: start: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		n, avg := agg.WindowStats(r.URL.Query().Get("soul_id"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status":"ok","window":{"count":%d,"avg":%.2f}}`, n, avg)
	})

	srv := &http.Server{
		Addr:         *httpAddr,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  30 * time.Second,
	}

	go func() {
		log.Printf("eval-aggregator: HTTP listening on %s", *httpAddr)
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("eval-aggregator: http: %v", err)
		}
	}()

	log.Println("eval-aggregator ready")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig

	log.Println("eval-aggregator shutting down")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	srv.Shutdown(shutdownCtx)
	agg.Shutdown()
}
