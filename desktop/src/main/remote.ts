// The renderer sends bounded task actions; endpoint and credentials stay here.
import { randomUUID } from 'node:crypto';
import { shell } from 'electron';
import Ajv2020 from 'ajv/dist/2020.js';
import schema from '../../../protocol/generated/remote.schema.json';
import type { Detail, RemoteState, Task, TaskSpec, TaskPage, Worker, Identity, GitHubStatus, GitHubSyncResult } from '../../../protocol/generated/remote';

type Emit = (event: object) => void;
const ajv = new Ajv2020({ strict: false, allErrors: true });
ajv.addSchema(schema);
function checked<T>(name: string, value: unknown): T {
  const valid = ajv.getSchema(`${schema.$id}#/$defs/${name}`);
  if (!valid || !valid(value)) throw new Error(`Invalid remote ${name} response`);
  return value as T;
}
function text(value: unknown, max: number): string {
  if (typeof value !== 'string' || !value.trim() || Array.from(value).length > max) throw new Error('Invalid remote task field');
  return value.trim();
}
function id(value: unknown): string {
  const result = text(value, 128);
  if (!/^[a-zA-Z0-9_-]+$/.test(result)) throw new Error('Invalid task identifier');
  return result;
}
function endpoint(value: string | undefined): string | null {
  if (!value) return null;
  const url = new URL(value);
  const local = ['127.0.0.1', 'localhost', '[::1]'].includes(url.hostname);
  if ((url.protocol !== 'https:' && !(url.protocol === 'http:' && local)) || url.username || url.password || url.search || url.hash || url.pathname !== '/') {
    throw new Error('Remote URL requires HTTPS (HTTP is allowed on loopback for development)');
  }
  return url.href.replace(/\/$/, '');
}

