-- User-enabled local devices delegate existing project access, never create it.
CREATE TABLE worker_devices (
    worker_id text PRIMARY KEY REFERENCES workers(id),
    owner_id text NOT NULL,
    device_id text NOT NULL,
    capabilities jsonb NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    last_request_id text NOT NULL,
    UNIQUE(owner_id,device_id)
);
CREATE TABLE worker_enrollment_receipts (
    owner_id text NOT NULL,
    device_id text NOT NULL,
    request_id text NOT NULL,
    worker_id text NOT NULL REFERENCES workers(id),
    fingerprint bytea NOT NULL,
    PRIMARY KEY(owner_id,device_id,request_id)
);
