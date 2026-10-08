import assert from 'node:assert/strict';
import test from 'node:test';
import {serve, CONTAINER_NAME} from '../.test-build/gateway.js';
import {call, request, result, token} from './fixtures.mjs';

function bindings(fetch=async()=>new Response(JSON.stringify(result),{headers:{'Content-Type':'application/json'}})) {
  const observed=[];
  return {observed, FIGURE_TOKEN:token, STUDY_HOST_ENABLED:'true',STUDY_CONTAINER:{getByName(name){observed.push(name);return {fetch};}}};
}

test('health, disabled origin, missing token and unauthorized traffic never start containers',async()=>{
  for(const [env,req,status] of [
    [bindings(),new Request('https://tools-sandbox.edsger.app/health'),200],
    [{...bindings(),STUDY_HOST_ENABLED:'false'},request(),503],
    [{...bindings(),FIGURE_TOKEN:undefined},request(),503],
    [bindings(),request('{}',{headers:{'Content-Type':'application/json'}}),401],
    [bindings(),request('{}',{headers:{Authorization:`Bearer ${'x'.repeat(32)}`,'Content-Type':'application/json'}}),401],
  ]) {
    const response=await serve(req,env);assert.equal(response.status,status);assert.equal(env.observed.length,0);
    assert.equal(response.headers.get('Cache-Control'),'no-store');assert.equal(response.headers.get('X-Content-Type-Options'),'nosniff');
  }
});

test('one through thirty questions use one fixed object and send only the origin credential',async()=>{
  const env=bindings(async req=>{
    assert.equal(req.url,'http://container/v1/tools/call');assert.equal(req.method,'POST');
    assert.deepEqual([...req.headers.keys()].sort(),['authorization','content-type']);
    assert.equal(req.headers.get('Authorization'),`Bearer ${token}`);
    const payload=await req.json();assert.equal(payload.tool,'make_study_app');assert([1,3,5,30].includes(payload.arguments.questions.length));
    return new Response(JSON.stringify(result),{headers:{'Content-Type':'application/json'}});
  });
  for(const n of [1,3,5,30]) {
    const response=await serve(request(JSON.stringify(call(n)),{headers:{Authorization:`Bearer ${token}`,'Content-Type':'application/json','X-Apple-Transaction-JWS':'synthetic-not-a-receipt','X-User-Selected-Origin':'https://attacker.invalid'}}),env);
    assert.equal(response.status,200);assert.deepEqual(await response.json(),result);
  }
  assert.deepEqual(env.observed,[CONTAINER_NAME,CONTAINER_NAME,CONTAINER_NAME,CONTAINER_NAME]);
});

test('all other tools, routes and ambiguous or oversized payloads are refused before object lookup',async()=>{
  const malformed=[
    JSON.stringify({...call(),tool:'calculate'}), JSON.stringify(call(0)),JSON.stringify(call(31)),
    JSON.stringify({...call(),url:'https://attacker.invalid'}),
    '{"tool":"calculate","tool":"make_study_app","arguments":{}}',
    '{"tool":"make_study_app","\\u0074ool":"make_study_app","arguments":{}}',
    JSON.stringify(call()).replace('Arithmetic practice','\\ud800'),
    JSON.stringify(call()).replace('Arithmetic practice','\\udc00'),
    JSON.stringify(call()).replace('Arithmetic practice','\\u0000'),
    JSON.stringify(call()).replace('Arithmetic practice','🤓'.repeat(31)),
    JSON.stringify(call()).replace('"correct_index":1','"correct_index":8'),
    JSON.stringify(call()).replace('"choices":["4","5","6"]','"choices":["5"," 5 "]'),
    JSON.stringify(call())+' {}', '[]','null', '{',
  ];
  for(const payload of malformed) {
    const env=bindings(),res=await serve(request(payload),env);assert.equal(res.status,400);assert.equal(env.observed.length,0);
  }
  for(const path of ['/v1/tools','/v1/tools/make_study_app','/v1/sessions','/v1/tools/%63all']) {
    const env=bindings(),res=await serve(new Request('https://tools-sandbox.edsger.app'+path),env);assert.equal(res.status,404);assert.equal(env.observed.length,0);
  }
  for(const req of [request(' '.repeat(262145)),request('{}',{headers:{Authorization:`Bearer ${token}`,'Content-Type':'application/json','Content-Length':'262145'}})]) {
    const env=bindings(),res=await serve(req,env);assert.equal(res.status,413);assert.equal(env.observed.length,0);
  }
  const env=bindings(),res=await serve(request(new ReadableStream({start(controller){controller.enqueue(new Uint8Array(262145));controller.close();}}),{duplex:'half'}),env);
  assert.equal(res.status,413);assert.equal(env.observed.length,0);
});

