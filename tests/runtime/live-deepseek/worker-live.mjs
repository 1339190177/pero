// Opt-in live model component. Importing this module performs no requests.
// Credentials are supplied by the caller in memory, never discovered or persisted.
import { createServer } from 'node:http';
import { spawn, execFileSync } from 'node:child_process';
import { mkdir, mkdtemp, writeFile, readFile, readdir, lstat, rm } from 'node:fs/promises';
import { join, isAbsolute } from 'node:path';
import { randomBytes, createHash } from 'node:crypto';
export const networkEnvironment = () => {
  const env=Object.fromEntries(['HTTPS_PROXY','HTTP_PROXY','NO_PROXY','SSL_CERT_FILE','SSL_CERT_DIR'].filter(name => typeof process.env[name] === 'string').map(name => [name,process.env[name]]));
  env.NO_PROXY=[env.NO_PROXY,'localhost','127.0.0.1','::1'].filter(Boolean).join(',');
  return env;
};
export async function verifyReadWorkspace(workspace, expectedContent) {
 const result={sourceUnchanged:false,allowedFilesOnly:false,privateLockValid:false};
 try {
  const names=(await readdir(workspace)).sort();
  result.allowedFilesOnly=JSON.stringify(names)===JSON.stringify(['.pero-worker.lock','source.txt']);
  const source=await lstat(join(workspace,'source.txt'));
  result.sourceUnchanged=source.isFile()&&!source.isSymbolicLink()&&source.nlink===1&&await readFile(join(workspace,'source.txt'),'utf8')===expectedContent;
  const lock=await lstat(join(workspace,'.pero-worker.lock'));
  result.privateLockValid=lock.isFile()&&!lock.isSymbolicLink()&&lock.nlink===1&&lock.size===0&&(lock.mode&0o777)===0o600&&(typeof process.getuid!=='function'||lock.uid===process.getuid());
 } catch {}
 return result;
}
const wait = ms => new Promise(resolve => setTimeout(resolve, ms));
const digest = async path => createHash('sha256').update(await readFile(path)).digest('hex');
const requireThat = (value, label) => { if (!value) throw new Error(label); };
const send = (res, value, status = 200) => { res.writeHead(status, {'content-type':'application/json'}); res.end(JSON.stringify(value)); };
const readBody = async req => { const chunks=[]; let size=0; for await (const b of req) { size+=b.length; if(size>1024*1024) throw new Error('cloud_body_limit'); chunks.push(b); } return JSON.parse(Buffer.concat(chunks).toString() || '{}'); };

// Pure diagnostics: no untrusted strings are returned, including tool IDs/arguments.
export function summarizeWorkerEvents(events, completes, nonce) {
  const kinds=new Set(['session_ready','session_update','session_error','session_closed','session_auth_required','session_auth_error','turn_finished','permission','permission_denied']);
  const names=new Set(['read_file','write_file','edit_file','list_directory','search_files','shell']);
  const statuses=new Set(['pending','in_progress','completed','failed']);
  const stops=new Set(['end_turn','max_tokens','max_turn_requests','refusal','cancelled']);
  const outcomes=new Set(['succeeded','failed','cancelled']);
  const d={eventKinds:{},toolNames:{},toolStatuses:{},matchedPath:false,nonceMatched:false,completionOutcomes:{},turnStopReasons:{},sessionErrors:{}};
  const bump=(map,label)=>{map[label]=(map[label]||0)+1;};
  const matchingIds=new Set();
  for(const e of events){
    bump(d.eventKinds,kinds.has(e.kind)?e.kind:'other');
    if(e.kind==='session_update'&&e.data?.sessionUpdate==='tool_call'){
      bump(d.toolNames,names.has(e.data.title)?e.data.title:'unknown');
      if(e.data.title==='read_file'&&e.data.rawInput?.path==='source.txt'){d.matchedPath=true;matchingIds.add(e.data.toolCallId);}
    }
    if(e.kind==='session_update'&&e.data?.sessionUpdate==='tool_call_update')bump(d.toolStatuses,statuses.has(e.data.status)?e.data.status:'unknown');
    if(e.kind==='turn_finished')bump(d.turnStopReasons,stops.has(e.data?.stopReason)?e.data.stopReason:'unknown');
    if(e.kind==='session_error'){
      const text=typeof e.text==='string'?e.text:'';
      const http=text.match(/openai adapter: HTTP ([1-5][0-9]{2})(?![0-9])/);
      const label=http?'http_'+http[1]:text.includes('provider request budget exhausted')?'budget_exhausted':text.includes('unsupported reasoning effort')?'unsupported_reasoning':text.includes('context deadline exceeded')?'deadline_exceeded':text.includes('context canceled')?'cancelled':'unknown';
      bump(d.sessionErrors,label);
    }
  }
  d.nonceMatched=typeof nonce==='string'&&nonce.length>0&&events.some(e=>e.kind==='session_update'&&e.data?.sessionUpdate==='tool_call_update'&&matchingIds.has(e.data.toolCallId)&&e.data.status==='completed'&&JSON.stringify(e.data.content??null).includes(nonce));
  for(const c of completes)bump(d.completionOutcomes,outcomes.has(c.outcome)?c.outcome:'unknown');
  return d;
}

