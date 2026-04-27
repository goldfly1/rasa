package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	mode := flag.String("mode", "aggregator", "Mode: aggregator | scorer")
	dsn := flag.String("db", "", "PostgreSQL DSN")
	natsURL := flag.String("nats", "nats://localhost:4222", "NATS URL")
	benchmarks := flag.String("benchmarks", "benchmarks/", "Benchmark directory")
	flag.Parse()

	if *dsn == "" {
		log.Fatal("--db is required")
	}

	log.Printf("eval-aggregator starting mode=%s db=%s nats=%s", *mode, *dsn, *natsURL)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig

	log.Println("eval-aggregator shutting down")
}
