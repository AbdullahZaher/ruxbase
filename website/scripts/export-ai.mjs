import { readFile, writeFile, mkdir } from 'node:fs/promises';
import { execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { fileURLToPath } from 'node:url';
import { dirname, join, resolve } from 'node:path';
import { unified } from 'unified';
import remarkParse from 'remark-parse';
import remarkGfm from 'remark-gfm';
import remarkStringify from 'remark-stringify';
import { parse as parseYaml } from 'yaml';
import { rewriteLegacyUrl } from './legacy-links.mjs';
import { SITE_BASE, absolute, routeFor, markdownFor } from './site-config.mjs';
import { validatePage, assertWithin } from './prepare.mjs';
import { TOPICS, introFor, topicPages, makeTopic } from './ai-topics.mjs';

const root = fileURLToPath(new URL('../../', import.meta.url));
const dist = join(root, 'website/dist');
const parser = unified().use(remarkParse).use(remarkGfm).use(remarkStringify, { fences:true, bullet:'-' });
const hash = (str) => createHash('sha256').update(str).digest('hex');

export function splitPage(raw) {
  const m = raw.match(/^---\r?\n([\s\S]*?)\r?\n---\r?\n/);
  if (!m) throw new Error('Missing page metadata');
  const meta = parseYaml(m[1]);
  if (!meta || typeof meta.title !== 'string' || !meta.title.trim() ||
      typeof meta.description !== 'string' || !meta.description.trim() ||
      /[\r\n]/.test(meta.title + meta.description) || meta.draft) throw new Error('Invalid public page metadata');
  return { title:meta.title, description:meta.description, body:raw.slice(m[0].length).trim() };
}
export function rewriteContextLinks(body, canonical) {
  const tree = parser.parse(body);
  function walk(node) {
    if (['link','definition','image'].includes(node.type) && typeof node.url === 'string') {
      const url = rewriteLegacyUrl(node.url, SITE_BASE);
      if (url.startsWith(SITE_BASE)) node.url = absolute(url);
      else if (url.startsWith('#')) node.url = new URL(url, canonical).href;
      else if (/^(https?:|mailto:)/i.test(url)) node.url = url;
      else throw new Error('Unresolved context URL: ' + url);
    }
    if (Array.isArray(node.children)) node.children.forEach(walk);
  }
  walk(tree);
  return parser.stringify(tree);
}
export function markdownDocument(page, identity, body) {
  return '# ' + page.title + '\n\n> Development documentation — not a certified production release.\n> Source commit: ' + identity.sourceCommit + '\n> Canonical page: ' + page.canonical + '\n\n' + body;
}
function git(...args) { return execFileSync('git', args, { cwd:root, encoding:'utf8' }).trim(); }

export async function exportAI() {
  const manifest = JSON.parse(await readFile(join(root, 'docs/public/manifest.json'), 'utf8'));
  const identity = { schemaVersion:1, product:'Ruxbase', documentationStatus:'development', releaseVersion:null,
    sourceCommit:git('rev-parse','HEAD'), sourceTree:git('rev-parse','HEAD^{tree}'), dirty:!!git('status','--porcelain','--untracked-files=no') };
  const pages = [];
  const outputs = new Map();
  for (const e of manifest.pages) {
    validatePage(e);
    const staged = resolve(root, 'website/src/content/docs', e.target);
    assertWithin(join(root,'website/src/content/docs'), staged);
    const {title,description,body} = splitPage(await readFile(staged,'utf8'));
    const page = { title, description, canonical:absolute(routeFor(e.target)), markdown:absolute(markdownFor(e.target)), source:e.source, target:e.target };
    const content = markdownDocument(page, identity, rewriteContextLinks(body,page.canonical));
    if (Buffer.byteLength(content) > 262144) throw new Error('Page exceeds context limit: ' + e.target);
    if (outputs.has(e.target)) throw new Error('Duplicate Markdown output');
    outputs.set(e.target,content);
    pages.push({...page, sha256:hash(content)});
  }
  const intro = introFor(identity);
  const index = intro + '## Documentation\n\n' + pages.map(p => '- [' + p.title + '](' + p.markdown + '): ' + p.description).join('\n') +
    '\n\n## Optional\n\n- [Full context](' + absolute(SITE_BASE+'llms-full.txt') + ')\n- [Build identity](' + absolute(SITE_BASE+'version.json') + ')\n';
  const full = intro + pages.map(p => outputs.get(p.target)).join('\n---\n\n');
  if (Buffer.byteLength(full)>1048576) throw new Error('Full context exceeds 1 MiB; introduce topic bundles before increasing the limit');
  for (const topic of TOPICS) {
    const selected = topicPages(pages, topic);
    outputs.set(topic.name, makeTopic(identity, topic, selected.map(p=>outputs.get(p.target))));
  }
  const topicIndex = TOPICS.map(t=>'- ['+t.title+']('+absolute(SITE_BASE+t.name)+')').join('\n');
  outputs.set('llms.txt',index+'\n## Topic context\n\n'+topicIndex+'\n\n## Machine-readable references (scoped)\n\n'+['versions.json','artifact-manifest.json','openapi.json','api-coverage.json','feature-manifest.json','cli-reference.json','configuration-reference.json'].map(name=>'- ['+name+']('+absolute(SITE_BASE+name)+')').join('\n')+'\n');
  outputs.set('llms-full.txt',full);
  outputs.set('version.json',JSON.stringify({...identity,pageCount:pages.length},null,2)+'\n');
  outputs.set('documentation-manifest.json',JSON.stringify({...identity,pages},null,2)+'\n');
  for (const [name,content] of outputs) {
    const dest = resolve(dist,name);
    assertWithin(dist,dest);
    await mkdir(dirname(dest),{recursive:true});
    await writeFile(dest,content,{flag:'wx'});
  }
  console.log('Generated ' + pages.length + ' Markdown pages, AI indexes and build identity from ' + identity.sourceCommit);
}
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) await exportAI();
