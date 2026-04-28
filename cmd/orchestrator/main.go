package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"

	"github.com/goldf/rasa/internal/bus"
	"github.com/goldf/rasa/internal/db"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: orchestrator <command> [args]")
		fmt.Fprintln(os.Stderr, "  submit  --soul <id> --title <text> [--goal <text>] [--wait] [--timeout <s>]")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "submit":
		cmdSubmit()
	default:
		log.Fatalf("unknown command: %s", os.Args[1])
	}
}

func cmdSubmit() {
	fs := flag.NewFlagSet("submit", flag.ExitOnError)
	soulID := fs.String("soul", "", "Soul ID (e.g. coder-v2-dev)")
	title := fs.String("title", "", "Task title")
	goal := fs.String("goal", "", "Task goal / prompt text")
	dsnFlag := fs.String("db", "", "PostgreSQL DSN (default: env-based rasa_orch)")
	wait := fs.Bool("wait", true, "Wait for task completion")
	timeout := fs.Int("timeout", 120, "Wait timeout in seconds")
	fs.Parse(os.Args[2:])

	if *soulID == "" || *title == "" {
		log.Fatal("--soul and --title are required")
	}

	dsn := *dsnFlag
	if dsn == "" {
		dsn = db.DSN("rasa_orch")
	}

	pgDB, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("db open: %v", err)
	}
	defer pgDB.Close()

	taskID := uuid.New().String()
	correlationID := uuid.New().String()

	payload := map[string]string{
		"type": "ad-hoc",
		"goal": *goal,
	}
	payloadJSON, _ := json.Marshal(payload)

	desc := *goal
	if desc == "" {
		desc = *title
	}

	_, err = pgDB.ExecContext(context.Background(),
		`INSERT INTO tasks (id, correlation_id, title, description, payload, status, soul_id, priority)
		 VALUES ($1, $2, $3, $4, $5, 'PENDING', $6, 5)`,
		taskID, correlationID, *title, desc, string(payloadJSON), *soulID,
	)
	if err != nil {
		log.Fatalf("insert task: %v", err)
	}
	log.Printf("created task %s (correlation=%s, soul=%s)", taskID[:8], correlationID[:8], *soulID)

	pgPub, err := bus.NewPGPub(dsn)
	if err != nil {
		log.Fatalf("pg pub: %v", err)
	}
	defer pgPub.Close()

	env, err := bus.NewEnvelope("orchestrator", "pool-controller",
		map[string]string{
			"task_id": taskID,
			"title":   *title,
			"goal":    *goal,
		},
		bus.Metadata{
			SoulID: *soulID,
			TaskID: taskID,
		},
		correlationID,
	)
	if err != nil {
		log.Fatalf("envelope: %v", err)
	}

	if err := pgPub.Publish(context.Background(), "tasks_assigned", env); err != nil {
		log.Fatalf("publish: %v", err)
	}
	log.Printf("published to tasks_assigned")

	if *wait {
		waitForCompletion(dsn, taskID, time.Duration(*timeout)*time.Second)
	} else {
		fmt.Printf(`{"task_id":"%s"}`+"\n", taskID)
	}
}

func waitForCompletion(dsn, taskID string, timeout time.Duration) {
	pgSub, err := bus.NewPGSub(dsn)
	if err != nil {
		log.Fatalf("pg sub: %v", err)
	}
	defer pgSub.Close()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	done := make(chan struct{})
	var finalStatus string
	var resultJSON []byte

	pgSub.Subscribe(ctx, "task_completed", func(env *bus.Envelope) {
		if env.Metadata.TaskID != taskID {
			return
		}

		db2, err := sql.Open("postgres", dsn)
		if err != nil {
			log.Printf("fetch result: db open: %v", err)
			return
		}
		defer db2.Close()

		var status string
		var result []byte
		err = db2.QueryRowContext(ctx,
			"SELECT status::text, result FROM tasks WHERE id = $1", taskID,
		).Scan(&status, &result)
		if err != nil {
			log.Printf("fetch result: query: %v", err)
			return
		}

		finalStatus = status
		if result != nil {
			resultJSON = result
		}
		log.Printf("task %s → %s", taskID[:8], status)
		close(done)
	})

	log.Printf("waiting up to %v for task completion...", timeout)
	select {
	case <-done:
		if resultJSON != nil {
			var pretty json.RawMessage
			if json.Unmarshal(resultJSON, &pretty) == nil {
				out, _ := json.MarshalIndent(pretty, "", "  ")
				fmt.Println(string(out))
			} else {
				fmt.Println(string(resultJSON))
			}
		}
		fmt.Printf("\ntask %s → %s\n", taskID[:8], finalStatus)
	case <-ctx.Done():
		log.Fatal("timed out waiting for task completion")
	}
}
