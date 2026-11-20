#!/usr/bin/env node
// Safe, deterministic and machine-readable starter creation. No shell,
// network, package installation, credentials, or backend mutation.
import { cp, copyFile, lstat, mkdir, readdir, readFile, realpath, rm, writeFile } from "node:fs/promises";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("../", import.meta.url));
const choices = ["react", "nextjs"];
const help = `Ruxbase project generator (development templates)
Usage:
  node scripts/create-ruxbase-app.mjs react my-app [--output ./projects]
  node scripts/create-ruxbase-app.mjs --framework nextjs --name my-app [--output ./projects]
  node scripts/create-ruxbase-app.mjs --list | --help
Options:
  --framework react|nextjs   Choose starter (also accepted positionally)
  --name NAME                Lowercase npm-safe name (2–50 chars)
  --output DIRECTORY         Existing or newly created parent (default: cwd)
  --dry-run                  Validate options and destination without writing
  --json                     One machine-readable result on stdout
  --list                     Print available templates
  --help                     Show usage
Command never installs dependencies or writes secrets. After generation:
  cd my-app && npm ci && npm run dev
Start Ruxbase first using the Docker Compose Quickstart.
`;

const input = process.argv.slice(2);
let opts = { framework: "", name: "", output: process.cwd(), json: false, dryRun: false, help: false, list: false };
let positional = [];
let parseError = "";
const seen = new Set();
for (let i = 0; i < input.length && !parseError; i++) {
  const arg = input[i];
  if (["--help", "-h", "--list", "--json", "--dry-run"].includes(arg)) {
    const key = { "--help": "help", "-h": "help", "--list": "list", "--json": "json", "--dry-run": "dryRun" }[arg];
    if (seen.has(key)) { parseError = "Duplicate option " + arg; break; }
    seen.add(key); opts[key] = true;
  } else if (["--framework", "--name", "--output"].includes(arg)) {
    if (seen.has(arg) || !input[i+1] || input[i+1].startsWith("--")) { parseError = "Missing or repeated value for " + arg; break; }
    seen.add(arg); opts[arg.slice(2)] = input[++i];
  } else if (arg.startsWith("-")) parseError = "Unknown option: " + arg;
  else positional.push(arg);
}
function fail(code, message) {
  if (opts.json) console.error(JSON.stringify({ ok: false, code, message }));
  else console.error("Ruxbase CLI [" + code + "]: " + message + "\nRun --help for usage.");
  process.exitCode = 2;
}
function out(data, text) { console.log(opts.json ? JSON.stringify(data) : text); }
async function safeCopy(from, to) {
  const meta = await lstat(from);
  if (meta.isSymbolicLink()) throw Error("Template contains a forbidden symlink");
  if (meta.isDirectory()) {
    await mkdir(to);
    for (const name of await readdir(from)) await safeCopy(join(from, name), join(to, name));
  } else if (meta.isFile()) await copyFile(from, to);
  else throw Error("Template contains an unsupported filesystem entry");
}
async function main() {
  if (parseError) return fail("USAGE", parseError);
  if (opts.help) { out({ ok: true, help }, help); return; }
  if (opts.list) { out({ ok: true, templates: choices }, "Available starters: " + choices.join(", ")); return; }
  if (positional.length > 2 || opts.framework && positional[0] || opts.name && positional[1]) {
    return fail("USAGE", "Use positional framework/name OR their named flags, not both.");
  }
  const framework = opts.framework || positional[0], name = opts.name || positional[1];
  if (!choices.includes(framework)) return fail("FRAMEWORK", "Select react or nextjs.");
  if (typeof name !== "string" || !/^[a-z][a-z0-9-]{1,49}$/.test(name) || name.endsWith("-"))
    return fail("NAME", "Project name must be 2–50 lowercase letters, numbers or hyphens; start with a letter.");
  const parent = resolve(opts.output), target = join(parent, name);
  try { await lstat(target); return fail("EXISTS", "Destination already exists: " + target); }
  catch (e) { if (e.code !== "ENOENT") return fail("PATH", "Cannot check target destination: " + e.code); }
  if (opts.dryRun) {
    out({ok:true,dryRun:true,framework,name,target,install:"npm ci",launch:"npm run dev"},
        "Valid " + framework + " project: " + target + " (dry-run; no files created)");
    return;
  }
  let created = false;
  try {
    await mkdir(parent, {recursive:true});
    // mkdir is the exclusive create lock. Concurrent calls cannot overwrite
    // an existing project, even if a racing process created an empty folder.
    await mkdir(target);
    created = true;
    const template = join(root, "starters", framework);
    for (const entry of await readdir(template)) {
      if (["node_modules", ".next", "dist", "compose.yaml"].includes(entry)) continue;
      await safeCopy(join(template,entry), join(target,entry));
    }
    const pkgPath = join(target,"package.json");
    const pkg = JSON.parse(await readFile(pkgPath,"utf8"));
    pkg.name = name;
    await writeFile(pkgPath,JSON.stringify(pkg,null,2)+"\n");
    const lockPath = join(target,"package-lock.json");
    const lock = JSON.parse(await readFile(lockPath,"utf8"));
    if (lock.lockfileVersion!==3 || !lock.packages?.[""] || !lock.packages?.["node_modules/pocketbase"]) throw Error("LOCK_INVALID");
    lock.name = name;
    lock.packages[""].name = name;
    await writeFile(lockPath,JSON.stringify(lock,null,2)+"\n");
    await writeFile(join(target,".env.example"),"# Backend URL is server-only and must be trusted.\nRUXBASE_INTERNAL_URL=http://127.0.0.1:8090\n");
    await writeFile(join(target,"RUXBASE_AI_CONTEXT.md"),
      "# Ruxbase generated application\n\nFramework: "+framework+"\n"+
      "Use npm ci (locked, no npm install) then npm run build and npm run dev.\n"+
      "Ruxbase backend: http://127.0.0.1:8090; run the root Docker Compose Quickstart.\n"+
      "Create the example posts collection and ordinary test users. Never put admin or database credentials into browser code.\n"+
      "See https://abdullahzaher.github.io/ruxbase/llms-integrations.txt and https://abdullahzaher.github.io/ruxbase/integrations/server-side/\n"+
      "Next.js protected session API requires a trusted RUXBASE_APP_ORIGIN, strict Origin validation and HTTPS in production.\n");
    out({ok:true,framework,name,target,install:"npm ci",launch:"npm run dev"},
        "Created "+framework+" starter at "+target+"\nNext: cd "+target+" && npm ci && npm run dev");
  } catch (e) {
    if (created) await rm(target,{recursive:true,force:true}).catch(()=>{});
    fail(e.code==="EEXIST"?"EXISTS":"GENERATION", "Project generation failed safely: " + (e.code || e.message));
  }
}
await main();
