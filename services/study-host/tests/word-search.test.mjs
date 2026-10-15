import assert from 'node:assert/strict';
import test from 'node:test';
import {readdir,readFile} from 'node:fs/promises';
import {serve} from '../.test-build/gateway.js';
import {wordSearchCall,wordSearchResult} from '../.test-build/word-search-contract.js';
import {WORD_SEARCH_CONTAINER_NAME,WORD_SEARCH_ADMISSION_NAME} from '../.test-build/word-search-gateway.js';
import {call,envelope,puzzleID} from './word-search-fixtures.mjs';
import {token} from './fixtures.mjs';
const req=(body=JSON.stringify(call()),options={})=>new Request('https://tools-sandbox.edsger.app/v1/word-searches',{method:'POST',headers:{'Content-Type':'application/json','CF-Connecting-IP':'192.0.2.1','X-Edsger-Word-Search-Consent':'v1',...options.headers},body,...(options.signal?{signal:options.signal}:{})});
function bindings(options={}) {
 const events=[];
 return {events,FIGURE_TOKEN:token,WORD_SEARCH_HOST_ENABLED:'true',STUDY_CONTAINER:{getByName(){throw Error('quiz reached');}},WORKSHEET_CONTAINER:{getByName(){throw Error('worksheet reached');}},
 WORD_SEARCH_IP_LIMIT:{async limit({key}){assert.equal(key,'192.0.2.1');events.push('ip');if(options.ipError)throw Error('private-content');return {success:!options.ipDenied};}},
 WORD_SEARCH_ADMISSION:{getByName(name){assert.equal(name,WORD_SEARCH_ADMISSION_NAME);return {async fetch(request){events.push('budget');assert.equal(await request.text(),'');assert.deepEqual([...request.headers],[]);return new Response(null,{status:options.budgetDenied?429:204});}};}},
 WORD_SEARCH_CONTAINER:{getByName(name){assert.equal(name,WORD_SEARCH_CONTAINER_NAME);return {async fetch(request){events.push('word-search');assert.equal(request.redirect,'manual');assert.deepEqual([...request.headers.keys()].sort(),['authorization','content-type']);assert.equal(request.headers.get('Authorization'),`Bearer ${token}`);const input=await request.json();return options.fetch?options.fetch(request,input):new Response(JSON.stringify(envelope(input)),{headers:{'Content-Type':'application/json'}});}};}}
 };
}
test('independent word-search pipeline verifies 1/3/5/30 words at all difficulties and strips caller credentials',async()=>{
 for(const n of [1,3,5,30])for(const difficulty of ['easy','medium','hard']){
  const env=bindings(),input=call(n,difficulty),response=await serve(req(JSON.stringify(input),{headers:{Authorization:'Bearer personal-provider-key','X-Apple-Transaction-JWS':'synthetic-not-a-receipt','X-User-Selected-Origin':'https://invalid'}}),env);
  assert.equal(response.status,200);assert.deepEqual(await response.json(),envelope(input));assert.deepEqual(env.events,['ip','budget','word-search']);
 }
});
test('actual checked-in Go fixtures pass independent edge parsing, seed/puzzle hashes, physical uniqueness and HTML gate',async()=>{
 const directory=new URL('./fixtures/word-search/',import.meta.url),files=(await readdir(directory)).filter(name=>name.endsWith('.json')).sort();assert.equal(files.length,16);
 for(const file of files){const fixture=JSON.parse(await readFile(new URL(file,directory),'utf8'));const input=wordSearchCall(fixture.input);await wordSearchResult(fixture.response,input);const response=await serve(req(JSON.stringify(fixture.input)),bindings({fetch:async()=>new Response(JSON.stringify(fixture.response),{headers:{'Content-Type':'application/json'}})}));assert.equal(response.status,200,file);assert.deepEqual(await response.json(),fixture.response);}
});
test('gate/control/IP failures happen before body reads or any container wake',async()=>{
 for(const missing of ['FIGURE_TOKEN','WORD_SEARCH_IP_LIMIT','WORD_SEARCH_ADMISSION','WORD_SEARCH_CONTAINER']) {const env=bindings();delete env[missing];assert.equal((await serve(req(),env)).status,503);assert.deepEqual(env.events,[]);}
 const env=bindings();env.WORD_SEARCH_HOST_ENABLED='false';assert.equal((await serve(req(),env)).status,503);assert.deepEqual(env.events,[]);
 const missing=req();missing.headers.delete('CF-Connecting-IP');assert.equal((await serve(missing,bindings())).status,503);
 for(const option of [{ipDenied:true},{budgetDenied:true},{ipError:true}]){const env=bindings(option);assert.equal((await serve(req(),env)).status,option.ipError?503:429);assert(!env.events.includes('word-search'));}
});
test('separate consent and strict request parsing refuse invalid tools/words/UTF8/fields before global admission',async()=>{
 const noConsent=req();noConsent.headers.delete('X-Edsger-Word-Search-Consent');const e=bindings();assert.equal((await serve(noConsent,e)).status,403);assert.deepEqual(e.events,['ip']);
 const invalid=[call(0),call(30,'easy',{words:Array(31).fill('CATS')}),{...call(),tool:'make_worksheet'},call(1,'invalid'),call(1,'easy',{size:7}),call(1,'easy',{size:24.5}),call(1,'easy',{seed:-1}),call(1,'easy',{seed:2147483648}),call(1,'easy',{seed:null}),call(1,'easy',{size:null}),call(1,'easy',{unknown:true}),call(1,'easy',{words:['CAT','cat']}),call(1,'easy',{words:['CAT','TAC']}),call(1,'easy',{words:['éclair']}),call(1,'easy',{words:['AB']}),call(1,'easy',{words:['X'.repeat(19)]}),call(1,'easy',{words:['-CAT']}),call(1,'easy',{words:['CAT\tDOG']}),call(1,'easy',{words:['CAT!']}),call(1,'easy',{words:['longerword'],size:8}),call(1,'easy',{title:'🧪'.repeat(31)})];
 for(const input of invalid){const env=bindings();assert.equal((await serve(req(JSON.stringify(input)),env)).status,400);assert.deepEqual(env.events,['ip']);}
 for(const body of ['{"tool":"make_word_search","tool":"make_word_search","arguments":{}}','null','[]','{',JSON.stringify(call()).replace('Animal word search','\\ud800')]){const env=bindings();assert.equal((await serve(req(body),env)).status,400);assert.deepEqual(env.events,['ip']);}
 const env=bindings();assert.equal((await serve(req(' '.repeat(16385)),env)).status,413);assert.deepEqual(env.events,['ip']);
 const raw=new Uint8Array([0xff]);assert.equal((await serve(req(raw),bindings())).status,400);
 assert.equal((await serve(req(undefined,{headers:{'Content-Length':'16385'}}),bindings())).status,413);
 assert.equal((await serve(req(undefined,{headers:{'Content-Encoding':'gzip'}}),bindings())).status,415);
});
test('canonical labels, Unicode title trim, optional seeds and palindrome physical deduplication',async()=>{
 const input=wordSearchCall(call(1,'hard',{title:'\u0085  Animal word search  \u0085',words:["  O'wl  "]}));assert.equal(input.arguments.title,'Animal word search');assert.deepEqual(input.arguments.words,["O'wl"]);
 const optional=call(1);delete optional.arguments.seed;delete optional.arguments.size;await wordSearchResult(envelope(optional),wordSearchCall(optional));
 const palindrome=call(1,'hard',{words:['LEVEL']});await wordSearchResult(envelope(palindrome),wordSearchCall(palindrome));
});
test('true flags cannot hide changed inputs, malformed placements, extra physical occurrences or invalid puzzle hashes',async()=>{
 const mutators=[r=>r.title='Other title',r=>r.difficulty='hard',r=>r.seed++,r=>r.size=23,r=>r.schema_version=2,r=>r.generator_version='wordsearch-v2',r=>r.puzzle_id='0'.repeat(64),r=>r.words[0].letters='DOGS',r=>r.words[0].label='cats',r=>r.words[0].id=1,r=>r.words.push(r.words[0]),r=>r.placements.pop(),r=>r.placements[0].word_id=1,r=>r.placements[0].direction='W',r=>r.placements[0].cells[1].row=1,r=>r.placements[0].cells[0].extra=true,r=>r.placements[0].start.column=1,r=>r.placements[0].end.column=2,r=>r.grid[0]='Q'.repeat(24),r=>r.grid[1]='CATS'+'Q'.repeat(20),r=>r.directions.reverse(),r=>r.directions.push('N'),r=>r.network_requests=true,r=>r.offline=false,r=>r.puzzle_verified=false,r=>r.content_verified=true,r=>r.preview_requires_scripts=true,r=>r.html='<!doctype html>'+ 'x'.repeat(98305),r=>r.extra=true];
 for(const mutate of mutators){const bad=envelope();mutate(bad.result);await assert.rejects(wordSearchResult(bad,wordSearchCall(call())));const response=await serve(req(),bindings({fetch:async()=>new Response(JSON.stringify(bad),{headers:{'Content-Type':'application/json'}})}));assert.equal(response.status,503);}
 // Even a self-consistent hash cannot launder two placements of the target.
 const duplicate=envelope();duplicate.result.grid[1]='CATS'+'Q'.repeat(20);duplicate.result.puzzle_id=puzzleID(duplicate.result);await assert.rejects(wordSearchResult(duplicate,wordSearchCall(call())));
});
test('origin status handling exposes only fixed generation failure or sanitized service errors',async()=>{
 for(const response of [new Response('private-content',{status:302,headers:{Location:'https://invalid'}}),new Response('private-content',{status:500}),new Response('private-content',{status:422}),new Response(' '.repeat(262145),{headers:{'Content-Type':'application/json'}})]){const result=await serve(req(),bindings({fetch:async()=>response}));assert.equal(result.status,503);assert(!(await result.text()).includes('private-content'));}
 const result=await serve(req(),bindings({fetch:async()=>new Response(JSON.stringify({ok:false,error:'generation_failed',code:'generation_failed'}),{status:422,headers:{'Content-Type':'application/json'}})}));assert.equal(result.status,422);assert.deepEqual(await result.json(),{ok:false,error:'generation_failed',code:'generation_failed'});
});
test('printable HTML only accepts the fixed inert Go template head and body grammar',async()=>{
 const mutations=[html=>html.replace('<main>','<main><script>alert(1)</script>'),html=>html.replace('<main>','<main><img src="https://invalid">'),html=>html.replace('<main>','<main><iframe src="https://invalid"></iframe>'),html=>html.replace('<main>','<main onload="alert(1)">'),html=>html.replace('<main>','<main><svg></svg>'),html=>html.replace('<main>','<main><math></math>'),html=>html.replace('<style>','<style>@import "https://invalid";'),html=>html.replace('body{','body{background:url(https://invalid);'),html=>html.replace("default-src 'none'","default-src *"),html=>html.replace('<main>','<main><meta http-equiv="refresh" content="0;url=https://invalid">'),html=>html.replace('<main>','<main><a href="https://invalid">Link</a>'),html=>html.replace('<main>','<main><style>p{display:none}</style>')];
 for(const mutate of mutations)for(const field of ['html','answer_key_html']){const bad=envelope();bad.result[field]=mutate(bad.result[field]);await assert.rejects(wordSearchResult(bad,wordSearchCall(call())));}
});
test('cancellation and fixed deadline prevent successful output after caller cancellation',async t=>{
 let begun,finish;const start=new Promise(resolve=>begun=resolve),controller=new AbortController();const waiting=serve(req(undefined,{signal:controller.signal}),bindings({fetch:async()=>{begun();return new Promise(resolve=>finish=resolve);}}));await start;controller.abort();assert.equal((await waiting).status,409);finish(new Response(JSON.stringify(envelope()),{headers:{'Content-Type':'application/json'}}));
 const aborted=new AbortController();aborted.abort();const env=bindings();assert.equal((await serve(req(undefined,{signal:aborted.signal}),env)).status,409);assert.deepEqual(env.events,['ip']);
 t.mock.timers.enable({apis:['setTimeout']});let entered;const ready=new Promise(resolve=>entered=resolve);const timeout=serve(req(),bindings({fetch:async()=>{entered();return new Promise(()=>{});}}));await ready;t.mock.timers.tick(14000);assert.equal((await timeout).status,503);
});
test('health word search flag is independent and liveness never wakes a container',async()=>{
 const env=bindings(),response=await serve(new Request('https://tools-sandbox.edsger.app/health'),env);const value=await response.json();assert.equal(value.word_search_enabled,true);assert.equal(value.worksheet_enabled,false);assert.equal(value.enabled,false);assert.deepEqual(env.events,[]);
});
test('sandbox and production configurations preserve quiz/worksheet isolation and add disabled independent word-search bindings',async()=>{
 const configuration=await readFile(new URL('../wrangler.toml',import.meta.url),'utf8');
 for(const gate of ['STUDY_HOST_ENABLED','WORKSHEET_HOST_ENABLED','WORD_SEARCH_HOST_ENABLED'])assert.equal((configuration.match(new RegExp(`^${gate} = "false"$`,'gm'))??[]).length,2,gate);
 for(const prefix of ['', 'env.production.']){
  for(const [name,className] of [['STUDY_CONTAINER','StudyContainer'],['WORKSHEET_CONTAINER','WorksheetContainer'],['WORKSHEET_ADMISSION','WorksheetAdmission'],['WORD_SEARCH_CONTAINER','WordSearchContainer'],['WORD_SEARCH_ADMISSION','WordSearchAdmission']])assert(configuration.includes(`[[${prefix}durable_objects.bindings]]\nname = "${name}"\nclass_name = "${className}"`));
  for(const [tag,classes] of [['v1-study-container','"StudyContainer"'],['v2-worksheet-isolation','"WorksheetContainer", "WorksheetAdmission"'],['v3-word-search-isolation','"WordSearchContainer", "WordSearchAdmission"']])assert(configuration.includes(`[[${prefix}migrations]]\ntag = "${tag}"\nnew_sqlite_classes = [${classes}]`));
  for(const className of ['StudyContainer','WorksheetContainer','WordSearchContainer'])assert(configuration.includes(`[[${prefix}containers]]\nclass_name = "${className}"\nimage = "Dockerfile"\nimage_build_context = "../.."\nmax_instances = 1\ninstance_type = "lite"`));
 }
 for(const [namespace,scope,name] of [['1003','','WORKSHEET'],['1004','env.production.','WORKSHEET'],['1005','','WORD_SEARCH'],['1006','env.production.','WORD_SEARCH']])assert(configuration.includes(`[[${scope}ratelimits]]\nname = "${name}_IP_LIMIT"\nnamespace_id = "${namespace}"\nsimple = { limit = 4, period = 60 }`));
 const source=await readFile(new URL('../src/index.ts',import.meta.url),'utf8');assert(source.includes('enableInternet = false'));assert(source.includes('WORD_SEARCH_ONLY: "true"'));assert(source.includes('WORKSHEET_ONLY: "true"'));assert.equal((source.match(/FIGURE_TOKEN: env.FIGURE_TOKEN/g)??[]).length,3);
});
