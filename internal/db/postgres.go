package db

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib"
)

var DB *sql.DB

// Connect initializes the PostgreSQL connection pool using environment variables.
func Connect() (*sql.DB, error) {
	host := os.Getenv("DB_HOST")
	port := os.Getenv("DB_PORT")
	user := os.Getenv("DB_USER")
	password := os.Getenv("DB_PASSWORD")
	dbname := os.Getenv("DB_NAME")
	sslmode := os.Getenv("DB_SSLMODE")

	if sslmode == "" {
		sslmode = "disable"
	}

	dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s", user, password, host, port, dbname, sslmode)

	database, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("unable to open database connection: %w", err)
	}

	if err := database.Ping(); err != nil {
		return nil, fmt.Errorf("database unreachable: %w", err)
	}

	log.Printf("Connected to PostgreSQL database: %s\n", dbname)
	DB = database
	return DB, nil
}
