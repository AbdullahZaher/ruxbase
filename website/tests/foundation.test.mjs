import test from 'node:test';
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { validatePng, validatePage, assertWithin } from '../scripts/prepare.mjs';

test('PNG provenance accepts matching header and blob; rejects tampering', () => {
  const data = Buffer.alloc(33);
  Buffer.from('89504e470d0a1a0a', 'hex').copy(data);
  data.writeUInt32BE(13, 8);
  data.write('IHDR', 12);
  data.writeUInt32BE(24, 16);
  data.writeUInt32BE(32, 20);
  const spec = { file:'fixture.png', width:24, height:32, bytes:data.length,
    gitBlob:createHash('sha1').update('blob ' + data.length + '\0').update(data).digest('hex') };
  assert.doesNotThrow(() => validatePng(data, spec));
  assert.throws(() => validatePng(data, { ...spec, width:25 }));
  const changed = Buffer.from(data);
  changed[32] = 1;
  assert.throws(() => validatePng(changed, spec), /changed/);
  assert.throws(() => validatePng(Buffer.alloc(2), spec), /Invalid PNG/);
});

test('public manifest only permits approved Markdown paths', () => {
  assert.doesNotThrow(() => validatePage({source:'docs/public/index.md',target:'index.md'}));
  for (const source of ['docs/RLS.md','docs/public/../RLS.md','docs/public/a//b.md','/tmp/index.md']) {
    assert.throws(() => validatePage({source,target:'index.md'}));
  }
  for (const target of ['../index.md','/index.md','a//b.md','index.html']) {
    assert.throws(() => validatePage({source:'docs/public/index.md',target}));
  }
});

test('containment rejects parent and sibling paths', () => {
  assert.doesNotThrow(() => assertWithin('/a/docs','/a/docs/index.md'));
  for (const path of ['/a/docs','/a/docs2/index.md','/a/index.md']) {
    assert.throws(() => assertWithin('/a/docs',path));
  }
});
