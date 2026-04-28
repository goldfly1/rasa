package policy

import (
	"database/sql"
	"os"
	"testing"

	_ "github.com/lib/pq"
)

// openTestDB returns a DB connection or skips the test if unavailable.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	pw := os.Getenv("RASA_DB_PASSWORD")
	if pw == "" {
		t.Skip("RASA_DB_PASSWORD not set")
	}
	host := os.Getenv("RASA_DB_HOST")
	if host == "" {
		host = "localhost"
	}
	port := os.Getenv("RASA_DB_PORT")
	if port == "" {
		port = "5432"
	}
	user := os.Getenv("RASA_DB_USER")
	if user == "" {
		user = "postgres"
	}
	dsn := "host=" + host + " port=" + port + " user=" + user + " password=" + pw + " dbname=rasa_policy sslmode=disable"
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Skip("cannot open policy DB: " + err.Error())
	}
	if err := db.Ping(); err != nil {
		db.Close()
		t.Skip("policy DB not reachable: " + err.Error())
	}
	return db
}
