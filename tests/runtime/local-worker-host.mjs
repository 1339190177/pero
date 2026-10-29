// Opt-in shell verification: actual Host + Worker, loopback Cloud, no Agent prompt.
import assert from 'node:assert/strict';
import { spawn, execFileSync } from 'node:child_process';
import { createServer } from 'node:http';
import { mkdtemp, mkdir, writeFile, readFile, readdir, rm, stat } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { createInterface } from 'node:readline';
import { EventEmitter, once } from 'node:events';
import { fileURLToPath } from 'node:url';

const repository=resolve(fileURLToPath(new URL('../..',import.meta.url)));
const root=await mkdtemp(join(tmpdir(),'pero-local-worker-host-'));
const workspace=join(root,'workspace');
await mkdir(workspace);
const agents=join(root,'agents.json');
await writeFile(agents,JSON.stringify([{id:'fixture',name:'Installed fixture',command:'/usr/bin/true'}]));
const token='private-worker-fixture-token';
let claims=0;
const cloud=createServer(async (request,response)=>{
  for await (const _chunk of request) {}
  assert.equal(request.headers.authorization,`Bearer ${token}`);
  assert.equal(request.url,'/api/worker/v1/claims');
  claims++;
  response.writeHead(200,{'Content-Type':'application/json'});
  response.end(JSON.stringify({run:null}));
});
cloud.listen(0,'127.0.0.1');await once(cloud,'listening');
const url=`http://127.0.0.1:${cloud.address().port}`;
const children=new Set();
const settings=join(root,'profile/settings.json');
const workerPath=process.env.PERO_WORKER_BIN || join(repository,'target/debug/pero-worker');
function launch() {
  const child=spawn(process.env.PERO_HOST_PATH || join(repository,'target/debug/pero-host'),['--stdio'],{
    cwd:workspace,env:{...process.env,PERO_SETTINGS_PATH:settings,PERO_HISTORY_PATH:join(root,'history.jsonl'),PERO_AGENTS_FILE:agents,PERO_WORKER_BIN:workerPath},stdio:['pipe','pipe','pipe'],
  });
  children.add(child);
  const exited=once(child,'exit');
  const events=[];const changed=new EventEmitter();
  createInterface({input:child.stdout}).on('line',line=>{events.push(JSON.parse(line));changed.emit('event');});
  let stderr='';child.stderr.on('data',chunk=>stderr+=chunk);
  return {child,events,exited,send:command=>child.stdin.write(JSON.stringify(command)+'\n'),async until(predicate){
    const deadline=Date.now()+10000;
    while (!events.some(predicate)) {
      const remaining=deadline-Date.now();assert.ok(remaining>0,`Missing Host state: ${stderr}`);
      await new Promise((resolve,reject)=>{const listener=()=>{clearTimeout(timer);resolve();};const timer=setTimeout(()=>{changed.off('event',listener);reject(Error(`Host state timed out: ${stderr}`));},remaining);changed.once('event',listener);});
    }
    return events.find(predicate);
  },async close(){child.stdin.end();const [code]=await exited;children.delete(child);assert.equal(code,0,stderr);}};
}
const config={id:'fixture-device',name:'Local fixture',url,token,bindings:[{project_id:'project',workspace_key:'workspace',path:workspace,agent_ids:['fixture']}]};
try {
  const first=launch();
  await first.until(event=>event.kind==='ready');
  first.send({action:'local_worker_get',request_id:'initial'});
  assert.equal((await first.until(event=>event.request_id==='initial')).data.status,'stopped');
  first.send({action:'local_worker_start',request_id:'missing-agent',worker:{...config,bindings:[{...config.bindings[0],agent_ids:['missing-fixture-agent']}]}});
  const unavailable=(await first.until(event=>event.request_id==='missing-agent')).data;
  assert.equal(unavailable.status,'needs_attention');assert.equal(unavailable.enabled,false);assert.equal(claims,0);
  first.send({action:'local_worker_start',request_id:'start',worker:config});
  const idle=await first.until(event=>event.kind==='local_worker_state' && event.data.status==='idle');
  assert.equal(idle.data.enabled,true);assert.ok(claims>0);
  assert.equal(idle.data.configuration.bindings[0].project_id,'project');
  first.send({action:'local_worker_stop',request_id:'stop'});
  assert.equal((await first.until(event=>event.request_id==='stop' && event.data.status==='stopped')).data.enabled,false);
  first.send({action:'local_worker_start',request_id:'restart',worker:config});
  await first.until(event=>event.request_id==='restart');
  await first.close(); // EOF must stop and reap the independently running Worker.
  const second=launch();await second.until(event=>event.kind==='ready');
  second.send({action:'local_worker_get',request_id:'cold'});
  const cold=(await second.until(event=>event.request_id==='cold')).data;
  assert.equal(cold.enabled,false);assert.equal(cold.status,'stopped');assert.equal(cold.configuration.id,'fixture-device');
  assert.ok(!JSON.stringify([...first.events,...second.events]).includes(token));
  const directory=join(root,'profile/workers/fixture-device');
  for(const name of await readdir(directory)) {if(name.endsWith('.json')) assert.ok(!(await readFile(join(directory,name),'utf8')).includes(token));}
  assert.equal((await stat(directory)).mode & 0o777,0o700);
  await second.close();
  const crashed=launch();await crashed.until(event=>event.kind==='ready');
  crashed.send({action:'local_worker_start',request_id:'crash-start',worker:config});
  await crashed.until(event=>event.kind==='local_worker_state' && event.data.status==='idle');
  const workerPid=execFileSync('/bin/ps',['-eo','pid=,ppid='],{encoding:'utf8'}).split('\n').map(line=>line.trim().split(/\s+/).map(Number)).find(row=>row[1]===crashed.child.pid)?.[0];
  assert.ok(workerPid,'Host must own a Worker child');
  crashed.child.kill('SIGKILL');await crashed.exited;children.delete(crashed.child);
  const deadline=Date.now()+4000;
  for (;;) {
    try {process.kill(workerPid,0);} catch(error) {if(error.code==='ESRCH') break;throw error;}
    assert.ok(Date.now()<deadline,'Worker survived a crashed Host');
    await new Promise(resolve=>setTimeout(resolve,20));
  }
  console.log(JSON.stringify({passed:true,claims,states:['missing_agent','idle','stopped','restart','cold_stopped','host_crash_cleanup'],credentials_excluded:true}));
} finally {
  for (const child of children) {child.kill();}
  await new Promise(resolve=>cloud.close(resolve));
  await rm(root,{recursive:true,force:true});
}
