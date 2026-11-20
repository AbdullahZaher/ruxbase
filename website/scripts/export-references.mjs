import { readFile, writeFile } from 'node:fs/promises';
import { createHash } from 'node:crypto';
import { fileURLToPath } from 'node:url';
import { resolve, join } from 'node:path';
import { SITE_BASE, absolute } from './site-config.mjs';
import { assertWithin } from './prepare.mjs';
const root=fileURLToPath(new URL('../../',import.meta.url));
const dist=join(root,'website/dist');
const json=async p=>JSON.parse(await readFile(p,'utf8'));
const assert=(ok,msg)=>{if(!ok)throw new Error(msg);};
const gitBlob=buf=>createHash('sha1').update('blob '+buf.length+'\0').update(buf).digest('hex');
export async function verifyReferenceInputs(){
 const refs=await json(join(root,'docs/contracts/reference.json'));
 const api=await json(join(root,'docs/contracts/openapi.json'));
 const manifest=await json(join(root,'docs/public/manifest.json'));
 assert(refs.documentationStatus==='development'&&refs.schemaVersion===1,'Incorrect reference status');
 for(const [name,sha] of Object.entries(refs.evidence)){
  const source=resolve(root,name);assertWithin(root,source);
  assert(gitBlob(await readFile(source))===sha,'Reference evidence changed: '+name);
 }
 assert(api.openapi==='3.1.0'&&Object.keys(api.paths).length===24&&api.paths['/api/health']?.get?.operationId==='getHealth','Unexpected API scope');
 const op=api.paths['/api/health'].get;
 assert(op.security.length===0&&op.parameters.some(p=>p.name==='ready'&&p.in==='query'&&!p.required),'Health parameter mismatch');
 assert(Object.keys(op.responses).sort().join(',')==='200,503','Health codes mismatch');
 assert(api.components.schemas.HealthResponse.required.join(',')==='message,code,data','Health schema mismatch');
 const actual=Object.entries(api.paths).flatMap(([path,methods])=>Object.entries(methods).map(([method,o])=>({method:method.toUpperCase(),path,operationId:o.operationId,success:Object.keys(o.responses).filter(k=>k.startsWith('2'))[0]})));
 assert(actual.length===31&&JSON.stringify(actual)===JSON.stringify(refs.operations),'Operation inventory mismatch');
 const adminPaths=new Set(['/api/collections','/api/collections/{collection}','/api/collections/{collection}/rls','/api/collections/meta/scaffolds','/api/collections/meta/oauth2-providers','/api/settings','/api/backups']);
 for(const path of adminPaths) for(const operation of Object.values(api.paths[path])) assert(JSON.stringify(operation.security)===JSON.stringify([{SuperuserToken:[]}]),'Missing superuser contract: '+path);
 const file=api.paths['/api/files/{collection}/{recordId}/{filename}'].get;
 assert(JSON.stringify(file.security)===JSON.stringify([{}, {FileToken:[]}]),'Unexpected file authentication contract');
 const apiMd=await readFile(join(root,'docs/public/reference/api.md'),'utf8');
 for(const o of actual.filter(o=>o.operationId!=='getHealth'))assert(apiMd.includes('| '+o.method+' | `'+o.path+'` | '+o.success+' |'),'Missing operation table row: '+o.operationId);
 for(const [status,resp] of Object.entries(op.responses)){
  const media=resp.content['application/json'];
  assert(media.schema.$ref==='#/components/schemas/HealthResponse'&&media.example.code===Number(status),'Health example mismatch');
  assert(apiMd.includes(JSON.stringify(media.example,null,2)),'Human API example drift');
 }
 for(const [items,name] of [[refs.cli,'cli'],[refs.configuration,'configuration']]){
  const md=await readFile(join(root,'docs/public/reference/'+name+'.md'),'utf8');
  assert(new Set(items.map(i=>i.name)).size===items.length,'Duplicate reference');
  for(const i of items)assert(md.includes('| `'+i.name+'` | '+i.defaultValue+' | '+i.description+' |'),'Reference drift: '+i.name);
 }
 const featureMd=await readFile(join(root,'docs/public/reference/features.md'),'utf8');
 for(const f of refs.features){
  assert(f.status==='documented-development'&&refs.evidence[f.source]&&manifest.pages.some(p=>p.source===f.source&&p.target===f.page+'.md'),'Feature scope mismatch');
  assert(featureMd.includes('| ['+f.id+']('+SITE_BASE+f.page+'/) | '+f.status+' | '+f.limitations+' |'),'Feature text drift');
 }
 return {refs,api};
}
export async function exportReferences(){
 const {refs,api}=await verifyReferenceInputs();
 const v=await json(join(dist,'version.json'));
 const identity={sourceCommit:v.sourceCommit,sourceTree:v.sourceTree,documentationStatus:v.documentationStatus,releaseVersion:v.releaseVersion};
 const outputs={
  'openapi.json':{...api,'x-ruxbase-source':identity},
  'feature-manifest.json':{schemaVersion:1,...identity,scope:refs.coverage.features,features:refs.features.map(f=>({...f,documentation:absolute(SITE_BASE+f.page+'/'),sourceBlob:refs.evidence[f.source]}))},
  'cli-reference.json':{schemaVersion:1,...identity,scope:refs.coverage.cli,flags:refs.cli},
  'configuration-reference.json':{schemaVersion:1,...identity,scope:refs.coverage.configuration,settings:refs.configuration},
 };
 for(const [name,value] of Object.entries(outputs))await writeFile(join(dist,name),JSON.stringify(value,null,2)+'\n',{flag:'wx'});
 console.log('Verified '+Object.keys(refs.evidence).length+' evidence blobs; exported '+refs.operations.length+' scoped REST operations, '+refs.features.length+' features, '+refs.cli.length+' CLI flags and '+refs.configuration.length+' configuration settings.');
}
if(process.argv[1]&&resolve(process.argv[1])===fileURLToPath(import.meta.url))await exportReferences();
