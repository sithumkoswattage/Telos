package db

import (
	"database/sql"
	_ "embed"
	"fmt"
	"log"
)

// schema.sql is compiled into the binary, so the server and Docker images
// don't need the file on disk.
//
//go:embed schema.sql
var schemaSQL string

// Migrate creates any missing tables and indexes. It is safe to run on every
// start because every statement in schema.sql uses IF NOT EXISTS.
func Migrate(database *sql.DB) error {
	if _, err := database.Exec(schemaSQL); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	log.Println("Database schema is up to date")
	return nil
}
