import { test } from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

test("document and example colors adhere to Ruxbase monochrome identity", async () => {
  const theme = await readFile(new URL("../src/styles/theme.css", import.meta.url), "utf8");
  const tokens = JSON.parse(await readFile(new URL("../../assets/brand/brand-tokens.json", import.meta.url), "utf8"));
  assert.equal(tokens.colors.logo.primary.hex, "#000000");
  assert.equal(tokens.colors.logo.inverse.hex, "#FFFFFF");
  assert.match(theme, /--sl-color-bg:\s*#000000/);
  assert.match(theme, /--sl-color-bg-nav:\s*#000000/);
  assert.match(theme, /--sl-color-bg:\s*#ffffff/i);
  assert.doesNotMatch(theme, /#(?:1d4ed8|60a5fa|93c5fd|dbeafe)/i);
});
