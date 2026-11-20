import { test } from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtemp, readFile, readdir, stat, rm } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";
const root=fileURLToPath(new URL("../",import.meta.url));
const cli=join(root,"scripts/create-ruxbase-app.mjs");
const run=args=>spawnSync(process.execPath,[cli,...args],{encoding:"utf8",cwd:root});
const json=r=>JSON.parse(r.status===0?r.stdout:r.stderr);

test("CLI gives actionable help, framework list and explicit errors",()=>{
  assert.match(run(["--help"]).stdout,/npm ci/);
  assert.deepEqual(json(run(["--list","--json"])).templates,["react","nextjs"]);
  for(const args of [["wrong","demo"],["react","../../escape"],["react","UPPER"],["react","ends-"],
    ["react","a"],["--framework","nextjs","--name","bad?"],["--unknown"],
    ["react","demo","--output"],["react","demo","--output",".","--output",".."]]){
    const response=run([...args,"--json"]);
    assert.equal(response.status,2,"Unexpected accepted arguments: "+args.join(" "));
    assert.equal(json(response).ok,false);
  }
});
test("Both starters scaffold deterministically with portable lock and no secrets",async()=>{
  const dir=await mkdtemp(join(tmpdir(),"ruxbase-scaffold-b02-"));
  try{
    for(const framework of ["react","nextjs"]){
      const name="generated-"+framework;
      const dry=run(["--framework",framework,"--name",name,"--output",dir,"--dry-run","--json"]);
      assert.equal(dry.status,0,dry.stderr);
      assert.equal(json(dry).dryRun,true);
      assert.deepEqual(await readdir(dir),[]);
      const proc=run([framework,name,"--output",dir,"--json"]);
      assert.equal(proc.status,0,proc.stderr);
      const target=join(dir,name);
      assert.equal(json(proc).target,target);
      const pkg=JSON.parse(await readFile(join(target,"package.json"),"utf8"));
      const lock=JSON.parse(await readFile(join(target,"package-lock.json"),"utf8"));
      assert.equal(pkg.name,name);
      assert.equal(lock.name,name);
      assert.equal(lock.packages[""].name,name);
      assert.equal(lock.lockfileVersion,3);
      assert.equal(pkg.dependencies.pocketbase,"0.28.1");
      assert.ok(Object.keys(lock.packages).length>=15);
      await stat(join(target,framework==="react"?"src/main.jsx":"app/page.jsx"));
      for(const f of ["public/ruxbase-horizontal-white.svg","public/ruxbase-horizontal-black.svg",
        ".env.example","RUXBASE_AI_CONTEXT.md","README.md"])await stat(join(target,f));
      assert.match(await readFile(join(target,"RUXBASE_AI_CONTEXT.md"),"utf8"),/npm ci/);
      assert.ok(!JSON.stringify(pkg).includes("POSTGRES_PASSWORD"));
      assert.equal(run([framework,name,"--output",dir,"--json"]).status,2,"No overwrite on existing directory");
      // Existing project must never be changed by rejected invocations.
      assert.equal(JSON.parse(await readFile(join(target,"package.json"),"utf8")).name,name);
    }
  }finally{await rm(dir,{recursive:true,force:true});}
});
