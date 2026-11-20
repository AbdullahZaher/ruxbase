import { readFileSync } from 'node:fs';
import { createHash } from 'node:crypto';
const raw = readFileSync('package-lock.json', 'utf8');
const lock = JSON.parse(raw);
const pkg = JSON.parse(readFileSync('package.json','utf8'));
if (lock.lockfileVersion !== 3 || !lock.packages || !lock.packages['']) throw new Error('Unexpected lockfile format');
for (const [name, version] of Object.entries(pkg.dependencies)) {
  if (lock.packages[''].dependencies?.[name] !== version || lock.packages['node_modules/' + name]?.version !== version) throw new Error('Direct dependency mismatch: ' + name);
}
for (const [name, item] of Object.entries(lock.packages)) {
  if (!name) continue;
  if (!item.resolved || !/^https:\/\/registry\.npmjs\.org\//.test(item.resolved) || !/^sha(512|1)-[A-Za-z0-9+/=]+$/.test(item.integrity || '')) throw new Error('Unexpected package origin or integrity: ' + name);
}
console.log('LOCK-SHA256 ' + createHash('sha256').update(raw).digest('hex'));
// This is public package metadata only. Bound output and never print environment values.
if (raw.length > 1000000) throw new Error('Lockfile exceeds review bound');
for (const [i, line] of raw.trimEnd().split('\n').entries()) console.log('LOCK-LINE ' + i + ' ' + line);
console.log('LOCK-END ' + raw.trimEnd().split('\n').length);
