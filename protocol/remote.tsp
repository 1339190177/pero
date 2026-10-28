// Operator and Worker HTTP protocol. Credentials never enter renderer snapshots.
import "@typespec/json-schema";
using JsonSchema;
@jsonSchema
namespace Pero.Remote;

model Closed { ...Record<never>; }
@minLength(1) @maxLength(128) scalar Id extends string;
union RunStatus { "queued", "running", "cancel_requested", "succeeded", "failed", "cancelled", "reconcile" }
union TaskStatus { "draft", RunStatus }
union Outcome { "succeeded", "failed", "cancelled", "reconcile" }

model TaskSpec {
  @minLength(1) @maxLength(240) title: string;
  @minLength(1) @maxLength(64000) prompt: string;
  @minLength(1) @maxLength(16000) acceptance: string;
  project_id: Id;
  workspace_key: Id;
  agent_id: Id;
  ...Closed;
}
model Run {
  id: Id;
  task_id: Id;
  generation: safeint;
  worker_id?: string;
  instance_id?: string;
  status: RunStatus;
  lease_expires_at?: string | null;
  spec: TaskSpec;
  summary?: string;
  created_at?: string;
  updated_at?: string;
  ...Closed;
}
model Task {
  ...TaskSpec;
  id: Id;
  state: TaskStatus;
  current_run_id?: string;
  run?: Run | null;
  created_at?: string;
  updated_at?: string;
  ...Closed;
}
model Worker {
  id: Id;
  name: string;
  projects: string[];
  labels?: string[];
  last_seen_at?: string | null;
  created_at?: string;
  ...Closed;
}
model RunEvent {
  seq: safeint;
  kind: string;
  text: string;
  data?: unknown;
  created_at?: string;
  ...Closed;
}
model Detail {
  task: Task;
  run: Run | null;
  events: RunEvent[];
  events_truncated: boolean;
  events_first_seq: safeint | null;
  events_last_seq: safeint | null;
  ...Closed;
}
model TaskPage { tasks: Task[]; has_more: boolean; next_cursor: string | null; ...Closed }
model EventReceipt { accepted: safeint; last_seq: safeint; ...Closed }
model RemoteState {
  configured: boolean;
  loading: boolean;
  error: string | null;
  tasks: Task[];
  has_more: boolean;
  workers: Worker[];
  selected_task_id: string | null;
  detail: Detail | null;
  ...Closed;
}
model ProvisionWorker { name: string; projects: string[]; labels?: string[]; ...Closed }
model WorkerCredential { worker: Worker; token: string; ...Closed }
model Dispatch { worker_id?: string; ...Closed }
model Claim { request_id: Id; instance_id: Id; ...Closed }
model ClaimResult { run: Run | null; ...Closed }
model LeaseIdentity { generation: safeint; instance_id: Id; ...Closed }
model RenewResult { cancel_requested: boolean; lease_expires_at: string; ...Closed }
model EventBatch { ...LeaseIdentity; events: RunEvent[]; ...Closed }
model Complete { ...LeaseIdentity; outcome: Outcome; @maxLength(16000) summary: string; ...Closed }
model ErrorResponse { error: string; ...Closed }

@oneOf
union RemoteProtocol { TaskSpec, Task, Run, Worker, RunEvent, RemoteState, TaskPage, EventReceipt, ProvisionWorker, WorkerCredential, Dispatch, Claim, ClaimResult, LeaseIdentity, RenewResult, EventBatch, Complete, ErrorResponse }
