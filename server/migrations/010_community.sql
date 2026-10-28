CREATE TABLE community_users (
    id bigint PRIMARY KEY CHECK (id > 0),
    login text NOT NULL,
    client_id text NOT NULL UNIQUE REFERENCES clients(id),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX community_login ON community_users(lower(login));
CREATE TABLE community_sessions (
    digest bytea PRIMARY KEY,
    user_id bigint REFERENCES community_users(id),
    operator boolean NOT NULL DEFAULT false,
    csrf text NOT NULL,
    expires_at timestamptz NOT NULL,
    CHECK ((user_id IS NOT NULL) <> operator)
);
CREATE INDEX community_session_expiry ON community_sessions(expires_at);
CREATE TABLE community_worker_owners (
    worker_id text PRIMARY KEY REFERENCES workers(id),
    user_id bigint NOT NULL REFERENCES community_users(id)
);
CREATE TABLE community_tasks (
    task_id text PRIMARY KEY REFERENCES tasks(id),
    title text NOT NULL,
    description text NOT NULL,
    project text NOT NULL,
    source_url text NOT NULL DEFAULT '',
    published_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE community_assignments (
    run_id text PRIMARY KEY REFERENCES runs(id),
    task_id text NOT NULL REFERENCES community_tasks(task_id),
    user_id bigint NOT NULL REFERENCES community_users(id),
    worker_id text NOT NULL REFERENCES workers(id),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE community_points (
    id text PRIMARY KEY,
    task_id text NOT NULL REFERENCES community_tasks(task_id),
    run_id text NOT NULL REFERENCES community_assignments(run_id),
    user_id bigint NOT NULL REFERENCES community_users(id),
    delta integer NOT NULL CHECK (delta IN (-1,1)),
    reverses text UNIQUE REFERENCES community_points(id),
    evidence_url text NOT NULL,
    note text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((delta = 1 AND reverses IS NULL) OR (delta = -1 AND reverses IS NOT NULL))
);
CREATE INDEX community_points_task ON community_points(task_id);
CREATE INDEX community_points_user ON community_points(user_id,created_at DESC);
