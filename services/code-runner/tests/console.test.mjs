import test from 'node:test';
import assert from 'node:assert/strict';
import {build} from 'esbuild';
import {mkdtemp, rm} from 'node:fs/promises';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
const dir = await mkdtemp(join(tmpdir(), 'edsger-console-'));
const outfile = join(dir, 'worker.mjs');
await build({entryPoints: ['src/index.ts'], outfile, bundle: true, platform: 'node', format: 'esm', plugins: [{name: 'do-context', setup(b) {
  b.onResolve({filter: /^cloudflare:workers$/}, () => ({path: 'shim', namespace: 'test'}));
  b.onLoad({filter: /.*/, namespace: 'test'}, () => ({contents: 'export class DurableObject {constructor(ctx,env){this.ctx=ctx;this.env=env}}', loader: 'js'}));
}}]});
const {serve, CodeCoreJob} = await import(outfile);
const helpers = join(dir, 'console.mjs');
await build({entryPoints: ['src/console.ts'], outfile: helpers, bundle: true, platform: 'node', format: 'esm'});
const {consumeConsole, consoleInput, interactiveExecutionInput} = await import(helpers);
test.after(() => rm(dir, {recursive: true, force: true}));
const headers = {'Content-Type': 'application/json', 'X-Edsger-Execution-Revision': 'edsger-execution-v1', 'X-Edsger-Console-Revision': 'edsger-console-v1'};
const input = {language: 'python', entrypoint: 'main.py', files: [{path: 'main.py', content: 'name=input("Name? "); print(name)'}], stdin: '', mode: 'run'};
const execution = {language: 'python', status: 'completed', stdout: '', stderr: '', exitCode: 0, truncated: false};
const uuid = '00000000-0000-4000-8000-000000000001';
const request = (path, body, extra = {}) => new Request('https://job/' + path, {method: 'POST', headers, body: JSON.stringify(body), ...extra});
const tick = () => new Promise(resolve => setTimeout(resolve, 0));
const frame = value => new TextEncoder().encode(JSON.stringify(value) + '\n');
function channel() {
  let controller;
  let cancelled = false;
  const body = new ReadableStream({start(c) {controller = c}, cancel() {cancelled = true}});
  return {body, send(value) {controller.enqueue(frame(value))}, close() {controller.close()}, get cancelled() {return cancelled}};
}
class Storage {
  data = new Map(); alarmTime = null; tail = Promise.resolve();
  async get(key) {return this.data.get(key)}
  async put(key, value) {this.data.set(key, structuredClone(value))}
  async delete(key) {this.data.delete(key)}
  async setAlarm(value) {this.alarmTime = Number(value)}
  async deleteAlarm() {this.alarmTime = null}
  async transaction(fn) {const old = this.tail; let release; this.tail = new Promise(r => release = r); await old; try {return await fn(this)} finally {release()}}
}
function fixture() {
  const stream = channel(), events = [], storage = new Storage(), pending = [];
  let inputResponse;
  const container = {
    start(options) {events.push(['start', options])},
    async setInactivityTimeout(ms) {events.push(['ttl', ms])},
    async destroy() {events.push(['destroy'])},
    getTcpPort() {return {fetch: async req => {
      const path = new URL(req.url).pathname;
      if (path === '/health') return new Response('{}');
      events.push([path, req.headers.get('X-Edsger-Session-Key'), await req.json()]);
      if (path === '/execute-stream') return new Response(stream.body, {headers: {'Content-Type': 'application/x-ndjson'}});
      if (path === '/input') return inputResponse ? inputResponse(req) : Response.json({accepted: true, sequence: events.at(-1)[2].sequence});
      throw Error('unexpected route');
    }}}
  };
  const env = {CODE_RUNNER_INTERACTIVE_ENABLED: 'true'};
  const ctx = {storage, container, waitUntil(p) {pending.push(p)}};
  return {stream, events, storage, container, env, ctx, actor: new CodeCoreJob(ctx, env), pending, setInputResponse(fn) {inputResponse = fn}};
}
const inputRequest = (sequence = 1, text = 'Ada\n', eof = false, language = 'python') => request('input', {sequence, text, eof}, {headers: {...headers, 'X-Edsger-Execution-Language': language}});
function namespace(fetch) {return {idFromName: id => id, get: () => ({fetch})}}

