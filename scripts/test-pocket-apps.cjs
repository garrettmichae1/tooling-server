// Browser-level checks against the compiled HTTP server, without paid services.
// npm install --prefix .toolchain/ui --no-audit --no-fund playwright@1.62.1
// .toolchain/ui/node_modules/.bin/playwright install --with-deps chromium
// go build -o bin/tooling-server ./cmd/figureserver
// node scripts/test-pocket-apps.cjs
const assert=require('node:assert/strict');
const fs=require('node:fs/promises');
const path=require('node:path');
const {pathToFileURL}=require('node:url');
const {spawn}=require('node:child_process');
const {randomBytes}=require('node:crypto');
const net=require('node:net');
const {chromium}=require(process.env.PLAYWRIGHT_MODULE||'../.toolchain/ui/node_modules/playwright');

(async()=>{
 const listener=net.createServer();await new Promise(r=>listener.listen(0,'127.0.0.1',r));const port=listener.address().port;await new Promise(r=>listener.close(r));
 const token=randomBytes(32).toString('hex'),base='http://127.0.0.1:'+port;
 const server=spawn(path.resolve('bin/tooling-server'),[],{env:{...process.env,FIGURE_TOKEN:token,FIGURE_TOOLS_ONLY:'1',FIGURE_BIND:'127.0.0.1:'+port},stdio:['ignore','ignore','pipe']});let log='';server.stderr.on('data',b=>{log+=b.toString()});
 const dir=path.resolve('.toolchain/pocket-qa');await fs.mkdir(dir,{recursive:true});let browser;
 async function call(tool,arguments_){const r=await fetch(base+'/v1/tools/call',{method:'POST',headers:{Authorization:'Bearer '+token,'Content-Type':'application/json'},body:JSON.stringify({tool,arguments:arguments_})});const json=await r.json();assert.equal(r.status,200,JSON.stringify(json));assert.equal(json.ok,true);return json.result}
 try{
  let ready=false;for(let i=0;i<100;i++){try{if((await fetch(base+'/health')).ok){ready=true;break}}catch{}await new Promise(r=>setTimeout(r,100))}assert(ready,'server did not start: '+log);
  const attack='</script><script>globalThis.PWNED=1</script><img src="https://example.invalid/leak" onerror="alert(1)">';
  const study=await call('make_study_app',{title:'Practice with Edsger',questions:[{prompt:'Which mapping has no free variables? '+attack,choices:['One-to-one','Always onto'],correct_index:0,explanation:'Pivot in every column.'},{prompt:'Correctness check',choices:['Wrong','Right'],correct_index:1,explanation:attack}]});
  const dashboard=await call('make_data_dashboard',{title:'A week of progress',labels:['Monday','Tuesday','Wednesday','Thursday'],series:[{name:'Math',values:[2,3,-1,4]},{name:'Programming',values:[1,4,2,3]}]});
  const sorting=await call('make_sorting_lab',{algorithm:'insertion',values:[5,-2,4,1,3]});
  const files={study,dashboard,sorting};for(const [name,result]of Object.entries(files))await fs.writeFile(path.join(dir,name+'.html'),result.html);
  browser=await chromium.launch({headless:true});
  for(const viewport of [{width:390,height:844},{width:1440,height:1000}]){
   const context=await browser.newContext({viewport,acceptDownloads:true});const errors=[],remoteRequests=[];context.on('request',r=>{if(/^https?:/.test(r.url()))remoteRequests.push(r.url())});
   const page=await context.newPage();page.on('pageerror',e=>errors.push(e.message));
   async function open(name){await page.goto(pathToFileURL(path.join(dir,name+'.html')).href);await page.waitForTimeout(60);assert.equal(await page.evaluate(()=>globalThis.PWNED),undefined);assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1),'horizontal page overflow')}
   await open('study');assert.equal(await page.locator('#choices button').count(),2);assert((await page.locator('#prompt').textContent()).includes(attack));await page.locator('#choices button').nth(1).click();assert.match(await page.locator('#feedback').textContent(),/Incorrect/);await page.locator('#next').click();await page.locator('#choices button').nth(1).click();await page.locator('#next').click();assert.match(await page.locator('#summary').textContent(),/1 correct · 1 missed/);
   await page.reload();assert.match(await page.locator('#summary').textContent(),/1 correct · 1 missed/);await page.locator('#missed').click();assert.equal(await page.locator('#position').textContent(),'Question 1 of 1');await page.locator('#choices button').nth(0).click();assert.match(await page.locator('#summary').textContent(),/2 correct · 0 missed/);
   const progressDownload=page.waitForEvent('download');await page.locator('#export').click();const progress=await progressDownload;const progressData=JSON.parse(await fs.readFile(await progress.path(),'utf8'));assert(progressData.questions.every(q=>q.last_result==='correct'));
   page.once('dialog',d=>d.accept());await page.locator('#clear').click();assert.match(await page.locator('#summary').textContent(),/0 correct · 0 missed · 0\/2 attempted/);await page.screenshot({path:path.join(dir,'study-'+viewport.width+'.png'),fullPage:true});
   await open('dashboard');assert.equal(await page.locator('#chart polyline').count(),2);await page.locator('#chart-type').selectOption('bar');assert.equal(await page.locator('#chart rect').count(),8);await page.locator('[data-index="0"]').uncheck();assert.equal(await page.locator('#chart rect').count(),4);assert.equal(await page.locator('#stats h2').textContent(),'Programming');const csvDownload=page.waitForEvent('download');await page.locator('#export').click();const csv=await csvDownload;assert((await fs.readFile(await csv.path(),'utf8')).startsWith('"Category","Programming"\r\n'));await page.locator('[data-index="1"]').uncheck();assert(await page.locator('#empty').isVisible());assert(await page.locator('#export').isDisabled());await page.locator('[data-index="0"]').check();await page.locator('[data-index="1"]').check();await page.screenshot({path:path.join(dir,'dashboard-'+viewport.width+'.png'),fullPage:true});
   await open('sorting');await page.locator('#step').click();assert.match(await page.locator('#position').textContent(),/Step 1 /);await page.locator('#back').click();assert.match(await page.locator('#position').textContent(),/Step 0 /);await page.locator('#speed').focus();await page.locator('#speed').press('Home');await page.locator('#play').click();await page.waitForFunction(()=>document.getElementById('position').textContent.startsWith('Step 2 '));await page.locator('#play').click();const paused=await page.locator('#position').textContent();await page.waitForTimeout(120);assert.equal(await page.locator('#position').textContent(),paused);await page.locator('#reset').click();await page.locator('#play').click();await page.waitForFunction(()=>document.getElementById('play').textContent==='Play'&&document.getElementById('step').disabled,{},{timeout:10000});assert.equal(await page.locator('#array').textContent(),'[-2,1,3,4,5]');await page.screenshot({path:path.join(dir,'sorting-'+viewport.width+'.png'),fullPage:true});
   assert.deepEqual(errors,[]);assert.deepEqual(remoteRequests,[]);await context.close();
  }
  // Storage-disabled previews must still run; progress export remains usable.
  const context=await browser.newContext();await context.addInitScript(()=>{Object.defineProperty(window,'localStorage',{get(){throw new Error('disabled')}})});const page=await context.newPage();await page.goto(pathToFileURL(path.join(dir,'study.html')).href);assert.match(await page.locator('#storage').textContent(),/unavailable/);await page.locator('#choices button').nth(0).click();assert.match(await page.locator('#feedback').textContent(),/Correct/);await context.close();
  console.log('Pocket app checks passed: responsive layouts, quiz/retry/persistence/export, chart toggles/CSV, sorting playback, storage fallback, escaped input, and zero remote requests.');
 }finally{if(browser)await browser.close();server.kill('SIGTERM');await new Promise(r=>server.once('exit',r))}
})().catch(e=>{console.error(e);process.exitCode=1});
