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
console.log('Validated ' + (Object.keys(lock.packages).length - 1) + ' registry package entries.');
