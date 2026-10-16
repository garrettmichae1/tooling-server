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
  // A mocked result is not platform shutdown confirmation. Real batch actors
  // release their own lease only after destruction; the edge cannot infer it.
  for(const [language,entrypoint,key] of examples){events.length=0;await serve(new Request('https://runner/'+uuid(1),{method:'POST',headers,body:JSON.stringify({...input,language,entrypoint,files:[{path:entrypoint,content:''}]})}),env);assert.deepEqual(events,[['CODE_ADMISSION','POST'],[key,'POST']]);}
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
test('platform capacity refusal is refundable but retains bounded unconfirmed admission without raw errors',async()=>{
  const message='There is no container instance that can be provided to this Durable Object, try again later';
  for(const upstream of [async()=>{throw new Error(message)},async()=>new Response(message,{status:503})]){
    const events=[];
    const namespace=fetch=>({idFromName:id=>id,get:()=>({fetch})});
    const env={CODE_RUNNER_ENABLED:'true',CODE_CORE_JOB:namespace(upstream),CODE_ADMISSION:namespace(async req=>{events.push(req.method);return new Response('{}')})};
    const response=await serve(new Request('https://runner/'+uuid(101),{method:'POST',headers,body:JSON.stringify(input)}),env);
    assert.equal(response.status,429);assert.deepEqual(await response.json(),{error:'capacity_limited'});
    assert.deepEqual(events,['POST']);
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

const leaseHeader='X-Edsger-Admission-Lease';
const nextTurn=()=>new Promise(resolve=>setTimeout(resolve,0));
async function until(predicate){for(let n=0;n<100;n++){if(predicate())return;await nextTurn()}assert.fail('actor did not reach the expected lifecycle state');}
function batchFixture(){
  const admissionStorage=new Storage(),admission=new CodeAdmission({storage:admissionStorage},{}),jobs=new Map();
  const state={destroyFails:false,releaseFailures:0,loseResponse:false,holdExecution:false,startFails:false,executionFails:false,cancelAfterAdmission:false,releaseCalls:0};
  const env={CODE_RUNNER_ENABLED:'true',CODE_ADMISSION:{idFromName:id=>id,get:()=>({fetch:async request=>{
    if(request.method==='DELETE'){
      state.releaseCalls++;
      if(state.releaseFailures-->0)throw Error('release transport unavailable');
    }
    return admission.fetch(request);
  }})},CODE_CORE_JOB:{idFromName:id=>id,get:id=>({fetch:async request=>{
    if(!jobs.has(id)){
      const storage=new Storage(),events=[];
      const job={storage,events,settled:undefined,finishExecution:undefined};
      const result=()=>Response.json({language:'python',status:'completed',stdout:'hello',stderr:'',exitCode:0,truncated:false});
      const container={
        start(){events.push('start');if(state.startFails)throw Error('bootstrap failed');},
        async setInactivityTimeout(ms){events.push(['ttl',ms]);},
        async destroy(){events.push('destroy');if(state.destroyFails)throw Error('temporary platform destruction failure');job.finishExecution?.();},
        getTcpPort:()=>({fetch:async req=>{
          if(req.method==='GET')return new Response('{}');
          events.push('execute');
          if(state.executionFails)throw Error('execution request deadline exceeded');
          if(state.holdExecution)await new Promise(resolve=>{job.finishExecution=resolve;});
          return result();
        }})
      };
      job.container=container;job.actor=new CodeCoreJob({storage,container},env);jobs.set(id,job);
      const transaction=storage.transaction.bind(storage);
      storage.transaction=async fn=>{
        const result=await transaction(fn);
        if(state.cancelAfterAdmission){state.cancelAfterAdmission=false;await job.actor.fetch(new Request('https://job/cancel',{method:'DELETE'}));}
        return result;
      };
    }
    const job=jobs.get(id), pending=job.actor.fetch(request);
    if(state.loseResponse&&request.method==='POST'){
      // A service-binding disconnection does not mean the actor stopped.
      job.settled=pending;
      throw Error('lost actor response');
    }
    return pending;
  }})}};
  return {state,env,jobs,admissionStorage,admission,
    run:n=>serve(new Request('https://runner/'+uuid(n),{method:'POST',headers,body:JSON.stringify(input)}),env),
    cancel:n=>serve(new Request('https://runner/'+uuid(n),{method:'DELETE',headers:{...headers,'X-Edsger-Execution-Language':'python'}}),env),
    active:()=>admissionStorage.list({prefix:'active:'})};
}
test('batch destruction failure retains eight reservations and blocks a ninth until alarm confirms shutdown',async()=>{
  const f=batchFixture();f.state.destroyFails=true;
  for(let n=1;n<=8;n++)assert.equal((await f.run(n)).status,502);
  assert.equal((await f.active()).size,8);
  assert.equal((await f.run(9)).status,429);assert.equal(f.jobs.size,8);
  assert.equal(f.state.releaseCalls,0);
  for(const job of f.jobs.values())assert(job.storage.alarmTime>Date.now());
  f.state.destroyFails=false;
  const first=f.jobs.get(uuid(1));
  await new CodeCoreJob({storage:first.storage,container:first.container},f.env).alarm();
  assert.equal((await f.active()).size,7);assert.equal(first.storage.alarmTime,null);
  assert.equal((await f.run(10)).status,200);assert.equal((await f.active()).size,7);
  assert.equal(first.events.filter(e=>e==='start').length,1);
});
test('normal batch cleanup, duplicate Stop and actor restart release once without restarting the program',async()=>{
  const f=batchFixture();assert.equal((await f.run(11)).status,200);
  const job=f.jobs.get(uuid(11));assert.equal((await f.active()).size,0);assert.equal(f.state.releaseCalls,1);
  assert.equal(job.events.filter(e=>e==='destroy').length,1);assert.equal(job.storage.alarmTime,null);
  job.actor=new CodeCoreJob({storage:job.storage,container:job.container},f.env);
  assert.equal((await f.cancel(11)).status,200);await job.actor.alarm();
  assert.equal(f.state.releaseCalls,1);assert.equal(job.events.filter(e=>e==='destroy').length,1);
  assert.equal((await f.run(11)).status,409);assert.equal(job.events.filter(e=>e==='start').length,1);
});
test('destroyed batch retains its alarm when admission release fails and restart retries only release',async()=>{
  const f=batchFixture();f.state.releaseFailures=1;
  assert.equal((await f.run(12)).status,502);
  const job=f.jobs.get(uuid(12));assert.equal(await job.storage.get('batchStopped'),true);
  assert.equal((await f.active()).size,1);assert(job.storage.alarmTime>Date.now());
  await new CodeCoreJob({storage:job.storage,container:job.container},f.env).alarm();
  assert.equal((await f.active()).size,0);assert.equal(f.state.releaseCalls,2);assert.equal(job.storage.alarmTime,null);
  assert.equal(job.events.filter(e=>e==='destroy').length,1);
});
test('lost batch response retains capacity until the original actor actually destroys its instance',async()=>{
  const f=batchFixture();f.state.loseResponse=true;f.state.holdExecution=true;
  assert.equal((await f.run(13)).status,502);
  const job=f.jobs.get(uuid(13));await until(()=>job.events.includes('execute'));
  assert.equal((await f.active()).size,1);assert.equal(f.state.releaseCalls,0);
  job.finishExecution();assert.equal((await job.settled).status,200);
  assert.equal((await f.active()).size,0);assert.equal(f.state.releaseCalls,1);
});
test('Stop with starts disabled confirms destruction and reclaims the original batch lease during execution',async()=>{
  const f=batchFixture();f.state.holdExecution=true;
  const running=f.run(14);await until(()=>f.jobs.get(uuid(14))?.events.includes('execute'));
  f.env.CODE_RUNNER_ENABLED='false';
  assert.equal((await f.cancel(14)).status,200);assert.equal((await running).status,200);
  assert.equal((await f.active()).size,0);assert.equal(f.state.releaseCalls,1);
  const job=f.jobs.get(uuid(14));assert.equal(job.events.filter(e=>e==='destroy').length,1);
  assert.equal((await f.run(15)).status,503);assert.equal(f.jobs.size,1);
});
test('failed Stop retains the batch lease and safety alarm until a successful hard-destroy retry',async()=>{
  const f=batchFixture();f.state.destroyFails=true;
  assert.equal((await f.run(16)).status,502);assert.equal((await f.cancel(16)).status,502);
  const job=f.jobs.get(uuid(16));assert.equal((await f.active()).size,1);assert(job.storage.alarmTime>Date.now());
  f.state.destroyFails=false;assert.equal((await f.cancel(16)).status,200);
  assert.equal((await f.active()).size,0);assert.equal(f.state.releaseCalls,1);assert.equal(job.storage.alarmTime,null);
});
test('bootstrap failures still perform authoritative destruction before releasing a batch reservation',async()=>{
  const f=batchFixture();f.state.startFails=true;
  const response=await f.run(17);assert.equal(response.status,200);assert.equal((await response.json()).execution.status,'timeout');
  assert.equal((await f.active()).size,0);assert.equal(f.state.releaseCalls,1);
  assert.deepEqual(f.jobs.get(uuid(17)).events,['start','destroy']);
});
test('batch execution timeout keeps the original deadline and releases only after successful hard destruction',async()=>{
  for(const destructionFails of [false,true]){
    const f=batchFixture();f.state.executionFails=true;f.state.destroyFails=destructionFails;
    const response=await f.run(171);assert.equal(response.status,destructionFails?502:200);
    if(!destructionFails)assert.equal((await response.json()).execution.status,'timeout');
    const job=f.jobs.get(uuid(171));assert(job.events.some(e=>Array.isArray(e)&&e[0]==='ttl'&&e[1]===45000));
    assert.equal((await f.active()).size,destructionFails?1:0);assert.equal(job.events.filter(e=>e==='destroy').length,1);
  }
});
test('cancel after batch admission cannot launch a new instance after confirmed cleanup',async()=>{
  const f=batchFixture();f.state.cancelAfterAdmission=true;
  const response=await f.run(172);assert.equal(response.status,200);assert.equal((await response.json()).execution.status,'cancelled');
  const job=f.jobs.get(uuid(172));assert.equal(job.events.filter(e=>e==='start').length,0);
  assert.equal(job.events.filter(e=>e==='destroy').length,1);assert.equal((await f.active()).size,0);assert.equal(f.state.releaseCalls,1);
});
test('scoped lease expiry reclaims capacity and stale cleanup cannot release a newer scoped or stream lease',async()=>{
  const storage=new Storage(),admission=new CodeAdmission({storage},{}),id=uuid(18),oldToken=uuid(181),newToken=uuid(182);
  const reserve=token=>admission.fetch(new Request('https://admission/'+id,{method:'POST',headers:token?{[leaseHeader]:token}:{}}));
  const release=token=>admission.fetch(new Request('https://admission/'+id,{method:'DELETE',headers:token?{[leaseHeader]:token}:{}}));
  const before=Date.now();assert.equal((await reserve(oldToken)).status,200);
  let lease=await storage.get('active:'+id);assert(lease.expiresAt>=before+60000&&lease.expiresAt<=Date.now()+60000);
  await storage.put('active:'+id,{...lease,expiresAt:0});assert.equal((await reserve(newToken)).status,200);
  assert.equal((await release(oldToken)).status,409);assert.equal((await release()).status,409);
  assert.equal((await storage.get('active:'+id)).token,newToken);
  assert.equal((await release(newToken)).status,200);assert.equal((await release(newToken)).status,200);
  assert.equal((await reserve()).status,200);assert.equal((await release(oldToken)).status,409);
  assert.equal(typeof await storage.get('active:'+id),'number');assert.equal((await release()).status,200);
});
test('actor refusals before fetch and forged completed replies cannot release unconfirmed batch capacity',async()=>{
  for(const reply of [async()=>{throw Error('There is no container instance that can be provided to this Durable Object')},async()=>Response.json({execution:{...input},cancelled:true})]){
    const f=batchFixture();f.env.CODE_CORE_JOB={idFromName:id=>id,get:()=>({fetch:reply})};
    const response=await f.run(19);assert([200,429].includes(response.status));
    const active=await f.active();assert.equal(active.size,1);assert.equal(f.state.releaseCalls,0);
    const [key,lease]=[...active][0];await f.admissionStorage.put(key,{...lease,expiresAt:0});
    assert.equal((await f.run(20)).status,response.status);assert.equal((await f.active()).size,1);
  }
});
