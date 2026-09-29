import { useEffect, useRef } from "react";
import type { ConversationRun } from "./api";
import { formatTokens, totalTokenUsage } from "./tokenAccounting";

export default function TokenUsage({ runs }: { runs: ConversationRun[] }) {
  const details = useRef<HTMLDetailsElement>(null);
  useEffect(() => {
    const dismiss = (event: PointerEvent | KeyboardEvent) => {
      if (event instanceof KeyboardEvent ? event.key === "Escape" : !details.current?.contains(event.target as Node)) {
        if (details.current) details.current.open = false;
      }
    };
    document.addEventListener("pointerdown", dismiss);
    document.addEventListener("keydown", dismiss);
    return () => {
      document.removeEventListener("pointerdown", dismiss);
      document.removeEventListener("keydown", dismiss);
    };
  }, []);
  const usage = totalTokenUsage(runs);
  const prefix = usage.estimated ? "≈ " : "";
  return <details ref={details} className="token-usage">
    <summary title="查看 Token 明细" aria-label={`会话 Token 用量：${prefix}${usage.input + usage.output}，输入 ${usage.input}，输出 ${usage.output}`}>
      <span>Tokens <strong>{prefix}{formatTokens(usage.input + usage.output)}</strong><span aria-hidden="true"> ⌄</span></span>
      <small>输入 {formatTokens(usage.input)} · 输出 {formatTokens(usage.output)}</small>
    </summary>
    <div className="token-usage-detail">
      <strong>会话累计用量</strong>
      <dl>
        <div><dt>输入</dt><dd>{usage.input.toLocaleString()}</dd></div>
        <div><dt>输出</dt><dd>{usage.output.toLocaleString()}</dd></div>
        <div><dt>已报告缓存命中</dt><dd>{usage.cached > 0 ? usage.cached.toLocaleString() : "—"}</dd></div>
        <div><dt>总计</dt><dd>{prefix}{(usage.input + usage.output).toLocaleString()}</dd></div>
      </dl>
      <p>包含所有成员、后台选人、压缩和重试。缓存命中属于输入，不重复计入总量。</p>
      {usage.cached === 0 && <p>未收到可识别的缓存命中量；服务商未提供明细与零命中可能无法区分。</p>}
      {usage.estimated && <p>部分响应未返回用量，已按实际收发内容估算，以服务商账单为准。</p>}
      <p>每次模型响应结束后更新；历史未记录的用量不补算。</p>
    </div>
  </details>;
}
