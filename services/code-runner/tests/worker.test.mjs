import test from 'node:test';
import assert from 'node:assert/strict';
import {build} from 'esbuild';
import {mkdtemp,rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
const dir=await mkdtemp(join(tmpdir(),'edsger-runner-'));
const outfile=join(dir,'worker.mjs');
await build({entryPoints:['src/index.ts'],outfile,bundle:true,platform:'node',format:'esm',plugins:[{name:'durable-object-context',setup(b){b.onResolve({filter:/^cloudflare:workers$/},()=>({path:'shim',namespace:'test'}));b.onLoad({filter:/.*/,namespace:'test'},()=>({contents:'export class DurableObject { constructor(ctx,env){this.ctx=ctx;this.env=env;} }',loader:'js'}));}}]});
const {serve,CodeAdmission,CodeCoreJob,CodeSystemsJob,CodeJVMJob,CodeDotnetJob,CodeSwiftJob}=await import(outfile);
test.after(()=>rm(dir,{recursive:true,force:true}));
class Storage {
  data=new Map();alarmTime=null;tail=Promise.resolve();
  async get(k){return structuredClone(this.data.get(k));}
  async put(k,v){this.data.set(k,structuredClone(v));}
  async delete(k){return this.data.delete(k);}
  async list({prefix=''}){return new Map([...this.data].filter(([k])=>k.startsWith(prefix)));}
  async getAlarm(){return this.alarmTime;}
  async setAlarm(n){this.alarmTime=Number(n);}
  async deleteAlarm(){this.alarmTime=null;}
  async transaction(fn){const prev=this.tail;let done;this.tail=new Promise(r=>done=r);await prev;const original=this.data;this.data=structuredClone(original);const alarm=this.alarmTime;try{return await fn(this)}catch(e){this.data=original;this.alarmTime=alarm;throw e}finally{done()}}
}
const uuid=n=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const input={language:'python',entrypoint:'main.py',files:[{path:'main.py',content:'print("hello")'}],stdin:'',mode:'run'};
const headers={'Content-Type':'application/json','X-Edsger-Execution-Revision':'edsger-execution-v1'};
const jobRequest=(body=input)=>new Request('https://job/run',{method:'POST',body:JSON.stringify(body)});
test('private edge fails closed, rejects arbitrary commands and routes each language to its family',async()=>{
  const events=[],env={CODE_RUNNER_ENABLED:'true'};
  for(const key of ['CODE_CORE_JOB','CODE_SYSTEMS_JOB','CODE_JVM_JOB','CODE_DOTNET_JOB','CODE_SWIFT_JOB','CODE_ADMISSION']) env[key]={idFromName:id=>id,get:()=>({fetch:async req=>{events.push([key,req.method]);return new Response('{}')}})};
  assert.equal((await serve(new Request('https://runner/'+uuid(1),{method:'POST',body:JSON.stringify(input)}),env)).status,503);
  assert.equal((await serve(new Request('https://runner/'+uuid(1),{method:'POST',headers,body:JSON.stringify({...input,command:'bash'})}),env)).status,400);
  const examples=[['python','main.py','CODE_CORE_JOB'],['c','main.c','CODE_SYSTEMS_JOB'],['java','Main.java','CODE_JVM_JOB'],['csharp','Program.cs','CODE_DOTNET_JOB'],['swift','main.swift','CODE_SWIFT_JOB']];
  for(const [language,entrypoint,key] of examples){events.length=0;await serve(new Request('https://runner/'+uuid(1),{method:'POST',headers,body:JSON.stringify({...input,language,entrypoint,files:[{path:entrypoint,content:''}]})}),env);assert.deepEqual(events,[['CODE_ADMISSION','POST'],[key,'POST'],['CODE_ADMISSION','DELETE']]);}
  env.CODE_RUNNER_ENABLED='false';events.length=0;
  assert.equal((await serve(new Request('https://runner/'+uuid(1),{method:'POST',headers,body:JSON.stringify(input)}),env)).status,503);
  await serve(new Request('https://runner/'+uuid(1),{method:'DELETE',headers:{...headers,'X-Edsger-Execution-Language':'python'}}),env);
  assert.deepEqual(events,[['CODE_CORE_JOB','DELETE']]);
});
test('persisted global admission caps concurrency and attempts across actor restarts',async()=>{
  const storage=new Storage(),admit=n=>new Request('https://admission/'+uuid(n),{method:'POST'});
  let actor=new CodeAdmission({storage},{});
  const accepted=await Promise.all(Array.from({length:9},(_,i)=>actor.fetch(admit(i+1))));
  assert.equal(accepted.filter(r=>r.status===200).length,8);assert.equal(accepted.filter(r=>r.status===429).length,1);
  actor=new CodeAdmission({storage},{});
  assert.equal((await actor.fetch(admit(1))).status,409);
  await actor.fetch(new Request('https://admission/'+uuid(1),{method:'DELETE'}));
  assert.equal((await actor.fetch(admit(10))).status,200);
  for(const limit of ['minuteCount','dayCount','monthCount']){
    await storage.put('counters',{...(await storage.get('counters')),[limit]:{minuteCount:30,dayCount:500,monthCount:10000}[limit]});
    for(const [key] of await storage.list({prefix:'active:'})) await storage.delete(key);
    assert.equal((await actor.fetch(admit(99))).status,429);
    await storage.put('counters',{...(await storage.get('counters')),[limit]:0});
  }
});
test('platform capacity refusal is refundable and releases global admission without raw error output',async()=>{
  const message='There is no container instance that can be provided to this Durable Object, try again later';
  for(const upstream of [async()=>{throw new Error(message)},async()=>new Response(message,{status:503})]){
    const events=[];
    const namespace=fetch=>({idFromName:id=>id,get:()=>({fetch})});
    const env={CODE_RUNNER_ENABLED:'true',CODE_CORE_JOB:namespace(upstream),CODE_ADMISSION:namespace(async req=>{events.push(req.method);return new Response('{}')})};
    const response=await serve(new Request('https://runner/'+uuid(101),{method:'POST',headers,body:JSON.stringify(input)}),env);
    assert.equal(response.status,429);assert.deepEqual(await response.json(),{error:'capacity_limited'});
    assert.deepEqual(events,['POST','DELETE']);
  }
});
test('unknown platform failures stay uncertain and cannot expose arbitrary errors',async()=>{
  for(const upstream of [async()=>{throw new Error('arbitrary internal error')},async()=>new Response('arbitrary internal error',{status:500}),async()=>new Response('x'.repeat(2048),{status:503})]){
    const namespace=fetch=>({idFromName:id=>id,get:()=>({fetch})});
    const env={CODE_RUNNER_ENABLED:'true',CODE_CORE_JOB:namespace(upstream),CODE_ADMISSION:namespace(async()=>new Response('{}'))};
    const response=await serve(new Request('https://runner/'+uuid(102),{method:'POST',headers,body:JSON.stringify(input)}),env);
    assert.equal(response.status,502);assert.deepEqual(await response.json(),{error:'runner_unavailable'});
  }
});
function jobFixture(Class=CodeCoreJob){
  const storage=new Storage(),events=[];
  const container={start:options=>events.push(['start',options]),setInactivityTimeout:async n=>events.push(['ttl',n]),destroy:async()=>events.push(['destroy']),getTcpPort:()=>({fetch:async req=>req.method==='GET'?new Response('{}'):new Response(JSON.stringify({language:'python',status:'completed',stdout:'hello',stderr:'',exitCode:0,truncated:false}),{headers:{'Content-Type':'application/json'}})})};
  return {storage,events,container,actor:new Class({storage,container},{})};
}
test('a successful VM has no internet, a hard alarm, no reuse, and unconditional destruction',async()=>{
  const f=jobFixture();const response=await f.actor.fetch(jobRequest());assert.equal(response.status,200);
  const body=await response.json();assert.equal(body.execution.stdout,'hello');assert.equal(body.revision,'edsger-execution-v1');assert(body.elapsedMs>0&&body.elapsedMs<=45000);
  assert.deepEqual(f.events,[['start',{enableInternet:false}],['ttl',45000],['destroy']]);assert.equal(f.storage.alarmTime,null);
  assert.equal((await new CodeCoreJob({storage:f.storage,container:f.container},{}).fetch(jobRequest())).status,409);assert.equal(f.events.filter(e=>e[0]==='start').length,1);
});
test('cancel-before-run prevents startup; alarms and invalid results destroy the VM',async()=>{
  const cancelled=jobFixture();await cancelled.actor.fetch(new Request('https://job/cancel',{method:'DELETE'}));assert.equal((await cancelled.actor.fetch(jobRequest())).status,409);assert(!cancelled.events.some(e=>e[0]==='start'));
  const failed=jobFixture();failed.container.getTcpPort=()=>({fetch:async r=>r.method==='GET'?new Response('{}'):new Response(JSON.stringify({untrusted:'shape'}))});
  const body=await (await failed.actor.fetch(jobRequest())).json();assert.equal(body.execution.status,'timeout');assert(failed.events.some(e=>e[0]==='destroy'));
  await failed.actor.alarm();assert.equal(await failed.storage.get('cancelled'),true);assert.equal(failed.events.filter(e=>e[0]==='destroy').length,2);
});
test('a family cannot run code intended for another runtime image',async()=>{
  for(const Class of [CodeSystemsJob,CodeJVMJob,CodeDotnetJob,CodeSwiftJob]){const f=jobFixture(Class);assert.equal((await f.actor.fetch(jobRequest())).status,400);assert.equal(f.events.length,0);}
});
