// Explicit opt-in runner. Fixed previously verified model; one scene, no discovery requests.
import { runWorker, networkEnvironment } from './worker-live.mjs';
import { spawn, execFileSync } from 'node:child_process';
import { mkdtemp, mkdir, writeFile, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, isAbsolute } from 'node:path';
import { createHash, randomBytes } from 'node:crypto';
const key=process.env.DEEPSEEK_API_KEY;
const budgets=['--max-requests','4','--max-tokens','512','--max-retries','0','--disable-thinking','--use-env-proxy'];
const hash=async p=>createHash('sha256').update(await readFile(p)).digest('hex');
const wait=ms=>new Promise(ok=>setTimeout(ok,ms));
async function runtime({client,agent,model,root}){
 const workspace=join(root,'workspace'),sessions=join(root,'sessions');await mkdir(workspace,{mode:0o700});await mkdir(sessions,{mode:0o700});
 const nonce=randomBytes(24).toString('hex'),config=join(root,'runtime.json');
 await writeFile(config,JSON.stringify({agent:{id:'live',name:'Bounded live',command:agent,args:['--endpoint','https://api.deepseek.com','--model',model,'--session-dir',sessions,'--api-key-env','DEEPSEEK_API_KEY','--workspace',workspace,'--enable-shell','--approval','ask',...budgets],env:{}},workspace,expected_content:nonce,prompt:`Use write_file to create live-result.txt with exactly this content and no newline: ${nonce}. Then read_file live-result.txt to verify it. Then use shell with exactly command pwd (no extra commands) to verify working directory. Do not perform any other operations. Report completion.`}),{mode:0o600});
 let child,closed,timer,output='',overflow=false,forced=false;let receipt={scenario:'explicit-allow-once',passed:false,error:'runtime_failed'};
 try{
  child=spawn(client,[config],{env:{...networkEnvironment(),PATH:'/usr/local/bin:/usr/bin:/bin',HOME:root,TMPDIR:root,LANG:'C.UTF-8',DEEPSEEK_API_KEY:key},stdio:['ignore','pipe','pipe']});
  child.stdout.on('data',b=>{if(output.length+b.length<=16384)output+=b.toString();else overflow=true;});child.stderr.resume();
  closed=new Promise(ok=>{child.once('error',()=>ok(-1));child.once('close',ok);});timer=setTimeout(()=>child.kill('SIGKILL'),65000);
  const code=await closed;clearTimeout(timer);
  if(code!==0||overflow||output.includes(key))throw Error('driver');
  const data=JSON.parse(output);if(data.passed!==true||data.write_readback!==true||data.shell_pwd_verified!==true||await readFile(join(workspace,'live-result.txt'),'utf8')!==nonce)throw Error('evidence');
  receipt={scenario:'explicit-allow-once',passed:true,write_readback:true,shell_pwd_verified:true,permission_grants:Number(data.permission_grants),permission_denials:Number(data.permission_denials)};
 }catch{}finally{
  clearTimeout(timer);if(child&&child.exitCode===null&&child.signalCode===null){child.kill('SIGKILL');if(closed)await closed;}
  try{const pids=()=>execFileSync('/bin/ps',['-eo','pid=,args='],{encoding:'utf8'}).split('\n').filter(l=>l.includes(agent)&&l.includes(sessions)).map(l=>Number(l.trim().split(/\s+/)[0]));for(let n=0;n<20&&pids().length;n++)await wait(50);forced=pids().length>0;for(const pid of pids())try{process.kill(pid,'SIGKILL')}catch{};for(let n=0;n<20&&pids().length;n++)await wait(50);if(forced||pids().length)receipt={scenario:'explicit-allow-once',passed:false,error:'owned_process_cleanup_required'};}catch{receipt={scenario:'explicit-allow-once',passed:false,error:'cleanup_failed'};}
 }
 return receipt;
}
async function main(){
 // Refuse before parsing files, hashing binaries, or opening a connection.
 if(!key){console.log(JSON.stringify({passed:false,error:'credential_environment_absent'}));process.exitCode=2;return;}
 let root;const receipt={passed:false,endpoint:'https://api.deepseek.com',scenes:[]};
 try{
  const args=process.argv.slice(2),options={};if(args.length!==10)throw Error('args');for(let i=0;i<args.length;i+=2){if(!['--agent','--worker','--client','--agent-sha256','--scene'].includes(args[i])||options[args[i]])throw Error('args');options[args[i]]=args[i+1];}
  const agent=options['--agent'],worker=options['--worker'],client=options['--client'],pin=options['--agent-sha256'];
  if(![agent,worker,client].every(p=>typeof p==='string'&&isAbsolute(p))||!/^[a-f0-9]{64}$/.test(pin)||await hash(agent)!==pin)throw Error('pin');
  const hashes={agent:pin,worker:await hash(worker),client:await hash(client)};receipt.binaryHashes=hashes;
  const scene=options['--scene'];if(!['worker','runtime'].includes(scene))throw Error('args');receipt.requestBudget=4;receipt.outputTokensPerRequest=512;const model="deepseek-flash";receipt.model=model;receipt.modelDiscovery="previously_verified";root=await mkdtemp(join(tmpdir(),'pero-live-bounded-'));
  if(scene==='worker'){receipt.scenes.push(await runWorker({worker,agent,model,key,root,budgetArgs:budgets}));}
  else {const rt=join(root,'runtime');await mkdir(rt,{mode:0o700});receipt.scenes.push(await runtime({client,agent,model,root:rt}));}
  if(await hash(agent)!==pin||await hash(worker)!==hashes.worker||await hash(client)!==hashes.client)throw Error('binary');
  receipt.passed=receipt.scenes.length===1&&receipt.scenes.every(s=>s.passed);
 }catch(error){const safe=['models_http_error','models_limit','models_unavailable','models_failed','worker','binary','pin','args'];receipt.error=safe.includes(error?.message)?error.message:'live_verification_failed';if(Number.isInteger(error?.status))receipt.httpStatus=error.status;if(typeof error?.networkCode==='string'&&/^[A-Z0-9_]{1,64}$/.test(error.networkCode))receipt.networkCode=error.networkCode;}finally{if(root)try{await rm(root,{recursive:true,force:true});}catch{receipt.passed=false;receipt.error='cleanup_failed';}}
 console.log(JSON.stringify(receipt));if(!receipt.passed)process.exitCode=1;
}
await main();
