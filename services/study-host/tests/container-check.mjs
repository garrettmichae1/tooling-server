import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import {setTimeout as delay} from 'node:timers/promises';
import {call, token} from './fixtures.mjs';

const image=process.env.STUDY_TEST_IMAGE ?? 'edsger-study-tool:review';
const env={...process.env,FIGURE_TOKEN:token,WRANGLER_SEND_METRICS:'false'};
for(const name of ['DOCKER_HOST','DOCKER_CONTEXT','DOCKER_TLS','DOCKER_TLS_VERIFY','DOCKER_CERT_PATH']) delete env[name];
const docker=(...args)=>execFileSync('docker',['--host','unix:///var/run/docker.sock',...args],{encoding:'utf8',env,stdio:['ignore','pipe','pipe']}).trim();
assert.equal(docker('image','inspect',image,'--format','{{.Config.User}}'),'65532:65532');
const id=docker('run','-d','--rm','--label','edsger.study-test=1','--read-only','--cap-drop=ALL','--security-opt=no-new-privileges','--memory=256m','--cpus=0.25','--pids-limit=32','--stop-timeout=10','--env','FIGURE_TOKEN','-p','127.0.0.1::8080',image);
assert(/^[a-f0-9]{64}$/.test(id));
try {
  const address=docker('port',id,'8080/tcp');assert(/^127\.0\.0\.1:\d+$/.test(address));
  const origin='http://'+address;
  let healthy=false;
  for(let n=0;n<30;n++) {
    try {healthy=(await fetch(origin+'/health',{signal:AbortSignal.timeout(1000)})).status===200;} catch {}
    if(healthy) break;await delay(100);
  }
  assert(healthy,'real container did not start');
  const post=(body,authorized=true,path='/v1/tools/call')=>fetch(origin+path,{method:'POST',headers:{'Content-Type':'application/json',...(authorized?{Authorization:`Bearer ${token}`}:{})},body:JSON.stringify(body),signal:AbortSignal.timeout(5000)});
  assert.equal((await post(call(),false)).status,401);
  assert.equal((await post({...call(),tool:'calculate'})).status,400);
  assert.equal((await post(call(6))).status,400);
  assert.equal((await post(call(),true,'/v1/tools/make_study_app')).status,404);
  for(const n of [1,3,5]) {
    const res=await post(call(n));assert.equal(res.status,200);const value=await res.json();
    assert.equal(value.result.filename,'study-app.html');assert.equal(value.result.network_requests,false);assert.equal(value.result.offline,true);assert(value.result.html.includes('Retry missed'));
  }
  assert.equal(docker('inspect',id,'--format','{{.HostConfig.ReadonlyRootfs}}'),'true');
  console.log('PASS: actual nonroot read-only Go image renders 1/3/5 questions and blocks every other tool');
  execFileSync(process.execPath,['tests/runtime-check.mjs'],{stdio:'inherit',env:{...env,STUDY_TEST_ORIGIN:origin}});
  docker('stop',id);
  console.log('PASS: real container accepts SIGTERM and stops cleanly');
} finally {
  try {docker('rm','-f',id);} catch {}
}
