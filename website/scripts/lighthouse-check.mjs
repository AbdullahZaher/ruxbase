import { createServer } from 'node:http';
import { spawn } from 'node:child_process';
import { readFile, mkdir, writeFile, stat } from 'node:fs/promises';
import { resolve, join, extname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { SITE_BASE } from './site-config.mjs';
const dist=fileURLToPath(new URL('../dist/',import.meta.url));
const output=fileURLToPath(new URL('../performance-evidence/',import.meta.url));
const executable=process.argv[2];
if(!executable)throw Error('Pass the isolated Lighthouse CLI executable');
await mkdir(output,{recursive:true});
const version=JSON.parse(await readFile(join(dist,'version.json'),'utf8'));
const mime={'.html':'text/html; charset=utf-8','.js':'text/javascript','.mjs':'text/javascript','.css':'text/css','.json':'application/json','.svg':'image/svg+xml','.png':'image/png','.webp':'image/webp','.wasm':'application/wasm','.md':'text/plain; charset=utf-8','.txt':'text/plain; charset=utf-8','.woff2':'font/woff2','.xml':'application/xml'};
const server=createServer(async(req,res)=>{
 try{
  const path=decodeURIComponent(new URL(req.url,'http://localhost').pathname);
  if(!path.startsWith(SITE_BASE))throw Error('Outside project base');
  let file=resolve(dist,path.slice(SITE_BASE.length));
  if(file!==resolve(dist)&&!file.startsWith(resolve(dist)+'/'))throw Error('Outside artifact');
  if((await stat(file)).isDirectory())file=join(file,'index.html');
  const content=await readFile(file);
  res.writeHead(200,{'Content-Type':mime[extname(file)]||'application/octet-stream','Content-Length':content.length});
  res.end(req.method==='HEAD'?undefined:content);
 }catch{res.writeHead(404);res.end('Not found');}
});
const run=(args)=>new Promise((ok,fail)=>{
 const proc=spawn(executable,args,{stdio:'inherit',env:{...process.env,CHROME_PATH:process.env.CHROME_BIN||'google-chrome'}});
 const timer=setTimeout(()=>{proc.kill();fail(Error('Lighthouse timed out'));},120000);
 proc.once('error',e=>{clearTimeout(timer);fail(e);});
 proc.once('exit',code=>{clearTimeout(timer);code===0?ok():fail(Error('Lighthouse exit '+code));});
});
const median=a=>[...a].sort((a,b)=>a-b)[Math.floor(a.length/2)];
const pages=[['home',''],['quickstart','getting-started/quickstart/'],['api','reference/api/']];
const results=[];
try{
 await new Promise(r=>server.listen(0,'127.0.0.1',r));
 const origin='http://127.0.0.1:'+server.address().port;
 for(const [name,path] of pages){
  const trials=[];
  for(let trial=1;trial<=3;trial++){
   const file=join(output,name+'-'+trial+'.json');
   const flags=['--headless','--disable-dev-shm-usage','--disable-background-networking'];
   if(process.env.DOCS_BROWSER_NO_SANDBOX==='1')flags.push('--no-sandbox');
   await run([origin+SITE_BASE+path,'--quiet','--output=json','--output-path='+file,'--only-categories=performance,accessibility,best-practices,seo','--throttling-method=simulate','--chrome-flags='+flags.join(' ')]);
   const report=JSON.parse(await readFile(file,'utf8'));
   if(report.runtimeError||report.lighthouseVersion!=='13.6.0'||report.finalDisplayedUrl!==origin+SITE_BASE+path)throw Error('Invalid Lighthouse report for '+name);
   const a=report.audits;
   const requests=a['network-requests']?.details?.items;
   if(!Array.isArray(requests))throw Error('Missing cold-navigation request evidence');
   const eager=requests.filter(item=>/\/pagefind\/|\/ui-core\.|\/Search\.astro/i.test(new URL(item.url,origin).pathname));
   if(eager.length)throw Error('Search assets downloaded before interaction: '+eager.map(x=>x.url).join(', '));
   const m={trial,performance:report.categories.performance.score,accessibility:report.categories.accessibility.score,seo:report.categories.seo.score,bestPractices:report.categories['best-practices'].score,lcpMs:a['largest-contentful-paint'].numericValue,cls:a['cumulative-layout-shift'].numericValue,tbtMs:a['total-blocking-time'].numericValue};
   if(Object.values(m).some(x=>typeof x!=='number'||!Number.isFinite(x)))throw Error('Missing numeric metrics');
   trials.push(m);
  }
  const metrics=Object.fromEntries(Object.keys(trials[0]).filter(k=>k!=='trial').map(k=>[k,median(trials.map(x=>x[k]))]));
  const passed=metrics.performance>=.95&&metrics.seo>=.95&&metrics.accessibility>=.95&&metrics.lcpMs<=2500&&metrics.cls<=.1;
  results.push({name,path,metrics,passed,trials});
  console.log('LIGHTHOUSE',name,JSON.stringify(metrics),'PASS',passed);
 }
 const summary={schemaVersion:1,sourceCommit:version.sourceCommit,sourceTree:version.sourceTree,lighthouseVersion:'13.6.0',method:'Local project-base static server, default mobile simulated throttling, median of 3 independent cold navigations per page',thresholds:{performance:.95,seo:.95,accessibility:.95,lcpMs:2500,cls:.1},limitations:'Lab only; not field Core Web Vitals, production network, search indexing, or full WCAG certification. Best-practices and TBT reported, not gated. Scalar viewer is noindex and outside this page sample.',results,passed:results.every(x=>x.passed)};
 await writeFile(join(output,'summary.json'),JSON.stringify(summary,null,2)+'\n');
 if(!summary.passed)throw Error('Lighthouse quality gate failed');
}finally{await new Promise(r=>server.close(r));}
