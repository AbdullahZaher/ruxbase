import { readFile, stat } from 'node:fs/promises';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';

const site = fileURLToPath(new URL('../', import.meta.url));
const manifest = JSON.parse(await readFile(new URL('../../docs/public/manifest.json', import.meta.url), 'utf8'));
const routes = new Map(manifest.pages.map(p => ['/ruxbase/' + p.target.replace(/index\.md$/, '').replace(/\.md$/, '/'), p]));
const html = new Map();
for (const [route] of routes) {
  const path = join(site, 'dist', route.slice('/ruxbase/'.length), 'index.html');
  const content = await readFile(path, 'utf8');
  if (!/<title>[^<]+<\/title>/i.test(content) || !/<h1\b[^>]*>/i.test(content)) throw new Error('Missing title or H1: ' + route);
  html.set(route, content);
}
for (const [route, content] of html) {
  for (const match of content.matchAll(/\bhref="(\/ruxbase\/[^"?#]*)(?:#([^"]*))?"/g)) {
    let [, target, fragment] = match;
    target = target.endsWith('/') ? target : target + '/';
    if (!html.has(target)) {
      const asset = match[1].slice('/ruxbase/'.length);
      if (!asset || asset.split('/').includes('..')) throw new Error('Invalid internal href: ' + match[0]);
      await stat(join(site, 'dist', asset)).catch(() => { throw new Error('Missing internal href: ' + route + ' → ' + match[0]); });
      continue;
    }
    if (fragment && !html.get(target).includes('id="' + decodeURIComponent(fragment) + '"')) {
      throw new Error('Missing guide anchor: ' + route + ' → ' + match[0]);
    }
  }
  if (/href="(?:\.\/)?[A-Z-]+\.md(?:#|")/.test(content)) throw new Error('Unrewritten guide link: ' + route);
}
if(process.env.DOCS_SCALAR_PREVIEW==='1'){
 const viewer=await readFile(join(site,'dist/api-viewer/index.html'),'utf8');
 if(!viewer.includes('content="noindex"')||!viewer.includes("connect-src 'self'"))throw new Error('Viewer indexing or resource policy changed');
 for(const asset of ['api-viewer/vendor/standalone.js','api-viewer/provenance.json'])await stat(join(site,'dist',asset));
}else{
 await import('./check-publication-scope.mjs');
}
for(const asset of ['favicon-16.png','favicon-32.png','pagefind/pagefind.js','openapi.json'])await stat(join(site,'dist',asset));
console.log('Verified static output for ' + routes.size + ' pages, mapped anchors, favicon and Pagefind entrypoint.');

// Ensure repository entry points link only to files actually emitted by the public profile.
const origin='https://abdullahzaher.github.io/ruxbase/';
for (const name of ['README.md','AGENTS.md']) {
  const source=await readFile(join(site,'..',name),'utf8');
  for(const required of ['', 'llms.txt', 'llms-full.txt', 'openapi.json', 'version.json']) {
    if(!source.includes(origin+required))throw new Error('Missing documentation discovery link in '+name+': '+required);
  }
  const links=[...source.matchAll(/https:\/\/abdullahzaher\.github\.io\/ruxbase\/[^\s"<>\])`]+|https:\/\/abdullahzaher\.github\.io\/ruxbase\//g)].map(m=>m[0]);
  for(const link of new Set(links)) {
    const path=new URL(link).pathname.slice('/ruxbase/'.length);
    if(path.split('/').includes('..'))throw new Error('Invalid discovery link: '+link);
    const target=join(site,'dist',path.endsWith('/')||!path?path+'index.html':path);
    const st=await stat(target).catch(()=>{throw new Error('Missing discovery target: '+name+' → '+link);});
    if(!st.isFile())throw new Error('Discovery target is not a file: '+link);
  }
}
console.log('Verified README and AGENTS public discovery links against built output.');
