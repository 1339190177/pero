// No model/credential/network service: real Worker + production Go through local HTTP fixture.
import {execFileSync} from 'node:child_process';
import {runWorker,verifyReadWorkspace} from './worker-live.mjs';
import {createServer} from 'node:http';import {mkdtemp,mkdir,writeFile,readFile,rm,chmod,symlink} from 'node:fs/promises';import {tmpdir} from 'node:os';import {join,resolve} from 'node:path';import assert from 'node:assert/strict';import {createHash} from 'node:crypto';
const worker=resolve(process.argv[2]),agent=resolve(process.argv[3]);const binaryHash=createHash('sha256').update(await readFile(agent)).digest('hex');
const ownedPids=root=>execFileSync('/bin/ps',['-eo','pid=,args='],{encoding:'utf8'}).split('\n').filter(line=>line.includes(agent)&&line.includes(root)&&line.includes('--session-dir')).map(line=>Number(line.trim().split(/\s+/)[0]));
const root=await mkdtemp(join(tmpdir(),'pero-live-worker-local-'));let requests=0;
const server=createServer(async(req,res)=>{let raw='';for await(const b of req)raw+=b;const body=JSON.parse(raw);assert.equal(body.max_tokens,512);assert.deepEqual(body.thinking,{type:'disabled'});assert.equal(body.reasoning_effort,undefined);requests++;const delta=requests===1?{tool_calls:[{index:0,id:'actual-read',type:'function',function:{name:'read_file',arguments:'{"path":"source.txt"}'}}]}:{content:'read complete'};res.writeHead(200,{'content-type':'text/event-stream'});res.end(`data: ${JSON.stringify({choices:[{index:0,delta,finish_reason:requests===1?'tool_calls':'stop'}]})}\n\ndata: [DONE]\n\n`)});
try {
 await new Promise(ok=>server.listen(0,'127.0.0.1',ok));const launcher=join(root,'fixture-launcher');
 // Only this test launcher substitutes the local fake provider, not live/run.mjs.
 const quote=s=>"'"+s.replaceAll("'","'\\''")+"'";
 await writeFile(launcher,`#!/bin/bash\nargs=("$@")\nfor ((i=0;i<\u0024{#args[@]};i++)); do if [[ \u0024{args[i]} == --endpoint ]]; then args[i+1]=${quote('http://127.0.0.1:'+server.address().port)}; fi; done\nexec ${quote(agent)} "\u0024{args[@]}"\n`,{mode:0o700});
 const receipt=await runWorker({worker,agent:launcher,model:'fixture',key:'local-fixture-not-real',root,budgetArgs:['--max-requests','4','--max-tokens','512','--max-retries','0','--disable-thinking','--use-env-proxy']});
 assert.equal(receipt.passed,true,JSON.stringify(receipt));assert.deepEqual(receipt.workspaceVerification,{sourceUnchanged:true,allowedFilesOnly:true,privateLockValid:true});assert.equal(requests,2);assert.deepEqual(ownedPids(root),[],'actual production Agent remained alive');
 const check=join(root,'path-check');await mkdir(check);await writeFile(join(check,'source.txt'),'exact');await writeFile(join(check,'.pero-worker.lock'),'',{mode:0o600});assert(Object.values(await verifyReadWorkspace(check,'exact')).every(Boolean));
 await writeFile(join(check,'extra'),'no');assert.equal((await verifyReadWorkspace(check,'exact')).allowedFilesOnly,false);await rm(join(check,'extra'));
 await chmod(join(check,'.pero-worker.lock'),0o644);assert.equal((await verifyReadWorkspace(check,'exact')).privateLockValid,false);await rm(join(check,'.pero-worker.lock'));await symlink('source.txt',join(check,'.pero-worker.lock'));assert.equal((await verifyReadWorkspace(check,'exact')).privateLockValid,false);
 assert.equal((await verifyReadWorkspace(check,'wrong')).sourceUnchanged,false);
 assert.equal(createHash('sha256').update(await readFile(agent)).digest('hex'),binaryHash);
 console.log(JSON.stringify({localFakeOnly:true,passed:true,actualProviderRequests:requests,realAgentSHA256:binaryHash,workspaceVerification:receipt.workspaceVerification,negativeWorkspaceChecks:4}));
} finally {const orphans=ownedPids(root);for(const pid of orphans){try{process.kill(pid,'SIGKILL')}catch{}};if(orphans.length)process.exitCode=1;server.closeAllConnections();await new Promise(ok=>server.close(ok));await rm(root,{recursive:true,force:true});}