test('console inputs are strict, UTF-8 bounded and normalize complete prefilled lines', () => {
  assert.equal(interactiveExecutionInput({...input, stdin: 'Ada'}).stdin, 'Ada\n');
  assert.equal(interactiveExecutionInput(input).stdin, '');
  assert.throws(() => interactiveExecutionInput({...input, stdin: 'bad\0input'}));
  assert.throws(() => consoleInput({sequence: 1, text: 'bad\0input', eof: false}));
  assert.throws(() => interactiveExecutionInput({...input, stdin: 'x'.repeat(8192)}));
  assert.equal(interactiveExecutionInput({...input, stdin: 'x'.repeat(8191) + '\n'}).stdin.length, 8192);
  for (const value of [{sequence: 0, text: '', eof: false}, {sequence: 129, text: '', eof: false}, {sequence: 1, text: 'x', eof: true}, {sequence: 1, text: '🙂'.repeat(2049), eof: false}, {sequence: 1, text: '', eof: false, url: 'bad'}]) assert.throws(() => consoleInput(value));
  assert.deepEqual(consoleInput({sequence: 128, text: '', eof: true}), {sequence: 128, text: '', eof: true});
  assert.throws(() => interactiveExecutionInput({...input, language: 'sql', entrypoint: 'main.sql', files: [{path: 'main.sql', content: ''}]}));
  assert.throws(() => interactiveExecutionInput({...input, mode: 'preview', language: 'javascript', entrypoint: 'main.js', files: [{path: 'main.js', content: ''}]}));
});

test('private edge gates interactive routes, requires trusted language and never admits input', async () => {
  const events = [], env = {CODE_RUNNER_ENABLED: 'true', CODE_RUNNER_INTERACTIVE_ENABLED: 'true', CODE_ADMISSION: namespace(async req => {events.push(['admission', req.method]); return Response.json({})}), CODE_CORE_JOB: namespace(async req => {events.push(['job', new URL(req.url).pathname, [...req.headers.keys()]]); return Response.json({accepted: true, sequence: 1})})};
  const incoming = () => new Request(`https://runner/${uuid}/input`, {method: 'POST', headers: {...headers, 'X-Edsger-Execution-Language': 'python', 'X-Guest-Unsafe': 'not-forwarded'}, body: JSON.stringify({sequence: 1, text: 'Ada\n', eof: false})});
  assert.equal((await serve(incoming(), {...env, CODE_RUNNER_INTERACTIVE_ENABLED: 'false'})).status, 503);
  const missing = incoming(); missing.headers.delete('X-Edsger-Console-Revision');
  assert.equal((await serve(missing, env)).status, 503);
  const language = incoming(); language.headers.delete('X-Edsger-Execution-Language');
  assert.equal((await serve(language, env)).status, 400);
  assert.equal((await serve(incoming(), env)).status, 200);
  assert.equal(events.length, 1); assert.equal(events[0][1], '/input'); assert(!events[0][2].includes('x-guest-unsafe'));
});

