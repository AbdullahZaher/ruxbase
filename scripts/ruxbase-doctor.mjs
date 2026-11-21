#!/usr/bin/env node
// Read-only local developer diagnostic. No credentials, shell execution,
// destructive operations, Docker starts or external-network requests.
const args=process.argv.slice(2);
let origin="http://127.0.0.1:8090",json=false,help=false,unknown="";
for(let i=0;i<args.length;i++){
  const arg=args[i];
  if(arg==="--json")json=true;
  else if(arg==="--help"||arg==="-h")help=true;
  else if(arg==="--url" && args[i+1])origin=args[++i];
  else unknown=arg;
}
function emit(message,status){
  console.log(json?JSON.stringify(message):typeof message==="string"?message:JSON.stringify(message,null,2));
  process.exitCode=status;
}
if(help)emit("Usage: node scripts/ruxbase-doctor.mjs [--url http://127.0.0.1:8090] [--json]\nRead-only local backend health/readiness and example collection checks.",0);
else if(unknown)emit({ok:false,code:"USAGE",message:"Unknown or missing argument: "+unknown},2);
else {
 let url;
 try{url=new URL(origin);}catch{}
 if(!url||url.protocol!=="http:"||!["127.0.0.1","localhost"].includes(url.hostname)||url.username||url.password||url.pathname!=="/"||url.search||url.hash){
   emit({ok:false,code:"URL",message:"Use a local HTTP origin (127.0.0.1 or localhost), no credentials, path or query."},2);
 } else {
   const base=url.origin;
   const check=async path=>{
     try {
       const response=await fetch(base+path,{headers:{"Accept":"application/json"},signal:AbortSignal.timeout(4500),cache:"no-store"});
       const body=await response.json().catch(()=>null);
       return {status:response.status,ok:response.ok,body};
     }catch{return {status:0,ok:false,body:null};}
   };
   const ready=await check("/api/health?ready");
   const posts=ready.ok?await check("/api/collections/posts/records?perPage=1"):null;
   const checks=[
     {id:"ruxbase-readiness",pass:ready.ok&&ready.body?.data?.ready?.database===true,detail:ready.ok?"Backend HTTP "+ready.status:"Backend not reachable or not ready; start Docker Compose."},
     {id:"example-posts",pass:posts?.ok===true,detail:!posts?"Skipped until backend is ready":posts.ok?"Example posts endpoint responds":"Create the posts collection and documented list/view rules; HTTP "+posts.status}
   ];
   const result={ok:checks.every(c=>c.pass),origin:base,checks,
     advice:"Start root Docker Compose, configure a disposable posts collection, use ordinary users; never paste passwords into commands or issue reports."};
   emit(result,result.ok?0:2);
 }
}
