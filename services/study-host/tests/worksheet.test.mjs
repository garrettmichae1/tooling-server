import assert from 'node:assert/strict';
import test from 'node:test';
import {serve} from '../.test-build/gateway.js';
import {WORKSHEET_CONTAINER_NAME,WORKSHEET_ADMISSION_NAME} from '../.test-build/worksheet-gateway.js';
import {token} from './fixtures.mjs';
export const worksheetCall=(n=3)=>({tool:'make_worksheet',arguments:{title:'Arithmetic worksheet',instructions:'Show your working.',exercises:Array.from({length:n},(_,i)=>({prompt:`Exercise ${i+1}: 2 + 3?`,answer:'5',explanation:'Two and three sum to five.'}))}});
export const worksheetResult={ok:true,tool:'make_worksheet',result:{filename:'worksheet.html',media_type:'text/html; charset=utf-8',html:'<!doctype html><html>Questions</html>',answer_key_html:'<!doctype html><html>Answer key</html>',offline:true,network_requests:false,preview_requires_scripts:false,content_verified:false}};
export const worksheetRequest=(payload=JSON.stringify(worksheetCall()),extra={})=>new Request('https://tools-sandbox.edsger.app/v1/worksheets',{method:'POST',headers:{'Content-Type':'application/json','CF-Connecting-IP':'192.0.2.1','X-Edsger-Worksheet-Consent':'v1',...extra.headers},body:payload,...(extra.signal?{signal:extra.signal}:{})});
function bindings(options={}) {
 const events=[];
 return {events,FIGURE_TOKEN:token,WORKSHEET_HOST_ENABLED:'true',STUDY_CONTAINER:{getByName(){throw Error('quiz container reached');}},
  WORKSHEET_IP_LIMIT:{async limit({key}){events.push('ip');assert.equal(key,'192.0.2.1');return {success:!options.ipDenied};}},
  WORKSHEET_ADMISSION:{getByName(name){assert.equal(name,WORKSHEET_ADMISSION_NAME);return {async fetch(req){assert.equal(await req.text(),'');assert.deepEqual([...req.headers],[]);events.push('budget');return new Response(null,{status:options.budgetDenied?429:204});}}; }},
  WORKSHEET_CONTAINER:{getByName(name){assert.equal(name,WORKSHEET_CONTAINER_NAME);events.push('worksheet');return {async fetch(req){assert.deepEqual([...req.headers.keys()].sort(),['authorization','content-type']);assert.equal(req.headers.get('Authorization'),`Bearer ${token}`);assert.equal((await req.json()).tool,'make_worksheet');return options.fetch?options.fetch(req):new Response(JSON.stringify(worksheetResult),{headers:{'Content-Type':'application/json'}});}};}}
 };
}
test('free worksheet admits no receipt or key, uses its own budget/container, and permits 1/3/5/30 exercises',async()=>{
 for(const n of [1,3,5,30]) {const env=bindings(),r=await serve(worksheetRequest(JSON.stringify(worksheetCall(n))),env);assert.equal(r.status,200);assert.deepEqual(await r.json(),worksheetResult);assert.deepEqual(env.events,['ip','budget','worksheet']);}
});
test('independent gate and required controls fail closed before body/container work',async()=>{
 for(const missing of ['WORKSHEET_IP_LIMIT','WORKSHEET_ADMISSION','WORKSHEET_CONTAINER','FIGURE_TOKEN']) {const env=bindings();delete env[missing];assert.equal((await serve(worksheetRequest(),env)).status,503);assert.deepEqual(env.events,[]);}
 const disabled=bindings();disabled.WORKSHEET_HOST_ENABLED='false';assert.equal((await serve(worksheetRequest(),disabled)).status,503);assert.deepEqual(disabled.events,[]);
 const req=worksheetRequest();req.headers.delete('CF-Connecting-IP');const env=bindings();assert.equal((await serve(req,env)).status,503);assert.deepEqual(env.events,[]);
});
test('IP/global caps and consent cannot be bypassed by headers or caller-selected origins',async()=>{
 for(const opt of [{ipDenied:true},{budgetDenied:true}]) {const env=bindings(opt),r=await serve(worksheetRequest(),env);assert.equal(r.status,429);assert(!env.events.includes('worksheet'));}
 const req=worksheetRequest();req.headers.delete('X-Edsger-Worksheet-Consent');const env=bindings();assert.equal((await serve(req,env)).status,403);assert.deepEqual(env.events,['ip']);
});
test('ambiguous JSON, code, oversized/unknown fields and other tools do not spend the global budget',async()=>{
 for(const body of [JSON.stringify(worksheetCall(0)),JSON.stringify(worksheetCall(31)),JSON.stringify({...worksheetCall(),tool:'make_study_app'}),JSON.stringify({...worksheetCall(),url:'https://invalid'}),JSON.stringify(worksheetCall()).replace('Arithmetic worksheet','\\ud800'),JSON.stringify(worksheetCall()).replace('Arithmetic worksheet','🤓'.repeat(31)),JSON.stringify(worksheetCall()).replace('"answer":"5"','"answer":" "'),'{"tool":"calculate","tool":"make_worksheet","arguments":{}}','[]','null','{']) {
  const env=bindings();assert.equal((await serve(worksheetRequest(body),env)).status,400);assert.deepEqual(env.events,['ip']);
 }
 const env=bindings();assert.equal((await serve(worksheetRequest(' '.repeat(262145)),env)).status,413);assert.deepEqual(env.events,['ip']);
});
test('worksheet safety flags, response byte cap, redirects and errors produce fixed failures only',async()=>{
 for(const response of [new Response('private-content',{status:302,headers:{Location:'https://invalid'}}),new Response('private-content',{status:500}),new Response(JSON.stringify({...worksheetResult,result:{...worksheetResult.result,preview_requires_scripts:true}}),{headers:{'Content-Type':'application/json'}}),new Response(' '.repeat(1100001),{headers:{'Content-Type':'application/json'}})]) {
  const r=await serve(worksheetRequest(),bindings({fetch:async()=>response}));assert.equal(r.status,503);assert(!(await r.text()).includes('private-content'));
 }
});
test('cancellation and deadline stop worksheet responses without changing quiz bindings',async t=>{
 let entered,complete;const start=new Promise(resolve=>entered=resolve);const ctl=new AbortController();
 const pending=serve(worksheetRequest(undefined,{signal:ctl.signal}),bindings({fetch:async()=>{entered();return new Promise(resolve=>complete=resolve);}}));
 await start;ctl.abort();assert.equal((await pending).status,409);complete(new Response(JSON.stringify(worksheetResult),{headers:{'Content-Type':'application/json'}}));
 t.mock.timers.enable({apis:['setTimeout']});let begun;const ready=new Promise(resolve=>begun=resolve);
 const waiting=serve(worksheetRequest(),bindings({fetch:async()=>{begun();return new Promise(()=>{});}}));await ready;t.mock.timers.tick(14000);assert.equal((await waiting).status,503);
});