test('VM streams before completion, stays alive for input and never persists the session key', async () => {
  const f = fixture();
  const response = await f.actor.fetch(request('run-stream', {...input, stdin: 'first'}));
  assert.equal(response.status, 200);
  assert.equal((await f.actor.fetch(inputRequest())).status, 409);
  f.stream.send({type: 'ready'}); f.stream.send({type: 'output', stream: 'stdout', text: 'Name? '});
  const reader = response.body.getReader();
  assert.match(new TextDecoder().decode((await reader.read()).value), /"ready"/);
  assert.match(new TextDecoder().decode((await reader.read()).value), /Name\? /);
  assert(!f.events.some(([name]) => name === 'destroy'));
  const ack = await f.actor.fetch(inputRequest()); assert.deepEqual(await ack.json(), {accepted: true, sequence: 1});
  const start = f.events.find(([name]) => name === 'start')[1]; assert.deepEqual(start, {enableInternet: false, env: {CODE_RUNNER_INTERACTIVE_ENABLED: 'true'}});
  const launched = f.events.find(([name]) => name === '/execute-stream'); assert.equal(launched[2].stdin, 'first\n');
  const delivered = f.events.find(([name]) => name === '/input'); assert.equal(delivered[1], launched[1]); assert.match(launched[1], /^[a-f0-9-]{36}$/);
  assert.deepEqual([...f.storage.data.keys()], ['used']); assert(!JSON.stringify([...f.storage.data]).includes(launched[1]));
  f.stream.send({type: 'result', execution: {...execution, stdout: 'Name? '}}); f.stream.close();
  const result = JSON.parse(new TextDecoder().decode((await reader.read()).value)); assert.equal(result.revision, 'edsger-execution-v1'); assert(result.elapsedMs > 0);
  assert.equal((await reader.read()).done, true); assert.equal(f.events.filter(([name]) => name === 'destroy').length, 1); assert.equal(f.storage.alarmTime, null);
  assert.equal((await f.actor.fetch(inputRequest())).status, 409);
  assert.equal((await new CodeCoreJob(f.ctx, f.env).fetch(request('run-stream', input))).status, 409);
});

test('valid ACK survives a quick program exit and destruction waits for its pending delivery', async () => {
  const f = fixture(); let resolveInput;
  f.setInputResponse(() => new Promise(resolve => {resolveInput = resolve}));
  const response = await f.actor.fetch(request('run-stream', input));
  f.stream.send({type: 'ready'}); await tick();
  const delivered = f.actor.fetch(inputRequest()); await tick();
  f.stream.send({type: 'result', execution}); f.stream.close(); await tick();
  assert(!f.events.some(([name]) => name === 'destroy'));
  resolveInput(Response.json({accepted: true, sequence: 1}));
  assert.deepEqual(await (await delivered).json(), {accepted: true, sequence: 1});
  await response.text(); assert.equal(f.events.filter(([name]) => name === 'destroy').length, 1);
});

test('runtime input errors are allowlisted and malformed ACKs never look successful', async () => {
  const f = fixture(), response = await f.actor.fetch(request('run-stream', input));
  f.stream.send({type: 'ready'}); await tick();
  assert.equal((await f.actor.fetch(inputRequest(1, 'x', false, 'ruby'))).status, 400);
  for (const [raw, status, mapped] of [['not_ready',409,'input_not_ready'],['out_of_order',409,'input_out_of_order'],['changed_duplicate',409,'input_out_of_order'],['limit',413,'input_limit'],['input_closed',409,'input_closed']]) {
    f.setInputResponse(() => Response.json({error: raw}, {status}));
    const rejected = await f.actor.fetch(inputRequest()); assert.equal(rejected.status, status); assert.deepEqual(await rejected.json(), {error: mapped});
  }
  for (const value of [{accepted: true, sequence: 2}, {accepted: true, sequence: 1, secret: 'never'}, {error: 'raw internal detail'}]) {
    f.setInputResponse(() => Response.json(value));
    const rejected = await f.actor.fetch(inputRequest()); assert.equal(rejected.status, 502); assert.deepEqual(await rejected.json(), {error: 'runner_unavailable'});
  }
  await response.body.cancel(); assert.equal(f.events.filter(([name]) => name === 'destroy').length, 1);
});

test('Stop and hard alarm close streams, destroy instances and prevent input or replay', async () => {
  for (const mode of ['cancel', 'alarm']) {
    const f = fixture(), response = await f.actor.fetch(request('run-stream', input));
    const consumed = response.text(); consumed.catch(() => {});
    f.stream.send({type: 'ready'}); await tick();
    if (mode === 'cancel') await f.actor.fetch(new Request('https://job/cancel', {method: 'DELETE'})); else await f.actor.alarm();
    await assert.rejects(consumed, /runner_unavailable/);
    assert.equal(f.events.filter(([name]) => name === 'destroy').length, 1);
    assert.equal(await f.storage.get('cancelled'), true);
    assert.equal((await f.actor.fetch(inputRequest())).status, 409);
    assert.equal((await f.actor.fetch(request('run-stream', input))).status, 409);
  }
});

