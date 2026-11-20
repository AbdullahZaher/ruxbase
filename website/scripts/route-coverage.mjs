import { readFile } from 'node:fs/promises';
import { join } from 'node:path';
const assert=(ok,msg)=>{if(!ok)throw Error(msg);};
export function scanBinder(source,binder){
 const marker='func '+binder.function+'(';
 const start=source.indexOf(marker);
 assert(start>=0,'Missing binder: '+binder.function);
 const next=source.indexOf('\nfunc ',start+marker.length);
 const block=source.slice(start,next<0?undefined:next);
 const prefixes=new Map([['rg',binder.base],['pbRouter',binder.base]]);
 const constants=new Map([...source.matchAll(/^const (\w+) = "([^"]*)"$/gm)].map(m=>[m[1],m[2]]));
 const routes=[];
 for(const line of block.split('\n')){
  const g=line.match(/^\s*(\w+)\s*:=\s*(\w+)\.Group\("([^"]*)"\)/);
  if(g){assert(prefixes.has(g[2]),'Unknown group: '+g[2]);prefixes.set(g[1],prefixes.get(g[2])+g[3]);continue;}
  const r=line.match(/^\s*(\w+)\.(GET|POST|PATCH|PUT|DELETE|HEAD|OPTIONS)\(("[^"]*"|\w+)\s*,/);
  if(!r)continue;
  assert(prefixes.has(r[1]),'Unknown route parent: '+r[1]);
  const suffix=r[3].startsWith('"')?r[3].slice(1,-1):constants.get(r[3]);
  assert(typeof suffix==='string','Unresolved route: '+r[3]);
  routes.push({method:r[2],path:prefixes.get(r[1])+suffix,source:binder.file});
 }
 assert(routes.length>0,'Empty binder: '+binder.function);
 return routes;
}
export async function buildCoverage(root,refs){
 const spec=JSON.parse(await readFile(join(root,'docs/contracts/route-coverage.json'),'utf8'));
 const base=await readFile(join(root,'apis/base.go'),'utf8');
 const actual=[...base.matchAll(/^\s*(bind\w+Api)\(app, (?:apiGroup|pbRouter)\)/gm)].map(m=>m[1]).sort();
 assert(JSON.stringify(actual)===JSON.stringify(spec.binders.map(b=>b.function).sort()),'Built-in binder set changed');
 const ops=new Set(refs.operations.map(o=>o.method+' '+o.path)),seen=new Set(),routes=[];
 for(const b of spec.binders){
  assert(refs.evidence[b.file],'Missing evidence: '+b.file);
  for(const r of scanBinder(await readFile(join(root,b.file),'utf8'),b)){
   const key=r.method+' '+r.path;
   assert(!seen.has(key),'Duplicate route: '+key);seen.add(key);
   routes.push({...r,status:ops.has(key)?'scoped-openapi':'not-yet-in-openapi'});
  }
 }
 assert([...ops].every(key=>seen.has(key)),'OpenAPI operation missing from inventory');
 assert(routes.length===60,'Static route count changed: '+routes.length);
 return {schemaVersion:1,scope:spec.scope,registered:routes.length,documented:ops.size,remaining:routes.length-ops.size,routes};
}
