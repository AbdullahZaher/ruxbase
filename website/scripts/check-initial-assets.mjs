// Static, deterministic budget independent of Lighthouse's runner timing.
// All approved HTML routes are checked; do not count gzip or cached bytes as a pass.
import { readFile, stat } from 'node:fs/promises';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { fromHtml } from 'hast-util-from-html';
import { SITE_BASE, routeFor } from './site-config.mjs';

const root=fileURLToPath(new URL('../dist/',import.meta.url));
const manifest=JSON.parse(await readFile(new URL('../../docs/public/manifest.json',import.meta.url),'utf8'));
const assert=(v,m)=>{if(!v)throw Error(m);};
let maxCSS=0;
for(const p of manifest.pages){
 const route=routeFor(p.target);
 const tree=fromHtml(await readFile(join(root,route.slice(SITE_BASE.length),'index.html'),'utf8'));
 let css=0; const seen=new Set();
 async function walk(n){
  if(n.type==='element'){
   const props=n.properties||{};
   const rel=Array.isArray(props.rel)?props.rel:[props.rel];
   const href=props.href||props.src;
   if(n.tagName==='script'||n.tagName==='link'&&rel.some(x=>['stylesheet','preload','modulepreload','prefetch'].includes(x))){
    if(typeof href==='string'){
     assert(!/\/pagefind\/|\/ui-core\.|\/Search\.astro/i.test(href),'Eager search asset: '+route+' '+href);
     if(n.tagName==='link'&&rel.includes('stylesheet')&&props.media!=='print'&&href.startsWith(SITE_BASE)){
      const local=href.slice(SITE_BASE.length);
      if(!seen.has(local)){seen.add(local);css+=(await stat(join(root,local))).size;}
     }
    }
   }
   if(n.tagName==='style'&&props.media!=='print')css+=Buffer.byteLength((n.children||[]).map(c=>c.value||'').join(''));
  }
  for(const child of n.children||[])await walk(child);
 }
 await walk(tree);
 // Baseline: 65,181 bytes common + 18,292 bytes code highlighting (83,473 total on code pages).
 assert(css<=70*1024,'Initial non-print CSS budget exceeded: '+route+' '+css+' bytes');
 maxCSS=Math.max(maxCSS,css);
}
for(const name of ['pagefind.js','pagefind-ui.js','pagefind-ui.css']){
 const st=await stat(join(root,'pagefind',name));assert(st.isFile()&&st.size>0,'Missing deferred search asset: '+name);
}
console.log('Initial asset budget PASS: '+manifest.pages.length+' routes; largest non-print CSS '+maxCSS+' bytes (limit 71680); no eager Pagefind asset tags.');
