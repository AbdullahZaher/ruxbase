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
for (const asset of ['favicon.svg', 'pagefind/pagefind.js']) await stat(join(site, 'dist', asset));
console.log('Verified static output for ' + routes.size + ' pages, mapped anchors, favicon and Pagefind entrypoint.');