export class RemoteAdapter {
  private base: string | null = null;
  private configError: string | null = null;
  private clientMode = !!process.env.PERO_CLIENT_TOKEN;
  private token = process.env.PERO_CLIENT_TOKEN || process.env.PERO_OPERATOR_TOKEN || '';
  private api = this.clientMode ? '/api/client/v1' : '/api/v1';
  private timer?: ReturnType<typeof setTimeout>;
  private busy = false;
  private closed = false;
  private queue: Promise<void> = Promise.resolve();
  private pages = 1;
  private state: RemoteState = {
    configured: false, loading: false, error: null, tasks: [], workers: [], has_more: false,
    selected_task_id: null, detail: null, identity: null, github: null,
  };
  constructor(private emit: Emit) {
    try { this.base = endpoint(process.env.PERO_CLOUD_URL); }
    catch (error) { this.configError = String((error as Error).message); }
    this.state.configured = !!this.base && !!this.token;
  }
  close() { this.closed = true; clearTimeout(this.timer); }
  private publish() {
    if (!this.closed) this.emit({ version: 1, kind: 'remote_state', request_id: null, text: '', data: this.state });
  }
  private async request(path: string, method = 'GET', body?: unknown): Promise<unknown> {
    const response = await fetch(`${this.base}${path}`, {
      method, redirect: 'error', signal: AbortSignal.timeout(8000),
      headers: { Authorization: `Bearer ${this.token}`, 'Content-Type': 'application/json',
        ...(method === 'POST' ? { 'Idempotency-Key': randomUUID() } : {}) },
      ...(body === undefined ? {} : { body: JSON.stringify(body) }),
    });
    const reader = response.body?.getReader();
    const chunks: Uint8Array[] = [];
    let size = 0;
    if (reader) {
      while (true) {
        const part = await reader.read();
        if (part.done) break;
        size += part.value.length;
        if (size > 2_000_000) { await reader.cancel(); throw new Error('Remote response is too large'); }
        chunks.push(part.value);
      }
    }
    const value = JSON.parse(Buffer.concat(chunks).toString('utf8'));
    if (!response.ok) throw new Error(`Remote ${response.status}: ${typeof value.error === 'string' ? value.error.slice(0, 300) : 'request failed'}`);
    return value;
  }
  private async load() {
    const [first, workerData, identity, github] = await Promise.all([
      this.request(`${this.api}/tasks?limit=50`),
      this.request(`${this.api}/workers`) as Promise<{ workers: unknown[] }>,
      this.request(`${this.api}/me`),
      this.clientMode ? Promise.resolve(null) : this.request('/api/v1/github/status'),
    ]);
    this.state.identity = checked<Identity>('Identity', identity);
    this.state.github = github === null ? null : checked<GitHubStatus>('GitHubStatus', github);
    let page = checked<TaskPage>('TaskPage', first);
    const tasks = [...page.tasks];
    for (let i = 1; i < this.pages && page.has_more && page.next_cursor; i++) {
      page = checked<TaskPage>('TaskPage', await this.request(`${this.api}/tasks?limit=50&cursor=${encodeURIComponent(page.next_cursor)}`));
      tasks.push(...page.tasks);
    }
    if (!Array.isArray(workerData.workers)) throw new Error('Invalid remote Worker list');
    this.state.tasks = tasks;
    this.state.has_more = page.has_more;
    this.state.workers = workerData.workers.map(value => checked<Worker>('Worker', value));
    if (this.state.selected_task_id) {
      this.state.detail = checked<Detail>('Detail', await this.request(`${this.api}/tasks/${id(this.state.selected_task_id)}`));
    }
  }
  command(input: unknown): Promise<void> {
    const pending = this.queue.then(() => this.perform(input));
    this.queue = pending.catch(() => {});
    return pending;
  }
  private async perform(input: unknown): Promise<void> {
    if (this.closed) return;
    const command = input as Record<string, unknown>;
    if (!command || typeof command !== 'object' || JSON.stringify(command).length > 260_000) throw new Error('Invalid remote command');
    const action = command.action;
    if (!['remote_refresh', 'remote_more', 'remote_select', 'remote_create', 'remote_dispatch', 'remote_cancel', 'remote_claim', 'remote_release', 'remote_sync_issues', 'remote_open_source'].includes(String(action))) throw new Error('Unknown remote action');
    clearTimeout(this.timer);
    if (!this.state.configured) {
      this.state.error = this.configError;
      this.publish();
      return;
    }
    this.busy = true;
    this.state.loading = true;
    this.state.error = null;
    this.publish();
    try {
      if (action === 'remote_more') this.pages += 1;
      if (action === 'remote_select') {
        this.state.selected_task_id = command.task_id === null ? null : id(command.task_id);
        this.state.detail = null;
      } else if (action === 'remote_create') {
        if (this.clientMode) throw new Error('Only the operator can create manual tasks');
        const spec: TaskSpec = {
          title: text(command.title, 240), prompt: text(command.prompt, 64000),
          acceptance: text(command.acceptance, 16000), project_id: text(command.project_id, 128),
          workspace_key: text(command.workspace_key, 128), agent_id: text(command.agent_id, 128),
        };
        const task = checked<Task>('Task', await this.request('/api/v1/tasks', 'POST', spec));
        this.state.selected_task_id = task.id;
      } else if (action === 'remote_dispatch' || action === 'remote_cancel' || action === 'remote_claim' || action === 'remote_release') {
        const taskID = id(command.task_id);
        const body = action === 'remote_dispatch' && command.worker_id ? { worker_id: id(command.worker_id) } : {};
        await this.request(`${this.api}/tasks/${taskID}/${String(action).slice('remote_'.length)}`, 'POST', body);
        this.state.selected_task_id = taskID;
      } else if (action === 'remote_sync_issues') {
        if (this.clientMode) throw new Error('Only the operator can sync GitHub sources');
        const synced = checked<GitHubSyncResult>('GitHubSyncResult', await this.request('/api/v1/github/sync', 'POST', {}));
        if (synced.partial) this.state.error = synced.errors.join('; ').slice(0, 500) || 'GitHub synchronization is incomplete';
      } else if (action === 'remote_open_source') {
        const detail = checked<Detail>('Detail', await this.request(`${this.api}/tasks/${id(command.task_id)}`));
        const source = detail.task.source;
        if (!source || source.kind !== 'github' || !/^[\w-]+\/[\w.-]+$/.test(source.repository) || !Number.isSafeInteger(source.issue_number) || source.issue_number < 1) throw new Error('Invalid GitHub source');
        const expected = `https://github.com/${source.repository}/issues/${source.issue_number}`;
        if (source.url !== expected) throw new Error('Invalid GitHub issue URL');
        await shell.openExternal(expected);
      }
      await this.load();
    } catch (error) {
      this.state.error = (error as Error).message || 'Remote request failed';
    } finally {
      this.busy = false;
      this.state.loading = false;
      this.publish();
      this.schedulePoll();
    }
  }
  private schedulePoll() {
    clearTimeout(this.timer);
    if (!this.closed) this.timer = setTimeout(() => {
      this.queue = this.queue.then(() => this.poll()).catch(() => {});
    }, 2000);
  }
  private async poll() {
    if (this.closed) return;
    this.busy = true;
    try { await this.load(); this.state.error = null; }
    catch (error) { this.state.error = (error as Error).message || 'Remote request failed'; }
    finally {
      this.busy = false;
      this.publish();
      this.schedulePoll();
    }
  }
}
