// Execute the complete bundle in workerd, then exercise the admission layer
// through an actual SQLite Durable Object. Optional localhost Go origin adds
// real Docker rendering to the same path; no paid backend or model is contacted.
import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import {mkdtemp, writeFile, rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {join, resolve} from 'node:path';
import {Miniflare, convertV4MiniflareOptions} from 'miniflare';
import {call, result, token} from './fixtures.mjs';

const origin=process.env.STUDY_TEST_ORIGIN;
if(origin && !/^http:\/\/127\.0\.0\.1:\d{1,5}$/.test(origin)) throw Error('test origin must be an explicit localhost HTTP port');
const dir=await mkdtemp(join(tmpdir(),'edsger-study-runtime-'));
const cli=resolve('node_modules/wrangler/bin/wrangler.js');
const env={...process.env,WRANGLER_SEND_METRICS:'false',WRANGLER_LOG_PATH:join(dir,'wrangler.log')};
const build=(args,config=resolve('wrangler.toml'))=>execFileSync(process.execPath,[cli,'deploy','--config',config,'--dry-run',...args],{stdio:'pipe',env});
const options=(script,extra={})=>convertV4MiniflareOptions({rootPath:dir,modulesRoot:dir,modules:true,scriptPath:script,compatibilityDate:'2026-10-08',compatibilityFlags:['nodejs_compat'],...extra});
try {
  const full=join(dir,'full');build(['--outdir',full]);
  const mf=new Miniflare(options(join(full,'index.js'),{durableObjects:{STUDY_CONTAINER:{className:'StudyContainer',useSQLite:true},WORKSHEET_CONTAINER:{className:'WorksheetContainer',useSQLite:true},WORKSHEET_ADMISSION:{className:'WorksheetAdmission',useSQLite:true}},ratelimits:{WORKSHEET_IP_LIMIT:{namespace_id:'1003',simple:{limit:4,period:60}}},bindings:{FIGURE_TOKEN:token,STUDY_HOST_ENABLED:'true',WORKSHEET_HOST_ENABLED:'true'}}));
  try {
    const health=await mf.dispatchFetch('https://tools-sandbox.edsger.app/health');assert.equal(health.status,200);assert.equal((await health.json()).service,'edsger-study-tool-edge');
    for(const [path,body,status] of [['/v1/tools/call',JSON.stringify(call()),401],['/v1/sessions','{}',404],['/v1/tools/make_study_app','{}',404]]) {
      const response=await mf.dispatchFetch('https://tools-sandbox.edsger.app'+path,{method:'POST',body});assert.equal(response.status,status);
    }
    const bad=await mf.dispatchFetch('https://tools-sandbox.edsger.app/v1/tools/call',{method:'POST',headers:{Authorization:`Bearer ${token}`,'Content-Type':'application/json'},body:'{"tool":"calculate","arguments":{}}'});assert.equal(bad.status,400);
    for(let n=0;n<5;n++) {
      const rate=await mf.dispatchFetch('https://tools-sandbox.edsger.app/v1/worksheets',{method:'POST',headers:{'CF-Connecting-IP':'198.51.100.2','X-Edsger-Worksheet-Consent':'v1','Content-Type':'application/json'},body:'{}'});
      assert.equal(rate.status,n<4?400:429);
    }
    console.log('PASS actual Workers IP rate binding limits anonymous worksheet probes before admission or container work');
    console.log('PASS: full Workers/Containers bundle starts; probes and refused tools never require a container');
  } finally {await mf.dispose();}

  const harness=join(dir,'harness.ts');
  await writeFile(harness,`import { DurableObject } from "cloudflare:workers";
import { serve } from ${JSON.stringify(resolve('src/gateway.ts'))};
export class FixtureContainer extends DurableObject { async fetch(request){return fetch("http://fixture-origin/v1/tools/call",request);} }
export default {fetch:serve};`);
  const output=join(dir,'harness');
  const harnessConfig=join(dir,'harness.json');
  await writeFile(harnessConfig,JSON.stringify({name:'study-runtime-fixture',main:harness,compatibility_date:'2026-10-08',compatibility_flags:['nodejs_compat'],durable_objects:{bindings:[{name:'STUDY_CONTAINER',class_name:'FixtureContainer'}]},migrations:[{tag:'v1',new_sqlite_classes:['FixtureContainer']}]}));
  build(['--outdir',output],harnessConfig);
  let executions=0;
  const pipeline=new Miniflare(options(join(output,'harness.js'),{durableObjects:{STUDY_CONTAINER:{className:'FixtureContainer',useSQLite:true}},bindings:{FIGURE_TOKEN:token,STUDY_HOST_ENABLED:'true'},outboundService:async req=>{
    executions++;assert.equal(req.url,'http://fixture-origin/v1/tools/call');assert.equal(req.headers.get('Authorization'),`Bearer ${token}`);
    assert.equal(req.headers.get('X-Apple-Transaction-JWS'),null);
    const body=await req.text();const parsed=JSON.parse(body);assert.equal(parsed.tool,'make_study_app');
    if(origin) return fetch(origin+'/v1/tools/call',{method:'POST',headers:{Authorization:`Bearer ${token}`,'Content-Type':'application/json'},body,signal:AbortSignal.timeout(5000)});
    return new Response(JSON.stringify(result),{headers:{'Content-Type':'application/json'}});
  }}));
  try {
    for(const count of [1,3,5,30]) {
      const args={method:'POST',headers:{Authorization:`Bearer ${token}`,'Content-Type':'application/json','X-Apple-Transaction-JWS':'synthetic-not-a-receipt'},body:JSON.stringify(call(count))};
      const response=await pipeline.dispatchFetch('https://tools-sandbox.edsger.app/v1/tools/call',args);assert.equal(response.status,200);
      const actual=await response.json();assert.equal(actual.tool,'make_study_app');assert.equal(actual.result.network_requests,false);assert.equal(actual.result.content_verified,false);assert.equal(actual.result.offline,true);assert(actual.result.html.startsWith('<!doctype html>'));
      if(origin) {assert(actual.result.html.includes('Retry missed'));assert(actual.result.html.includes('Arithmetic practice'));}
      const replay=await pipeline.dispatchFetch('https://tools-sandbox.edsger.app/v1/tools/call',args);assert.equal(replay.status,200);assert.deepEqual(await replay.json(),actual);
    }
    assert.equal(executions,8);
    console.log(`PASS: real Workers SQLite transport handles 1/3/5/30-question quizzes and identical replays${origin?' through the real Go Docker image':''}`);
  } finally {await pipeline.dispose();}
} finally {await rm(dir,{recursive:true,force:true});}
