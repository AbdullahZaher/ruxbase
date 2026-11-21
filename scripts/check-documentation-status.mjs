#!/usr/bin/env node
// Reconcile active documentation labels without rewriting Git history or legacy paths.
import { readFileSync, readdirSync } from "node:fs";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const markdownFiles = [
  "AGENTS.md",
  "README.md",
  "MASTER-ROADMAP.md",
  "MASTER-CHECKLIST.md",
  "website/AGENTS.md",
  "website/README.md",
];
function collectMarkdown(dir) {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = join(dir, entry.name);
    if (entry.isDirectory()) collectMarkdown(full);
    else if (entry.isFile() && /\.mdx?$/i.test(entry.name)) {
      markdownFiles.push(relative(root, full));
    }
  }
}
collectMarkdown(join(root, "docs"));

const errors = [];
for (const name of markdownFiles) {
  const content = readFileSync(join(root, name), "utf8");
  const lines = content.split(/\r?\n/);
  for (let index = 0; index < lines.length; index++) {
    if (/\bphase[ _-]*0?3\b|\bphase[ _-]*iii\b/i.test(lines[index])) {
      errors.push(`${name}:${index + 1}: retired phase label in Markdown`);
    }
    if (/the documentation site is being established/i.test(lines[index])) {
      errors.push(`${name}:${index + 1}: outdated site-publication status`);
    }
  }
}

const status = readFileSync(join(root, "docs/architecture/developer-experience-status.md"), "utf8");
for (const marker of [
  "Engineering complete and merged",
  "formal milestone closure pending",
  "PR #32",
  "PR #35",
  "5157c50a291617a260a9c4cd203f7156adef04a3",
  "External-model AI evaluation NOT RUN",
  "Wave 04 remains OPEN",
]) {
  if (!status.includes(marker)) errors.push(`developer-experience-status.md: missing status evidence ${marker}`);
}

if (errors.length > 0) {
  console.error(errors.join("\n"));
  process.exitCode = 1;
} else {
  console.log(`Documentation status OK: ${markdownFiles.length} Markdown files checked; canonical evidence present.`);
}
