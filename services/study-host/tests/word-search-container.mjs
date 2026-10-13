import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import {setTimeout as delay} from 'node:timers/promises';
import {token,call as quiz} from './fixtures.mjs';
const image=process.env.STUDY_TEST_IMAGE??'edsger-study-tool:review';
const env={...process.env,FIGURE_TOKEN:token,WORD_SEARCH_ONLY:'true',WRANGLER_SEND_METRICS:'false'};
delete env.WORKSHEET_ONLY;
for(const name of ['DOCKER_HOST','DOCKER_CONTEXT','DOCKER_TLS','DOCKER_TLS_VERIFY','DOCKER_CERT_PATH'])delete env[name];
const docker=(...args)=>execFileSync('docker',['--host','unix:///var/run/docker.sock',...args],{encoding:'utf8',env,stdio:['ignore','pipe','pipe']}).trim();
// An ambiguous deployment selector must exit before binding a listener.
let ambiguous=false;
try {docker('run','--rm','--read-only','--cap-drop=ALL','--security-opt=no-new-privileges','--network=none','--env','FIGURE_TOKEN','--env','WORD_SEARCH_ONLY','--env','WORKSHEET_ONLY=true',image);} catch(error) {assert.equal(error.status,1);assert.equal(String(error.stderr).trim(),'invalid_tool_selector');ambiguous=true;}
assert(ambiguous,'simultaneous narrow tool selectors did not fail closed');
const id=docker('run','-d','--rm','--read-only','--cap-drop=ALL','--security-opt=no-new-privileges','--memory=256m','--cpus=0.25','--pids-limit=32','--env','FIGURE_TOKEN','--env','WORD_SEARCH_ONLY','-p','127.0.0.1::8080',image);
try {
 const address=docker('port',id,'8080/tcp');assert(/^127\.0\.0\.1:\d+$/.test(address));const origin='http://'+address;
 let healthy=false;for(let n=0;n<30;n++){try{healthy=(await fetch(origin+'/health')).status===200;}catch{}if(healthy)break;await delay(100);}assert(healthy);
 const health=await (await fetch(origin+'/health')).json();assert.equal(health.tool,'make_word_search');
 const post=body=>fetch(origin+'/v1/tools/call',{method:'POST',headers:{Authorization:`Bearer ${token}`,'Content-Type':'application/json'},body:JSON.stringify(body)});
 assert.equal((await post(quiz())).status,400,'word-search process admitted quiz');
 assert.equal((await post({tool:'make_worksheet',arguments:{title:'Arithmetic',instructions:'Work it out',exercises:[{prompt:'2 + 3?',answer:'5',explanation:'Two and three sum to five.'}]}})).status,400,'word-search process admitted worksheet');
 const failure=await post({tool:'make_word_search',arguments:{title:'Unsatisfiable fixed fixture',words:['CAT','CATCAT'],difficulty:'hard'}});assert.equal(failure.status,422);assert.deepEqual(await failure.json(),{ok:false,error:'generation_failed',code:'generation_failed'});
 assert.equal(docker('image','inspect',image,'--format','{{.Config.User}}'),'65532:65532');
 assert.equal(docker('inspect',id,'--format','{{.HostConfig.ReadonlyRootfs}}'),'true');
 execFileSync(process.execPath,['tests/word-search-runtime.mjs'],{stdio:'inherit',env:{...env,WORD_SEARCH_TEST_ORIGIN:origin}});
 assert.equal(docker('logs',id),'','private word-search process emitted supplied content or diagnostics');
 docker('stop',id);
 console.log('PASS word-search-only image is nonroot/read-only, selectors fail closed, foreign tools reject, logs remain empty and shutdown is clean');
} finally {try{docker('rm','-f',id);}catch{}}
