import type { ConversationRun } from "./api";

type UsageRun = Pick<ConversationRun, "inputTokens" | "outputTokens" | "cachedTokens" | "usageEstimated">;

export function totalTokenUsage(runs: UsageRun[]) {
  return runs.reduce((sum, run) => ({
    input: sum.input + run.inputTokens,
    output: sum.output + run.outputTokens,
    cached: sum.cached + run.cachedTokens,
    estimated: sum.estimated || run.usageEstimated,
  }), { input: 0, output: 0, cached: 0, estimated: false });
}

export function formatTokens(value: number) {
  if (value < 1000) return String(value);
  const unit = value >= 1000000 ? 1000000 : 1000;
  return `${(value / unit).toFixed(1)}${unit === 1000 ? "k" : "M"}`;
}
