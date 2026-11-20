#!/usr/bin/env node
// Deterministic Ruxbase starter generator; safe for human and AI-agent usage.
// Never generates secrets, runs a shell, modifies the backend or overwrites files.
import { cp, mkdir, readFile, readdir, stat, lstat, writeFile } from "node:fs/promises";
import { resolve, join, basename } from "node:path";
import { fileURLToPath } from "node:url";

const root=fileURLToPath(new URL("../",import.meta.url));
const argv=process.argv.slice(2);
if(argv.includes("--help") || argv.includes("-h")){
  console.log("Usage: node scripts/create-ruxbase-app.mjs <react|nextjs> <project-name> [--output <parent-directory>]");
  process.exit(0);
}
const [framework,name,...rest]=argv;
const fail=(s)=>{console.error("Create Ruxbase app:",s);process.exit(2);};
if(!["react","nextjs"].includes(framework))fail("Choose react or nextjs.");
if(!/^[a-z][a-z0-9-]{1,49}$/.test(name)||name.endsWith("-"))fail("Use a lowercase npm-safe name (2–50 characters).");
if(rest.length && (rest.length!==2||rest[0]!=="--output"||!rest[1]))fail("Use --output <parent-directory> only.");
const parent=resolve(rest[1]||process.cwd());
const dest=join(parent,name);
const source=join(root,"starters",framework);
try{await stat(dest);fail("Target already exists: "+dest);}catch(e){if(e.code!=="ENOENT")throw e;}
await mkdir(parent,{recursive:true});
const entries=await readdir(source);
for(const item of entries){
 if(["node_modules",".next","dist","compose.yaml","package-lock.json"].includes(item))continue;
 const from=join(source,item),to=join(dest,item);
 const details=await lstat(from);
 if(details.isSymbolicLink())fail("Template symlink forbidden.");
 await cp(from,to,{recursive:true,errorOnExist:true,force:false});
}
const pkgFile=join(dest,"package.json");
const pkg=JSON.parse(await readFile(pkgFile,"utf8"));
pkg.name=name;
await writeFile(pkgFile,JSON.stringify(pkg,null,2)+"\n");
await writeFile(join(dest,".env.example"),
  "# Ruxbase must already be running on this trusted local origin.\nRUXBASE_INTERNAL_URL=http://127.0.0.1:8090\n");
await writeFile(join(dest,"RUXBASE_AI_CONTEXT.md"),
  "# Ruxbase generated app\n\nFramework: "+framework+"\n"+
  "Ruxbase origin: http://127.0.0.1:8090 (development only).\n"+
  "Set up the posts collection and ordinary users first. Do not generate admin secrets.\n"+
  "Run npm install then npm run dev. Verify same-origin /api/health.\n"+
  "Sources: https://abdullahzaher.github.io/ruxbase/llms-integrations.txt\n"+
  "Server-side sessions: https://abdullahzaher.github.io/ruxbase/integrations/server-side/\n");
console.log("Created "+framework+" starter at "+dest);
console.log("Start Ruxbase on http://127.0.0.1:8090, then: cd "+dest+" && npm install && npm run dev");
