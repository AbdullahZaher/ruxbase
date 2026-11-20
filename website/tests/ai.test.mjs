import test from 'node:test';
import assert from 'node:assert/strict';
import { splitPage, rewriteContextLinks, markdownDocument } from '../scripts/export-ai.mjs';
import { addDiscovery } from '../scripts/prepare.mjs';
import { absolute,routeFor,markdownFor } from '../scripts/site-config.mjs';

test('discovery is generated once on each staged page',()=>{
  const entry={target:'getting-started/development.md'};
  const source='---\ntitle: Development\ndescription: Local development\n---\n\nBody\n';
  const result=addDiscovery(entry,source);
  assert.match(result,/text\/markdown/);
  assert.match(result,/View as Markdown/);
  assert.equal(splitPage(result).title,'Development');
  assert.throws(()=>addDiscovery(entry,result),/Reserved/);
  assert.equal(routeFor('index.md'),'/ruxbase/');
});
test('AI link rewriting preserves code and resolves guide references',()=>{
  const text='[Ops](OPERATIONS.md#health-checks)\n\n[local](#section)\n\n[home](/ruxbase/)\n\n```md\n[code](RLS.md)\n```\n';
  const canonical=absolute(routeFor('getting-started/development.md'));
  const out=rewriteContextLinks(text,canonical);
  assert.ok(out.includes(absolute('/ruxbase/operations/overview/#health-checks')));
  assert.ok(out.includes(canonical+'#section'));
  assert.ok(out.includes('[code](RLS.md)'));
  assert.throws(()=>rewriteContextLinks('[x](INTERNAL.md)',canonical),/Unmapped/);
  assert.throws(()=>rewriteContextLinks('[x](../unmapped)',canonical),/Unresolved/);
  assert.throws(()=>rewriteContextLinks('[x](javascript:alert)',canonical),/Unresolved/);
});
test('exports include exact source version and do not claim release approval',()=>{
  const page={title:'Local development',canonical:absolute('/ruxbase/getting-started/development/')};
  const out=markdownDocument(page,{sourceCommit:'a'.repeat(40)},'Body\n');
  assert.ok(out.includes('Development documentation'));
  assert.ok(out.includes('a'.repeat(40)));
  assert.ok(out.endsWith('Body\n'));
  assert.throws(()=>splitPage('no metadata'),/Missing/);
  assert.throws(()=>splitPage('---\ntitle: T\ndescription: D\ndraft: true\n---\nA'),/Invalid/);
});
