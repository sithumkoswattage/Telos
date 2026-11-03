-- Telos database schema.
-- Applied automatically on server start by db.Migrate(). Every statement is
-- idempotent (IF NOT EXISTS), so running it again is safe.
--
-- The allowed values for status / priority / role must match internal/model.

-- ---------------------------------------------------------------- users
CREATE TABLE IF NOT EXISTS users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email         TEXT        NOT NULL UNIQUE,
    name          TEXT        NOT NULL DEFAULT '',
    password_hash TEXT        NOT NULL,            -- bcrypt/argon2, never plain text
    role          TEXT        NOT NULL DEFAULT 'user'
                  CHECK (role IN ('admin', 'user')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- -------------------------------------------------------------- workers
-- Workers authenticate with a secret token (only its hash is stored).
CREATE TABLE IF NOT EXISTS workers (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name           TEXT        NOT NULL UNIQUE,
    token_hash     TEXT        NOT NULL,
    status         TEXT        NOT NULL DEFAULT 'offline'
                   CHECK (status IN ('online', 'offline')),
    last_heartbeat TIMESTAMPTZ,
    registered_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ----------------------------------------------------------------- jobs
-- Progress is NOT stored: it is computed from tasks (done / total).
CREATE TABLE IF NOT EXISTS jobs (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type         TEXT        NOT NULL CHECK (type IN ('matrix_multiply')),
    priority     TEXT        NOT NULL DEFAULT 'normal'
                 CHECK (priority IN ('high', 'normal', 'low')),
    status       TEXT        NOT NULL DEFAULT 'queued'
                 CHECK (status IN ('queued', 'running', 'completed', 'failed', 'cancelled')),
    input        JSONB,
    result       JSONB,
    error        TEXT        NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at   TIMESTAMPTZ,
    completed_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS jobs_user_idx   ON jobs (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS jobs_queued_idx ON jobs (priority, created_at) WHERE status = 'queued';

-- ---------------------------------------------------------------- tasks
CREATE TABLE IF NOT EXISTS tasks (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    job_id       UUID        NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    task_index   INT         NOT NULL CHECK (task_index >= 0),
    priority     TEXT        NOT NULL CHECK (priority IN ('high', 'normal', 'low')),
    status       TEXT        NOT NULL DEFAULT 'pending'
                 CHECK (status IN ('pending', 'assigned', 'running', 'done', 'failed')),
    worker_id    UUID        REFERENCES workers(id) ON DELETE SET NULL,
    attempts     INT         NOT NULL DEFAULT 0,
    max_attempts INT         NOT NULL DEFAULT 3,
    input        JSONB,
    result       JSONB,
    error        TEXT        NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    assigned_at  TIMESTAMPTZ,
    started_at   TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,

    CONSTRAINT tasks_attempts_check CHECK (attempts >= 0 AND attempts <= max_attempts),
    -- A task that a worker holds must say which worker.
    CONSTRAINT tasks_worker_when_active_check
        CHECK (status NOT IN ('assigned', 'running') OR worker_id IS NOT NULL),
    -- One task per position in a job (used to reassemble results in order).
    CONSTRAINT tasks_job_index_uniq UNIQUE (job_id, task_index)
);

-- Scheduler: find pending tasks by priority, oldest first.
CREATE INDEX IF NOT EXISTS tasks_pending_idx    ON tasks (priority, created_at) WHERE status = 'pending';
-- Progress: SELECT status, COUNT(*) FROM tasks WHERE job_id = $1 GROUP BY status
CREATE INDEX IF NOT EXISTS tasks_job_status_idx ON tasks (job_id, status);
-- Fault tolerance: find tasks held by a worker that went silent.
CREATE INDEX IF NOT EXISTS tasks_worker_idx     ON tasks (worker_id) WHERE status IN ('assigned', 'running');

-- --------------------------------------------------------------- events
-- Append-only log for the job details page and for the evaluation metrics.
CREATE TABLE IF NOT EXISTS events (
    id         BIGSERIAL PRIMARY KEY,
    job_id     UUID REFERENCES jobs(id)    ON DELETE CASCADE,
    task_id    UUID REFERENCES tasks(id)   ON DELETE CASCADE,
    worker_id  UUID REFERENCES workers(id) ON DELETE SET NULL,
    type       TEXT        NOT NULL,   -- e.g. job.submitted, task.assigned, worker.lost
    message    TEXT        NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS events_job_idx ON events (job_id, created_at);
