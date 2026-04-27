package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	dsn := flag.String("db", "", "PostgreSQL DSN")
	natsURL := flag.String("nats", "nats://localhost:4222", "NATS URL")
	flag.Parse()

	if *dsn == "" {
		log.Fatal("--db is required")
	}

	log.Printf("policy-engine starting db=%s nats=%s", *dsn, *natsURL)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig

	log.Println("policy-engine shutting down")
}
