import { createHash } from 'node:crypto';
import { readFile, mkdir, writeFile, rm, realpath } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { resolve, join, relative, isAbsolute, sep } from 'node:path';
import { SITE_BASE, absolute, markdownFor, routeFor } from './site-config.mjs';

const root = fileURLToPath(new URL('../../', import.meta.url));
const site = join(root, 'website');
const brand = join(root, 'assets/brand');
const legacy = new Set(["DEV.md","COMPATIBILITY.md","CLUSTER.md","RLS.md","REALTIME.md","SEARCH.md","VECTOR.md","MCP.md","OPERATIONS.md","MIGRATING.md"]);

export function assertWithin(parent, child) {
  const rel = relative(parent, child);
  if (!rel || rel === '..' || rel.startsWith('..' + sep) || isAbsolute(rel)) {
    throw new Error('Path outside approved directory: ' + child);
  }
}

export function validatePng(data, spec) {
  if (data.length < 33 || data.subarray(0, 8).toString('hex') !== '89504e470d0a1a0a' ||
      data.readUInt32BE(8) !== 13 || data.toString('ascii', 12, 16) !== 'IHDR') {
    throw new Error('Invalid PNG: ' + spec.file);
  }
  const blob = createHash('sha1').update('blob ' + data.length + '\0').update(data).digest('hex');
  if (data.readUInt32BE(16) !== spec.width || data.readUInt32BE(20) !== spec.height ||
      data.length !== spec.bytes || blob !== spec.gitBlob) {
    throw new Error('Brand original changed: ' + spec.file);
  }
}

export function validatePage(entry) {
  if (!( /^docs\/public\/[a-z0-9][a-z0-9/-]*\.md$/.test(entry.source) || legacy.has(entry.source.replace(/^docs\//, '')) && entry.source.startsWith('docs/') ) ||
      !/^[a-z0-9][a-z0-9/-]*\.md$/.test(entry.target) ||
      entry.source.includes('//') || entry.target.includes('//')) {
    throw new Error('Invalid public content mapping');
  }
}

export function stagePage(entry, raw) {
  if (entry.source.startsWith('docs/public/')) {
    if (!raw.startsWith('---\n')) throw new Error('Missing page frontmatter');
    return raw;
  }
  if (typeof entry.title !== 'string' || !entry.title.trim() || typeof entry.description !== 'string' || !entry.description.trim()) throw new Error('Missing legacy metadata');
  const match = raw.match(/^# [^\r\n]+\r?\n/);
  if (!match) throw new Error('Missing legacy document title: ' + entry.source);
  return '---\ntitle: ' + JSON.stringify(entry.title) + '\ndescription: ' + JSON.stringify(entry.description) +
    '\n---\n\n> Development documentation. Review compatibility and operational limitations; this is not a release certification.\n\n' + raw.slice(match[0].length).trimStart();
}

export function addDiscovery(entry, staged) {
  const match = staged.match(/^---\r?\n([\s\S]*?)\r?\n---\r?\n/);
  if (!match || /^(?:head|draft):/m.test(match[1])) throw new Error('Reserved generated frontmatter key: ' + entry.target);
  const tags = [
    { tag:'link', attrs:{ rel:'alternate', type:'text/markdown', href:absolute(markdownFor(entry.target)) } },
    { tag:'link', attrs:{ rel:'describedby', href:absolute(SITE_BASE + 'llms.txt') } },
  ];
  return '---\n' + match[1] + '\nhead: ' + JSON.stringify(tags) + '\n---\n' +
    staged.slice(match[0].length).trimEnd() +
    '\n\n[View as Markdown](' + markdownFor(entry.target) + ') · [AI documentation index](' + SITE_BASE + 'llms.txt)\n';
}

export async function prepare() {
  const bm = JSON.parse(await readFile(join(brand, 'manifest.json'), 'utf8'));
  const pm = JSON.parse(await readFile(join(root, 'docs/public/manifest.json'), 'utf8'));
  if (bm.schemaVersion !== 1 || pm.schemaVersion !== 1 ||
      !Array.isArray(bm.originals) || !Array.isArray(pm.pages) || !pm.pages.length) {
    throw new Error('Unsupported or empty manifest');
  }
  const expected = new Set(['ruxbase.png', 'ruxbase_logo.png', 'ruxbase_name.png']);
  const assets = new Map();
  for (const spec of bm.originals) {
    if (!expected.delete(spec.file)) throw new Error('Unexpected or duplicate original');
    const path = await realpath(join(brand, spec.file));
    assertWithin(await realpath(brand), path);
    const data = await readFile(path);
    validatePng(data, spec);
    assets.set(spec.file, data);
  }
  if (expected.size) throw new Error('Missing brand original');
  const pages = new Map();
  for (const entry of pm.pages) {
    validatePage(entry);
    if (pages.has(entry.target)) throw new Error('Duplicate documentation target');
    const source = await realpath(resolve(root, entry.source));
    assertWithin(await realpath(join(root, entry.source.startsWith('docs/public/') ? 'docs/public' : 'docs')), source);
    // Legacy root documents must resolve to the named file, not another source.
    if (!entry.source.startsWith('docs/public/') && source !== resolve(root, entry.source)) throw new Error('Legacy source symlink');
    const content = await readFile(source, 'utf8');
    pages.set(entry.target, addDiscovery(entry, stagePage(entry, content)));
  }
  if (!pages.has('index.md')) throw new Error('Missing homepage');
  // Only generated directories are replaced, regardless of manifest contents.
  const contentDest = join(site, 'src/content/docs');
  const brandDest = join(site, 'src/assets/brand');
  for (const dir of [contentDest, brandDest]) {
    await rm(dir, { recursive: true, force: true });
    await mkdir(dir, { recursive: true });
  }
  for (const [name, body] of pages) {
    const dest = join(contentDest, name);
    assertWithin(contentDest, dest);
    await mkdir(resolve(dest, '..'), { recursive: true });
    await writeFile(dest, body);
  }
  // Only the symbol is consumed by this candidate; do not copy large unused PNGs.
  await writeFile(join(brandDest, 'ruxbase_logo.png'), assets.get('ruxbase_logo.png'));
  await writeFile(join(brandDest, 'tokens.css'), await readFile(join(brand, 'tokens.css')));
  for (const name of ['symbol-on-light.svg', 'symbol-on-dark.svg']) {
    await writeFile(join(brandDest, name), await readFile(join(brand, 'svg-candidates', name)));
  }
  const publicDir = join(site, 'public');
  await mkdir(publicDir, { recursive: true });
  await writeFile(join(publicDir, 'favicon.svg'), await readFile(join(brand, 'svg-candidates', 'favicon.svg')));
  console.log('Validated ' + assets.size + ' originals and staged ' + pages.size + ' public pages.');
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  await prepare();
}