test('global admission remains held through streamed execution and hard destruction', async () => {
  const f = fixture(), events = [];
  const env = {...f.env, CODE_RUNNER_ENABLED: 'true', CODE_CORE_JOB: namespace(req => f.actor.fetch(req)), CODE_ADMISSION: namespace(async req => {events.push(req.method); return Response.json({})})};
  const response = await serve(new Request(`https://runner/${uuid}/stream`, {method: 'POST', headers, body: JSON.stringify(input)}), env);
  assert.deepEqual(events, ['POST']); assert(!f.events.some(([name]) => name === 'destroy'));
  f.stream.send({type: 'ready'}); await tick(); assert.deepEqual(events, ['POST']);
  f.stream.send({type: 'result', execution}); f.stream.close();
  await response.text(); assert.deepEqual(events, ['POST', 'DELETE']); assert.equal(f.events.filter(([name]) => name === 'destroy').length, 1);
});

test('cancelled edge does not release admission before confirmed destruction, or after uncertain shutdown', async () => {
  for (const confirmed of [true, false]) {
    const source = channel(), events = []; let finishCancel;
    const env = {CODE_RUNNER_ENABLED: 'true', CODE_RUNNER_INTERACTIVE_ENABLED: 'true', CODE_ADMISSION: namespace(async req => {events.push(req.method); return Response.json({})}), CODE_CORE_JOB: namespace(async req => {
      if (req.method === 'DELETE') return new Promise(resolve => {finishCancel = () => resolve(Response.json(confirmed ? {cancelled: true} : {error: 'runner_unavailable'}, {status: confirmed ? 200 : 502}))});
      return new Response(source.body, {headers: {'Content-Type': 'application/x-ndjson'}});
    })};
    const response = await serve(new Request(`https://runner/${uuid}/stream`, {method: 'POST', headers, body: JSON.stringify(input)}), env);
    const cancellation = response.body.cancel(); await tick();
    assert.deepEqual(events, ['POST']); finishCancel(); await cancellation; await tick();
    assert.deepEqual(events, confirmed ? ['POST', 'DELETE'] : ['POST']);
  }
});

test('strict frame decoder accepts split UTF-8 and rejects missing finals or malformed/unbounded streams', async () => {
  const good = [frame({type: 'ready'}), frame({type: 'output', stream: 'stdout', text: '🙂'}), frame({type: 'result', execution: {...execution, stdout: '🙂'}})];
  const bytes = Buffer.concat(good), accepted = [];
  await consumeConsole(new ReadableStream({start(c) {for (const b of bytes) c.enqueue(Uint8Array.of(b)); c.close()}}), 'python', false, value => accepted.push(value));
  assert.equal(accepted[1].text, '🙂'); assert.equal(accepted.length, 3);
  const malformed = [
    [], [frame({type:'ready'})], [frame({type:'ready'}), frame({type:'ready'})],
    [frame({type:'output',stream:'stdin',text:'bad'})], [frame({type:'ready',key:'secret'})],
    [frame({type:'result',execution:{...execution,previewHTML:'no'}})],
    [frame({type:'result',execution:{...execution,stdout:'unstreamed suffix'}})],
    [frame({type:'result',execution}), frame({type:'output',stream:'stdout',text:'late'})],
    [new TextEncoder().encode('x'.repeat(524289))], [Uint8Array.of(0xff)],
    [frame({type:'output',stream:'stdout',text:'x'.repeat(65537)})],
    Array.from({length:1025},()=>frame({type:'output',stream:'stdout',text:''})),
    [new TextEncoder().encode(JSON.stringify({type:'result',execution}))],
  ];
  for (const chunks of malformed) await assert.rejects(consumeConsole(new ReadableStream({start(c) {for (const chunk of chunks) c.enqueue(chunk); c.close()}}), 'python', false, () => {}));
});

