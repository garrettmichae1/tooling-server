import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import {setTimeout as delay} from 'node:timers/promises';
import {token,call} from './fixtures.mjs';
const image=process.env.STUDY_TEST_IMAGE??'edsger-study-tool:review';
const env={...process.env,FIGURE_TOKEN:token,WORKSHEET_ONLY:'true',WRANGLER_SEND_METRICS:'false'};
for(const name of ['DOCKER_HOST','DOCKER_CONTEXT','DOCKER_TLS','DOCKER_TLS_VERIFY','DOCKER_CERT_PATH'])delete env[name];
const docker=(...args)=>execFileSync('docker',['--host','unix:///var/run/docker.sock',...args],{encoding:'utf8',env,stdio:['ignore','pipe','pipe']}).trim();
const id=docker('run','-d','--rm','--read-only','--cap-drop=ALL','--security-opt=no-new-privileges','--memory=256m','--cpus=0.25','--pids-limit=32','--env','FIGURE_TOKEN','--env','WORKSHEET_ONLY','-p','127.0.0.1::8080',image);
try {
 const address=docker('port',id,'8080/tcp');assert(/^127\.0\.0\.1:\d+$/.test(address));const origin='http://'+address;
 let healthy=false;for(let n=0;n<30;n++){try{healthy=(await fetch(origin+'/health')).status===200;}catch{}if(healthy)break;await delay(100);}assert(healthy);
 const post=(body)=>fetch(origin+'/v1/tools/call',{method:'POST',headers:{Authorization:`Bearer ${token}`,'Content-Type':'application/json'},body:JSON.stringify(body)});
 assert.equal((await post(call())).status,400,'worksheet process admitted quiz');
 assert.equal(docker('image','inspect',image,'--format','{{.Config.User}}'),'65532:65532');
 assert.equal(docker('inspect',id,'--format','{{.HostConfig.ReadonlyRootfs}}'),'true');
 execFileSync(process.execPath,['tests/worksheet-runtime.mjs'],{stdio:'inherit',env:{...env,WORKSHEET_TEST_ORIGIN:origin}});
 docker('stop',id);
 console.log('PASS worksheet-only container is nonroot/read-only, rejects quiz tool and shuts down cleanly');
} finally {try{docker('rm','-f',id);}catch{}}
