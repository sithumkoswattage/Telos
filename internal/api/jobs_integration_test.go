package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"telos/internal/db"
)

func openAPIJobTestDB(t *testing.T) *sql.DB {
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
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`TRUNCATE events, tasks, jobs, workers, users CASCADE`); err != nil {
		t.Fatal(err)
	}
	return database
}

func TestSubmitUsesServerIdentityInsteadOfClientUserID(t *testing.T) {
	database := openAPIJobTestDB(t)
	var serverUserID, clientUserID string
	if err := database.QueryRow(
		`INSERT INTO users (email, password_hash) VALUES ('server@telos.local', 'x') RETURNING id::text`,
	).Scan(&serverUserID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(
		`INSERT INTO users (email, password_hash) VALUES ('client@telos.local', 'x') RETURNING id::text`,
	).Scan(&clientUserID); err != nil {
		t.Fatal(err)
	}

	h := &JobsHandler{DB: database, DevelopmentUserID: serverUserID}
	body := `{"user_id":"` + clientUserID + `","type":"text-processing","priority":"normal","input_method":"text","content":"server-owned job"}`
	rec := httptest.NewRecorder()
	h.Submit(rec, httptest.NewRequest(http.MethodPost, "/api/jobs", strings.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var response struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	var ownerID string
	if err := database.QueryRowContext(context.Background(),
		`SELECT user_id::text FROM jobs WHERE id = $1`, response.ID,
	).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	if ownerID != serverUserID {
		t.Fatalf("job owner = %q, want server identity %q", ownerID, serverUserID)
	}
}
