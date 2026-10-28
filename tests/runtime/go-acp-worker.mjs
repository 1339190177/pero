// Opt-in real Go ACP executable + real Rust Worker, using only loopback fixtures.
// Usage: node tests/runtime/go-acp-worker.mjs /absolute/pero-worker /absolute/pero-agent-acp
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { spawn, execFileSync } from 'node:child_process';
import { mkdtemp, mkdir, writeFile, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { createHash } from 'node:crypto';

const worker = resolve(process.argv[2] || 'target/debug/pero-worker');
const agent = resolve(process.argv[3] || 'target/debug/pero-agent-acp');
const permissionAgent = process.argv[4] ? resolve(process.argv[4]) : null;
const hash = async path => createHash('sha256').update(await readFile(path)).digest('hex');
const binaryHashes = { worker: await hash(worker), agent: await hash(agent) };
if(permissionAgent) binaryHashes.permissionAgent=await hash(permissionAgent);
const listen = async server => { await new Promise(ok => server.listen(0, '127.0.0.1', ok)); return `http://127.0.0.1:${server.address().port}`; };
const jsonBody = async request => { let raw = ''; for await (const b of request) raw += b; return raw ? JSON.parse(raw) : {}; };
const send = (response, body, status = 200) => { response.writeHead(status, {'content-type':'application/json'}); response.end(JSON.stringify(body)); };
const sleep = ms => new Promise(ok => setTimeout(ok, ms));
const expiry = () => new Date(Date.now() + 3000).toISOString();
const textStream = response => {
 response.writeHead(200, {'content-type':'text/event-stream'});
 response.end('data: {"choices":[{"index":0,"delta":{"content":"fixture answer"},"finish_reason":"stop"}]}\n\ndata: [DONE]\n\n');
};

async function scenario(name) {
 const root = await mkdtemp(join(tmpdir(), 'pero-go-acp-e2e-'));
 await mkdir(join(root, 'workspace'), {mode:0o700}); await mkdir(join(root, 'sessions'), {mode:0o700});
 const state = { events: [], requests: [], completes: [], claims: [], renewals: 0, providerClosed: 0 };
 const timers = new Set(); const errors = []; const children = new Set();
 let didKill = false;
 const provider = createServer(async (request, response) => {
  try {
   assert.equal(request.url, '/v1/chat/completions');
   assert.equal(request.headers.authorization, 'Bearer fixture-provider-secret');
   for (const key of Object.keys(request.headers)) assert(!/deepseek|point/.test(key), `product header: ${key}`);
   const body = await jsonBody(request); state.requests.push(body);
   assert.equal(body.model, 'fixture'); assert(!('dsh_session_log' in body));
   if (['cancel', 'lease-loss', 'process-failure'].includes(name)) {
    response.writeHead(200, {'content-type':'text/event-stream'}); response.flushHeaders();
    const timer = setTimeout(() => { timers.delete(timer); if (!response.destroyed) response.end('data: {"choices":[{"index":0,"delta":{"content":"late answer"},"finish_reason":"stop"}]}\n\ndata: [DONE]\n\n'); }, 12000);
    timers.add(timer);
    response.on('close', () => { state.providerClosed++; clearTimeout(timer); timers.delete(timer); });
    if (name === 'process-failure') {
     const rows = execFileSync('/bin/ps', ['-eo','pid=,args='], {encoding:'utf8'}).trim().split('\n');
     const row = rows.find(line => line.includes(agent) && line.includes(join(root, 'sessions')));
     assert(row, 'real agent process not found'); process.kill(Number(row.trim().split(/\s+/)[0]), 'SIGKILL'); didKill = true;
    }
   } else if (name==='permission' && state.requests.length===1) {
    assert(body.tools.some(tool=>tool.function.name==='fixture_guarded_noop'),'guarded fixture tool was not advertised');
    response.writeHead(200,{'content-type':'text/event-stream'});
    const chunk={choices:[{index:0,delta:{tool_calls:[{index:0,id:'guarded-call',type:'function',function:{name:'fixture_guarded_noop',arguments:'{}'}}]},finish_reason:'tool_calls'}]};
    response.end('data: '+JSON.stringify(chunk)+'\n\ndata: [DONE]\n\n');
   } else {if(name==='permission'){assert(body.messages.some(m=>m.role==='tool'),'tool refusal was not projected');assert(!JSON.stringify(body).includes('NOOP_EXECUTED'),'guarded tool executed');}textStream(response);}
  } catch (error) { errors.push(String(error.stack || error)); if (!response.headersSent) send(response,{error:{message:'fixture failure'}},500); else response.destroy(); }
 });
 const providerURL = await listen(provider);
 const run = {id:'run_1',generation:1,instance_id:'',status:'running',lease_expires_at:expiry(),spec:{title:'Loopback fixture',prompt:'Respond with a short fixture answer.',workspace_key:'fixture',agent_id:'fixture',acceptance:''}};
 const cloud = createServer(async (request,response) => {
  try {
   assert.equal(request.headers.authorization, 'Bearer fixture-cloud-secret');
   const body = await jsonBody(request);
   if (request.url.endsWith('/claims')) {
    state.claims.push(body);
    if (state.completes.length && run.status !== 'running') return send(response,{run:null});
    run.instance_id=body.instance_id;run.lease_expires_at=expiry();return send(response,{run});
   }
   if (request.url.endsWith('/renew')) {
    state.renewals++;
    if(name==='lease-loss' && state.renewals>1) return send(response,{error:'fixture lease unavailable'},503);
    run.lease_expires_at=expiry();return send(response,{lease_expires_at:run.lease_expires_at,cancel_requested:name==='cancel' && state.requests.length>0 && state.renewals>=3});
   }
   if (request.url.endsWith('/events')) {
    for(const event of body.events) if(!state.events.some(e=>e.seq===event.seq)) state.events.push(event);
    return send(response,{last_seq:state.events.at(-1)?.seq || 0});
   }
   if (request.url.endsWith('/complete')) {
    state.completes.push(body);
    if(name==='duplicate-result' && state.completes.length===1) return request.socket.destroy();
    if(name==='late-result' && state.completes.length===1){run.status='cancel_requested';return send(response,{error:'cancel requested before result acceptance'},409);}
    run.status=body.outcome;return send(response,run);
   }
   return send(response,run);
  } catch(error) {errors.push(String(error.stack||error));if(!response.headersSent)send(response,{error:'fixture failure'},500);else response.destroy();}
 });
 const cloudURL = await listen(cloud);
 const config = join(root,'config.json');const journal = join(root,'state.json');
 await writeFile(join(root,'agents.json'), JSON.stringify([{id:'fixture',name:'Real Go ACP fixture',command:name==='permission'?permissionAgent:agent,args:['--endpoint',providerURL+'/v1','--model','fixture','--session-dir',join(root,'sessions'),'--api-key-env','ACP_FIXTURE_API_KEY','--approval','ask'],env:{ACP_FIXTURE_API_KEY:'fixture-provider-secret'}}]),{mode:0o600});
 await writeFile(config,JSON.stringify({workspaces:{fixture:join(root,'workspace')},agents_file:join(root,'agents.json')}),{mode:0o600});
 if(name==='reconcile') await writeFile(journal,JSON.stringify({pending_claim:{request_id:'original-claim',instance_id:'old-instance'},active:null}),{mode:0o600});
 async function runWorker() {
  // Explicit allowlist: no inherited account, provider, telemetry or proxy secrets.
  const child=spawn(worker,['--once'],{env:{PATH:'/usr/local/bin:/usr/bin:/bin',HOME:root,TMPDIR:root,LANG:'C.UTF-8',PERO_CLOUD_URL:cloudURL,PERO_WORKER_TOKEN:'fixture-cloud-secret',PERO_OPERATOR_TOKEN:'fixture-operator-secret',PERO_WORKER_CONFIG:config,PERO_WORKER_STATE:journal},stdio:['ignore','pipe','pipe']});children.add(child);
  let stderr='',stdout='';child.stderr.on('data',b=>stderr+=b);child.stdout.on('data',b=>stdout+=b);
  const timeout=setTimeout(()=>child.kill('SIGKILL'),20000);
  const code=await new Promise((ok,fail)=>{child.on('error',fail);child.on('close',ok)});clearTimeout(timeout);children.delete(child);return {code,stderr,stdout};
 }
 try {
  let output = await runWorker();
  if(name==='duplicate-result') {assert.notEqual(output.code,0,'lost completion must retain reconciliation state');assert.equal(state.requests.length,1);output=await runWorker();assert.equal(state.completes.length,2);assert.deepEqual(state.completes[0],state.completes[1]);}
  assert.deepEqual(errors,[]);
  if(name==='lease-loss') {
   const stored=JSON.parse(await readFile(journal,'utf8'));assert.equal(stored.active?.result?.outcome || state.completes.at(-1)?.outcome,'reconcile');assert(state.providerClosed>0);
  } else {
   assert.equal(output.code,0,output.stderr);
   const expected=name==='cancel'?'cancelled':name==='process-failure'?'failed':['reconcile','late-result'].includes(name)?'reconcile':'succeeded';
   assert.equal(state.completes.at(-1)?.outcome,expected,output.stderr);
  }
  if(name==='reconcile'){assert.equal(state.requests.length,0);assert.equal(state.claims[0].request_id,'original-claim');assert.equal(state.claims[0].instance_id,'old-instance');}
  else assert.equal(state.requests.length,name==='permission'?2:1,'one prompt may cause only one fixture provider call');
  if(name==='cancel'){assert(state.providerClosed>0);assert(!state.completes.some(c=>c.outcome==='succeeded'));}
  if(name==='process-failure')assert(didKill);
  if(name==='late-result'){assert.deepEqual(state.completes.map(c=>c.outcome),['succeeded','reconcile']);assert.equal(run.status,'reconcile');}
  if(name==='permission'){assert(state.events.some(e=>e.kind==='permission_denied'));await assert.rejects(readFile(join(root,'sessions','tool-executed')), {code:'ENOENT'});}
  const encoded=JSON.stringify(state.events);for(const secret of ['fixture-provider-secret','fixture-cloud-secret','fixture-operator-secret'])assert(!encoded.includes(secret),'credential leaked into Worker events');
  return {name,passed:true,providerCalls:state.requests.length,eventCount:state.events.length,completionAttempts:state.completes.length,outcome:state.completes.at(-1)?.outcome||'durable reconcile',providerCancelled:state.providerClosed>0};
 } finally {
  for(const child of children)child.kill('SIGKILL');for(const timer of timers)clearTimeout(timer);cloud.closeAllConnections();provider.closeAllConnections();await Promise.all([new Promise(ok=>cloud.close(ok)),new Promise(ok=>provider.close(ok))]);await rm(root,{recursive:true,force:true});
 }
}

const results=[];
for(const name of ['success','cancel','process-failure','duplicate-result','late-result','reconcile','lease-loss',...(permissionAgent?['permission']:[])]) {
 try {const result=await scenario(name);results.push(result);console.log(JSON.stringify(result));}
 catch(error){results.push({name,passed:false,error:String(error.stack||error)});console.error(JSON.stringify(results.at(-1)));}
}
const finalHashes={worker:await hash(worker),agent:await hash(agent)};if(permissionAgent)finalHashes.permissionAgent=await hash(permissionAgent);
assert.deepEqual(finalHashes,binaryHashes,'binary changed during verification; rerun required');
const report={binaryHashes,results,limitations:['Cloud is a contract fixture, not PostgreSQL/control-plane deployment.','No real model request or credential.',permissionAgent?'Permission denial uses a test-only assembly of real core/ACP with one explicitly guarded no-op; production CLI installs no tools.':'Permission denial not run: pass a test-only guarded-tool assembly as fourth argument.','Worker-level repeated result and interrupted-claim checks do not establish database authorization/lease acceptance.']};
console.log(JSON.stringify(report,null,2));
if(results.some(r=>!r.passed))process.exitCode=1;
