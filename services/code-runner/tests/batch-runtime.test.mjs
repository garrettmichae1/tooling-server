import test from 'node:test';
import assert from 'node:assert/strict';
import {build} from 'esbuild';
import {Miniflare,convertV4MiniflareOptions} from 'miniflare';
import {mkdtemp,rm} from 'node:fs/promises';
import {join} from 'node:path';
import {tmpdir} from 'node:os';

// Actual Workers Durable Object storage/transactions and service-binding fetch;
// only the platform container is fake. This never allocates a VM or calls a host.
const dir=await mkdtemp(join(tmpdir(),'edsger-batch-runtime-'));
const outfile=join(dir,'worker.mjs');
await build({stdin:{contents:`
import {serve,CodeAdmission,CodeCoreJob} from './src/index.ts';
export class AuditAdmission extends CodeAdmission {
 async fetch(request){
  const path=new URL(request.url).pathname;
  if(path==='/audit')return Response.json(Object.fromEntries(await this.ctx.storage.list()));
  if(path==='/control'){await this.ctx.storage.put('failRelease',await request.json());return new Response('ok');}
  if(request.method==='DELETE'&&await this.ctx.storage.get('failRelease'))throw Error('test release failure');
  return super.fetch(request);
 }
}
export class AuditJob extends CodeCoreJob {
 constructor(ctx,env){
  let starts=0;
  const container={
   start:()=>{starts++;},setInactivityTimeout:async()=>{},
   destroy:async()=>{
    await ctx.storage.put('destroyAttempts',(await ctx.storage.get('destroyAttempts')??0)+1);
    if(await ctx.storage.get('failDestroy'))throw Error('test hard destruction failure');
   },
   getTcpPort:()=>({fetch:async request=>request.method==='GET'?new Response('{}'):Response.json({language:'python',status:'completed',stdout:'hello',stderr:'',exitCode:0,truncated:false})})
  };
  // Preserve the branded real DurableObjectState; only substitute its platform
  // container property. Storage, input gates and alarms remain workerd's own.
  Object.defineProperty(ctx,'container',{value:container});
  super(ctx,env);this.starts=()=>starts;
 }
 async fetch(request){
  const path=new URL(request.url).pathname;
  if(path==='/audit')return Response.json({...Object.fromEntries(await this.ctx.storage.list()),starts:this.starts(),alarm:await this.ctx.storage.getAlarm()});
  if(path==='/control'){await this.ctx.storage.put('failDestroy',await request.json());return new Response('ok');}
  if(path==='/trigger-alarm'){await this.ctx.storage.setAlarm(Date.now()+20);return new Response('ok');}
  if(path==='/retry-alarm'){await super.alarm();return new Response('ok');}
  return super.fetch(request);
 }
}
export default {async fetch(request,env,ctx){
 const url=new URL(request.url),parts=url.pathname.split('/');
 if(parts[1]==='audit-admission')return env.CODE_ADMISSION.get(env.CODE_ADMISSION.idFromName('global-v1')).fetch(new Request('https://actor/'+parts[2],request));
 if(parts[1]==='audit-job')return env.CODE_CORE_JOB.get(env.CODE_CORE_JOB.idFromName(parts[2])).fetch(new Request('https://actor/'+parts[3],request));
 return serve(request,env,ctx);
}};`,resolveDir:process.cwd(),sourcefile:'batch-runtime-fixture.ts',loader:'ts'},outfile,bundle:true,platform:'browser',format:'esm',external:['cloudflare:workers']});
let mf;
const runtimeOptions={...convertV4MiniflareOptions({name:'batch-audit',modules:true,modulesRoot:dir,scriptPath:outfile,compatibilityDate:'2026-10-08',bindings:{CODE_RUNNER_ENABLED:'true'},durableObjects:{CODE_ADMISSION:{className:'AuditAdmission',useSQLite:true},CODE_CORE_JOB:{className:'AuditJob',useSQLite:true}}}),telemetry:{enabled:false},resourcePersistencePath:join(dir,'storage')};
try {
 mf=new Miniflare(runtimeOptions);
 await mf.ready;
} catch(error){await rm(dir,{recursive:true,force:true});throw error;}
test.after(async()=>{await mf.dispose();await rm(dir,{recursive:true,force:true});});
const uuid=n=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const input={language:'python',entrypoint:'main.py',files:[{path:'main.py',content:'print("hello")'}],stdin:'',mode:'run'};
const call=async(path,body,method='POST')=>{
 const response=await mf.dispatchFetch('https://runner/'+path,{method,headers:{'Content-Type':'application/json','X-Edsger-Execution-Revision':'edsger-execution-v1'},...(body!==undefined?{body:JSON.stringify(body)}:{})});
 // Drain the local transport before eviction; retained response streams are
 // still outstanding Worker requests even when a test only inspected status.
 return new Response(await response.arrayBuffer(),{status:response.status,headers:response.headers});
};
const admission=async()=>Object.entries(await (await call('audit-admission/audit',undefined,'GET')).json()).filter(([key])=>key.startsWith('active:'));
const job=async n=>(await call('audit-job/'+uuid(n)+'/audit',undefined,'GET')).json();
const run=n=>call(uuid(n),input);

test('actual Workers batch storage holds eight failed-destroy leases and alarm retry reclaims only confirmed shutdowns',async()=>{
 for(let n=1;n<=8;n++){
  const control=await call('audit-job/'+uuid(n)+'/control',true);assert.equal(control.status,200,await control.text());
  assert.equal((await run(n)).status,502);
 }
 assert.equal((await admission()).length,8);
 assert.equal((await run(9)).status,429);assert.equal((await job(9)).starts,0);
 const failed=await job(1);assert.equal(failed.starts,1);assert.equal(failed.destroyAttempts,1);assert(failed.alarm>Date.now());assert.equal(failed.batchStopped,undefined);
 await call('audit-job/'+uuid(1)+'/control',false);
 assert.equal((await call('audit-job/'+uuid(1)+'/retry-alarm')).status,200);
 assert.equal((await admission()).length,7);const recovered=await job(1);assert.equal(recovered.batchStopped,true);assert.equal(recovered.batchReleased,true);assert.equal(recovered.alarm,null);
 assert.equal((await run(9)).status,200);assert.equal((await admission()).length,7);
 for(let n=2;n<=8;n++){await call('audit-job/'+uuid(n)+'/control',false);assert.equal((await call('audit-job/'+uuid(n)+'/retry-alarm')).status,200);}
 assert.equal((await admission()).length,0);
});
test('actual Workers release failure retains durable stopped state and alarm retries without another destruction',async()=>{
 await call('audit-admission/control',true);
 assert.equal((await run(10)).status,502);
 const failed=await job(10);assert.equal(failed.batchStopped,true);assert.equal(failed.destroyAttempts,1);assert(failed.alarm>Date.now());assert.equal((await admission()).length,1);
 await mf.dispose();mf=new Miniflare(runtimeOptions);await mf.ready;
 await call('audit-admission/control',false);
 assert.equal((await call('audit-job/'+uuid(10)+'/trigger-alarm')).status,200);
 for(let attempt=0;attempt<150&&(await admission()).length;attempt++)await new Promise(resolve=>setTimeout(resolve,20));
 assert.equal((await admission()).length,0);const recovered=await job(10);assert.equal(recovered.destroyAttempts,1);assert.equal(recovered.alarm,null);assert.equal(recovered.batchReleased,true);
 assert.equal(recovered.starts,0);
 assert.equal((await call('audit-job/'+uuid(10)+'/retry-alarm')).status,200);assert.equal((await job(10)).destroyAttempts,1);
});
