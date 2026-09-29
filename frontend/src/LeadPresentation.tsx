import ArtifactCards from "./ArtifactCards";
import type { Artifact } from "./api";
import { useEffect, useRef, useState } from "react";
import type { Chain, LeadStep, WorkTask, WorkVersion } from "./api";
import { isDelivered } from "./chatPresentation";
import CopyButton from "./CopyButton";
import MarkdownText from "./MarkdownText";
import VersionDiff from "./VersionDiff";
import { api } from "./api";
import { CitationScope, SourceReferences } from "./Sources";

export function AcceptanceDetails({ step, reason }: { step: LeadStep; reason?: string }) {
  const met = step.checks.filter((check) => check.status === "met").length;
  return (
    <details className="acceptance-details">
      <summary>
        验收记录 · {met}/{step.checks.length} 项满足 <span>主要助手自检</span>
      </summary>
      {reason && <p className="lead-reason">{reason}</p>}
      <ul>
        {step.checks.map((check, index) => (
          <li key={index}>
            <div>
              <span className={`check-status ${check.status}`}>
                {{ met: "已满足", unmet: "未满足", pending: "待检查" }[
                  check.status
                ] || "待检查"}
              </span>
              <strong>{check.criterion}</strong>
            </div>
            {check.evidence && <p>{check.evidence}</p>}
          </li>
        ))}
      </ul>
    </details>
  );
}

export function LeadMessage({
  step,
  delivered,
}: {
  step: LeadStep;
  delivered: boolean;
}) {
  return (
    <div className="lead-message">
      {step.action === "delegate" ? (
        <>
          <MarkdownText text={step.reason} />
          {step.result && (
            <details className="draft-details">
              <summary>查看本轮草稿</summary>
              <CitationScope citations={step.citations}><MarkdownText text={step.result} /></CitationScope>
            </details>
          )}
        </>
      ) : (
        <>
          <p className={`delivery-label ${delivered ? "delivered" : ""}`}>
            {delivered
              ? "成果已交付 · 主要助手自检通过"
              : "当前草稿 · 尚未完成"}
          </p>
          <CitationScope citations={step.citations}><MarkdownText text={step.result || step.reason} /></CitationScope>
        </>
      )}
      <PendingQuestions step={step} />
      <SourceReferences citations={step.citations} />
      <AcceptanceDetails step={step} reason={step.action !== "delegate" && step.result ? step.reason : undefined} />
    </div>
  );
}

export function PendingQuestions({ step }: { step: LeadStep }) {
  if (!step.questions?.length) return null;
  return (
    <aside className="pending-questions">
      <strong>等待你补充</strong>
      <ul>
        {step.questions.map((question, index) => (
          <li key={index}>{question}</li>
        ))}
      </ul>
      <p>在聊天里补充这些信息，主要助手会接着完善。</p>
    </aside>
  );
}

