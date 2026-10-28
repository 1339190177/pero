CREATE TABLE github_repositories (
    project_id text NOT NULL,
    name text NOT NULL,
    repository_id text NOT NULL,
    last_synced_at timestamptz,
    partial boolean NOT NULL DEFAULT false,
    last_error text NOT NULL DEFAULT '',
    PRIMARY KEY(project_id,name)
);
CREATE TABLE github_issues (
    project_id text NOT NULL,
    repository_id text NOT NULL,
    issue_id text NOT NULL,
    node_id text NOT NULL,
    task_id text NOT NULL UNIQUE REFERENCES tasks(id),
    revision text NOT NULL,
    updated_at timestamptz NOT NULL,
    latest jsonb NOT NULL,
    PRIMARY KEY(project_id,repository_id,issue_id)
);
CREATE TABLE github_issue_versions (
    project_id text NOT NULL,
    repository_id text NOT NULL,
    issue_id text NOT NULL,
    revision text NOT NULL,
    observed_at timestamptz NOT NULL DEFAULT now(),
    payload jsonb NOT NULL,
    PRIMARY KEY(project_id,repository_id,issue_id,revision)
);
