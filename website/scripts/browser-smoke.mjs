import { createServer } from 'node:http';
import { spawn } from 'node:child_process';
import { mkdtemp, readFile, writeFile, mkdir, rm, stat } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve, extname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { SITE_BASE } from './site-config.mjs';

const dist=fileURLToPath(new URL('../dist/',import.meta.url));
const output=fileURLToPath(new URL('../browser-evidence/',import.meta.url));
const profile=await mkdtemp(join(tmpdir(),'rux-docs-browser-'));
const types={'.html':'text/html; charset=utf-8','.js':'text/javascript','.mjs':'text/javascript','.css':'text/css','.json':'application/json','.svg':'image/svg+xml','.png':'image/png','.webp':'image/webp','.wasm':'application/wasm','.md':'text/plain; charset=utf-8','.txt':'text/plain; charset=utf-8','.woff2':'font/woff2'};
const server=createServer(async(req,res)=>{
 try{
  const path=decodeURIComponent(new URL(req.url,'http://localhost').pathname);
  if(!path.startsWith(SITE_BASE))throw Error('Outside base');
  const target=resolve(dist,path.slice(SITE_BASE.length));
  if(target!==resolve(dist)&&!target.startsWith(resolve(dist)+'/'))throw Error('Invalid path');
  let file=target;
  if((await stat(file)).isDirectory())file=join(file,'index.html');
  res.writeHead(200,{'Content-Type':types[extname(file)]||'application/octet-stream'});
  res.end(await readFile(file));
 }catch{res.writeHead(404);res.end('Not found');}
});
let chrome,ws;
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
const assert=(ok,msg)=>{if(!ok)throw Error(msg);};
async function until(fn,label,ms=15000){
 const deadline=Date.now()+ms;
 while(Date.now()<deadline){const v=await fn();if(v)return v;await sleep(150);}
 throw Error('Timed out: '+label);
}
try{
 await mkdir(output,{recursive:true});
 await new Promise(r=>server.listen(0,'127.0.0.1',r));
 const origin='http://127.0.0.1:'+server.address().port;
 // Only a disposable public documentation artifact is loaded. Never use personal browser profiles.
 const args=['--headless=new','--remote-debugging-port=0','--user-data-dir='+profile,'--no-first-run','--disable-dev-shm-usage','--disable-background-networking','about:blank'];
 if(process.env.DOCS_BROWSER_NO_SANDBOX==='1')args.unshift('--no-sandbox');
 chrome=spawn(process.env.CHROME_BIN||'google-chrome',args,{stdio:['ignore','ignore','inherit']});
 let launchError;chrome.on('error',e=>{launchError=e;});
 const port=await until(async()=>{if(launchError)throw launchError;if(chrome.exitCode!==null)throw Error('Chrome exited '+chrome.exitCode);try{return Number((await readFile(join(profile,'DevToolsActivePort'),'utf8')).split('\n')[0]);}catch{return false;}},'Chrome debugging port',60000);
 const target=await until(async()=>{try{return (await (await fetch('http://127.0.0.1:'+port+'/json/list')).json()).find(x=>x.type==='page');}catch{return false;}},'page target',30000);
 ws=new WebSocket(target.webSocketDebuggerUrl);
 await new Promise((ok,fail)=>{ws.addEventListener('open',ok,{once:true});ws.addEventListener('error',fail,{once:true});});
 let seq=0;const pending=new Map();const failures=[];const viewerRequests=[];let viewingScalar=false;
 ws.addEventListener('message',e=>{
  const m=JSON.parse(e.data);
  if(m.id&&pending.has(m.id)){const p=pending.get(m.id);pending.delete(m.id);clearTimeout(p.timer);m.error?p.reject(Error(m.error.message)):p.resolve(m.result);}
  if(viewingScalar&&m.method==='Network.requestWillBeSent') viewerRequests.push({url:m.params.request.url,method:m.params.request.method});
  if(m.method==='Network.responseReceived'&&m.params.response.url.startsWith(origin+SITE_BASE)&&m.params.response.status>=400)failures.push(m.params.response.url+' '+m.params.response.status);
  if(m.method==='Runtime.exceptionThrown')failures.push(m.params.exceptionDetails.text+' '+(m.params.exceptionDetails.exception?.description||''));
 });
 function send(method,params={}){
  return new Promise((resolve,reject)=>{const id=++seq;const timer=setTimeout(()=>{pending.delete(id);reject(Error('CDP timeout '+method));},20000);pending.set(id,{resolve,reject,timer});ws.send(JSON.stringify({id,method,params}));});
 }
 async function evaluate(expression){const v=await send('Runtime.evaluate',{expression,returnByValue:true,awaitPromise:true});if(v.exceptionDetails)throw Error(v.exceptionDetails.exception?.description||v.exceptionDetails.text);return v.result.value;}
 async function visit(path){
  const url=origin+SITE_BASE+path;
  await send('Page.navigate',{url});
  await until(()=>evaluate('location.href === '+JSON.stringify(url)+' && document.readyState === "complete"'),'navigation '+path);
 }
 async function viewport(width,height,mobile){await send('Emulation.setDeviceMetricsOverride',{width,height,deviceScaleFactor:1,mobile});}
 async function screenshot(name){const shot=await send('Page.captureScreenshot',{format:'png'});await writeFile(join(output,name+'.png'),Buffer.from(shot.data,'base64'));}
 await send('Page.enable');await send('Runtime.enable');await send('Network.enable');
 await viewport(1440,900,false);
 await visit('');
 assert(await evaluate('!!document.querySelector("main h1") || !!document.querySelector("h1")'),'Missing heading');
 await until(()=>evaluate('!!customElements.get("starlight-theme-select")'),'theme ready');
 for(const theme of ['light','dark']){
  await evaluate(`(()=>{const s=document.querySelector('starlight-theme-select select');s.value='${theme}';s.dispatchEvent(new Event('change',{bubbles:true}));})()`);
  await until(()=>evaluate('document.documentElement.dataset.theme === '+JSON.stringify(theme)),'theme '+theme);
  const expectedLogo='ruxbase-horizontal-'+(theme==='light'?'black':'white');
  await until(()=>evaluate('Array.from(document.querySelectorAll(".site-title img")).some(i=>i.complete&&i.naturalWidth>0&&i.getClientRects().length>0&&i.currentSrc.includes('+JSON.stringify(expectedLogo)+'))'),'theme logo '+theme);
  const palette=await evaluate('({background:getComputedStyle(document.documentElement).getPropertyValue("--sl-color-bg").trim(),accent:getComputedStyle(document.documentElement).getPropertyValue("--sl-color-accent").trim(),nav:getComputedStyle(document.documentElement).getPropertyValue("--sl-color-bg-nav").trim()})');
  assert(palette.background === (theme === 'dark' ? '#000000' : '#ffffff'),'Wrong monochrome documentation background '+theme+': '+JSON.stringify(palette));
  assert(palette.nav === palette.background,'Nav surface is not monochrome '+theme);
  assert(palette.accent === (theme === 'dark' ? '#f5f5f5' : '#171717'),'Non-monochrome accent '+theme);
  await screenshot('desktop-'+theme);
 }
 await visit('integrations/javascript/');
 assert(await evaluate('document.querySelector("h1").textContent.includes("JavaScript")'),'Integration heading');
 assert(await evaluate('!!document.querySelector(\'a[href="/ruxbase/integrations/javascript.md"]\')'),'Integration Markdown link');
 const topicResponse=await fetch(origin+SITE_BASE+'llms-integrations.txt');
 assert(topicResponse.ok&&(await topicResponse.text()).includes('Build a complete local posts application'),'Integration bundle retrieval');
 await visit('reference/api/');
 assert(await evaluate('document.querySelector("h1").textContent.includes("API reference")'),'Reference heading');
 assert(await evaluate('!!document.querySelector(\'a[href="/ruxbase/reference/api.md"]\')'),'Markdown link');
 await until(()=>evaluate('!!document.querySelector("button[data-open-modal]:not([disabled])")'),'search ready');
 // Leave a cold navigation idle long enough to catch the former requestIdleCallback load.
 await sleep(1800);
 const searchRequests=()=>evaluate('performance.getEntriesByType("resource").map(r=>r.name).filter(n=>/\\/pagefind\\/|\\/ui-core\\.|\\/Search\\.astro/i.test(n))');
 assert((await searchRequests()).length===0,'Search assets requested before user intent');
 await evaluate('document.querySelector("button[data-open-modal]").click()');
 await until(()=>evaluate('!!document.querySelector("#starlight__search input")'),'Pagefind input');
 await evaluate(`(()=>{const input=document.querySelector('#starlight__search input');input.value='readiness';input.dispatchEvent(new Event('input',{bubbles:true}));})()`);
 await until(()=>evaluate('Array.from(document.querySelectorAll("#starlight__search a")).some(a=>a.href.includes("/ruxbase/reference/api/")||a.href.includes("/ruxbase/operations/overview/"))'),'Pagefind readiness result',25000);
 const deferredRequests=await searchRequests();
 assert(deferredRequests.some(n=>n.endsWith('/pagefind/pagefind-ui.js'))&&deferredRequests.some(n=>n.endsWith('/pagefind/pagefind-ui.css')),'Deferred search assets were not loaded on demand');
 await screenshot('desktop-search');
 await evaluate('document.querySelector("site-search dialog").close()');
 await until(()=>evaluate('document.activeElement === document.querySelector("[data-open-modal]")'),'search focus restored');
 await evaluate('window.dispatchEvent(new KeyboardEvent("keydown",{key:"k",ctrlKey:true,bubbles:true,cancelable:true}))');
 await until(()=>evaluate('document.querySelector("site-search dialog").open && document.activeElement === document.querySelector("#starlight__search input")'),'keyboard reopen and focus');
 assert((await searchRequests()).filter(n=>n.endsWith('/pagefind/pagefind-ui.js')).length===1,'Search UI fetched twice');
 await evaluate('document.querySelector("site-search dialog").close()');
 await writeFile(join(output,'deferred-search.json'),JSON.stringify({beforeIntent:[],afterIntent:deferredRequests,keyboardReopen:true,focusRestored:true},null,2)+'\n');
 await viewport(390,844,true);
 await visit('getting-started/quickstart/');
 assert(await evaluate('document.documentElement.scrollWidth <= innerWidth + 2'),'Mobile horizontal overflow');
 const menu='button[popovertarget="starlight__sidebar"]';
 assert(await evaluate('!!document.querySelector('+JSON.stringify(menu)+')?.getClientRects().length'),'Mobile navigation button missing');
 await evaluate('document.querySelector('+JSON.stringify(menu)+').click()');
 await until(()=>evaluate('!!document.querySelector("#starlight__sidebar:popover-open")'),'Mobile menu open');
 await screenshot('mobile-menu');
 await evaluate('document.querySelector('+JSON.stringify(menu)+').click()');
 await screenshot('mobile-quickstart');
 await send('Emulation.setScriptExecutionDisabled',{value:true});
 await visit('reference/api/');
 assert(await evaluate('document.body.innerText.includes("GET /api/health")'),'Missing static reference without JavaScript');
 await send('Emulation.setScriptExecutionDisabled',{value:false});
 assert(failures.length===0,'Browser exceptions or missing local assets: '+failures.join('; '));
 const markdown=await fetch(origin+SITE_BASE+'reference/api.md');
 assert(markdown.ok&&(await markdown.text()).includes('GET /api/health'),'Markdown retrieval');
 const spec=await(await fetch(origin+SITE_BASE+'openapi.json')).json();
 assert(spec.openapi==='3.1.0','OpenAPI retrieval');
 if(process.env.DOCS_SCALAR_PREVIEW==='1'){
  await viewport(1440,900,false);
  viewingScalar=true;
  await visit('api-viewer/');
  try {
   await until(()=>evaluate('document.querySelector("#api-reference")?.innerText.includes("List records") && document.querySelector("#api-reference")?.innerText.includes("Search records by vector") && document.querySelector("#api-reference")?.innerText.includes("List authentication methods") && document.querySelector("#api-reference")?.innerText.includes("Request protected file token") && document.querySelector("#api-reference")?.innerText.includes("List collections") && document.querySelector("#api-reference")?.innerText.includes("Read application settings") && document.querySelector("#api-reference")?.innerText.includes("List application logs") && document.querySelector("#api-reference")?.innerText.includes("Request backup restore") && document.querySelector("#api-reference")?.innerText.includes("Connect realtime event stream") && document.querySelector("#api-reference")?.innerText.includes("Submit record batch")'),'Scalar rendered operations',30000);
  } catch(error) {
   await screenshot('scalar-failure');
   console.log('Scalar diagnostic:',await evaluate('document.querySelector("#api-reference")?.innerText.slice(0,1500)'),failures);
   throw error;
  }
  assert(await evaluate('!Array.from(document.querySelectorAll("button")).some(b=>/test request/i.test(b.innerText))'),'Scalar must not expose test request buttons');
  assert(await evaluate('!Array.from(document.querySelectorAll("button,a")).some(b=>b.getClientRects().length>0 && /^(ask ai|ask ai agent|generate mcp|connect mcp)$/i.test(b.innerText.trim()))'),'Unexpected auxiliary AI/MCP actions');
  await screenshot('scalar-candidate');
  assert(failures.length===0,'Scalar browser exceptions or missing local assets: '+failures.join('; '));
  viewingScalar=false;
  assert(viewerRequests.every(r=>['GET','HEAD'].includes(r.method) && (r.url.startsWith(origin+SITE_BASE+'api-viewer/') || r.url===origin+SITE_BASE+'openapi.json' || r.url===origin+'/favicon.ico' || r.url.startsWith('data:') || r.url.startsWith('blob:'))),'Unexpected viewer requests: '+JSON.stringify(viewerRequests));
  await writeFile(join(output,'scalar-requests.json'),JSON.stringify(viewerRequests,null,2)+'\n');
  console.log('Scalar viewer rendered scoped operations with test requests hidden and network requests limited to documentation resources.');
 }
 if(process.env.DOCS_AXE_FILE){
  const axeCode=await readFile(process.env.DOCS_AXE_FILE,'utf8');
  const results=[];
  for(const [path,theme,width] of [['','light',1440],['','dark',1440],['reference/api/','light',1440],['authorization/row-level-security/','dark',1440],['getting-started/quickstart/','light',390]]){
   await viewport(width,900,width===390);
   await visit(path);
   await until(()=>evaluate('!!customElements.get("starlight-theme-select")'),'a11y theme ready');
   await evaluate(`(()=>{const s=document.querySelector('starlight-theme-select select');s.value='${theme}';s.dispatchEvent(new Event('change',{bubbles:true}));})()`);
   await until(()=>evaluate('document.documentElement.dataset.theme === '+JSON.stringify(theme)),'a11y theme');
   await evaluate(axeCode+'\n;void 0');
   const result=await evaluate(`axe.run(document,{runOnly:{type:'tag',values:['wcag2a','wcag2aa','wcag21a','wcag21aa','wcag22aa']}}).then(r=>({version:r.testEngine.version,passes:r.passes.length,incomplete:r.incomplete.length,violations:r.violations.map(v=>({id:v.id,impact:v.impact,description:v.description,nodes:v.nodes.map(n=>({target:n.target,summary:n.failureSummary}))}))}))`);
   results.push({path,theme,width,...result});
  }
  await writeFile(join(output,'accessibility.json'),JSON.stringify(results,null,2)+'\n');
  const blockers=results.flatMap(r=>r.violations.filter(v=>v.impact==='critical'||v.impact==='serious').map(v=>({path:r.path,theme:r.theme,id:v.id,impact:v.impact})));
  assert(!blockers.length,'Accessibility blockers: '+JSON.stringify(blockers));
  console.log('Representative accessibility audit passed serious/critical gate: '+results.length+' configurations.');
 }
 await writeFile(join(output,'report.json'),JSON.stringify({browser:await evaluate('navigator.userAgent'),scalarViewer:process.env.DOCS_SCALAR_PREVIEW==='1'?'passed':'excluded-from-public-profile',checks:['desktop light','desktop dark','reference navigation','Pagefind readiness result','mobile menu','mobile overflow','static reference','Markdown retrieval','OpenAPI retrieval','local asset and exception check']},null,2)+'\n');
 console.log('Browser smoke passed: themes, search, mobile menu, static reference and machine-readable retrieval.');
}finally{
 if(ws)ws.close();
 if(chrome&&chrome.exitCode===null){chrome.kill();await Promise.race([new Promise(r=>chrome.once('exit',r)),sleep(3000)]);}
 await new Promise(r=>server.close(r));await rm(profile,{recursive:true,force:true,maxRetries:3,retryDelay:200});
}
