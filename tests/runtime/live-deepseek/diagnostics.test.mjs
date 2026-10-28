// Offline only: no executable launches, credentials, HTTP, or model calls.
import test from 'node:test';
import assert from 'node:assert/strict';
import { summarizeWorkerEvents } from './worker-live.mjs';
const nonce='fixture-private-nonce';
const call={kind:'session_update',data:{sessionUpdate:'tool_call',toolCallId:'private-call-id',title:'read_file',rawInput:{path:'source.txt'}}};
const completed={kind:'session_update',data:{sessionUpdate:'tool_call_update',toolCallId:'private-call-id',status:'completed',content:[{type:'content',content:{type:'text',text:nonce}}]}};

test('actual completed read matches path and nonce without returning either content or ID',()=>{
 const result=summarizeWorkerEvents([call,completed],[{outcome:'succeeded'}],nonce);
 assert.equal(result.matchedPath,true);assert.equal(result.nonceMatched,true);
 assert.equal(result.toolNames.read_file,1);assert.equal(result.toolStatuses.completed,1);
 assert.equal(result.completionOutcomes.succeeded,1);
 assert(!JSON.stringify(result).includes(nonce));assert(!JSON.stringify(result).includes('private-call-id'));
});
test('prose alone or failed/unrelated tool result does not establish read success',()=>{
 const prose={kind:'session_update',data:{sessionUpdate:'agent_message_chunk',content:{text:nonce}}};
 for(const events of [[prose],[call,{...completed,data:{...completed.data,status:'failed'}}],[call,{...completed,data:{...completed.data,toolCallId:'other'}}]]){
  assert.equal(summarizeWorkerEvents(events,[],nonce).nonceMatched,false);
 }
 assert.equal(summarizeWorkerEvents([prose],[],nonce).matchedPath,false);
});
test('max_tokens remains a fixed diagnostic reason',()=>{
 assert.deepEqual(summarizeWorkerEvents([{kind:'turn_finished',data:{stopReason:'max_tokens'}}],[],nonce).turnStopReasons,{max_tokens:1});
});
test('HTTP400 extracts only local prefix status, never remote body',()=>{
 const result=summarizeWorkerEvents([{kind:'session_error',text:'openai adapter: HTTP 400 private-remote-body'}],[],nonce);
 assert.deepEqual(result.sessionErrors,{http_400:1});assert(!JSON.stringify(result).includes('private-remote-body'));
});
test('request budget error becomes fixed category',()=>{
 assert.deepEqual(summarizeWorkerEvents([{kind:'session_error',text:'provider request budget exhausted private-extra'}],[],nonce).sessionErrors,{budget_exhausted:1});
});
test('unknown names, statuses, outcomes and error text are never copied',()=>{
 const result=summarizeWorkerEvents([
  {kind:'secret-kind'},
  {kind:'session_update',data:{sessionUpdate:'tool_call',title:'secret-tool',rawInput:{path:'secret-path'}}},
  {kind:'session_update',data:{sessionUpdate:'tool_call_update',status:'secret-status'}},
  {kind:'turn_finished',data:{stopReason:'secret-stop'}},
  {kind:'session_error',text:'secret-body HTTP 400'}
 ],[{outcome:'secret-outcome'}],nonce);
 assert.equal(result.eventKinds.other,1);assert.equal(result.toolNames.unknown,1);
 assert.equal(result.toolStatuses.unknown,1);assert.equal(result.turnStopReasons.unknown,1);
 assert.equal(result.completionOutcomes.unknown,1);assert.equal(result.sessionErrors.unknown,1);
 assert(!JSON.stringify(result).includes('secret-'));
});
test('empty inputs and absent nonce remain safely false',()=>{
 const result=summarizeWorkerEvents([],[],undefined);assert.equal(result.nonceMatched,false);assert.equal(result.matchedPath,false);
 assert.equal(summarizeWorkerEvents([call,completed],[],'').nonceMatched,false);
});
