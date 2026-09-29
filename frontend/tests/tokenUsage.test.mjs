import test from "node:test";
import assert from "node:assert/strict";
import { totalTokenUsage, formatTokens } from "../src/tokenAccounting.ts";

test("conversation usage includes failed and selector runs without charging cache twice", () => {
  const usage = totalTokenUsage([
    { inputTokens: 10000, outputTokens: 2000, cachedTokens: 6000, usageEstimated: false },
    { inputTokens: 400, outputTokens: 50, cachedTokens: 0, usageEstimated: true },
  ]);
  assert.deepEqual(usage, { input: 10400, output: 2050, cached: 6000, estimated: true });
  assert.equal(usage.input + usage.output, 12450);
  assert.deepEqual(totalTokenUsage([]), { input: 0, output: 0, cached: 0, estimated: false });
});

test("compact counts retain a useful scale", () => {
  assert.equal(formatTokens(0), "0");
  assert.equal(formatTokens(12450), "12.4k");
  assert.equal(formatTokens(1250000), "1.3M");
});
