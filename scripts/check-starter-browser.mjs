// Disposable Chromium integration for a generated React or Next.js starter.
// Uses real browser events, ordinary-user login and Ruxbase API via same-origin proxy.
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
const [origin, draftName] = process.argv.slice(2);
const email=process.env.RUXBASE_TEST_EMAIL, password=process.env.RUXBASE_TEST_PASSWORD;
if(!origin?.startsWith("http://127.0.0.1:")||!draftName||!email||!password)throw Error("Missing browser test URL and ephemeral credentials");
const profile=await mkdtemp(join(tmpdir(),"rux-starter-browser-"));
let chrome,ws;
let diagnostics=async()=>"DOM unavailable";
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
async function until(fn,message,timeout=40000){
 const end=Date.now()+timeout;
 while(Date.now()<end){let v=await fn();if(v)return v;await sleep(250);}
 throw Error("Timed out: "+message);
}
try{
 const args=["--headless=new","--remote-debugging-port=0","--user-data-dir="+profile,"--no-first-run","--disable-dev-shm-usage","--disable-background-networking","about:blank"];
 if(process.env.DOCS_BROWSER_NO_SANDBOX==="1")args.unshift("--no-sandbox");
 chrome=spawn(process.env.CHROME_BIN||"google-chrome",args,{stdio:"ignore"});
 let bootError; chrome.on("error",e=>{bootError=e;});
 const port=await until(async()=>{
  if(bootError)throw bootError;if(chrome.exitCode!==null)throw Error("Chrome exited");
  try{return Number((await readFile(join(profile,"DevToolsActivePort"),"utf8")).split("\n")[0]);}catch{return false;}
 },"chromium start",45000);
 const pages=await until(async()=>{try{return await (await fetch("http://127.0.0.1:"+port+"/json/list")).json();}catch{return null;}},"CDP pages");
 const page=pages.find(x=>x.type==="page");assert.ok(page,"No browser page");
 ws=new WebSocket(page.webSocketDebuggerUrl);
 await new Promise((resolve,reject)=>{ws.addEventListener("open",resolve,{once:true});ws.addEventListener("error",reject,{once:true});});
 const pending=new Map();let seq=0;const failures=[];const failedRequests=[];const consoleErrors=[];const apiStatuses=[];
 ws.addEventListener("message",e=>{
  const m=JSON.parse(e.data);
  if(m.id&&pending.has(m.id)){const p=pending.get(m.id);pending.delete(m.id);clearTimeout(p.timer);m.error?p.reject(Error(m.error.message)):p.resolve(m.result);}
  if(m.method==="Runtime.exceptionThrown")failures.push(m.params.exceptionDetails.exception?.description||m.params.exceptionDetails.text);
  if(m.method==="Network.loadingFailed")failedRequests.push(m.params.errorText);
  if(m.method==="Network.responseReceived"&&m.params.response.url.includes("/api/"))apiStatuses.push({url:m.params.response.url,status:m.params.response.status});
  if(m.method==="Runtime.consoleAPICalled"&&m.params.type==="error")consoleErrors.push(m.params.args?.map(x=>x.value||x.description).join(" "));
 });
 async function send(method,params={}){
  return new Promise((resolve,reject)=>{
   const id=++seq,timer=setTimeout(()=>{pending.delete(id);reject(Error("CDP timeout: "+method));},20000);
   pending.set(id,{resolve,reject,timer});ws.send(JSON.stringify({id,method,params}));
  });
 }
 async function evaluate(expression){
  const result=await send("Runtime.evaluate",{expression,returnByValue:true,awaitPromise:true});
  if(result.exceptionDetails)throw Error(result.exceptionDetails.exception?.description||result.exceptionDetails.text);
  return result.result.value;
 }
 diagnostics=async()=>JSON.stringify({body:await evaluate('document.body?.innerText.slice(0,1200) || document.documentElement.outerHTML.slice(0,1200)'),exceptions:failures.slice(-6),failedRequests:failedRequests.slice(-8),consoleErrors:consoleErrors.slice(-8),apiStatuses:apiStatuses.slice(-10),scripts:await evaluate('performance.getEntriesByType("resource").filter(x=>x.name.includes("/_next/")).map(x=>x.name).slice(-12)')});
 await send("Page.enable");await send("Runtime.enable");await send("Network.enable");
 await send("Page.navigate",{url:origin});
 await until(()=>evaluate('document.readyState === "complete" && !!document.querySelector("h1")'),"starter DOM");
 await until(()=>evaluate('document.body.textContent.includes("Backend connected")'),"backend proxy connectivity");
 assert.equal(await evaluate('getComputedStyle(document.documentElement).backgroundColor'),"rgb(0, 0, 0)","True black night mode");
 await evaluate('document.querySelector("button[aria-label=\\\"Switch to light mode\\\"]").click()');
 await until(()=>evaluate('getComputedStyle(document.documentElement).backgroundColor==="rgb(255, 255, 255)"'),"white light mode");
 await evaluate('document.querySelector("button[aria-label=\\\"Switch to dark mode\\\"]").click()');
 await until(()=>evaluate('getComputedStyle(document.documentElement).backgroundColor==="rgb(0, 0, 0)"'),"true-black restoration");
 async function setInput(id,value){
  await evaluate(`(()=>{const el=document.getElementById(${JSON.stringify(id)});const value=${JSON.stringify(value)};Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,"value").set.call(el,value);el.dispatchEvent(new Event("input",{bubbles:true}));el.dispatchEvent(new Event("change",{bubbles:true}));})()`);
 }
 await setInput("email",email);await setInput("password",password);
 await evaluate('document.querySelector("form").requestSubmit()');
 await until(()=>evaluate('!!document.querySelector("[data-testid=signed-in]")'),"user sign-in");
 await setInput("title",draftName);
 await evaluate('document.querySelector("form").requestSubmit()');
 await until(()=>evaluate(`Array.from(document.querySelectorAll("[data-testid=posts] li")).some(x=>x.textContent.includes(${JSON.stringify(draftName)}))`),"new private draft");
 await evaluate('Array.from(document.querySelectorAll("button")).find(x=>x.textContent.trim()==="Sign out").click()');
 await until(()=>evaluate('!document.querySelector("[data-testid=signed-in]")'),"user logout");
 await until(()=>evaluate(`!Array.from(document.querySelectorAll("[data-testid=posts] li")).some(x=>x.textContent.includes(${JSON.stringify(draftName)}))`),"draft not exposed after sign-out");
 assert.deepEqual(await evaluate('Object.keys(localStorage).filter(k=>/pocketbase|auth|token/i.test(k))'),[],"Unexpected persistent browser token");
 assert.deepEqual(failures,[],"Uncaught browser exceptions");
 console.log("Starter browser PASS:",origin,"health, black/white theme, ordinary user login, owner draft, logout isolation.");
}catch(error){
  let body="not available";
  try { body=await diagnostics(); }catch{}
  console.error("Starter browser diagnostic:",origin,error.message,"DOM:",body);
  throw error;
}finally{
 try{ws?.close();}catch{}
 if(chrome?.pid){chrome.kill();await new Promise(resolve=>{if(chrome.exitCode!==null)resolve();else chrome.once("exit",resolve);});}
 // Chromium can briefly keep auxiliary profile handles active after its
 // parent exits; do not mask the actual browser acceptance failure.
 try{await rm(profile,{recursive:true,force:true,maxRetries:12,retryDelay:500});}
 catch(error){console.warn("Chromium temporary profile cleanup:",error.code);}
}
