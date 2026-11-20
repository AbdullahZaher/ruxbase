import { test } from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtemp, readFile, stat, rm } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";

const root=fileURLToPath(new URL("../",import.meta.url));
const cli=join(root,"scripts/create-ruxbase-app.mjs");
const run=(args)=>spawnSync(process.execPath,[cli,...args],{encoding:"utf8",cwd:root});

test("AI can deterministically create complete React and Next.js projects without secrets",async()=>{
  const dir=await mkdtemp(join(tmpdir(),"ruxbase-ai-scaffold-"));
  try{
    for(const framework of ["react","nextjs"]){
      const name="demo-"+framework;
      const response=run([framework,name,"--output",dir]);
      assert.equal(response.status,0,response.stderr);
      const dst=join(dir,name);
      const pkg=JSON.parse(await readFile(join(dst,"package.json"),"utf8"));
      assert.equal(pkg.name,name);
      assert.equal(pkg.dependencies.pocketbase,"0.28.1");
      await stat(join(dst,framework==="react"?"src/main.jsx":"app/page.jsx"));
      for(const file of ["public/ruxbase-horizontal-white.svg","public/ruxbase-horizontal-black.svg",".env.example","RUXBASE_AI_CONTEXT.md","README.md"])await stat(join(dst,file));
      assert.match(await readFile(join(dst,"RUXBASE_AI_CONTEXT.md"),"utf8"),/llms-integrations/);
      assert.equal(run([framework,name,"--output",dir]).status,2,"Must not overwrite existing projects");
    }
    for(const invalid of ["../../escape","UPPER","a","ends-"]){
      assert.equal(run(["react",invalid,"--output",dir]).status,2,"Rejected unsafe project name: "+invalid);
    }
  }finally{await rm(dir,{recursive:true,force:true});}
});
