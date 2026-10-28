CREATE TABLE clients (
    id text PRIMARY KEY,
    name text NOT NULL,
    projects text[] NOT NULL,
    token_hash bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE tasks ADD COLUMN claimed_by_client_id text REFERENCES clients(id);
CREATE INDEX tasks_project ON tasks ((spec->>'project_id'),created_at DESC,id DESC);