export async function runWorker({worker, agent, model, key, root, budgetArgs}) {
  const receipt={scene:'worker-live-read',passed:false,endpoint:'https://api.deepseek.com',permissionPolicy:'worker-deny',writeExecutionClaimed:false};
  let ownRoot, cloud, child, deadline, closed, nonce, forced=false, failure='setup_failed';
  const events=[], completes=[]; let cloudFailed=false, outputLeak=false;
  const token=randomBytes(24).toString('hex');
  try {
    requireThat([worker,agent,root].every(p=>typeof p==='string'&&isAbsolute(p)), 'invalid_paths');
    requireThat(typeof key==='string'&&key.length>0&&typeof model==='string'&&model.length>0, 'invalid_configuration');
    requireThat(JSON.stringify(budgetArgs)===JSON.stringify(['--max-requests','4','--max-tokens','512','--max-retries','0','--disable-thinking','--use-env-proxy']), 'invalid_budget');
    receipt.binaryHashes={worker:await digest(worker),agent:await digest(agent)};
    ownRoot=await mkdtemp(join(root,'worker-live-')); await mkdir(join(ownRoot,'workspace'),{mode:0o700}); await mkdir(join(ownRoot,'sessions'),{mode:0o700});
    const workspace=join(ownRoot,'workspace'), sessions=join(ownRoot,'sessions');
    nonce=randomBytes(24).toString('hex'); const content=`Live read verification: ${nonce}\n`;
    await writeFile(join(workspace,'source.txt'),content,{mode:0o600});
    const expiry=()=>new Date(Date.now()+5000).toISOString();
    const run={id:'live_read_run',generation:1,instance_id:'',status:'running',lease_expires_at:expiry(),spec:{title:'Read-only live check',prompt:'Use read_file to read source.txt in the current workspace, then report its complete contents. Do not write or edit files. Do not use shell or any other tools.',workspace_key:'live',agent_id:'live',acceptance:''}};
    cloud=createServer(async(req,res)=>{try{
      requireThat(req.headers.authorization===`Bearer ${token}`,'cloud_auth');const data=await readBody(req);
      if(JSON.stringify(data).includes(key)) { outputLeak=true; throw new Error('secret_in_events'); }
      if(req.url.endsWith('/claims')) {run.instance_id=data.instance_id; run.lease_expires_at=expiry(); return send(res,{run});}
      if(req.url.endsWith('/renew')) return send(res,{lease_expires_at:expiry(),cancel_requested:false});
      if(req.url.endsWith('/events')) {for(const event of data.events)if(!events.some(e=>e.seq===event.seq))events.push(event);requireThat(events.length<=1000,'event_limit');return send(res,{last_seq:events.at(-1)?.seq||0});}
      if(req.url.endsWith('/complete')) {completes.push(data);run.status=data.outcome;return send(res,run);}
      send(res,run);
    }catch {cloudFailed=true;send(res,{error:'local_fixture_error'},500);}});
    await new Promise((ok,bad)=>{cloud.once('error',bad);cloud.listen(0,'127.0.0.1',ok);});
    const config=join(ownRoot,'config.json');
    await writeFile(join(ownRoot,'agents.json'),JSON.stringify([{id:'live',name:'Live read check',command:agent,args:['--endpoint','https://api.deepseek.com','--model',model,'--session-dir',sessions,'--api-key-env','DEEPSEEK_API_KEY','--workspace',workspace,'--approval','ask',...budgetArgs],env:{}}]),{mode:0o600});
    await writeFile(config,JSON.stringify({workspaces:{live:workspace},agents_file:join(ownRoot,'agents.json')}),{mode:0o600});
    failure='worker_failed';
    child=spawn(worker,['--once'],{env:{...networkEnvironment(),PATH:'/usr/local/bin:/usr/bin:/bin',HOME:ownRoot,TMPDIR:ownRoot,LANG:'C.UTF-8',DEEPSEEK_API_KEY:key,PERO_CLOUD_URL:`http://127.0.0.1:${cloud.address().port}`,PERO_WORKER_TOKEN:token,PERO_WORKER_CONFIG:config,PERO_WORKER_STATE:join(ownRoot,'state.json')},stdio:['ignore','pipe','pipe']});
    // Discard all child output. Keep only a sliding secret detector, never an error body.
    for(const stream of [child.stdout,child.stderr]) {let tail='';stream.on('data',b=>{const text=tail+b.toString();if(text.includes(key))outputLeak=true;tail=text.slice(-Math.max(0,key.length-1));});}
    closed=new Promise(resolve=>{child.once('error',()=>resolve(-1));child.once('close',code=>resolve(code));});
    deadline=setTimeout(()=>{failure='deadline_exceeded';child.kill('SIGKILL');},55000);
    const code=await closed;clearTimeout(deadline);
    requireThat(code===0&&!cloudFailed&&!outputLeak,'worker_failed');
    receipt.workspaceVerification=await verifyReadWorkspace(workspace,content);
    failure='tool_verification_failed';
    const calls=events.filter(e=>e.kind==='session_update'&&e.data?.sessionUpdate==='tool_call'&&e.data.title==='read_file'&&e.data.rawInput?.path==='source.txt');
    const toolVerified=calls.some(c=>events.some(e=>e.kind==='session_update'&&e.data?.sessionUpdate==='tool_call_update'&&e.data.toolCallId===c.data.toolCallId&&e.data.status==='completed'&&JSON.stringify(e.data.content).includes(nonce)));
    requireThat(toolVerified&&completes.at(-1)?.outcome==='succeeded','tool_verification_failed');
    failure='workspace_verification_failed';
    requireThat(Object.values(receipt.workspaceVerification).every(Boolean),'workspace_modified');
    requireThat(await digest(worker)===receipt.binaryHashes.worker&&await digest(agent)===receipt.binaryHashes.agent,'binary_changed');
    receipt.toolCompleted=true;receipt.nonceVerified=true;receipt.outcome='succeeded';receipt.permissionDenials=events.filter(e=>e.kind==='permission_denied').length;receipt.passed=true;
  } catch { receipt.error=failure; }
  finally {
    receipt.diagnostics=summarizeWorkerEvents(events,completes,nonce);
    clearTimeout(deadline);
    if(child&&child.exitCode===null&&child.signalCode===null) {child.kill('SIGKILL');if(closed)await closed;}
    if(cloud){cloud.closeAllConnections();await new Promise(resolve=>cloud.close(resolve));}
    if(ownRoot){
      const pids=()=>execFileSync('/bin/ps',['-eo','pid=,args='],{encoding:'utf8'}).split('\n').filter(line=>line.includes(agent)&&line.includes(join(ownRoot,'sessions'))).map(line=>Number(line.trim().split(/\s+/)[0]));
      try {for(let i=0;i<10&&pids().length;i++)await wait(50);forced=pids().length>0;for(const pid of pids())try{process.kill(pid,'SIGKILL');}catch{};for(let i=0;i<10&&pids().length;i++)await wait(50);if(forced||pids().length){receipt.passed=false;receipt.error='owned_process_cleanup_required';}}
      catch {receipt.passed=false;receipt.error='cleanup_failed';}
      try {await rm(ownRoot,{recursive:true,force:true});}catch {receipt.passed=false;receipt.error='cleanup_failed';}
    }
  }
  return receipt;
}
