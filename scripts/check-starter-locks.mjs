// Enforce the committed transitive npm dependency identity of both starters.
// npm ci additionally validates lock/manifest coherence at install time.
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
for(const framework of ["react","nextjs"]){
  const folder=new URL("../starters/"+framework+"/",import.meta.url);
  const packageJson=JSON.parse(await readFile(new URL("package.json",folder),"utf8"));
  const lock=JSON.parse(await readFile(new URL("package-lock.json",folder),"utf8"));
  assert.equal(lock.lockfileVersion,3);
  assert.equal(lock.name,packageJson.name);
  assert.equal(lock.packages?.[""]?.name,packageJson.name);
  assert.deepEqual(lock.packages?.[""]?.dependencies,packageJson.dependencies);
  assert.deepEqual(lock.packages?.[""]?.devDependencies||{},packageJson.devDependencies||{});
  assert.ok(Object.keys(lock.packages).length>=15,"Lockfile missing resolved transitive graph");
  assert.equal(packageJson.dependencies.pocketbase,"0.28.1");
  for(const [name,meta] of Object.entries(lock.packages)){
    if(name.startsWith("node_modules/") && meta.resolved){
      assert.match(meta.resolved,/^https:\/\/registry\.npmjs\.org\//,"Untrusted npm tarball origin: "+name);
      assert.match(meta.integrity||"",/^sha(?:512|384|256)-/,"Missing registry integrity digest: "+name);
    }
  }
  console.log("Locked",framework,"packages:",Object.keys(lock.packages).length);
}