export function ResultDialog({
  chain,
  conversationID,
  question,
  task,
  versions = [],
  artifacts = [],
  onUseVersion,
  onClose,
}: {
  chain: Chain;
  conversationID?: string;
  question: string;
  task?: WorkTask;
  versions?: WorkVersion[];
  artifacts?: Artifact[];
  onUseVersion?: (version: WorkVersion) => void;
  onClose: () => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const [versionID, setVersionID] = useState(
    versions.find((version) => version.chainID === chain.id)?.id ?? "",
  );
  const [comparing, setComparing] = useState(false);
  const [compareID, setCompareID] = useState("");
  const [exportFormat, setExportFormat] = useState("docx");
  const [exporting, setExporting] = useState(false);
  const [exportMessage, setExportMessage] = useState("");
  const completedVersionID = versions.find(
    (item) => item.chainID === chain.id,
  )?.id;
  useEffect(() => {
    if (!versionID && completedVersionID) setVersionID(completedVersionID);
  }, [versionID, completedVersionID]);
  useEffect(() => {
    dialog.current?.showModal();
  }, []);
  const version = versions.find((version) => version.id === versionID);
  const step = version?.step ?? chain.work ?? chain.basis?.work;
  const delivered = version ? true : isDelivered(chain);
  const baseID = version?.baseVersionID ?? chain.baseVersionID;
  const base =
    versions.find((item) => item.id === (compareID || baseID)) ??
    versions
      .filter((item) => item.number < (version?.number ?? Infinity))
      .at(-1);
  const canCompare = Boolean(base && base.id !== version?.id && step?.result);
  async function exportVersion() {
    if (!version || !conversationID) return;
    setExporting(true);
    setExportMessage("");
    try {
      const path = await api.exportVersion(
        conversationID,
        version.id,
        exportFormat,
      );
      if (path) setExportMessage(`V${version.number} 已导出：${path}`);
    } catch (error) {
      setExportMessage(String(error));
    } finally {
      setExporting(false);
    }
  }
  return (
    <dialog
      ref={dialog}
      className="result-dialog"
      aria-labelledby="result-title"
      onCancel={onClose}
    >
      <header className="result-heading">
        <div>
          <p className="eyebrow">{task?.title || "本次协作"}</p>
          <h2 id="result-title">
            {delivered
              ? `交付成果${version ? ` · V${version.number}` : ""}`
              : "当前草稿"}
          </h2>
        </div>
        <div className="result-actions">
          {version && conversationID && (
            <div className="export-actions">
              <select
                aria-label="导出格式"
                value={exportFormat}
                onChange={(e) => setExportFormat(e.target.value)}
                disabled={exporting}
              >
                <option value="docx">Word</option>
                <option value="md">Markdown</option>
              </select>
              <button
                className="text-button"
                type="button"
                disabled={exporting}
                onClick={exportVersion}
              >
                {exporting ? "正在导出…" : `导出 V${version.number}`}
              </button>
            </div>
          )}
          {step?.result && <CopyButton text={step.result} label="复制全文" />}
          <button
            type="button"
            className="text-button"
            onClick={onClose}
            autoFocus
          >
            关闭
          </button>
        </div>
      </header>
      {exportMessage && (
        <p className="export-feedback" role="status">
          {exportMessage}
        </p>
      )}
      {versions.length > 0 && (
        <div className="version-toolbar">
          <label>
            版本记录
            <select
              aria-label="查看成果版本"
              value={versionID}
              onChange={(event) => {
                setVersionID(event.target.value);
                setCompareID("");
                setComparing(false);
              }}
            >
              {!versions.some((item) => item.chainID === chain.id) && (
                <option value="">当前草稿 · 尚未交付</option>
              )}
              {[...versions].reverse().map((item) => (
                <option key={item.id} value={item.id}>
                  V{item.number} ·{" "}
                  {new Date(item.createdAt).toLocaleString("zh-CN", {
                    month: "2-digit",
                    day: "2-digit",
                    hour: "2-digit",
                    minute: "2-digit",
                  })}
                </option>
              ))}
            </select>
          </label>
          {canCompare && (
            <button
              type="button"
              className="text-button"
              onClick={() => setComparing(!comparing)}
            >
              {comparing ? "阅读正文" : "比较改动"}
            </button>
          )}
          {version && onUseVersion && (
            <button
              type="button"
              className="text-button"
              onClick={() => onUseVersion(version)}
            >
              基于 V{version.number} 继续修改
            </button>
          )}
        </div>
      )}
      <div className="result-content" key={`${versionID}-${comparing}`}>
        <details className="result-source">
          <summary>查看本版要求</summary>
          {step?.brief && <p>{step.brief}</p>}
          <p>本次要求：{version?.request ?? question}</p>
          {task && <p>最初目标：{task.goal}</p>}
        </details>
        <p className={`delivery-label ${delivered ? "delivered" : ""}`}>
          {delivered
            ? "已交付 · 主要助手自检通过"
            : chain.status === "active"
              ? "正在推进 · 尚未交付"
              : "尚未完成 · 以下内容供继续完善"}
        </p>
        {(version?.summary || step?.change_summary) && (
          <p className="version-summary">
            {version?.summary || step?.change_summary}
          </p>
        )}
        {comparing && canCompare && base && step ? (
          <>
            <label className="compare-selector">
              对照版本
              <select
                aria-label="选择对照版本"
                value={base.id}
                onChange={(event) => setCompareID(event.target.value)}
              >
                {versions
                  .filter((item) => item.id !== version?.id)
                  .map((item) => (
                    <option value={item.id} key={item.id}>
                      V{item.number}
                    </option>
                  ))}
              </select>
              <span>→ {version ? `V${version.number}` : "当前草稿"}</span>
            </label>
            <VersionDiff before={base.step.result} after={step.result} />
          </>
        ) : step?.result ? (
          <CitationScope citations={step.citations}><MarkdownText text={step.result} /></CitationScope>
        ) : (
          <p className="result-empty">
            主要助手尚未整理出正文，可返回讨论查看当前进展。
          </p>
        )}
        {step && conversationID && <ArtifactCards key={versionID} conversationID={conversationID} artifacts={artifacts} files={step.files ?? []} delivered={delivered} />}
        {step && (
          <>
            <p className="lead-reason">
              {version ? step.reason : chain.reason || step.reason}
            </p>
            <PendingQuestions step={step} />
            <SourceReferences citations={step.citations} />
            <AcceptanceDetails step={step} />
          </>
        )}
      </div>
    </dialog>
  );
}
