// Real production Go tools through the existing Rust ACP runtime, with narrow
// per-call allow-once answers. All files/processes belong to fresh test roots.
// Usage: node tests/runtime/go-tools-runtime.mjs /absolute/runtime-tool-client /absolute/pero-agent-acp
import assert from 'node:assert/strict';
import {createServer} from 'node:http';
import {spawn,execFileSync} from 'node:child_process';
import {mkdtemp,mkdir,writeFile,readFile,readlink,symlink,rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {join,resolve} from 'node:path';
import {createHash} from 'node:crypto';

const client=resolve(process.argv[2]);const agent=resolve(process.argv[3]);
const hash=async p=>createHash('sha256').update(await readFile(p)).digest('hex');
const hashes={client:await hash(client),agent:await hash(agent)};
const secret='fixture-provider-secret';const outside='OUTSIDE_PRIVATE_FIXTURE_SENTINEL';
const call=(name,args)=>({name,args});
const sleep=ms=>new Promise(ok=>setTimeout(ok,ms));
// Only fixture-owned processes may be cleaned up; never target a broad name.
const ownedAgents=sessions=>execFileSync('/bin/ps',['-eo','pid=,args='],{encoding:'utf8'}).split('\n').filter(row=>row.includes(agent)&&row.includes('--session-dir')&&row.includes(sessions)).map(row=>Number(row.trim().split(/\s+/)[0])).filter(pid=>pid>1&&pid!==process.pid);
async function cleanupProcesses(sessions,workspace){
 let forced=false;let agents=ownedAgents(sessions);
 for(let i=0;agents.length&&i<20;i++){await sleep(50);agents=ownedAgents(sessions);}
 for(const pid of agents){forced=true;try{process.kill(pid,'SIGTERM')}catch(e){if(e.code!=='ESRCH')throw e;}}
 if(agents.length)await sleep(300);
 for(const pid of ownedAgents(sessions)){try{process.kill(pid,'SIGKILL')}catch(e){if(e.code!=='ESRCH')throw e;}}
 for(const file of ['shell.pid','child.pid']){try{const pid=Number(await readFile(join(workspace,file),'utf8'));if(pid>1&&await readlink(`/proc/${pid}/cwd`)===workspace){forced=true;try{process.kill(pid,'SIGKILL')}catch(e){if(e.code!=='ESRCH')throw e;}}}catch(e){if(e.code!=='ENOENT'&&e.code!=='ESRCH')throw e;}}
 for(let i=0;ownedAgents(sessions).length&&i<20;i++)await sleep(50);
 assert.equal(ownedAgents(sessions).length,0,'fixture Agent process survived cleanup');return forced;
}
const plans=[
 {name:'write-edit-read',calls:[call('write_file',{path:'created.txt',content:'alpha\nbeta\n'}),call('edit_file',{path:'created.txt',old_text:'beta',new_text:'gamma'}),call('read_file',{path:'created.txt'})],allow:[1,2],check:async c=>{assert.equal(await readFile(join(c.workspace,'created.txt'),'utf8'),'alpha\ngamma\n');assert(c.toolTexts[2].includes('gamma'));}},
 {name:'shell-success',calls:[call('shell',{command:"printf 'shell-ok\\n'; printf 'created' > shell-result.txt"})],allow:[1],check:async c=>{assert.equal(await readFile(join(c.workspace,'shell-result.txt'),'utf8'),'created');assert.equal(JSON.parse(c.toolTexts[0]).stdout.text,'shell-ok\n');}},
 {name:'shell-minimal-environment',calls:[call('shell',{command:'printf "%s|%s" "${ACP_FIXTURE_API_KEY-unset}" "${UNRELATED_PRIVATE_TOKEN-unset}"'})],allow:[1],check:async c=>assert.equal(JSON.parse(c.toolTexts[0]).stdout.text,'unset|unset')},
 {name:'shell-nonzero-exit',calls:[call('shell',{command:"printf 'expected failure' >&2; exit 7"})],allow:[1],failed:[1],check:async c=>{const r=JSON.parse(c.toolTexts[0]).result;assert.equal(r.exitCode,7);assert.equal(r.stderr.text,'expected failure');}},
 {name:'shell-timeout',calls:[call('shell',{command:'sleep 2',timeout_ms:50})],allow:[1],failed:[1],check:async c=>assert.equal(JSON.parse(c.toolTexts[0]).result.timedOut,true)},
 {name:'shell-output-limit',calls:[call('shell',{command:'i=0; while [ "$i" -lt 70000 ]; do printf x; i=$((i+1)); done'})],allow:[1],check:async c=>{const r=JSON.parse(c.toolTexts[0]);assert.equal(r.stdout.truncated,true);assert(Buffer.byteLength(r.stdout.text)<=16384);}},
 {name:'shell-escaped-output-preserves-failure',calls:[call('shell',{command:"printf '%*s' 40000 '' | tr ' ' '\\001'; printf '%*s' 40000 '' | tr ' ' '\\001' >&2; exit 7"})],allow:[1],failed:[1],check:async c=>{assert(Buffer.byteLength(c.toolTexts[0])<=65536);const r=JSON.parse(c.toolTexts[0]);assert(r.error);assert.equal(r.result.exitCode,7);assert.equal(r.result.stdout.truncated,true);assert.equal(r.result.stderr.truncated,true);}},
 {name:'long-path-error-remains-structured',calls:[call('read_file',{path:'x'.repeat(80000)})],allow:[],failed:[1],check:async c=>{assert(Buffer.byteLength(c.toolTexts[0])<=65536);const r=JSON.parse(c.toolTexts[0]);assert.equal(typeof r.error,'string');assert(r.error.length>0);}},
 {name:'shell-cancel-descendants',calls:[call('shell',{command:"printf '%s' $$ > shell.pid; sleep 30 & child=$!; printf '%s' \"$child\" > child.pid; printf started > started.txt; wait; printf late > late.txt"})],allow:[1],cancel:true,check:async c=>{assert.equal(await readFile(join(c.workspace,'started.txt'),'utf8'),'started');await assert.rejects(readFile(join(c.workspace,'late.txt')),{code:'ENOENT'});for(const name of ['shell.pid','child.pid']){const pid=Number(await readFile(join(c.workspace,name),'utf8'));let running=false;try{process.kill(pid,0);const stat=await readFile(`/proc/${pid}/stat`,'utf8');running=!/\) [ZX] /.test(stat);}catch(e){if(e.code!=='ESRCH'&&e.code!=='ENOENT')throw e;}assert(!running,`${name} remains running`);}}},
 {name:'approved-write-parent-escape',calls:[call('write_file',{path:'../outside.txt',content:'must-not-write'})],allow:[1],failed:[1]},
 {name:'approved-edit-symlink-escape',calls:[call('edit_file',{path:'outside-link.txt',old_text:'OUTSIDE',new_text:'changed'})],allow:[1],failed:[1]},
 {name:'read-large-file',calls:[call('read_file',{path:'large.txt'})],allow:[],failed:[1]},
 {name:'read-bounded-output',calls:[call('read_file',{path:'bounded.txt'})],allow:[],check:async c=>{assert(Buffer.byteLength(c.toolTexts[0])<=65536);assert.equal(JSON.parse(c.toolTexts[0]).truncated,true);}},
 {name:'never-policy-rejects-mutation',calls:[call('write_file',{path:'forbidden.txt',content:'must-not-write'})],allow:[1],policy:'never',failed:[1],permissions:0,check:async c=>assert.rejects(readFile(join(c.workspace,'forbidden.txt')),{code:'ENOENT'})},
 {name:'unapproved-call-is-denied',calls:[call('write_file',{path:'forbidden.txt',content:'must-not-write'})],allow:[],failed:[1],permissions:1,check:async c=>assert.rejects(readFile(join(c.workspace,'forbidden.txt')),{code:'ENOENT'})},
];

