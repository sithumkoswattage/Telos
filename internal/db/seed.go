package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// DevelopmentIdentityEnabled reports whether the explicitly requested
// development-only identity should be enabled.
func DevelopmentIdentityEnabled(environment string) bool {
	return environment == "development"
}

// SeedDevelopmentUser creates the configured development user if it does not
// exist and returns its database-generated UUID. Existing users are unchanged.
func SeedDevelopmentUser(ctx context.Context, database *sql.DB, email, name, password string) (string, error) {
	email = strings.TrimSpace(email)
	name = strings.TrimSpace(name)
	if email == "" || name == "" || len(password) < 16 {
		return "", errors.New("development user email, name, and password are required")
	}
	if database == nil {
		return "", errors.New("database is required")
	}

	var userID string
	err := database.QueryRowContext(ctx,
		`SELECT id::text FROM users WHERE email = $1`, email,
	).Scan(&userID)
	if err == nil {
		return userID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("find development user: %w", err)
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash development user password: %w", err)
	}

	err = database.QueryRowContext(ctx,
		`INSERT INTO users (email, name, password_hash, role)
		 VALUES ($1, $2, $3, 'user')
		 ON CONFLICT (email) DO NOTHING
		 RETURNING id::text`,
		email, name, string(passwordHash),
	).Scan(&userID)
	if err == nil {
		return userID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("create development user: %w", err)
	}

	if err := database.QueryRowContext(ctx,
		`SELECT id::text FROM users WHERE email = $1`, email,
	).Scan(&userID); err != nil {
		return "", fmt.Errorf("find development user after conflict: %w", err)
	}
	return userID, nil
}
