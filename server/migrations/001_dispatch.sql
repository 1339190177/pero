CREATE TABLE tasks (
    id text PRIMARY KEY,
    spec jsonb NOT NULL,
    current_run_id text,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE workers (
    id text PRIMARY KEY,
    name text NOT NULL,
    projects text[] NOT NULL,
    labels text[] NOT NULL DEFAULT '{}',
    token_hash bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE runs (
    id text PRIMARY KEY,
    task_id text NOT NULL REFERENCES tasks(id),
    generation bigint NOT NULL,
    spec jsonb NOT NULL,
    target_worker_id text REFERENCES workers(id),
    worker_id text REFERENCES workers(id),
    instance_id text,
    status text NOT NULL CHECK (status IN ('queued','running','cancel_requested','succeeded','failed','cancelled','reconcile')),
    lease_expires_at timestamptz,
    summary text NOT NULL DEFAULT '',
    completion jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(task_id, generation)
);
CREATE UNIQUE INDEX one_active_run_per_task ON runs(task_id)
    WHERE status IN ('queued','running','cancel_requested');
CREATE INDEX queued_runs ON runs(created_at) WHERE status = 'queued';
CREATE TABLE claims (
    worker_id text NOT NULL REFERENCES workers(id),
    instance_id text NOT NULL,
    request_id text NOT NULL,
    run_id text REFERENCES runs(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(worker_id, instance_id, request_id)
);
CREATE TABLE run_events (
    run_id text NOT NULL REFERENCES runs(id),
    generation bigint NOT NULL,
    seq bigint NOT NULL CHECK (seq > 0),
    kind text NOT NULL,
    text text NOT NULL,
    data jsonb NOT NULL DEFAULT 'null',
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(run_id, generation, seq)
);
CREATE TABLE operator_receipts (
    key text PRIMARY KEY,
    operation text NOT NULL,
    digest text NOT NULL,
    resource_id text NOT NULL
);
