/* Generated from protocol/remote.tsp by scripts/codegen/contracts.mjs. Do not edit. */

/**
 * Generated from protocol/remote.tsp. Do not edit.
 */
export type RemoteProtocol = RemoteProtocol1;
export type RemoteProtocol1 =
  | TaskSpec
  | Task
  | Run
  | Worker
  | RunEvent
  | RemoteState
  | TaskPage
  | EventReceipt
  | ProvisionWorker
  | WorkerCredential
  | Dispatch
  | Claim
  | ClaimResult
  | LeaseIdentity
  | RenewResult
  | EventBatch
  | Complete
  | ErrorResponse;
export type Id = string;
export type TaskStatus = "draft" | RunStatus;
export type RunStatus = "queued" | "running" | "cancel_requested" | "succeeded" | "failed" | "cancelled" | "reconcile";
export type Outcome = "succeeded" | "failed" | "cancelled" | "reconcile";

export interface TaskSpec {
  title: string;
  prompt: string;
  acceptance: string;
  project_id: Id;
  workspace_key: Id;
  agent_id: Id;
}
export interface Task {
  title: string;
  prompt: string;
  acceptance: string;
  project_id: Id;
  workspace_key: Id;
  agent_id: Id;
  id: Id;
  state: TaskStatus;
  current_run_id?: string;
  run?: Run | null;
  created_at?: string;
  updated_at?: string;
  [k: string]:
    | {
        [k: string]: unknown | undefined;
      }
    | string
    | TaskStatus
    | Run
    | null
    | undefined;
}
export interface Run {
  id: Id;
  task_id: Id;
  generation: number;
  worker_id?: string;
  instance_id?: string;
  status: RunStatus;
  lease_expires_at?: string | null;
  spec: TaskSpec;
  summary?: string;
  created_at?: string;
  updated_at?: string;
}
export interface Worker {
  id: Id;
  name: string;
  projects: string[];
  labels?: string[];
  last_seen_at?: string | null;
  created_at?: string;
}
export interface RunEvent {
  seq: number;
  kind: string;
  text: string;
  data?: unknown;
  created_at?: string;
}
export interface RemoteState {
  configured: boolean;
  loading: boolean;
  error: string | null;
  tasks: Task[];
  has_more: boolean;
  workers: Worker[];
  selected_task_id: string | null;
  detail: Detail | null;
}
export interface Detail {
  task: Task;
  run: Run | null;
  events: RunEvent[];
  events_truncated: boolean;
  events_first_seq: number | null;
  events_last_seq: number | null;
}
export interface TaskPage {
  tasks: Task[];
  has_more: boolean;
  next_cursor: string | null;
}
export interface EventReceipt {
  accepted: number;
  last_seq: number;
}
export interface ProvisionWorker {
  name: string;
  projects: string[];
  labels?: string[];
}
export interface WorkerCredential {
  worker: Worker;
  token: string;
}
export interface Dispatch {
  worker_id?: string;
}
export interface Claim {
  request_id: Id;
  instance_id: Id;
}
export interface ClaimResult {
  run: Run | null;
}
export interface LeaseIdentity {
  generation: number;
  instance_id: Id;
}
export interface RenewResult {
  cancel_requested: boolean;
  lease_expires_at: string;
}
export interface EventBatch {
  generation: number;
  instance_id: Id;
  events: RunEvent[];
  [k: string]:
    | {
        [k: string]: unknown | undefined;
      }
    | number
    | Id
    | RunEvent[]
    | undefined;
}
export interface Complete {
  generation: number;
  instance_id: Id;
  outcome: Outcome;
  summary: string;
  [k: string]:
    | {
        [k: string]: unknown | undefined;
      }
    | number
    | Id
    | Outcome
    | undefined;
}
export interface ErrorResponse {
  error: string;
}
