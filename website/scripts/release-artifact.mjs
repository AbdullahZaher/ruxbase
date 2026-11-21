import { readFile, writeFile, readdir, lstat } from 'node:fs/promises';
import { createHash } from 'node:crypto';
import { resolve, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { SITE_BASE, absolute } from './site-config.mjs';
const manifestName='artifact-manifest.json';
const sha=data=>createHash('sha256').update(data).digest('hex');
const assert=(ok,msg)=>{if(!ok)throw Error(msg);};
const json=async(file)=>JSON.parse(await readFile(file,'utf8'));
const serialize=value=>JSON.stringify(value,null,2)+'\n';
async function scan(root){
 const files=[];
 async function walk(dir,prefix=''){
  for(const name of (await readdir(dir)).sort()){
   const path=prefix+name,full=join(dir,name);
   assert(!name.startsWith('.')&&!name.includes('\\')&&!/[\u0000-\u001f]/.test(name),'Unexpected artifact path: '+path);
   const st=await lstat(full);
   if(st.isDirectory())await walk(full,path+'/');
   else{
    assert(st.isFile()&&!st.isSymbolicLink(),'Non-regular artifact file: '+path);
    if(path!==manifestName){const data=await readFile(full);files.push({path,bytes:data.length,sha256:sha(data)});}
   }
  }
 }
 assert((await lstat(root)).isDirectory(),'Artifact root must be a real directory');
 await walk(root);
 return files.sort((a,b)=>a.path<b.path?-1:a.path>b.path?1:0);
}
async function identity(root){
 const v=await json(join(root,'version.json'));
 assert(v.product==='Ruxbase'&&v.documentationStatus==='development'&&v.releaseVersion===null&&v.dirty===false,'Only clean development snapshots are supported');
 assert(/^[0-9a-f]{40}$/.test(v.sourceCommit)&&/^[0-9a-f]{40}$/.test(v.sourceTree),'Invalid Git identity');
 return {sourceCommit:v.sourceCommit,sourceTree:v.sourceTree,documentationStatus:v.documentationStatus,releaseVersion:v.releaseVersion};
}
function versions(id){
 return {schemaVersion:1,product:'Ruxbase',...id,channels:{development:{status:'candidate',url:absolute(SITE_BASE)},stable:null},releases:[],notice:'Development snapshot only. No approved stable release, /next or historical version is published by this build.'};
}
export async function seal(root){
 const id=await identity(root);
 await writeFile(join(root,'versions.json'),serialize(versions(id)),{flag:'wx'});
 const files=await scan(root);
 const m={schemaVersion:1,product:'Ruxbase',...id,selfExcluded:manifestName,fileCount:files.length,totalBytes:files.reduce((n,f)=>n+f.bytes,0),files};
 await writeFile(join(root,manifestName),serialize(m),{flag:'wx'});
 return verify(root,id.sourceCommit);
}
export async function verify(root,expectedCommit){
 const id=await identity(root);
 assert(!expectedCommit||id.sourceCommit===expectedCommit,'Unexpected artifact source');
 const m=await json(join(root,manifestName));
 assert(m.schemaVersion===1&&m.product==='Ruxbase'&&m.selfExcluded===manifestName,'Invalid manifest schema');
 for(const [k,v] of Object.entries(id))assert(m[k]===v,'Artifact identity mismatch: '+k);
 const catalog=await json(join(root,'versions.json'));
 assert(serialize(catalog)===serialize(versions(id)),'Version catalog drift');
 const actual=await scan(root);
 assert(m.fileCount===actual.length&&m.totalBytes===actual.reduce((n,f)=>n+f.bytes,0),'Artifact count/size mismatch');
 assert(serialize(m.files)===serialize(actual),'Artifact file set or content mismatch');
 const manifestSha256=sha(await readFile(join(root,manifestName)));
 console.log('Verified release artifact: '+m.fileCount+' files, manifest SHA256 '+manifestSha256);
 return {sourceCommit:id.sourceCommit,fileCount:m.fileCount,manifestSha256};
}
if(process.argv[1]&&resolve(process.argv[1])===fileURLToPath(import.meta.url)){
 const root=fileURLToPath(new URL('../dist/',import.meta.url));
 if(process.argv[2]==='seal')await seal(root);
 else if(process.argv[2]==='verify')await verify(root,process.argv[3]);
 else throw Error('Usage: release-artifact.mjs seal|verify [expectedCommit]');
}
