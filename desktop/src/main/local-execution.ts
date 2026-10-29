// Device enrollment and credentials belong to the main process, never the renderer.
import { safeStorage } from 'electron';
import { randomBytes, randomUUID } from 'node:crypto';
import { mkdir, open, readFile, rename, stat, unlink } from 'node:fs/promises';
import { dirname, isAbsolute } from 'node:path';
import type { LocalWorkerConfig, LocalWorkerState, LocalWorkerSummary } from '../../../protocol/generated/host';
import type { EnrollWorker } from '../../../protocol/generated/remote';
import { RemoteRequestError, type RemoteAdapter } from './remote';

type Form = { name: string; project_id: string; workspace_key: string; path: string; agent_id: string };
type Connection = { url: string; mode: 'client' | 'operator'; owner_id: string };
type Pending = { request_id: string; enrollment_secret: string; form: Form };
type RecordState = {
  version: 1; device_id: string; onboarding_complete: boolean; enabled: boolean;
  connection?: Connection; pending?: Pending; worker?: LocalWorkerConfig;
};
const actions = new Set(['local_worker_init','local_worker_enroll','local_worker_resume','local_worker_disable','local_worker_skip']);
type SetupErrorKey = 'invalid_configuration' | 'storage_unavailable' | 'storage_failed' | 'reauthenticate' | 'account_changed' | 'permission_denied' | 'stop_required' | 'server_unsupported' | 'enrollment_conflict' | 'server_unavailable' | 'network' | 'invalid_response' | 'setup_failed';
class SetupFailure extends Error {
  constructor(readonly key: SetupErrorKey) { super(key); }
}

function field(value: unknown, max = 128): string {
  if (typeof value !== 'string' || !value.trim() || value.length > max || /[\u0000-\u001f\u007f]/.test(value)) throw new SetupFailure('invalid_configuration');
  return value.trim();
}
function form(input: Record<string, unknown>): Form {
  const result = { name: field(input.name), project_id: field(input.project_id), workspace_key: field(input.workspace_key), path: field(input.path,4096), agent_id: field(input.agent_id) };
  if (!isAbsolute(result.path)) throw new SetupFailure('invalid_configuration');
  return result;
}
function sameConnection(a: Connection | undefined, b: Connection | undefined): boolean {
  return !!a && !!b && a.url === b.url && a.mode === b.mode && a.owner_id === b.owner_id;
}

export class LocalExecutionController {
  private record?: RecordState;
  private initialization?: Promise<void>;
  private queue: Promise<void> = Promise.resolve();
  private error: string | null = null;
  private errorKey: string | null = null;
  private errorStatus: number | null = null;
  private startedFor: string | null = null;
  private hostState?: LocalWorkerState;
  private closed = false;
  constructor(private path: string, private remote: RemoteAdapter, private host: (request: object) => void, private emit: (event: object) => void) {}

  private clearError() { this.error=null; this.errorKey=null; this.errorStatus=null; }
  private setError(key: SetupErrorKey, status: number | null = null) {
    this.error='Local execution setup could not complete.';
    this.errorKey=`worker.local.error.${key}`; this.errorStatus=status;
  }
  private capture(error: unknown) {
    if (error instanceof SetupFailure) { this.setError(error.key); return; }
    if (!(error instanceof RemoteRequestError)) { this.setError('setup_failed'); return; }
    if (error.kind !== 'http') { this.setError(error.kind); return; }
    const status=error.status ?? 0;
    const key: SetupErrorKey = status===404 || status===405 ? 'server_unsupported'
      : status===401 ? 'reauthenticate' : status===403 ? 'permission_denied'
      : status===409 ? 'enrollment_conflict' : status===400 || status===422 ? 'invalid_configuration'
      : status===408 || status===429 || status>=500 ? 'server_unavailable' : 'setup_failed';
    this.setError(key,error.status ?? null);
  }

