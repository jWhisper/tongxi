import { ImagePreview, isImage } from "./Images";
import { createContext, useContext, useEffect, useRef, useState } from "react";
import type { ReactNode } from "react";
import { api } from "./api";
import type { Citation, Source, SourcePage, WorkspacePage } from "./api";
import { BrowserOpenURL } from "../wailsjs/runtime/runtime";

type SourcesContextValue = {
  conversationID: string;
  sources: Source[];
  open: (id: string, segment: number, citation?: Citation) => void;
};
const SourcesContext = createContext<SourcesContextValue | null>(null);
const CitationContext = createContext<Citation[]>([]);
export function CitationScope({ citations = [], children }: { citations?: Citation[]; children: ReactNode }) {
  return <CitationContext.Provider value={citations}>{children}</CitationContext.Provider>;
}
export function sourceTarget(url: string) {
  const match = /^source:\/\/([a-f0-9]{32})\/([1-9][0-9]*)$/.exec(url);
  return match ? { id: match[1], segment: Number(match[2]) } : null;
}
export function SourceLink({
  url,
  children,
}: {
  url: string;
  children: ReactNode;
}) {
  const context = useContext(SourcesContext);
  const target = sourceTarget(url);
  const citations = useContext(CitationContext);
  if (!target || !context) return <span>{children}</span>;
  return (
    <button
      type="button"
      className="source-citation"
      onClick={() => context.open(target.id, target.segment, citations.find(c => c.source_id === target.id && c.segment === target.segment))}
    >
      {children}
    </button>
  );
}
export function SourceReferences({
  citations = [],
}: {
  citations?: Citation[];
}) {
  const context = useContext(SourcesContext);
  if (!citations.length) return null;
  return (
    <details className="source-references">
      <summary>引用依据 · {citations.length} 处原文</summary>
      <ol>
        {citations.map((citation) => (
          <li key={`${citation.source_id}-${citation.segment}`}>
            <button type="button" className="source-citation" onClick={() => context?.open(citation.source_id, citation.segment, citation)}>
              {citation.name || context?.sources.find((s) => s.id === citation.source_id)?.name || "查看来源"}{" "}
              · {citation.location || `片段 ${citation.segment}`}
            </button>
            <blockquote>{citation.quote}</blockquote>
          </li>
        ))}
      </ol>
      <p>引用已核对原文存在；结论是否得到充分支持仍需结合上下文判断。</p>
    </details>
  );
}
export function SourceProvider({
  conversationID,
  sources,
  children,
}: {
  conversationID: string;
  sources: Source[];
  children: ReactNode;
}) {
  const [target, setTarget] = useState<{ id: string; segment: number; citation?: Citation } | null>(
    null,
  );
  return (
    <SourcesContext.Provider
      value={{ conversationID, sources, open: (id, segment, citation) => setTarget({ id, segment, citation }) }}
    >
      {children}
      {target && (
        <SourceReader
          key={`${target.id}-${target.segment}-${target.citation?.quote ?? ""}`}
          conversationID={conversationID}
          id={target.id}
          start={target.segment}
          citation={target.citation}
          onClose={() => setTarget(null)}
        />
      )}
    </SourcesContext.Provider>
  );
}
function SourceReader({
  conversationID,
  id,
  start,
  citation,
  onClose,
}: {
  conversationID: string;
  id: string;
  start: number;
  citation?: Citation;
  onClose: () => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const [page, setPage] = useState<SourcePage | null>(null);
  const [position, setPosition] = useState(start);
  const [showQuote, setShowQuote] = useState(!!citation);
  const [history, setHistory] = useState<number[]>([]);
  const [error, setError] = useState("");
  useEffect(() => {
    dialog.current?.showModal();
  }, []);
  useEffect(() => {
    if (showQuote) return;
    let ignore = false;
    setPage(null);
    setError("");
    api
      .readSource(conversationID, id, position)
      .then((p) => {
        if (!ignore) setPage(p);
      })
      .catch((e) => {
        if (!ignore) setError(String(e));
      });
    return () => {
      ignore = true;
    };
  }, [conversationID, id, position, showQuote]);
  return (
    <dialog
      ref={dialog}
      className="source-reader"
      onCancel={onClose}
      aria-labelledby="source-title"
    >
      <header>
        <div>
          <p className="eyebrow">{showQuote ? "引用时的摘录" : page?.source.kind === "workspace" ? "当前文件" : "保存的来源"}</p>
          <h2 id="source-title">{(showQuote && citation?.name) || page?.source.name || "读取资料"}</h2>
        </div>
        <button className="text-button" onClick={onClose} autoFocus>
          关闭资料
        </button>
      </header>
      <div className="source-reader-content">
        {showQuote && citation ? <>
          <section className="source-segment"><h3>{citation.location || `片段 ${citation.segment}`}</h3><pre>{citation.quote}</pre></section>
          <p className="source-note">保留引用时的文字；文件后续修改不会改变这段记录。</p>
          <button className="text-button" onClick={() => { setPosition(1); setShowQuote(false); }}>查看当前来源 →</button>
        </> : error ? (
          <p className="form-error" role="alert">
            {error}
          </p>
        ) : !page ? (
          <p>正在读取…</p>
        ) : (
          <>
            <p className="source-meta">
              {page.source.format.toUpperCase()} ·{" "}
              {isImage(page.source.format) ? "图片" : `${page.source.characters.toLocaleString()} 字`}
            </p>
            {page.source.url && (
              <button
                className="text-button source-origin"
                onClick={() => BrowserOpenURL(page.source.url)}
              >
                打开原网页 ↗
              </button>
            )}
            {page.source.kind === "workspace" && <button className="text-button source-origin"
              onClick={() => void api.openSourceFile(conversationID, id).catch(e => setError(String(e)))}>用系统应用打开 ↗</button>}
            {!isImage(page.source.format) && <p className="source-note">{page.source.kind === "workspace" ? "读取工作目录中的最新内容。" : "保留获取时的内容。"}{page.source.note}</p>}
            {isImage(page.source.format) && <ImagePreview conversationID={conversationID} id={id} name={page.source.name} />}
            {page.segments.map((segment) => (
              <section key={segment.number} className="source-segment">
                <h3>
                  {segment.location}
                  <span>片段 {segment.number}</span>
                </h3>
                <pre>{segment.content}</pre>
              </section>
            ))}
            {!isImage(page.source.format) && <nav className="source-pages" aria-label="资料分页">
              <button
                className="text-button"
                disabled={!history.length && position === 1}
                onClick={() => {
                  setPosition(history.at(-1) || 1);
                  setHistory(history.slice(0, -1));
                }}
              >
                上一页
              </button>
              <span>
                片段 {page.segments[0]?.number}–{page.segments.at(-1)?.number} /{" "}
                {page.source.segments}
              </span>
              <button
                className="text-button"
                disabled={!page.next}
                onClick={() => {
                  setHistory([...history, position]);
                  setPosition(page.next);
                }}
              >
                下一页
              </button>
            </nav>}
          </>
        )}
      </div>
    </dialog>
  );
}
export function AddFilesButton({ conversationID, onChanged, onBusy, disabled = false }: {
  conversationID: string;
  onChanged: (sources: Source[]) => void;
  onBusy?: (busy: boolean) => void;
  disabled?: boolean;
}) {
  const [importing, setImporting] = useState(false);
  const [feedback, setFeedback] = useState("");
  async function importFiles() {
    setImporting(true);
    onBusy?.(true);
    setFeedback("");
    try {
      const result = await api.importSources(conversationID);
      if (result.sources.length) onChanged(result.sources);
      setFeedback([

        ...result.errors,
      ].filter(Boolean).join("；"));
    } catch (error) {
      setFeedback(String(error));
    } finally {
      setImporting(false);
      onBusy?.(false);
    }
  }
  return <div className="file-import">
    <button type="button" className="text-button" disabled={disabled || importing}
      onClick={() => void importFiles()}>
      <span aria-hidden="true">＋ </span>{importing ? "正在添加…" : "添加文件"}
    </button>
    {feedback && <span className="file-import-feedback" role="status" title={feedback}>{feedback}</span>}
  </div>;
}

export function SourcesPanel({
  conversationID,
  sources,
  onChanged,
}: {
  conversationID: string;
  sources: Source[];
  onChanged: () => void;
}) {
  sources = sources.filter(source => source.kind !== "artifact");
  const context = useContext(SourcesContext);
  const [busy, setBusy] = useState("");
  const [message, setMessage] = useState("");
  const [url, setURL] = useState("");
  const [webOpen, setWebOpen] = useState(false);
  async function addWeb() {
    if (!url.trim()) return;
    setBusy("web");
    setMessage("");
    try {
      await api.addWebSource(conversationID, url.trim());
      setURL("");
      setWebOpen(false);
      onChanged();
      setMessage("网页正文已保存，可以发送消息让伙伴使用。");
    } catch (e) {
      setMessage(String(e));
    } finally {
      setBusy("");
    }
  }
  return (
    <div className="sources-panel">
      <div className="sources-heading">
        <h3>会话资料{sources.length > 0 && <span>{sources.length}</span>}</h3>
        <div className="source-actions">
          <AddFilesButton conversationID={conversationID} onChanged={onChanged} disabled={!!busy} />
          <button
            className="text-button"
            disabled={!!busy}
            onClick={() => setWebOpen(!webOpen)}
          >
            添加网页
          </button>
        </div>
      </div>
      <div className="source-list">
        {sources.length ? (
          sources.map((source) => (
            <button
              key={source.id}
              onClick={() => context?.open(source.id, 1)}
            >
              <span className="source-format">
                {source.format.toUpperCase()}
              </span>
              <strong>{source.name}</strong>
              <small>{source.kind === "workspace" ? "当前文件" : "网页资料"}</small>
            </button>
          ))
        ) : (
          <p>
            添加文件或网页，同会话伙伴可按需读取。也可以直接在聊天里要求搜索网页。
          </p>
        )}
      </div>
      {webOpen && (
        <div className="source-web-form">
          <input
            aria-label="网页链接"
            placeholder="粘贴公开网页链接"
            type="url"
            value={url}
            onChange={(e) => setURL(e.target.value)}
          />
          <button
            className="small-primary"
            disabled={!!busy || !url.trim()}
            onClick={addWeb}
          >
            {busy === "web" ? "正在读取…" : "读取网页"}
          </button>
        </div>
      )}
      {message && (
        <p className="source-feedback" role="status">
          {message}
        </p>
      )}
      <p className="source-hint">
        TXT / MD / PDF / DOCX / XLSX / CSV / 图片 · 20MB 内 ·
        用到的片段会发送给会话模型
      </p>
    </div>
  );
}

export function WorkDirectoryPanel({ conversationID, directory, onBind, onChanged, active }: {
  conversationID: string;
  directory: string;
  onBind: () => void;
  onChanged: () => void;
  active: boolean;
}) {
  const context = useContext(SourcesContext);
  const [expanded, setExpanded] = useState(false);
  const [page, setPage] = useState<WorkspacePage | null>(null);
  const [offset, setOffset] = useState(0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function load(path = ".", start = 0) {
    setBusy(true); setError("");
    try { setPage(await api.listWorkspaceFiles(conversationID, path, start)); setOffset(start); }
    catch(e) { setPage(null); setError(String(e)); }
    finally { setBusy(false); }
  }
  async function read(path: string) {
    setBusy(true); setError("");
    try { const p = await api.readWorkspaceFile(conversationID, path); onChanged(); context?.open(p.source.id, 1); }
    catch(e) { setError(String(e)); }
    finally { setBusy(false); }
  }
  return <section className="work-directory" aria-label="会话工作目录">
    <div className="work-directory-heading">
      <div><strong>工作目录{directory && <small>已固定</small>}</strong>
        <p title={directory}>{directory || "尚未选择，可补选一次。"}</p>
      </div>
      {directory ? <div className="work-directory-actions">
        <button className="text-button" disabled={busy} onClick={() => { setExpanded(!expanded); if(!expanded) void load(); }}>{expanded ? "收起文件" : "浏览文件"}</button>
        <button className="text-button" onClick={() => { void api.openWorkspaceDirectory(conversationID).catch(e => setError(String(e))); }}>打开文件夹</button>
      </div> : <button className="text-button" disabled={active} onClick={onBind}>选择工作目录</button>}
    </div>
    {expanded && <div className="work-directory-browser">
      <div className="work-directory-actions">
        <button className="text-button" disabled={busy || !page || page.path === "."} onClick={() => void load(page!.path.split("/").slice(0,-1).join("/") || ".")}>上一级</button>
        <span>{page?.path || "."}</span>
        <button className="text-button" disabled={busy} onClick={() => void load(page?.path || ".")}>刷新文件</button>
      </div>
      <div className="source-list">
        {page?.files.map(file => <button disabled={busy} key={file.path} onClick={() => file.directory ? void load(file.path) : void read(file.path)}>
          <span className="source-format">{file.directory ? "目录" : file.name.split(".").at(-1)?.toUpperCase()}</span><strong>{file.name}</strong><small>{file.directory ? "打开 →" : `${Math.max(1, Math.ceil(file.size / 1024))} KB`}</small>
        </button>)}
        {page && !page.files.length && <p>没有可读文件。可放入文档、表格或 PNG、JPEG、WebP 图片，也可进入子目录查看。</p>}
      </div>
      {page && (offset > 0 || page.next > 0) && <nav className="work-directory-actions" aria-label="目录分页">
        <button className="text-button" disabled={busy || !offset} onClick={() => void load(page.path, Math.max(0, offset - 100))}>上一页</button>
        <span>第 {Math.floor(offset / 100) + 1} 页</span>
        <button className="text-button" disabled={busy || !page.next} onClick={() => void load(page.path, page.next)}>下一页</button>
      </nav>}
      <p className="source-hint">所有伙伴可按需读取；隐藏文件、依赖目录和符号链接不显示。每次读取最新内容。</p>
    </div>}
    {busy && <p className="source-hint" role="status">正在读取…</p>}
    {error && <p className="form-error" role="alert">{error}</p>}
  </section>;
}

export function FileAttachments({ files, onRemove, disabled = false }: {
  files: Source[]; onRemove?: (id: string) => void; disabled?: boolean;
}) {
  const context = useContext(SourcesContext);
  if (!files.length) return null;
  return <div className="file-attachments" aria-label={onRemove ? "待发送附件" : "消息附件"}>
    {files.map(file => <span className={`file-chip ${isImage(file.format) ? "file-chip-image" : ""}`} key={file.id}>
      <button type="button" className="file-chip-preview" title={file.name}
        onClick={() => context?.open(file.id, 1)}>
        {isImage(file.format) && context ? <ImagePreview conversationID={context.conversationID} id={file.id} name={file.name} thumbnail /> : <span className="file-chip-format">{file.format.toUpperCase()}</span>}<span>{file.name.split("/").at(-1)}</span>
      </button>
      {onRemove && <button type="button" className="file-chip-remove" disabled={disabled}
        aria-label={`移除附件 ${file.name}`} onClick={() => onRemove(file.id)}>×</button>}
    </span>)}
  </div>;
}
