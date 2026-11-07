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

## Running with Docker

1. Install Docker Desktop.
2. Copy `.env.example` to `.env` and set `DB_PASSWORD`.
3. Start everything:
   ```
   docker compose up --build
   ```
4. Open http://localhost:8080

Stop with `Ctrl+C`; `docker compose down -v` also deletes the database data.
If port 8080 is taken, change `PORT` in `.env`.

## Submitting a job (MVP: text processing)

`POST /api/jobs` with JSON. Typed/pasted text and an uploaded `.txt` file are sent the same way: the frontend puts the text in `content`.

```json
{ "user_id": "<uuid>", "name": "My job", "priority": "normal", "content": "text to process..." }
```

- `priority`: `high` | `normal` | `low` (optional, default `normal`)
- `queue`: optional, default `text-processing`
- Response `201`: `{ "id": "<uuid>", "status": "queued" }`
- Stored in `jobs` as `type = text_processing`, `input = {"content": "..."}`.
- The job ID is the database UUID. Show it shortened (e.g. `J-` + first 8 characters) in the UI if you like, but always use the full UUID in API calls.

`user_id` comes from the request for now; it will come from the login token once auth exists. Until then, create a dev user and use its id:

```
"INSERT INTO users (email, password_hash) VALUES ('dev@telos.local','dev-only') RETURNING id;" | docker compose exec -T db psql -U postgres -d telos
```

If your local database was created before this change, reset it with `docker compose down -v` (or drop and recreate the `telos` database).

## Checking on jobs

- `GET /api/jobs?user_id=<uuid>` lists that user's jobs, newest first (at most 100): `id`, `name`, `type`, `queue`, `priority`, `status`, `created_at`.
- `GET /api/jobs/{id}?user_id=<uuid>` returns one job: the fields above plus `updated_at`, `started_at`, `completed_at`, `error` and `result` (the result is only present once the job has completed).
- `status` is one of `queued`, `running`, `completed`, `failed`, `cancelled`.
- `404` if the job does not exist or belongs to another user; `400` if `user_id` is missing or an id is not a valid UUID.
- As with submitting, `user_id` comes from the query string for now and will come from the login token later.
