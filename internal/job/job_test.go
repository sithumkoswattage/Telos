package job

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"telos/internal/db"
	"telos/internal/model"
)

func openJobTestDB(t *testing.T) *sql.DB {
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

func TestCreateAssignsFrontendDefaults(t *testing.T) {
	database := openJobTestDB(t)

	var userID string
	if err := database.QueryRow(
		`INSERT INTO users (email, password_hash) VALUES ('job-test@telos.local', 'x') RETURNING id`,
	).Scan(&userID); err != nil {
		t.Fatal(err)
	}

	for _, priority := range []string{"high", "normal", "low"} {
		t.Run(priority, func(t *testing.T) {
			resp, err := Create(context.Background(), database, SubmitRequest{
				UserID:      userID,
				Type:        "text-processing",
				Priority:    priority,
				InputMethod: "text",
				Content:     "hello from the submit form",
				FileName:    "ignored.txt",
			})
			if err != nil {
				t.Fatal(err)
			}
			if resp.ID == "" || resp.Status != string(model.JobQueued) {
				t.Fatalf("response = %#v, want id and queued status", resp)
			}

			var name, typ, queue, storedPriority string
			var input []byte
			err = database.QueryRow(
				`SELECT name, type, queue, priority, input FROM jobs WHERE id = $1`,
				resp.ID,
			).Scan(&name, &typ, &queue, &storedPriority, &input)
			if err != nil {
				t.Fatal(err)
			}
			if name != DefaultJobName || typ != string(model.JobTypeTextProcessing) ||
				queue != DefaultQueue || storedPriority != priority {
				t.Fatalf("stored job = name %q type %q queue %q priority %q",
					name, typ, queue, storedPriority)
			}

			var storedInput map[string]string
			if err := json.Unmarshal(input, &storedInput); err != nil {
				t.Fatal(err)
			}
			if storedInput["content"] != "hello from the submit form" {
				t.Fatalf("stored input = %#v", storedInput)
			}
		})
	}
}

func TestCreateRejectsUnknownUser(t *testing.T) {
	database := openJobTestDB(t)
	_, err := Create(context.Background(), database, SubmitRequest{
		UserID:      "96f2fd54-1afb-4cfe-9116-35c740888656",
		Type:        "text_processing",
		Priority:    "normal",
		InputMethod: "file",
		Content:     "file content",
		FileName:    "document.txt",
	})
	if err != ErrUnknownUser {
		t.Fatalf("err = %v, want ErrUnknownUser", err)
	}
}
