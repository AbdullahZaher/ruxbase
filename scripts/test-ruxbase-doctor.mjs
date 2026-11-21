import {test} from "node:test";
import assert from "node:assert/strict";
import {createServer} from "node:http";
import {spawn} from "node:child_process";
import {fileURLToPath} from "node:url";
const exe=fileURLToPath(new URL("./ruxbase-doctor.mjs",import.meta.url));
async function run(args){
 return await new Promise(resolve=>{
   const p=spawn(process.execPath,[exe,...args],{stdio:["ignore","pipe","pipe"]});
   let stdout="",stderr="";
   p.stdout.on("data",x=>stdout+=x);p.stderr.on("data",x=>stderr+=x);
   p.on("close",code=>resolve({code,stdout,stderr}));
 });
}
test("Doctor rejects remote origins and does not connect to external services",async()=>{
 for(const url of ["http://169.254.169.254/","https://example.com/","http://user:pass@127.0.0.1:8090/","http://localhost/secret"]){
  const p=await run(["--url",url,"--json"]);
  assert.equal(p.code,2);
  assert.equal(JSON.parse(p.stdout).code,"URL");
 }
 assert.equal((await run(["--unknown","--json"])).code,2);
});
test("Doctor reports actual local readiness and missing posts without secrets",async()=>{
 let postsAvailable=false;
 const server=createServer((req,res)=>{
   res.setHeader("content-type","application/json");
   if(req.url.startsWith("/api/health"))res.end(JSON.stringify({data:{ready:{database:true,cluster:true}}}));
   else if(!postsAvailable){res.writeHead(404);res.end("{}");}
   else res.end(JSON.stringify({items:[]}));
 });
 await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve));
 try{
   const origin="http://127.0.0.1:"+server.address().port;
   const failure=await run(["--url",origin,"--json"]);
   assert.equal(failure.code,2);
   const data=JSON.parse(failure.stdout);
   assert.equal(data.checks[0].pass,true);
   assert.equal(data.checks[1].pass,false);
   postsAvailable=true;
   const success=await run(["--url",origin,"--json"]);
   assert.equal(success.code,0);
   assert.equal(JSON.parse(success.stdout).ok,true);
 }finally{await new Promise(resolve=>server.close(resolve));}
});
