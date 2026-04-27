package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	cfg := flag.String("config", "config/pool.yaml", "Pool config path")
	dsn := flag.String("db", "", "PostgreSQL DSN")
	natsURL := flag.String("nats", "nats://localhost:4222", "NATS URL")
	flag.Parse()

	if *dsn == "" {
		log.Fatal("--db is required")
	}

	log.Printf("pool-controller starting config=%s db=%s nats=%s", *cfg, *dsn, *natsURL)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig

	log.Println("pool-controller shutting down")
}
