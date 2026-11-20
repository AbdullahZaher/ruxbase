import { readFile, stat, readdir } from 'node:fs/promises';
import { createHash } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { fromHtml } from 'hast-util-from-html';
import { SITE_BASE, SITE_ORIGIN, absolute, routeFor, markdownFor } from './site-config.mjs';
import { splitPage, rewriteContextLinks, markdownDocument } from './export-ai.mjs';

const root = fileURLToPath(new URL('../../',import.meta.url));
const dist = join(root,'website/dist');
const read = (name) => readFile(join(dist,name),'utf8');
const sourceManifest = JSON.parse(await readFile(join(root,'docs/public/manifest.json'),'utf8'));
const output = JSON.parse(await read('documentation-manifest.json'));
const version = JSON.parse(await read('version.json'));
const index = await read('llms.txt');
const full = await read('llms-full.txt');
const git = (...args) => execFileSync('git',args,{cwd:root,encoding:'utf8'}).trim();
const assert = (ok,msg) => { if (!ok) throw new Error(msg); };
assert(version.sourceCommit===git('rev-parse','HEAD') && version.sourceTree===git('rev-parse','HEAD^{tree}'),'Incorrect build identity');
assert(version.documentationStatus==='development' && version.releaseVersion===null,'Unapproved release claim');
if (process.env.CI) assert(!version.dirty,'Dirty CI build');
assert(output.sourceCommit===version.sourceCommit && output.sourceTree===version.sourceTree && output.documentationStatus===version.documentationStatus && output.releaseVersion===null && output.dirty===version.dirty && output.pages.length===sourceManifest.pages.length && version.pageCount===output.pages.length,'Manifest/version mismatch');
const titles = new Set(), descriptions = new Set(), expectedRoutes = new Set(), expectedMarkdown = new Set();
const allSitemapFiles = (await readdir(dist)).filter(n=>/^sitemap-\d+\.xml$/.test(n));
assert(allSitemapFiles.length>0,'Missing sitemap');
const sitemap = (await Promise.all(allSitemapFiles.map(read))).join('\n');
function elements(tree) {
  const result=[];
  function walk(n) { if(n.type==='element') result.push(n); for(const child of n.children||[]) walk(child); }
  walk(tree);
  return result;
}
function textOf(node) { return node.type==='text'?node.value:(node.children||[]).map(textOf).join(''); }
const sha=(v)=>createHash('sha256').update(v).digest('hex');
for (const e of sourceManifest.pages) {
  const route=routeFor(e.target);
  const canonical=absolute(route);
  const markdown=absolute(markdownFor(e.target));
  expectedRoutes.add(canonical);
  expectedMarkdown.add(e.target);
  const p=output.pages.find(x=>x.target===e.target && x.source===e.source);
  assert(p && p.canonical===canonical && p.markdown===markdown,'Bad public mapping: '+e.target);
  const md=await read(e.target);
  const staged=splitPage(await readFile(join(root,'website/src/content/docs',e.target),'utf8'));
  assert(p.title===staged.title && p.description===staged.description,'Stale metadata: '+e.target);
  assert(md===markdownDocument(p,version,rewriteContextLinks(staged.body,canonical)) && sha(md)===p.sha256,'Markdown content mismatch: '+e.target);
  assert(index.includes(']('+markdown+')') && full.includes(md),'AI index or bundle missing page: '+e.target);
  const html=await read(route.slice(SITE_BASE.length)+'index.html');
  const nodes=elements(fromHtml(html));
  const title=nodes.find(n=>n.tagName==='title');
  const description=nodes.find(n=>n.tagName==='meta' && n.properties.name==='description');
  const links=nodes.filter(n=>n.tagName==='link');
  const rel=(n,v)=>Array.isArray(n.properties.rel)?n.properties.rel.includes(v):n.properties.rel===v;
  assert(title && textOf(title).trim() && !titles.has(textOf(title)),'Missing/duplicate HTML title: '+route);
  titles.add(textOf(title));
  assert(description?.properties.content===p.description && !descriptions.has(p.description),'Missing/duplicate description: '+route);
  descriptions.add(p.description);
  assert(links.filter(n=>rel(n,'canonical')).length===1 && links.some(n=>rel(n,'canonical')&&n.properties.href===canonical),'Wrong canonical: '+route);
  assert(links.some(n=>rel(n,'alternate')&&n.properties.type==='text/markdown'&&n.properties.href===markdown),'Missing Markdown discovery: '+route);
  assert(links.some(n=>rel(n,'describedby')&&n.properties.href===absolute(SITE_BASE+'llms.txt')),'Missing AI discovery: '+route);
  assert(nodes.some(n=>n.tagName==='main') && nodes.some(n=>n.tagName==='h1'&&textOf(n).includes(p.title)),'No static main heading: '+route);
  assert(nodes.some(n=>n.tagName==='a'&&n.properties.href===markdownFor(e.target)),'Missing visible Markdown link: '+route);
  assert(sitemap.includes('<loc>'+canonical+'</loc>'),'Missing sitemap entry: '+canonical);
}
const sitemapUrls=[...sitemap.matchAll(/<loc>([^<]+)<\/loc>/g)].map(m=>m[1]);
assert(sitemapUrls.every(url=>expectedRoutes.has(url)) && new Set(sitemapUrls).size===expectedRoutes.size,'Unexpected sitemap pages');
assert(output.pages.every(p=>expectedMarkdown.has(p.target)),'Unapproved context source');
for(const match of index.matchAll(/\]\((https?:\/\/[^)]+)\)/g)) {
  const url=new URL(match[1]);
  assert(url.origin===SITE_ORIGIN && url.pathname.startsWith(SITE_BASE),'Unexpected index origin');
  await stat(join(dist,url.pathname.slice(SITE_BASE.length)));
}
console.log('Verified ' + output.pages.length + ' Markdown/HTML pairs, unique metadata, canonical URLs, sitemap, AI discovery and source identity.');
