CREATE TABLE roadmap_items (
    project_id text NOT NULL,
    repository_id text NOT NULL,
    path text NOT NULL,
    item_id text NOT NULL,
    task_id text NOT NULL UNIQUE REFERENCES tasks(id),
    latest jsonb NOT NULL,
    PRIMARY KEY(project_id,repository_id,path,item_id)
);
CREATE TABLE roadmap_versions (
    project_id text NOT NULL,
    repository_id text NOT NULL,
    path text NOT NULL,
    item_id text NOT NULL,
    revision text NOT NULL,
    payload jsonb NOT NULL,
    observed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(project_id,repository_id,path,item_id,revision)
);
