package db

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"golang.org/x/crypto/bcrypt"
)

func openSeedTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TELOS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TELOS_TEST_DATABASE_URL not set; skipping database tests")
	}

	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := Migrate(database); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`TRUNCATE events, tasks, jobs, workers, users CASCADE`); err != nil {
		t.Fatal(err)
	}
	return database
}

func TestDevelopmentIdentityEnabledOnlyForExplicitDevelopment(t *testing.T) {
	for _, environment := range []string{"", "test", "production", "Development"} {
		if DevelopmentIdentityEnabled(environment) {
			t.Errorf("environment %q unexpectedly enabled development identity", environment)
		}
	}
	if !DevelopmentIdentityEnabled("development") {
		t.Fatal("development environment did not enable development identity")
	}
}

func TestSeedDevelopmentUserRequiresConfiguration(t *testing.T) {
	tests := []struct {
		email, name, password string
	}{
		{"", "Telos Dev", "a-secure-development-password"},
		{"dev@telos.local", "", "a-secure-development-password"},
		{"dev@telos.local", "Telos Dev", "short"},
	}
	for _, tt := range tests {
		if _, err := SeedDevelopmentUser(nil, nil, tt.email, tt.name, tt.password); err == nil {
			t.Fatalf("SeedDevelopmentUser(%q, %q, ...) returned nil error", tt.email, tt.name)
		}
	}
}

func TestSeedDevelopmentUserIsIdempotent(t *testing.T) {
	database := openSeedTestDB(t)
	const (
		email    = "seed-dev@telos.local"
		name     = "Seed Development User"
		password = "a-secure-development-password"
	)

	firstID, err := SeedDevelopmentUser(context.Background(), database, email, name, password)
	if err != nil {
		t.Fatal(err)
	}
	secondID, err := SeedDevelopmentUser(context.Background(), database, email, "Changed Name", "another-secure-password")
	if err != nil {
		t.Fatal(err)
	}
	if firstID != secondID {
		t.Fatalf("seed IDs differ: first=%q second=%q", firstID, secondID)
	}

	var count int
	var storedName, storedHash, role string
	if err := database.QueryRow(
		`SELECT COUNT(*), MAX(name), MAX(password_hash), MAX(role) FROM users WHERE email = $1`,
		email,
	).Scan(&count, &storedName, &storedHash, &role); err != nil {
		t.Fatal(err)
	}
	if count != 1 || storedName != name || role != "user" {
		t.Fatalf("stored user = count %d name %q role %q", count, storedName, role)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(password)); err != nil {
		t.Fatalf("stored password hash does not match initial password: %v", err)
	}
	if bcrypt.CompareHashAndPassword([]byte(storedHash), []byte("another-secure-password")) == nil {
		t.Fatal("second seed overwrote the existing password")
	}
}

func TestSeedDevelopmentUserReturnsDatabaseID(t *testing.T) {
	database := openSeedTestDB(t)
	id, err := SeedDevelopmentUser(
		context.Background(), database,
		"seed-id@telos.local", "Seed ID User", "a-secure-development-password",
	)
	if err != nil {
		t.Fatal(err)
	}
	var storedID string
	if err := database.QueryRow(
		`SELECT id::text FROM users WHERE email = $1`, "seed-id@telos.local",
	).Scan(&storedID); err != nil {
		t.Fatal(err)
	}
	if id != storedID {
		t.Fatalf("returned ID %q, database ID %q", id, storedID)
	}
}

func TestSeedDevelopmentUserReportsDatabaseErrors(t *testing.T) {
	_, err := SeedDevelopmentUser(context.Background(), nil,
		"seed@telos.local", "Seed User", "a-secure-development-password")
	if err == nil || errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("err = %v, want database error", err)
	}
}
