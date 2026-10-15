import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import {mkdtemp,writeFile,rm} from 'node:fs/promises';
import {join,resolve} from 'node:path';
import {tmpdir} from 'node:os';
import {Miniflare,convertV4MiniflareOptions} from 'miniflare';
import {token} from './fixtures.mjs';
import {call,envelope} from './word-search-fixtures.mjs';
const origin=process.env.WORD_SEARCH_TEST_ORIGIN;
if(origin&&!/^http:\/\/127\.0\.0\.1:\d{1,5}$/.test(origin))throw Error('explicit localhost origin required');
const dir=await mkdtemp(join(tmpdir(),'word-search-runtime-'));

try {
 const full=join(dir,'full');
 execFileSync(process.execPath,[resolve('node_modules/wrangler/bin/wrangler.js'),'deploy','--dry-run','--containers-rollout','none','--env','','--outdir',full],{stdio:'pipe',env:{...process.env,WRANGLER_SEND_METRICS:'false',WRANGLER_LOG_PATH:join(dir,'wrangler.log')}});
 const real=new Miniflare(convertV4MiniflareOptions({rootPath:dir,modulesRoot:dir,modules:true,scriptPath:join(full,'index.js'),compatibilityDate:'2026-10-08',compatibilityFlags:['nodejs_compat'],durableObjects:{STUDY_CONTAINER:{className:'StudyContainer',useSQLite:true},WORKSHEET_CONTAINER:{className:'WorksheetContainer',useSQLite:true},WORKSHEET_ADMISSION:{className:'WorksheetAdmission',useSQLite:true},WORD_SEARCH_CONTAINER:{className:'WordSearchContainer',useSQLite:true},WORD_SEARCH_ADMISSION:{className:'WordSearchAdmission',useSQLite:true}},ratelimits:{WORD_SEARCH_IP_LIMIT:{namespace_id:'1005',simple:{limit:4,period:60}}},bindings:{FIGURE_TOKEN:token,STUDY_HOST_ENABLED:'true',WORD_SEARCH_HOST_ENABLED:'true'}}));
 try {
  const health=await (await real.dispatchFetch('https://tools-sandbox.edsger.app/health')).json();assert.equal(health.word_search_enabled,true);assert.equal(health.enabled,true);assert.equal(health.worksheet_enabled,false);
  for(let n=0;n<5;n++){const response=await real.dispatchFetch('https://tools-sandbox.edsger.app/v1/word-searches',{method:'POST',headers:{'CF-Connecting-IP':'198.51.100.25','Content-Type':'application/json','X-Edsger-Word-Search-Consent':'v1'},body:'{}'});assert.equal(response.status,n<4?400:429);}
  console.log('PASS actual full Workers bundle and separate word-search IP limit without a container wake');
 } finally {await real.dispose();}
 const harness=join(dir,'harness.ts');
 // Clock control exists only in this isolated fixture subclass, never the app
 // bundle. The actual production class/storage transaction enforces every cap.
 await writeFile(harness,`import {DurableObject} from "cloudflare:workers";
import {serve} from ${JSON.stringify(resolve('src/gateway.ts'))};
import {WordSearchAdmission} from ${JSON.stringify(resolve('src/word-search-admission.ts'))};
const RealDate=Date;let clock=RealDate.now();globalThis.Date=class extends RealDate {static now(){return clock;}};
export class AdmissionFixture extends WordSearchAdmission {async fetch(request){
 const value=request.headers.get("X-Test-Time");if(value)clock=Number(value);
 if(new URL(request.url).pathname==="/test-state") {
  if(request.method==="POST") {const data=await request.json();await this.ctx.storage.put("budget",data.nan?{...data.value,recent:NaN}:data.value);return new Response(null,{status:204});}
  return new Response(JSON.stringify(await this.ctx.storage.get("budget")),{headers:{"Content-Type":"application/json"}});
 }
 return super.fetch(request);
}}
export class WordSearchFixture extends DurableObject {async fetch(request){return fetch("http://word-search-origin/v1/tools/call",request);}}
export default {fetch(request,env){return serve(request,{...env,WORD_SEARCH_IP_LIMIT:{limit:async()=>({success:true})}});}};`);
 const config=join(dir,'wrangler.json'),output=join(dir,'bundle');
 await writeFile(config,JSON.stringify({name:'word-search-runtime-fixture',main:harness,compatibility_date:'2026-10-08',compatibility_flags:['nodejs_compat'],durable_objects:{bindings:[{name:'WORD_SEARCH_ADMISSION',class_name:'AdmissionFixture'},{name:'WORD_SEARCH_CONTAINER',class_name:'WordSearchFixture'}]},migrations:[{tag:'v1',new_sqlite_classes:['AdmissionFixture','WordSearchFixture']}]}));
 execFileSync(process.execPath,[resolve('node_modules/wrangler/bin/wrangler.js'),'deploy','--config',config,'--dry-run','--outdir',output],{stdio:'pipe',env:{...process.env,WRANGLER_SEND_METRICS:'false',WRANGLER_LOG_PATH:join(dir,'wrangler.log')}});
 const options={rootPath:dir,modulesRoot:dir,modules:true,scriptPath:join(output,'harness.js'),compatibilityDate:'2026-10-08',compatibilityFlags:['nodejs_compat'],durableObjects:{WORD_SEARCH_ADMISSION:{className:'AdmissionFixture',useSQLite:true},WORD_SEARCH_CONTAINER:{className:'WordSearchFixture',useSQLite:true}},bindings:{FIGURE_TOKEN:token,WORD_SEARCH_HOST_ENABLED:'true'}};
 let calls=0;
 const make=()=>new Miniflare(convertV4MiniflareOptions({...options,resourcePersistencePath:join(dir,'state'),outboundService:async req=>{
  calls++;assert.equal(req.url,'http://word-search-origin/v1/tools/call');assert([...req.headers.keys()].every(name=>['authorization','content-type','content-length','host','cf-worker'].includes(name)));assert(!req.headers.has('X-Apple-Transaction-JWS'));assert(!req.headers.has('X-User-Selected-Origin'));assert.equal(req.headers.get('Authorization'),`Bearer ${token}`);
  const body=await req.text();assert.equal(JSON.parse(body).tool,'make_word_search');
  if(origin)return fetch(origin+'/v1/tools/call',{method:'POST',headers:{Authorization:`Bearer ${token}`,'Content-Type':'application/json'},body,signal:AbortSignal.timeout(5000)});
  return new Response(JSON.stringify(envelope(JSON.parse(body))),{headers:{'Content-Type':'application/json'}});
 }}));
 let mf=make();
 try {
  const firstNamespace=await mf.getDurableObjectNamespace('WORD_SEARCH_ADMISSION');
  const probe=await firstNamespace.get(firstNamespace.idFromName('word-search-admission-v1')).fetch('http://admission/admit',{method:'POST'});
  assert.equal(probe.status,204,await probe.text());
  for(const n of [1,3,5,30]) for(const difficulty of ['easy','medium','hard']) {
   const args={method:'POST',headers:{'CF-Connecting-IP':'192.0.2.1','Content-Type':'application/json','X-Edsger-Word-Search-Consent':'v1'},body:JSON.stringify(call(n,difficulty))};
   const res=await mf.dispatchFetch('https://tools-sandbox.edsger.app/v1/word-searches',args);assert.equal(res.status,200,res.status===200?undefined:await res.text());const value=await res.json();assert.equal(value.tool,'make_word_search');assert.equal(value.result.preview_requires_scripts,false);assert.equal(value.result.content_verified,false);
   assert.equal(value.result.words.length,n);assert.equal(value.result.difficulty,difficulty);assert.equal(value.result.puzzle_verified,true);assert.equal(value.result.placements.length,n);
   if(origin){assert(!value.result.html.includes('class="found"'));assert(value.result.answer_key_html.includes('class="found"'));}
   const replay=await mf.dispatchFetch('https://tools-sandbox.edsger.app/v1/word-searches',args);assert.equal(replay.status,200);assert.deepEqual(await replay.json(),value);
  }
  assert.equal(calls,24);
  let ns=await mf.getDurableObjectNamespace('WORD_SEARCH_ADMISSION');let stub=ns.get(ns.idFromName('word-search-admission-v1'));
  const day=21000*86400000;
  const admit=(time)=>stub.fetch('http://admission/admit',{method:'POST',headers:{'X-Test-Time':String(time)}});
  for(let n=0;n<30;n++)assert.equal((await admit(day)).status,204);
  assert.equal((await admit(day)).status,429);
  for(let n=30;n<500;n++)assert.equal((await admit(day+(n-29)*60000)).status,204);
  assert.equal((await admit(day+600*60000)).status,429);
  await mf.dispose();mf=make();ns=await mf.getDurableObjectNamespace('WORD_SEARCH_ADMISSION');stub=ns.get(ns.idFromName('word-search-admission-v1'));
  assert.equal((await admit(day+601*60000)).status,429);
  assert.equal((await admit(day+86400000)).status,204);
  const currentDay=21001,currentMinute=currentDay*1440;
  const valid={day:currentDay,used:1,minute:currentMinute,recent:1};
  const corrupt=[0,false,null,'malformed',[],{}, {...valid,extra:1}, {...valid,used:0}, {...valid,used:501}, {...valid,recent:31}, {...valid,recent:2}, {...valid,day:-1}, {...valid,minute:1.5}, {...valid,day:currentDay+1,minute:currentMinute+1440}, {...valid,minute:currentMinute+1}, {...valid,day:currentDay-1}, {...valid,used:'1'}, {...valid,recent:null}];
  for(const value of corrupt){
   assert.equal((await stub.fetch('http://admission/test-state',{method:'POST',body:JSON.stringify({value})})).status,204);
   assert.equal((await admit(day+86400000)).status,500,'corrupt budget was treated as absent');
   assert.deepEqual(await (await stub.fetch('http://admission/test-state')).json(),value,'corrupt budget was silently reset');
   const refused=await mf.dispatchFetch('https://tools-sandbox.edsger.app/v1/word-searches',{method:'POST',headers:{'CF-Connecting-IP':'192.0.2.1','Content-Type':'application/json','X-Edsger-Word-Search-Consent':'v1'},body:JSON.stringify(call())});assert.equal(refused.status,503);
  }
  assert.equal((await stub.fetch('http://admission/test-state',{method:'POST',body:JSON.stringify({value:valid,nan:true})})).status,204);
  assert.equal((await admit(day+86400000)).status,500,'NaN persisted counts were accepted');
  assert.deepEqual(await (await stub.fetch('http://admission/test-state')).json(),{...valid,recent:null});
  assert.equal(calls,24,'budget requests reached a renderer');
  console.log('PASS actual Workers/SQLite word-search pipeline, identical 1/3/5/30 replays at all difficulties and independent uniqueness/hash validation, persisted 30/minute and 500/day caps, UTC rollover and corrupt/future SQLite state fail-closed checks'+(origin?' through real nonroot Go container':''));
 } finally {await mf.dispose();}
} finally {await rm(dir,{recursive:true,force:true});}
