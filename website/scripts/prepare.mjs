import { createHash } from 'node:crypto';
import { readFile, mkdir, writeFile, rm, realpath } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { resolve, join, relative, isAbsolute, sep } from 'node:path';

const root = fileURLToPath(new URL('../../', import.meta.url));
const site = join(root, 'website');
const brand = join(root, 'assets/brand');

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
  if (!/^docs\/public\/[a-z0-9][a-z0-9/-]*\.md$/.test(entry.source) ||
      !/^[a-z0-9][a-z0-9/-]*\.md$/.test(entry.target) ||
      entry.source.includes('//') || entry.target.includes('//')) {
    throw new Error('Invalid public content mapping');
  }
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
    assertWithin(await realpath(join(root, 'docs/public')), source);
    const content = await readFile(source, 'utf8');
    if (!content.startsWith('---\n')) throw new Error('Missing page frontmatter');
    pages.set(entry.target, content);
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
  console.log('Validated ' + assets.size + ' originals and staged ' + pages.size + ' public pages.');
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  await prepare();
}
