// Rewrite only known legacy Markdown destinations in the parsed syntax tree.
// Code blocks and inline code are not link nodes and remain unchanged.
import { readFileSync } from 'node:fs';
const manifest = JSON.parse(readFileSync(new URL('../../docs/public/manifest.json', import.meta.url), 'utf8'));
const routes = new Map(manifest.pages.filter(p => /^docs\/[A-Z-]+\.md$/.test(p.source)).map(p => [p.source.slice(5), p.target.replace(/\.md$/, '/')]));
export function rewriteLegacyUrl(url, base = '/ruxbase/') {
  if (typeof url !== 'string') return url;
  const match = /^(?:\.\/)?([A-Z-]+\.md)(#[^\s]*)?$/.exec(url);
  if (!match) return url;
  if (!routes.has(match[1])) throw new Error('Unmapped legacy document: ' + match[1]);
  return base + routes.get(match[1]) + (match[2] || '');
}
export default function legacyLinks({base = '/ruxbase/'} = {}) {
  return (tree) => {
    function walk(node) {
      if (node.type === 'link' || node.type === 'definition') node.url = rewriteLegacyUrl(node.url, base);
      if (Array.isArray(node.children)) node.children.forEach(walk);
    }
    walk(tree);
  };
}
