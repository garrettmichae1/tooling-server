import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import {mkdtemp,writeFile,rm} from 'node:fs/promises';
import {join,resolve} from 'node:path';
import {tmpdir} from 'node:os';
import {Miniflare,convertV4MiniflareOptions} from 'miniflare';
import {token} from './fixtures.mjs';
const origin=process.env.WORKSHEET_TEST_ORIGIN;
if(origin&&!/^http:\/\/127\.0\.0\.1:\d{1,5}$/.test(origin))throw Error('explicit localhost origin required');
const dir=await mkdtemp(join(tmpdir(),'worksheet-runtime-'));
const fixture=n=>({tool:'make_worksheet',arguments:{title:'Arithmetic worksheet',instructions:'Show your working.',exercises:Array.from({length:n},(_,i)=>({prompt:`Exercise ${i+1}: 2 + 3?`,answer:'5',explanation:'Two and three sum to five.'}))}});
try {
 const harness=join(dir,'harness.ts');
 // Clock control exists only in this isolated fixture subclass, never the app
 // bundle. The actual production class/storage transaction enforces every cap.
 await writeFile(harness,`import {DurableObject} from "cloudflare:workers";
import {serve} from ${JSON.stringify(resolve('src/gateway.ts'))};
import {WorksheetAdmission} from ${JSON.stringify(resolve('src/worksheet-admission.ts'))};
const RealDate=Date;let clock=RealDate.now();globalThis.Date=class extends RealDate {static now(){return clock;}};
export class AdmissionFixture extends WorksheetAdmission {async fetch(request){const value=request.headers.get("X-Test-Time");if(value)clock=Number(value);return super.fetch(request);}}
export class WorksheetFixture extends DurableObject {async fetch(request){return fetch("http://worksheet-origin/v1/tools/call",request);}}
export default {fetch(request,env){return serve(request,{...env,WORKSHEET_IP_LIMIT:{limit:async()=>({success:true})}});}};`);
 const config=join(dir,'wrangler.json'),output=join(dir,'bundle');
 await writeFile(config,JSON.stringify({name:'worksheet-runtime-fixture',main:harness,compatibility_date:'2026-10-08',compatibility_flags:['nodejs_compat'],durable_objects:{bindings:[{name:'WORKSHEET_ADMISSION',class_name:'AdmissionFixture'},{name:'WORKSHEET_CONTAINER',class_name:'WorksheetFixture'}]},migrations:[{tag:'v1',new_sqlite_classes:['AdmissionFixture','WorksheetFixture']}]}));
 execFileSync(process.execPath,[resolve('node_modules/wrangler/bin/wrangler.js'),'deploy','--config',config,'--dry-run','--outdir',output],{stdio:'pipe',env:{...process.env,WRANGLER_SEND_METRICS:'false',WRANGLER_LOG_PATH:join(dir,'wrangler.log')}});
 const options={rootPath:dir,modulesRoot:dir,modules:true,scriptPath:join(output,'harness.js'),compatibilityDate:'2026-10-08',compatibilityFlags:['nodejs_compat'],durableObjects:{WORKSHEET_ADMISSION:{className:'AdmissionFixture',useSQLite:true},WORKSHEET_CONTAINER:{className:'WorksheetFixture',useSQLite:true}},bindings:{FIGURE_TOKEN:token,WORKSHEET_HOST_ENABLED:'true'}};
 let calls=0;
 const make=()=>new Miniflare(convertV4MiniflareOptions({...options,resourcePersistencePath:join(dir,'state'),outboundService:async req=>{
  calls++;assert.equal(req.url,'http://worksheet-origin/v1/tools/call');assert(!req.headers.has('X-Apple-Transaction-JWS'));assert(!req.headers.has('X-User-Selected-Origin'));assert.equal(req.headers.get('Authorization'),`Bearer ${token}`);
  const body=await req.text();assert.equal(JSON.parse(body).tool,'make_worksheet');
  if(origin)return fetch(origin+'/v1/tools/call',{method:'POST',headers:{Authorization:`Bearer ${token}`,'Content-Type':'application/json'},body,signal:AbortSignal.timeout(5000)});
  return new Response(JSON.stringify({ok:true,tool:'make_worksheet',result:{filename:'worksheet.html',media_type:'text/html; charset=utf-8',html:'<!doctype html><html>Questions</html>',answer_key_html:'<!doctype html><html>Answers</html>',offline:true,network_requests:false,preview_requires_scripts:false,content_verified:false}}),{headers:{'Content-Type':'application/json'}});
 }}));
 let mf=make();
 try {
  const firstNamespace=await mf.getDurableObjectNamespace('WORKSHEET_ADMISSION');
  const probe=await firstNamespace.get(firstNamespace.idFromName('worksheet-admission-v1')).fetch('http://admission/admit',{method:'POST'});
  assert.equal(probe.status,204,await probe.text());
  for(const n of [1,3,5,30]) {
   const args={method:'POST',headers:{'CF-Connecting-IP':'192.0.2.1','Content-Type':'application/json','X-Edsger-Worksheet-Consent':'v1'},body:JSON.stringify(fixture(n))};
   const res=await mf.dispatchFetch('https://tools-sandbox.edsger.app/v1/worksheets',args);assert.equal(res.status,200,res.status===200?undefined:await res.text());const value=await res.json();assert.equal(value.tool,'make_worksheet');assert.equal(value.result.preview_requires_scripts,false);assert.equal(value.result.content_verified,false);
   if(origin){assert(value.result.html.includes('Space for your answer'));assert(value.result.answer_key_html.includes('Adding')||value.result.answer_key_html.includes('sum to five'));assert(!value.result.html.includes('sum to five'));}
   const replay=await mf.dispatchFetch('https://tools-sandbox.edsger.app/v1/worksheets',args);assert.equal(replay.status,200);assert.deepEqual(await replay.json(),value);
  }
  assert.equal(calls,8);
  let ns=await mf.getDurableObjectNamespace('WORKSHEET_ADMISSION');let stub=ns.get(ns.idFromName('worksheet-admission-v1'));
  const day=21000*86400000;
  const admit=(time)=>stub.fetch('http://admission/admit',{method:'POST',headers:{'X-Test-Time':String(time)}});
  for(let n=0;n<30;n++)assert.equal((await admit(day)).status,204);
  assert.equal((await admit(day)).status,429);
  for(let n=30;n<500;n++)assert.equal((await admit(day+(n-29)*60000)).status,204);
  assert.equal((await admit(day+600*60000)).status,429);
  await mf.dispose();mf=make();ns=await mf.getDurableObjectNamespace('WORKSHEET_ADMISSION');stub=ns.get(ns.idFromName('worksheet-admission-v1'));
  assert.equal((await admit(day+601*60000)).status,429);
  assert.equal((await admit(day+86400000)).status,204);
  assert.equal(calls,8,'budget requests reached a renderer');
  console.log('PASS actual Workers/SQLite worksheet pipeline, identical 1/3/5/30 replays, persisted 30/minute and 500/day caps and UTC rollover'+(origin?' through real nonroot Go container':''));
 } finally {await mf.dispose();}
} finally {await rm(dir,{recursive:true,force:true});}
