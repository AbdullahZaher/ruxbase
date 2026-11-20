import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdtemp, mkdir, writeFile, rm, symlink, rename, readFile, unlink } from 'node:fs/promises';
import { join } from 'node:path';
import { tmpdir } from 'node:os';
import { seal, verify } from '../scripts/release-artifact.mjs';
const idA='a'.repeat(40),idB='b'.repeat(40);
async function snapshot(dir,id){
 await mkdir(dir);
 await writeFile(join(dir,'version.json'),JSON.stringify({product:'Ruxbase',documentationStatus:'development',releaseVersion:null,dirty:false,sourceCommit:id,sourceTree:id}));
 await writeFile(join(dir,'index.html'),'<h1>'+id+'</h1>');
 return seal(dir);
}
test('seals exact bytes and rejects modified, missing or unexpected files',async()=>{
 const root=await mkdtemp(join(tmpdir(),'docs-manifest-'));
 try{
  const dir=join(root,'a');const info=await snapshot(dir,idA);
  assert.equal(info.fileCount,3);
  await assert.rejects(()=>verify(dir,idB),/Unexpected artifact source/);
  await writeFile(join(dir,'index.html'),'changed');
  await assert.rejects(()=>verify(dir,idA),/Artifact/);
  await writeFile(join(dir,'index.html'),'<h1>'+idA+'</h1>');
  await verify(dir,idA);
  await writeFile(join(dir,'unexpected.txt'),'unreviewed');
  await assert.rejects(()=>verify(dir,idA),/Artifact/);
  await unlink(join(dir,'unexpected.txt'));
  await unlink(join(dir,'index.html'));
  await assert.rejects(()=>verify(dir,idA),/Artifact/);
 }finally{await rm(root,{recursive:true,force:true});}
});
test('rejects symlinks in output and restores previous verified bytes in disposable activation fixture',async()=>{
 const root=await mkdtemp(join(tmpdir(),'docs-activate-'));
 try{
  const a=join(root,'a'),b=join(root,'b');
  const old=await snapshot(a,idA),next=await snapshot(b,idB);
  await symlink(join(a,'index.html'),join(b,'alias'));
  await assert.rejects(()=>verify(b,idB),/Non-regular/);
  await unlink(join(b,'alias'));
  for(const [target,id,expected] of [[a,idA,old],[b,idB,next],[a,idA,old]]){
   await verify(target,id);
   await symlink(target,join(root,'pending'),'dir');
   await rename(join(root,'pending'),join(root,'active'));
   // Only the rehearsal alias can be a symlink; snapshot roots remain real directories.
   const bytes=JSON.parse(await readFile(join(root,'active','version.json'),'utf8'));
   assert.equal(bytes.sourceCommit,id);
   const activeManifest=await readFile(join(root,'active','artifact-manifest.json'));
   assert.deepEqual(activeManifest,await readFile(join(target,'artifact-manifest.json')));
   assert.equal((await verify(target,id)).manifestSha256,expected.manifestSha256);
  }
 }finally{await rm(root,{recursive:true,force:true});}
});