test('plain HTTP, old TLS, URL credentials, query values, upgrades and unsupported media are rejected',async()=>{
  const badTls=request();Object.defineProperty(badTls,'cf',{value:{tlsVersion:'TLSv1.1'}});
  for(const [req,status] of [
    [new Request('http://tools-sandbox.edsger.app/health'),400], [badTls,400],
    [new Request('https://tools-sandbox.edsger.app/health?token=x'),400],
    [request('{}',{headers:{Authorization:`Bearer ${token}`,'Content-Type':'text/plain'}}),415],
    [request('{}',{headers:{Authorization:`Bearer ${token}`,'Content-Type':'application/json;foo=bar'}}),415],
    [request('{}',{headers:{Authorization:`Bearer ${token}`,'Content-Type':'application/json','Content-Encoding':'gzip'}}),415],
    [request('{}',{headers:{Authorization:`Bearer ${token}`,'Content-Type':'application/json',Upgrade:'websocket'}}),400],
  ]) {const env=bindings();assert.equal((await serve(req,env)).status,status);assert.equal(env.observed.length,0);}
});

test('redirects, upstream failures and unsafe artifacts return only fixed errors',async()=>{
  for(const response of [
    new Response('private-content',{status:302,headers:{Location:'https://attacker.invalid'}}),
    new Response('private-content',{status:500}),
    new Response('private-content',{headers:{'Content-Type':'text/plain'}}),
    new Response(JSON.stringify({...result,result:{...result.result,network_requests:true}}),{headers:{'Content-Type':'application/json'}}),
    new Response(JSON.stringify({...result,result:{...result.result,content_verified:true}}),{headers:{'Content-Type':'application/json'}}),
    new Response(' '.repeat(1100001),{headers:{'Content-Type':'application/json'}}),
  ]) {
    const res=await serve(request(),bindings(async()=>response));assert.equal(res.status,503);
    const body=await res.text();assert(!body.includes('private-content'));assert(!body.includes(token));
  }
  const rate=await serve(request(),bindings(async()=>new Response('private-content',{status:429})));
  assert.equal(rate.status,429);assert.equal(rate.headers.get('Retry-After'),'60');
  const failed=await serve(request(),bindings(async()=>{throw Error('private-content '+token);}));
  assert.deepEqual(await failed.json(),{ok:false,error:'service_unavailable',code:'service_unavailable'});
});

test('observed cancellation returns promptly and late success cannot reach the caller',async()=>{
  const controller=new AbortController();let complete,started;
  const entered=new Promise(resolve=>{started=resolve;});
  const env=bindings(async()=>{started();return new Promise(resolve=>{complete=resolve;});});
  const pending=serve(request(undefined,{signal:controller.signal}),env);
  await entered;controller.abort();const res=await pending;assert.equal(res.status,409);
  complete(new Response(JSON.stringify(result),{headers:{'Content-Type':'application/json'}}));
  const aborted=new AbortController();aborted.abort();const untouched=bindings();
  assert.equal((await serve(request(undefined,{signal:aborted.signal}),untouched)).status,409);assert.equal(untouched.observed.length,0);
});

test('the cold-start budget bounds an origin which never returns',async t=>{
  t.mock.timers.enable({apis:['setTimeout']});let started;
  const entered=new Promise(resolve=>{started=resolve;});
  const pending=serve(request(),bindings(async()=>{started();return new Promise(()=>{});}));
  await entered;t.mock.timers.tick(14000);
  const res=await pending;assert.equal(res.status,503);assert.equal(res.headers.get('Retry-After'),'5');
});