  private connection(): Connection | undefined {
    const connection = this.remote.localWorkerConnection();
    if (!connection?.identity) return;
    return { url: connection.url, mode: connection.mode, owner_id: connection.identity.id };
  }
  private summary(): LocalWorkerSummary | null {
    const record = this.record;
    const worker = record?.worker;
    if (record?.pending && !record.enabled) {
      const selected = record.pending.form;
      return {id:worker?.id ?? '',name:selected.name,url:record.connection?.url ?? '',bindings:[
        {project_id:selected.project_id,workspace_key:selected.workspace_key,path:selected.path,agent_ids:[selected.agent_id]},
      ]};
    }
    if (!worker) return null;
    return { id: worker.id, name: worker.name, url: worker.url, bindings: worker.bindings };
  }
  private publish() {
    if (this.closed) return;
    this.emit({version:1,kind:'local_worker_setup',request_id:null,text:'',data:{
      onboarding_complete:this.record?.onboarding_complete ?? false,enabled:this.record?.enabled ?? false,
      has_connection:!!this.connection(),error:this.error,error_key:this.errorKey,error_status:this.errorStatus,configuration:this.summary(),
    }});
  }
  private async encryptionAvailable() {
    const available = await safeStorage.isAsyncEncryptionAvailable();
    const backend = process.platform === 'linux' ? safeStorage.getSelectedStorageBackend() : '';
    if (!available || backend === 'basic_text' || backend === 'unknown') throw new SetupFailure('storage_unavailable');
  }
  private async save(record: RecordState) {
    await this.encryptionAvailable();
    const encrypted = await safeStorage.encryptStringAsync(JSON.stringify(record));
    await mkdir(dirname(this.path),{recursive:true,mode:0o700});
    const temporary = `${this.path}.${randomUUID()}.tmp`;
    try {
      const file = await open(temporary,'wx',0o600);
      try { await file.writeFile(JSON.stringify({version:1,encrypted_record:encrypted.toString('base64')})); await file.sync(); }
      finally { await file.close(); }
      await rename(temporary,this.path);
    } catch {
      await unlink(temporary).catch(()=>{});
      throw new SetupFailure('storage_failed');
    }
    this.record = record;
  }
  private async initialize() {
    return this.initialization ??= (async()=>{
      await this.remote.initialize();
      try {
        const info = await stat(this.path);
        if (info.size > 128_000) throw new Error('Invalid device record');
        const envelope = JSON.parse(await readFile(this.path,'utf8'));
        if (envelope.version !== 1 || typeof envelope.encrypted_record !== 'string' || !/^[A-Za-z0-9+/]+={0,2}$/.test(envelope.encrypted_record)) throw new Error('Invalid encrypted device record');
        await this.encryptionAvailable();
        const decrypted = await safeStorage.decryptStringAsync(Buffer.from(envelope.encrypted_record,'base64'));
        const value = JSON.parse(decrypted.result) as RecordState;
        if (value.version !== 1 || typeof value.device_id !== 'string' || !value.device_id || typeof value.enabled !== 'boolean' || typeof value.onboarding_complete !== 'boolean') throw new Error('Invalid device record');
        if (value.pending) {
          field(value.pending.request_id); field(value.pending.enrollment_secret,64); form(value.pending.form);
        }
        if (value.worker && (!value.connection || value.worker.url !== value.connection.url || !value.worker.id || !value.worker.token || !Array.isArray(value.worker.bindings))) throw new Error('Invalid saved Worker configuration');
        this.record = value;
        if (decrypted.shouldReEncrypt) await this.save(value);
      } catch (error) {
        if ((error as NodeJS.ErrnoException).code !== 'ENOENT') {
          this.record = undefined;
          this.setError('storage_failed');
          return;
        }
        await this.save({version:1,device_id:randomUUID(),onboarding_complete:!!this.remote.localWorkerConnection(),enabled:false});
      }
    })().finally(()=>{if (!this.record) this.initialization=undefined;});
  }
  private stop() {
    if (this.startedFor || this.hostState && !['stopped','unavailable','needs_attention'].includes(this.hostState.status)) this.host({action:'local_worker_stop',request_id:randomUUID()});
    this.startedFor = null;
  }
  private async syncAccount() {
    if (this.closed) return;
    await this.initialize();
    if (this.closed) return;
    const record = this.record;
    const connection = this.connection();
    if (!record || !record.enabled || !record.worker || !sameConnection(record.connection,connection)) {
      this.stop();
      if (record?.enabled && record.worker && connection && !sameConnection(record.connection,connection)) {
        this.setError('account_changed');
      }
      this.publish(); return;
    }
    const account = this.remote.localWorkerConnection()!.identity!;
    if (account.role !== 'operator' && record.worker.bindings.some(binding=>!account.projects.includes(binding.project_id))) {
      this.stop(); this.setError('permission_denied'); this.publish(); return;
    }
    this.clearError();
    const key = `${connection!.url}/${connection!.mode}/${connection!.owner_id}/${record.pending?.request_id ?? record.worker.id}`;
    if (this.startedFor !== key) {
      this.host({action:'local_worker_start',request_id:randomUUID(),worker:record.worker});
      this.startedFor=key;
    }
    this.publish();
  }
  sync() {
    this.queue = this.queue.then(()=>this.syncAccount()).catch((error)=>{
      this.capture(error); this.publish();
    });
  }
  accept(message: {kind?: string; data?: unknown}) {
    if (message.kind !== 'local_worker_state') return;
    const state = message.data as LocalWorkerState;
    this.hostState = state;
    if (['needs_attention','unavailable'].includes(state.status)) {
      const workerID = state.worker_id;
      this.startedFor=null;
      this.queue=this.queue.then(async()=>{
        if (this.record?.enabled && (!workerID || this.record.worker?.id===workerID)) {
          await this.save({...this.record,enabled:false});
          this.publish();
        }
      }).catch((error)=>{this.capture(error);this.publish();});
    }
    // The supervisor owns temporary-network retries. Terminal attention needs
    // explicit user resume, including after restarting Desktop.
  }
  command(input: unknown): Promise<void> {
    const pending=this.queue.then(()=>this.perform(input));
    this.queue=pending.catch(()=>{});return pending;
  }
  private async perform(input: unknown) {
    if (!input || typeof input !== 'object' || Array.isArray(input) || JSON.stringify(input).length > 16_000) throw new Error('Invalid local execution command');
    const command=input as Record<string,unknown>;
    const action=String(command.action);
    if (!actions.has(action)) throw new Error('Unknown local execution command');
    try {
      await this.initialize();
      if (!this.record) {this.publish();return;}
      this.clearError();
      if (action==='local_worker_init') {
        await this.syncAccount();
        this.host({action:'local_worker_get',request_id:randomUUID()});
        return;
      }
      if (action==='local_worker_skip') {
        await this.save({...this.record,onboarding_complete:true});return;
      }
      if (action==='local_worker_disable') {
        await this.save({...this.record,enabled:false});this.stop();return;
      }
      const connection=this.connection();
      if (!connection) throw new SetupFailure('reauthenticate');
      if (action==='local_worker_resume') {
        if (!sameConnection(this.record.connection,connection)) throw new SetupFailure('account_changed');
        if (!this.record.pending) {
          if (!this.record.worker) throw new SetupFailure('invalid_configuration');
          await this.save({...this.record,enabled:true,onboarding_complete:true});
          this.startedFor=null;await this.syncAccount();return;
        }
        // Recover a lost enrollment reply before attempting an older token.
      }
      const selected=action==='local_worker_resume' && this.record.pending ? this.record.pending.form : form(command);
      const identity=this.remote.localWorkerConnection()!.identity!;
      if (identity.role!=='operator' && !identity.projects.includes(selected.project_id)) throw new SetupFailure('permission_denied');
      if (this.hostState && ['starting','idle','running','offline','stopping'].includes(this.hostState.status)) throw new SetupFailure('stop_required');
      const previous=this.record.pending;
      const samePending=sameConnection(this.record.connection,connection) && previous && JSON.stringify(previous.form)===JSON.stringify(selected);
      const enrollment: Pending=samePending ? previous : {request_id:randomUUID(),enrollment_secret:randomBytes(32).toString('hex'),form:selected};
      // This is the recovery point for an HTTP reply lost after server commit.
      await this.save({...this.record,enabled:false,connection,pending:enrollment});
      const body: EnrollWorker={device_id:this.record.device_id,request_id:enrollment.request_id,name:selected.name,projects:[selected.project_id],
        capabilities:[{project_id:selected.project_id,workspace_key:selected.workspace_key,agent_id:selected.agent_id}],enrollment_secret:enrollment.enrollment_secret};
      const receipt=await this.remote.enrollLocalWorker(body,connection);
      const worker: LocalWorkerConfig={id:receipt.worker.id,name:receipt.worker.name,url:connection.url,token:receipt.token,
        bindings:[{project_id:selected.project_id,workspace_key:selected.workspace_key,path:selected.path,agent_ids:[selected.agent_id]}]};
      await this.save({...this.record,connection,pending:undefined,worker,enabled:true,onboarding_complete:true});
      if (sameConnection(connection,this.connection())) await this.syncAccount();
      else {this.stop();this.setError('account_changed');}
    } catch (error) {
      // Network/server messages are not copied: a credential-bearing response
      // must never be reflected in public diagnostics or renderer events.
      this.capture(error);
    } finally {this.publish();}
  }
  close() {this.closed=true;this.startedFor=null;}
}
