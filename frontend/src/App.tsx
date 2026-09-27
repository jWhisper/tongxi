import { useCallback, useEffect, useRef, useState } from "react";
import type { FormEvent } from "react";
import { api, emptySettings, isDesktop, mergeRuns, subscribe } from "./api";
import type { Run, Settings } from "./api";
import Workspace from "./Workspace";
import StreamingText from "./StreamingText";

const example =
  "请调用 count_characters 工具，统计「同席」的 Unicode 字符数，再用一句中文告诉我结果。";
const labels: Record<string, string> = {
  running: "正在回复",
  completed: "已完成",
  failed: "未完成",
  cancelled: "已停止",
  interrupted: "上次执行中断",
};
function Mark({ small = false }: { small?: boolean }) {
  return (
    <span aria-hidden="true" className={`mark ${small ? "small" : ""}`}>
      <i />
      <i />
      <i />
      <i />
    </span>
  );
}

export default function App() {
  const [page, setPage] = useState<
    "agents" | "conversations" | "chat" | "settings"
  >("agents");
  const [settings, setSettings] = useState<Settings>(emptySettings);
  const [runs, setRuns] = useState<Run[]>([]);
  const [source, setSource] = useState<"local" | "model">("local");
  const [prompt, setPrompt] = useState("");
  const [busy, setBusy] = useState(false);
  const [ready, setReady] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [baseURL, setBaseURL] = useState("");
  const [model, setModel] = useState("");
  const [key, setKey] = useState("");
  const end = useRef<HTMLDivElement>(null);
  const active = runs.find((run) => run.status === "running");
  const desktop = isDesktop();
  const followReply = useCallback(() => {
    if (page === "chat") end.current?.scrollIntoView({ block: "end" });
  }, [page]);

  useEffect(() => {
    if (!isDesktop()) return;
    let disposed = false;
    const off = subscribe((run) => {
      if (!disposed) setRuns((previous) => mergeRuns(previous, [run]));
    });
    api
      .snapshot()
      .then((snapshot) => {
        if (disposed) return;
        setSettings(snapshot.settings);
        setBaseURL(snapshot.settings.baseURL);
        setModel(snapshot.settings.model);
        setRuns((previous) => mergeRuns(previous, snapshot.runs));
        setReady(true);
      })
      .catch((err) => {
        if (!disposed) setError(String(err));
      });
    return () => {
      disposed = true;
      off();
    };
  }, []);

  useEffect(() => {
    if (runs.length > 0 && page === "chat") {
      end.current?.scrollIntoView({ behavior: "instant", block: "end" });
    }
  }, [runs, page]);

  async function start(event: FormEvent) {
    event.preventDefault();
    if (!ready || busy || active) return;
    setBusy(true);
    setError("");
    setNotice("");
    try {
      const run = await api.start(source, prompt);
      setRuns((previous) => mergeRuns(previous, [run]));
      if (source === "model") setPrompt("");
    } catch (err) {
      setError(String(err));
    } finally {
      setBusy(false);
    }
  }

  async function stop() {
    if (!active) return;
    setBusy(true);
    setError("");
    try {
      await api.stop(active.id);
    } catch (err) {
      setError(String(err));
    } finally {
      setBusy(false);
    }
  }

  async function save(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError("");
    setNotice("");
    try {
      const saved = await api.saveSettings({ baseURL, model, apiKey: key });
      setSettings(saved);
      setBaseURL(saved.baseURL);
      setModel(saved.model);
      setNotice("连接配置已保存。返回连接验证后，可发送真实模型请求。");
      setSource("model");
    } catch (err) {
      setError(String(err));
    } finally {
      setKey("");
      setBusy(false);
    }
  }

  const changePage = (
    next: "agents" | "conversations" | "chat" | "settings",
  ) => {
    setPage(next);
    setError("");
    setNotice("");
    setKey("");
  };
  return (
    <div className="shell">
      <aside className="sidebar">
        <div className="brand">
          <Mark small />
          <div>
            <strong>同席</strong>
            <span>TONGXI</span>
          </div>
        </div>
        <div className="workspace-label">我的工作区</div>
        <nav aria-label="工作区导航">
          <button
            className={page === "agents" ? "nav-item selected" : "nav-item"}
            onClick={() => changePage("agents")}
          >
            <span aria-hidden="true">◎</span>角色
          </button>
          <button
            className={
              page === "conversations" ? "nav-item selected" : "nav-item"
            }
            onClick={() => changePage("conversations")}
          >
            <span aria-hidden="true">◫</span>会话
          </button>
          <button
            className={page === "chat" ? "nav-item selected" : "nav-item"}
            onClick={() => changePage("chat")}
          >
            <span aria-hidden="true">◉</span>连接验证
          </button>
          <button
            className={page === "settings" ? "nav-item selected" : "nav-item"}
            onClick={() => changePage("settings")}
          >
            <span aria-hidden="true">⚙</span>模型设置
            {settings.hasKey && (
              <i className="configured" aria-label="已配置" />
            )}
          </button>
        </nav>
        <div className="up-next">
          <span className="workspace-label">一起完成这件事</span>
          <p>
            说出目标，伙伴一起参与。
            <br />
            助手带队，或自由交流。
          </p>
          <span className="future-tag">主要助手带队 · 自由讨论</span>
        </div>
        <footer className="sidebar-footer">
          <span className="version">0.1.2 · MVP</span>
          <p>伙伴就席，一起做事。</p>
        </footer>
      </aside>
      <main className="main">
        <header className="topbar">
          <div>
            <span className="breadcrumb">我的工作区</span>
            <span className="slash">/</span>
            <strong>
              {
                {
                  agents: "角色",
                  conversations: "会话",
                  chat: "连接验证",
                  settings: "模型设置",
                }[page]
              }
            </strong>
          </div>
          <span className="connection">
            <i className={ready ? "dot ready" : "dot"} />
            {ready ? "本地已就绪" : desktop ? "正在初始化" : "界面预览"}
          </span>
        </header>
        {!desktop && (
          <div className="banner">
            当前为浏览器界面预览。请通过桌面应用运行检查、保存配置和调用模型。
          </div>
        )}
        {error && (
          <div className="banner error" role="alert">
            {error}
          </div>
        )}
        {notice && (
          <div className="banner success" role="status">
            {notice}
          </div>
        )}
        <Workspace page={page} settings={settings} navigate={changePage} />
        {page === "settings" ? (
          <div className="settings-page">
            <span className="eyebrow">模型连接</span>
            <h1>给同席接上一个模型。</h1>
            <p className="intro">
              使用支持工具调用和流式输出的 OpenAI 兼容接口。
            </p>
            <form className="settings-form" onSubmit={save}>
              <label>
                Base URL
                <input
                  autoComplete="off"
                  type="url"
                  required
                  value={baseURL}
                  onChange={(e) => setBaseURL(e.target.value)}
                  placeholder="https://your-provider.example/v1"
                />
              </label>
              <p className="field-help">
                填写接口根地址，通常以 /v1 结尾，不需要包含 /chat/completions。
              </p>
              <label>
                模型名称
                <input
                  autoComplete="off"
                  required
                  value={model}
                  onChange={(e) => setModel(e.target.value)}
                  placeholder="填写服务商提供的模型 ID"
                />
              </label>
              <label>
                API Key{" "}
                <span className="optional">
                  {settings.hasKey ? "留空保留已有密钥" : "首次连接时填写"}
                </span>
                <input
                  type="password"
                  autoComplete="new-password"
                  value={key}
                  onChange={(e) => setKey(e.target.value)}
                  placeholder={
                    settings.hasKey
                      ? "已有密钥保存在系统凭据存储中"
                      : "仅保存到系统凭据存储"
                  }
                />
              </label>
              <div className="storage-note">
                <strong>配置保存在本机</strong>
                <p>
                  API Key
                  保存在系统凭据存储，聊天记录保存在本地。执行真实模型验证时，请求内容会发送至你配置的服务。
                </p>
              </div>
              <div className="form-actions">
                <button
                  className="primary"
                  disabled={!ready || busy || !!active}
                >
                  {busy ? "正在保存…" : "保存连接"}
                </button>
                <button
                  type="button"
                  className="text-button"
                  onClick={() => changePage("chat")}
                >
                  返回连接验证
                </button>
              </div>
              {active && (
                <p className="field-help">请先结束当前验证，再修改连接配置。</p>
              )}
            </form>
          </div>
        ) : page === "chat" ? (
          <>
            <section className="conversation" aria-label="验证记录">
              {runs.length === 0 ? (
                <div className="welcome">
                  <Mark />
                  <span className="eyebrow">欢迎来到同席</span>
                  <h1>让伙伴坐到一起。</h1>
                  <p>
                    先确认同席能够接收请求、调用工具并展示回复。
                    <br />
                    从一次本地检查开始，也可以连接自己的模型。
                  </p>
                  <div className="onboarding">
                    <div>
                      <span className="step">1</span>
                      <strong>运行本地检查</strong>
                      <p>无需密钥，验证基础链路</p>
                    </div>
                    <div>
                      <span className="step">2</span>
                      <button onClick={() => changePage("settings")}>
                        配置模型连接 <span aria-hidden="true">↗</span>
                      </button>
                      <p>准备好后，发送真实请求</p>
                    </div>
                  </div>
                </div>
              ) : (
                <div className="messages">
                  {runs.map((run) => (
                    <article className="run" key={run.id}>
                      <div className="user-message">
                        <span className="message-author">
                          你{" "}
                          <span>
                            {run.source === "local" ? "本地检查" : "模型验证"}
                          </span>
                        </span>
                        <p>{run.prompt}</p>
                      </div>
                      <div className="assistant-message">
                        <div className="assistant-heading">
                          <Mark small />
                          <strong>
                            {run.source === "local"
                              ? "本地检查"
                              : "连接验证助手"}
                          </strong>
                          <span className={`run-status ${run.status}`}>
                            {labels[run.status] ?? run.status}
                          </span>
                        </div>
                        {run.tools.length > 0 && (
                          <div className="tool-result">
                            已调用字符统计工具 <span>× {run.tools.length}</span>
                          </div>
                        )}
                        <p className="reply">
                          <StreamingText
                            text={run.text}
                            streaming={run.status === "running"}
                            onReveal={followReply}
                          />
                          {!run.text &&
                            (run.status === "running"
                              ? "正在等待输出…"
                              : "没有生成回复。")}
                          {run.status === "running" && (
                            <span aria-hidden="true" className="cursor" />
                          )}
                        </p>
                        {run.error && <p className="run-error">{run.error}</p>}
                        {run.source === "model" &&
                          run.status === "completed" &&
                          run.tools.length === 0 && (
                            <p className="field-help">
                              文本回复已完成；本次没有工具调用，尚不能据此确认工具能力。
                            </p>
                          )}
                      </div>
                    </article>
                  ))}
                </div>
              )}
              <div ref={end} />
            </section>
            <div className="compose-area">
              <form className="composer" onSubmit={start}>
                <div className="composer-toolbar">
                  <div className="segmented" aria-label="验证方式">
                    <button
                      type="button"
                      aria-pressed={source === "local"}
                      className={source === "local" ? "active" : ""}
                      disabled={!!active}
                      onClick={() => setSource("local")}
                    >
                      本地检查
                    </button>
                    <button
                      type="button"
                      aria-pressed={source === "model"}
                      className={source === "model" ? "active" : ""}
                      disabled={!!active}
                      onClick={() => setSource("model")}
                    >
                      真实模型
                    </button>
                  </div>
                  <span className="model-label">
                    {source === "local"
                      ? "不联网 · 固定测试"
                      : settings.model || "尚未配置模型"}
                  </span>
                </div>
                {source === "local" ? (
                  <p className="local-description">
                    统计「同席」的字符数，检查工具调用、连续输出和结果保存。
                  </p>
                ) : (
                  <textarea
                    aria-label="发送给模型的验证请求"
                    value={prompt}
                    onChange={(e) => setPrompt(e.target.value)}
                    placeholder="输入一条请求，或使用下面的工具示例…"
                    rows={3}
                    maxLength={8000}
                    disabled={!!active}
                    onKeyDown={(e) => {
                      if (e.key === "Enter" && (e.metaKey || e.ctrlKey))
                        e.currentTarget.form?.requestSubmit();
                    }}
                  />
                )}
                <div className="composer-bottom">
                  <span>
                    {source === "local" ? (
                      "本地检查不会调用真实模型"
                    ) : (
                      <button
                        type="button"
                        className="text-button"
                        disabled={!!active}
                        onClick={() => setPrompt(example)}
                      >
                        填入工具验证示例
                      </button>
                    )}
                  </span>
                  {active ? (
                    <button
                      type="button"
                      className="stop"
                      onClick={stop}
                      disabled={busy}
                    >
                      ■ 停止
                    </button>
                  ) : (
                    <button
                      className="primary"
                      disabled={
                        !ready ||
                        busy ||
                        (source === "model" &&
                          (!settings.hasKey || !prompt.trim()))
                      }
                    >
                      {busy
                        ? "正在启动…"
                        : source === "local"
                          ? "运行检查"
                          : "发送请求"}{" "}
                      <span aria-hidden="true">↑</span>
                    </button>
                  )}
                </div>
              </form>
              <p className="composer-hint">
                {source === "model" && !settings.hasKey ? (
                  <button
                    className="text-button"
                    onClick={() => changePage("settings")}
                  >
                    先配置模型连接 →
                  </button>
                ) : (
                  "每次验证独立执行；结果保存在本机，尚不包含多轮记忆。"
                )}
              </p>
            </div>
          </>
        ) : null}
      </main>
    </div>
  );
}