async function scenario(plan){
 const root=await mkdtemp(join(tmpdir(),'pero-production-tools-'));const workspace=join(root,'workspace');const sessions=join(root,'sessions');
 await mkdir(workspace,{mode:0o700});await mkdir(sessions,{mode:0o700});
 await writeFile(join(root,'outside.txt'),outside,{mode:0o600});await symlink('../outside.txt',join(workspace,'outside-link.txt'));
 await writeFile(join(workspace,'large.txt'),'x'.repeat(1048577));await writeFile(join(workspace,'bounded.txt'),'x'.repeat(90000));
 const requests=[];const toolTexts=[];const errors=[];let child;let passed=false;
 const server=createServer(async(req,res)=>{try{
  assert.equal(req.url,'/v1/chat/completions');assert.equal(req.headers.authorization,`Bearer ${secret}`);
  let raw='';for await(const b of req){raw+=b;assert(raw.length<2*1024*1024,'fixture request exceeded bound');}const body=JSON.parse(raw);requests.push(body);
  assert.equal(body.model,'fixture');for(const key of Object.keys(req.headers))assert(!/deepseek|point/.test(key));
  if(requests.length>1){const t=body.messages.filter(m=>m.role==='tool').at(-1);assert(t,'missing actual tool result');toolTexts.push(t.content);}
  const index=requests.length-1;res.writeHead(200,{'content-type':'text/event-stream'});
  if(index<plan.calls.length){const spec=plan.calls[index];assert(body.tools.some(t=>t.function.name===spec.name),'production tool not advertised');res.end(`data: ${JSON.stringify({choices:[{index:0,delta:{tool_calls:[{index:0,id:`fixture-call-${index+1}`,type:'function',function:{name:spec.name,arguments:JSON.stringify(spec.args)}}]},finish_reason:'tool_calls'}]})}\n\ndata: [DONE]\n\n`);}
  else res.end('data: {"choices":[{"index":0,"delta":{"content":"fixture complete"},"finish_reason":"stop"}]}\n\ndata: [DONE]\n\n');
 }catch(e){errors.push(String(e.stack||e));res.destroy();}});
 await new Promise(ok=>server.listen(0,'127.0.0.1',ok));const endpoint=`http://127.0.0.1:${server.address().port}/v1`;
 const conf={agent:{id:'production-tools',name:'Production Go tools',command:agent,args:['--endpoint',endpoint,'--model','fixture','--session-dir',sessions,'--api-key-env','ACP_FIXTURE_API_KEY','--approval',plan.policy||'ask','--workspace',workspace,'--enable-shell'],env:{ACP_FIXTURE_API_KEY:secret,UNRELATED_PRIVATE_TOKEN:'unrelated-fixture-secret'}},workspace,prompt:'Execute only the fixture tool plan.',allow_once_call_ids:plan.allow.map(n=>`fixture-call-${n}`)};
 if(plan.cancel)conf.cancel_when_file_exists=join(workspace,'started.txt');const configPath=join(root,'client.json');await writeFile(configPath,JSON.stringify(conf),{mode:0o600});
 try{
  child=spawn(client,[configPath],{env:{PATH:'/usr/local/bin:/usr/bin:/bin',HOME:root,TMPDIR:root,LANG:'C.UTF-8'},stdio:['ignore','pipe','pipe']});let stdout='',stderr='';child.stdout.on('data',b=>stdout+=b);child.stderr.on('data',b=>stderr+=b);
  const timeout=setTimeout(()=>child.kill('SIGKILL'),30000);const code=await new Promise((ok,fail)=>{child.on('error',fail);child.on('close',ok)});clearTimeout(timeout);assert.equal(code,0,stderr);assert.deepEqual(errors,[]);
  const events=stdout.trim().split('\n').filter(Boolean).map(s=>JSON.parse(s));const permissions=events.filter(e=>e.kind==='permission');
  const finished=events.find(e=>e.kind==='turn_finished');assert(finished);assert.equal(finished.data.stopReason,plan.cancel?'cancelled':'end_turn');
  assert.equal(permissions.length,plan.permissions??plan.allow.length);
  if(!plan.cancel){assert.equal(requests.length,plan.calls.length+1);for(let n=1;n<=plan.calls.length;n++){const update=events.find(e=>e.kind==='session_update'&&e.data.sessionUpdate==='tool_call_update'&&e.data.toolCallId===`fixture-call-${n}`);assert(update,`missing actual update ${n}`);assert.equal(update.data.status,plan.failed?.includes(n)?'failed':'completed');}}
  else assert.equal(requests.length,1,'cancelled shell must not trigger another model turn');
  const allText=JSON.stringify({events,toolTexts,requests});for(const value of [secret,'unrelated-fixture-secret',outside])assert(!allText.includes(value),'private fixture data leaked');
  assert.equal(await readFile(join(root,'outside.txt'),'utf8'),outside,'outside file changed');
  await plan.check?.({root,workspace,sessions,events,toolTexts});
  passed=true;return {name:plan.name,passed:true,providerCalls:requests.length,permissions:permissions.length,stopReason:finished.data.stopReason};
 }finally{child?.kill('SIGKILL');server.closeAllConnections();await new Promise(ok=>server.close(ok));const forced=await cleanupProcesses(sessions,workspace);await rm(root,{recursive:true,force:true});if(passed)assert(!forced,'successful case required forced orphan cleanup');}
}

const results=[];for(const plan of plans){try{const result=await scenario(plan);results.push(result);console.log(JSON.stringify(result));}catch(error){const result={name:plan.name,passed:false,error:String(error.stack||error)};results.push(result);console.error(JSON.stringify(result));}}
assert.deepEqual({client:await hash(client),agent:await hash(agent)},hashes,'executables changed during suite');
console.log(JSON.stringify({binaryHashes:hashes,results,limitations:['Explicit allow-once belongs only to these named fixture calls in private temporary directories.','This does not change unattended Worker default denial.','Shell has no OS/network sandbox; no real model, credential, user file or deployment was used.']},null,2));
if(results.some(r=>!r.passed))process.exitCode=1;
