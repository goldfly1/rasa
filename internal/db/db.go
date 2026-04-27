package db

import (
	"fmt"
	"os"
	"time"

	"github.com/lib/pq"
)

// DSN builds a connection string from environment or fallback values.
func DSN(name string) string {
	host := env("RASA_DB_HOST", "localhost")
	port := env("RASA_DB_PORT", "5432")
	user := env("RASA_DB_USER", "postgres")
	pass := env("RASA_DB_PASSWORD", "")
	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable", host, port, user, pass, name)
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// RegisterTypes registers custom types for pq driver.
func RegisterTypes() {
	_ = pq.Eawb // force import
}
