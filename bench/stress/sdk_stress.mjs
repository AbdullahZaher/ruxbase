// Concurrent PocketBase JS SDK stress & compatibility verifier (Test D).
// Uses the official pocketbase@0.28.1 SDK already installed in tests/sdk/js/node_modules.
//
// Usage:
//   node bench/stress/sdk_stress.mjs --url http://127.0.0.1:18090 --concurrency 25 --duration 10
import { createRequire } from 'node:module';
import { performance } from 'node:perf_hooks';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const require = createRequire(resolve(process.cwd(), 'tests/sdk/js/package.json'));
const PocketBase = require('pocketbase').default || require('pocketbase');

const args = process.argv.slice(2);
const getArg = (name, def) => {
  const idx = args.indexOf(name);
  return idx >= 0 && args[idx + 1] ? args[idx + 1] : def;
};

const baseURL = getArg('--url', 'http://127.0.0.1:18090');
const concurrency = parseInt(getArg('--concurrency', '25'), 10);
const durationSec = parseFloat(getArg('--duration', '10'));
const stateFile = getArg('--state', '');

let tenants = [];
if (stateFile) {
  const st = JSON.parse(readFileSync(stateFile, 'utf8'));
  tenants = st.tenants;
} else {
  for (let i = 0; i < 10; i++) {
    tenants.push({
      tenantId: `tenant_${String(i).padStart(2, '0')}`,
      email: `user_${String(i).padStart(2, '0')}@stress.local`,
      password: 'StressUserPass!2026',
    });
  }
}

function percentile(sorted, p) {
  if (sorted.length === 0) return 0;
  const idx = Math.min(sorted.length - 1, Math.max(0, Math.ceil((sorted.length * p) / 100) - 1));
  return Math.round(sorted[idx] * 1000) / 1000;
}

async function main() {
  // Each worker gets an isolated PocketBase client instance (SSR-safe pattern)
  const clients = [];
  for (let w = 0; w < concurrency; w++) {
    const tu = tenants[w % tenants.length];
    const pb = new PocketBase(baseURL);
    pb.autoCancellation(false);
    if (tu.token) {
      pb.authStore.save(tu.token, { id: tu.userId, tenant_id: tu.tenantId });
    } else {
      await pb.collection('users').authWithPassword(tu.email, tu.password);
    }
    clients.push({ pb, tu });
  }

  const deadline = performance.now() + durationSec * 1000;
  const start = performance.now();
  const latencies = [];
  let ok = 0;
  let errs = 0;
  let authViolations = 0;
  let seq = 0;

  await Promise.all(
    clients.map(async ({ pb, tu }, w) => {
      while (performance.now() < deadline) {
        const t0 = performance.now();
        try {
          const op = seq++ % 5;
          if (op === 0) {
            // Create + verify tenant_id + delete
            const rec = await pb.collection('stress_items').create({
              tenant_id: tu.tenantId,
              owner: tu.userId || '',
              category: 'cat_sdk',
              n: 700000 + seq,
              amount: 12.5,
              published: true,
              title: `SDK Stress #${seq}`,
              body: 'pocketbase js sdk concurrent payload',
              unindexed_tag: 'sdk',
            });
            if (rec.tenant_id !== tu.tenantId) {
              authViolations++;
            }
            await pb.collection('stress_items').delete(rec.id);
          } else {
            // Filtered list read with skipTotal
            const list = await pb.collection('stress_items').getList(1, 20, {
              sort: '-n',
              filter: 'published = true',
              skipTotal: true,
              fields: 'id,tenant_id,n,title',
            });
            for (const item of list.items) {
              if (item.tenant_id !== tu.tenantId) {
                authViolations++;
              }
            }
          }
          latencies.push(performance.now() - t0);
          ok++;
        } catch (e) {
          errs++;
        }
      }
    }),
  );

  const elapsedSec = (performance.now() - start) / 1000;
  latencies.sort((a, b) => a - b);

  console.log(
    JSON.stringify({
      timestamp: new Date().toISOString(),
      testId: 'D-SDK-JS',
      scenario: 'pocketbase-js-sdk-concurrent-mixed',
      sdkVersion: 'pocketbase@0.28.1',
      concurrency,
      durationSeconds: Math.round(elapsedSec * 1000) / 1000,
      totalRequests: ok + errs,
      successRequests: ok,
      errors: errs,
      authViolations,
      achievedRps: Math.round((ok / elapsedSec) * 100) / 100,
      p50ms: percentile(latencies, 50),
      p95ms: percentile(latencies, 95),
      p99ms: percentile(latencies, 99),
      maxMs: latencies.length ? Math.round(latencies[latencies.length - 1] * 1000) / 1000 : 0,
    }),
  );

  if (authViolations > 0) {
    process.exit(1);
  }
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