test('invalid or missing finals destroy the VM without exposing a successful-looking result', async () => {
  for (const frames of [
    [{type:'ready'}, {type:'output',stream:'stdout',text:'Name? '}],
    [{type:'result',execution}, {type:'output',stream:'stdout',text:'late'}],
    [{type:'output',stream:'stdout',text:'not in result'}, {type:'result',execution}],
  ]) {
    const f = fixture(), response = await f.actor.fetch(request('run-stream', input));
    const reader = response.body.getReader(), delivered = [];
    const consumed = (async () => {for (;;) {const next = await reader.read(); if (next.done) break; delivered.push(new TextDecoder().decode(next.value))}})(); consumed.catch(()=>{});
    for (const value of frames) f.stream.send(value); f.stream.close();
    await assert.rejects(consumed, /runner_unavailable/);
    assert(!delivered.join('').includes('"type":"result"'));
    assert.equal(f.events.filter(([name]) => name === 'destroy').length, 1);
  }
});

test('failed hard destruction preserves the safety alarm and suppresses the final frame', async () => {
  const f = fixture(); f.container.destroy = async () => {f.events.push(['destroy']); throw Error('platform failed')};
  const response = await f.actor.fetch(request('run-stream', input));
  f.stream.send({type:'result',execution}); f.stream.close();
  await assert.rejects(response.text(), /runner_unavailable/);
  assert(f.storage.alarmTime > Date.now());
  assert.equal(f.events.filter(([name]) => name === 'destroy').length, 1);
});

test('bounded pending input cannot postpone a hard alarm', async () => {
  const f = fixture(); let release;
  const blocked = new Promise(resolve => {release = resolve});
  f.setInputResponse(async req => {await blocked; return Response.json({accepted:true,sequence:1})});
  const response = await f.actor.fetch(request('run-stream', input));
  f.stream.send({type:'ready'}); await tick();
  const pending = Array.from({length:8},() => f.actor.fetch(inputRequest())); await tick();
  assert.equal((await f.actor.fetch(inputRequest())).status, 429);
  const started = performance.now(); await f.actor.alarm();
  assert(performance.now() - started < 200);
  assert.equal(f.events.filter(([name]) => name === 'destroy').length, 1);
  release(); await Promise.all(pending); await assert.rejects(response.text(), /runner_unavailable/);
});

test('an uncertain stream startup cannot free admission without confirmed shutdown', async () => {
  const events = [];
  const env = {CODE_RUNNER_ENABLED:'true',CODE_RUNNER_INTERACTIVE_ENABLED:'true',CODE_ADMISSION:namespace(async req => {events.push(req.method);return Response.json({})}),CODE_CORE_JOB:namespace(async req => Response.json({error:'unavailable'},{status:502}))};
  const response = await serve(new Request(`https://runner/${uuid}/stream`,{method:'POST',headers,body:JSON.stringify(input)}),env);
  assert.equal(response.status,502); assert.deepEqual(events,['POST']);
});


test('escaped NUL remains valid console output and exact final transcript', async () => {
  const frames = [frame({type:'output',stream:'stdout',text:'hello\0world'}),frame({type:'result',execution:{...execution,stdout:'hello\0world'}})];
  const accepted = [];
  await consumeConsole(new ReadableStream({start(c) {for (const value of frames) c.enqueue(value); c.close()}}),'python',false,value=>accepted.push(value));
  assert.equal(accepted.length,2); assert.equal(accepted[1].execution.stdout,'hello\0world');
});

test('duplicate stream rejection remains readable after cancellation cleanup aborts the original RPC', async () => {
  const events=[];
  const env={CODE_RUNNER_ENABLED:'true',CODE_RUNNER_INTERACTIVE_ENABLED:'true',CODE_ADMISSION:namespace(async req=>{events.push(req.method);return Response.json({})}),CODE_CORE_JOB:namespace(async req=>{
    if(req.method==='DELETE')return Response.json({cancelled:true});
    let sent=false;
    const body=new ReadableStream({start(controller){req.signal.addEventListener('abort',()=>controller.error(Error('RPC response aborted')),{once:true})},pull(controller){if(sent)controller.close();else{sent=true;controller.enqueue(new TextEncoder().encode(JSON.stringify({error:'duplicate_request'})))}}});
    return new Response(body,{status:409,headers:{'Content-Type':'application/json'}});
  })};
  const response=await serve(new Request(`https://runner/${uuid}/stream`,{method:'POST',headers,body:JSON.stringify(input)}),env);
  assert.equal(response.status,409);assert.deepEqual(await response.json(),{error:'duplicate_request'});assert.deepEqual(events,['POST','DELETE']);
});
