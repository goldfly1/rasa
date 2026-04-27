package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	addr := flag.String("addr", ":8080", "Listen address")
	dsn := flag.String("db", "", "PostgreSQL DSN")
	natsURL := flag.String("nats", "nats://localhost:4222", "NATS URL")
	flag.Parse()

	if *dsn == "" {
		log.Fatal("--db is required")
	}

	log.Printf("orchestrator starting on %s", *addr)
	log.Printf("db=%s nats=%s", *dsn, *natsURL)

	// TODO: wire db pool, NATS connection, task lifecycle engine

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig

	log.Println("orchestrator shutting down")
}
