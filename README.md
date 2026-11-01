# Telos
A Job Management Software to distribute computational heavy work between nodes utilizing a priority queue

## Running locally

1. Install PostgreSQL (16 or newer) and remember the password you set for the `postgres` user.
2. Create the database:
   ```
   psql -U postgres -c "CREATE DATABASE telos;"
   ```
3. Copy `.env.example` to `.env` and set `DB_PASSWORD` to your postgres password.
4. Start the server:
   ```
   go run ./cmd/server
   ```
   On start the server creates any missing tables from `internal/db/schema.sql`.
5. Open http://localhost:8080

Run the tests with `go test ./...`.
