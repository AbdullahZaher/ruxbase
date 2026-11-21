import test from 'node:test';
import assert from 'node:assert/strict';
import legacyLinks, { rewriteLegacyUrl } from '../scripts/legacy-links.mjs';
test('legacy links retain anchors and honor hosting base', () => {
  assert.equal(rewriteLegacyUrl('OPERATIONS.md#health-checks'), '/ruxbase/operations/overview/#health-checks');
  assert.equal(rewriteLegacyUrl('./RLS.md', '/'), '/authorization/row-level-security/');
  assert.equal(rewriteLegacyUrl('https://example.org/RLS.md'), 'https://example.org/RLS.md');
  assert.throws(() => rewriteLegacyUrl('INTERNAL.md'), /Unmapped/);
});
test('AST rewrite preserves code nodes and external links', () => {
  const tree = {type:'root',children:[{type:'link',url:'DEV.md',children:[]},{type:'code',value:'[example](DEV.md)'},{type:'definition',url:'MCP.md'}]};
  legacyLinks()(tree);
  assert.equal(tree.children[0].url,'/ruxbase/getting-started/development/');
  assert.equal(tree.children[1].value,'[example](DEV.md)');
  assert.equal(tree.children[2].url,'/ruxbase/ai/mcp/');
});
