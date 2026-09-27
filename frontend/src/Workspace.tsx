import { Fragment, useCallback, useEffect, useRef, useState } from "react";
import type { FormEvent } from "react";
import {
  api,
  isDesktop,
  subscribeWorkspace,
  subscribeConversationRuns,
  mergeConversationRuns,
} from "./api";
import type {
  Agent,
  AgentInput,
  Conversation,
  ConversationDetail,
  ConversationRun,
  Settings,
  WorkspaceData,
} from "./api";
import "./workspace.css";
import StreamingText from "./StreamingText";

type Page = "agents" | "conversations" | "chat" | "settings";
const emptyWorkspace: WorkspaceData = { agents: [], conversations: [] };
const shortID = (id: string) => id.slice(0, 6);
const actionNames: Record<string, string> = {
  lead: "主要助手带队",
  discussion: "自由讨论",
  direct: "私聊",
  publish: "仅发布消息",
  mention: "点名发言",
  round: "大家说一轮",
  summary: "指定总结",
};

export default function Workspace({
  page,
  settings,
  navigate,
}: {
  page: Page;
  settings: Settings;
  navigate: (page: Page) => void;
}) {
  const [workspace, setWorkspace] = useState<WorkspaceData>(emptyWorkspace);
  const [ready, setReady] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [selectedID, setSelectedID] = useState("");
  const [detail, setDetail] = useState<ConversationDetail | null>(null);
  const [drafts, setDrafts] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [agentDraft, setAgentDraft] = useState<AgentInput | null>(null);
  const [conversationDraft, setConversationDraft] =
    useState<Conversation | null>(null);
  const [formError, setFormError] = useState("");
  const dialog = useRef<HTMLDialogElement>(null);
  const refreshID = useRef(0);
  const detailID = useRef(0);
  const followOutput = useRef(true);
  const runCache = useRef(new Map<string, ConversationRun>());
  const deliveries = useRef<Record<string, { text: string; id: string }>>({});
  const retryRequests = useRef<Record<string, string>>({});
  const end = useRef<HTMLDivElement>(null);
  const visible = page === "agents" || page === "conversations";
  const selected = detail?.conversation.id === selectedID ? detail : null;
  const editing = agentDraft !== null || conversationDraft !== null;
  const running = selected?.runs.find((run) => run.status === "running");
  const followReply = useCallback(() => {
    if (page === "conversations" && followOutput.current)
      end.current?.scrollIntoView({ block: "end" });
  }, [page]);

  async function refresh() {
    const request = ++refreshID.current;
    try {
      const data = await api.workspace();
      if (request !== refreshID.current) return;
      setWorkspace(data);
      setReady(true);
    } catch (err) {
      if (request === refreshID.current) setError(String(err));
    }
  }
  useEffect(() => {
    if (!isDesktop()) return;
    void refresh();
    const off = subscribeWorkspace(() => void refresh());
    let frame = 0;
    const pending = new Map<string, ConversationRun>();
    const flush = () => {
      frame = 0;
      const incoming = [...pending.values()];
      pending.clear();
      setDetail((current) => {
        if (!current) return current;
        const matching = incoming.filter(
          (run) => run.conversationID === current.conversation.id,
        );
        return matching.length
          ? { ...current, runs: mergeConversationRuns(current.runs, matching) }
          : current;
      });
    };
    const offRuns = subscribeConversationRuns((run) => {
      const old = runCache.current.get(run.id);
      if (old && old.revision >= run.revision) return;
      runCache.current.set(run.id, run);
      pending.set(run.id, run);
      if (run.status !== "running") {
        cancelAnimationFrame(frame);
        flush();
      } else if (!frame) {
        frame = requestAnimationFrame(flush);
      }
    });
    return () => {
      off();
      offRuns();
      cancelAnimationFrame(frame);
      refreshID.current++;
    };
  }, []);
  useEffect(() => {
    if (visible && isDesktop()) void refresh();
  }, [page]);
  useEffect(() => {
    const request = ++detailID.current;
    if (!selectedID || !isDesktop() || page !== "conversations") return;
    api
      .conversation(selectedID)
      .then((value) => {
        if (request === detailID.current) {
          const cached = [...runCache.current.values()].filter(
            (r) => r.conversationID === selectedID,
          );
          setDetail({
            ...value,
            runs: mergeConversationRuns(value.runs, cached),
          });
        }
      })
      .catch((err) => {
        if (request === detailID.current) setError(String(err));
      });
    return () => {
      detailID.current++;
    };
  }, [selectedID, workspace, page]);
  useEffect(() => {
    if (editing) dialog.current?.showModal();
    else dialog.current?.close();
  }, [editing]);
  useEffect(() => {
    if (formError)
      dialog.current
        ?.querySelector('[role="alert"]')
        ?.scrollIntoView({ block: "nearest" });
  }, [formError]);
  useEffect(() => {
    followOutput.current = true;
  }, [selectedID, page]);
  useEffect(() => {
    if (selected?.messages.length) followReply();
  }, [selected, followReply]);

  function openAgent(agent?: Agent) {
    setFormError("");
    setAgentDraft(
      agent
        ? { ...agent, apiKey: "" }
        : {
            id: "",
            name: "",
            description: "",
            instruction: "",
            baseURL: settings.baseURL,
            model: settings.model,
            apiKey: "",
            tools: ["count_characters"],
            version: 0,
          },
    );
  }
  function openConversation(value?: Conversation, agent?: Agent) {
    setFormError("");
    setConversationDraft(
      value
        ? {
            ...value,
            memberIDs: [...value.memberIDs],
          }
        : {
            id: "",
            title: agent ? `与${agent.name}的私聊` : "",
            kind: agent ? "private" : "group",
            mode: "lead",
            leadAgentID: agent?.id ?? "",
            memberIDs: agent ? [agent.id] : [],
            createdAt: "",
            updatedAt: "",
          },
    );
  }
  function closeEditor() {
    if (busy) return;
    setAgentDraft(null);
    setConversationDraft(null);
    setFormError("");
  }
  async function saveEditor(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setFormError("");
    setError("");
    try {
      if (agentDraft) {
        await api.saveAgent(agentDraft);
        setAgentDraft(null);
        setNotice("角色已保存。");
      } else if (conversationDraft) {
        const saved = await api.saveConversation(conversationDraft);
        setConversationDraft(null);
        setSelectedID(saved.id);
        navigate("conversations");
        setNotice("会话已保存。");
      }
      await refresh();
    } catch (err) {
      setFormError(String(err));
    } finally {
      setAgentDraft((value) => (value ? { ...value, apiKey: "" } : null));
      setBusy(false);
    }
  }
  async function toggleAgent(agent: Agent) {
    setBusy(true);
    setError("");
    try {
      await api.setAgentEnabled(agent.id, !agent.enabled);
      setNotice(
        agent.enabled
          ? "角色已停用，相关在途协作已停止；历史记录保留。"
          : "角色已启用。",
      );
      await refresh();
    } catch (err) {
      setError(String(err));
    } finally {
      setBusy(false);
    }
  }
  async function post(event: FormEvent) {
    event.preventDefault();
    const id = selectedID;
    const text = drafts[id] ?? "";
    if (!text.trim() || busy || !selected) return;
    setBusy(true);
    setError("");
    try {
      const selection =
        selected.conversation.kind === "private"
          ? { action: "direct", agentIDs: [] }
          : {
              action:
                selected.conversation.mode === "discussion"
                  ? "discussion"
                  : "lead",
              agentIDs: [],
            };
      const fingerprint = JSON.stringify([text, selection]);
      if (deliveries.current[id]?.text !== fingerprint)
        deliveries.current[id] = { text: fingerprint, id: crypto.randomUUID() };
      await api.schedule({
        conversationID: id,
        content: text,
        requestID: deliveries.current[id].id,
        ...selection,
      });
      delete deliveries.current[id];
      followOutput.current = true;
      setDrafts((values) => ({ ...values, [id]: "" }));
      await refresh();
    } catch (err) {
      setError(String(err));
    } finally {
      setBusy(false);
    }
  }
  async function stopRun(id: string) {
    try {
      await api.stopRun(id);
    } catch (err) {
      setError(String(err));
    }
  }
  async function stopChain(id: string) {
    try {
      await api.stopChain(id);
      await refresh();
    } catch (err) {
      setError(String(err));
    }
  }
  async function retryRun(id: string) {
    setBusy(true);
    setError("");
    try {
      retryRequests.current[id] ??= crypto.randomUUID();
      await api.retryRun(id, retryRequests.current[id]);
      await refresh();
    } catch (err) {
      setError(String(err));
    } finally {
      setBusy(false);
    }
  }
  function canRetry(run: ConversationRun) {
    const chain = selected?.chains.find((c) => c.id === run.chainID);
    return (
      (run.status === "failed" || run.status === "interrupted") &&
      chain?.status === "failed" &&
      chain.reserved <
        (chain.action === "lead" && run.agentID !== chain.leadAgentID
          ? 5
          : 6) &&
      (chain.action === "lead" || chain.action === "direct") &&
      workspace.agents.some((a) => a.id === run.agentID && a.enabled) &&
      !selected?.runs.some((r) => r.retryOf === run.id)
    );
  }
  const memberName = (id: string) =>
    workspace.agents.find((a) => a.id === id)?.name ?? "未知角色";
  function leadFor(conversation: Conversation) {
    return (
      conversation.leadAgentID ||
      conversation.memberIDs.find((id) =>
        workspace.agents.some((a) => a.id === id && a.enabled),
      ) ||
      ""
    );
  }
  function changeMembers(id: string, checked: boolean) {
    setConversationDraft((c) => {
      if (!c) return c;
      const memberIDs =
        c.kind === "private"
          ? checked
            ? [id]
            : []
          : checked
            ? [...c.memberIDs, id]
            : c.memberIDs.filter((v) => v !== id);
      const leadAgentID =
        c.mode === "discussion"
          ? ""
          : memberIDs.includes(c.leadAgentID)
            ? c.leadAgentID
            : (memberIDs[0] ?? "");
      return { ...c, memberIDs, leadAgentID };
    });
  }

  return (
    <section className="workspace" hidden={!visible}>
      {error && (
        <div className="banner error" role="alert">
          {error}{" "}
          <button
            className="text-button"
            onClick={() => {
              setError("");
              void refresh();
            }}
          >
            重试读取
          </button>
        </div>
      )}
      {notice && (
        <div className="banner success" role="status">
          {notice}
        </div>
      )}
      {page === "agents" ? (
        <div className="role-page">
          <header className="workspace-heading">
            <div>
              <span className="eyebrow">同席的伙伴</span>
              <h1>让每位伙伴各有所长。</h1>
              <p>为角色定义分工，再邀请他们加入会话。</p>
            </div>
            <button
              className="primary"
              disabled={!ready || busy}
              onClick={() => openAgent()}
            >
              ＋ 创建角色
            </button>
          </header>
          {!settings.hasKey && (
            <p className="connection-note">
              尚未设置默认模型连接。
              <button
                className="text-button"
                onClick={() => navigate("settings")}
              >
                配置模型连接 →
              </button>
            </p>
          )}
          {workspace.agents.length === 0 ? (
            <div className="workspace-empty">
              <div className="empty-seat" aria-hidden="true">
                ＋
              </div>
              <h2>留一个位置，给你的第一位伙伴。</h2>
              <p>
                可以从写作助手、评审助手开始。角色的指令与模型配置会保存在本机。
              </p>
              <button
                className="text-button"
                disabled={!ready}
                onClick={() => openAgent()}
              >
                创建第一位角色 →
              </button>
            </div>
          ) : (
            <div className="role-grid">
              {workspace.agents.map((agent) => (
                <article
                  className={`role-card ${agent.enabled ? "" : "inactive"}`}
                  key={agent.id}
                >
                  <div className="role-card-top">
                    <span className="role-avatar">
                      {Array.from(agent.name)[0]}
                    </span>
                    <div>
                      <h2>{agent.name}</h2>
                      <span className="identity">{shortID(agent.id)}</span>
                    </div>
                    <span className="role-status">
                      {agent.enabled ? "已启用" : "已停用"}
                    </span>
                  </div>
                  <p className="role-description">
                    {agent.description || "尚未填写角色简介"}
                  </p>
                  <p className="role-model">
                    {agent.model} ·{" "}
                    {agent.tools.length ? "字符统计" : "未启用工具"}
                  </p>
                  <div className="role-actions">
                    <button
                      className="text-button"
                      disabled={busy}
                      onClick={() => openAgent(agent)}
                      aria-label={`编辑 ${agent.name} ${shortID(agent.id)}`}
                    >
                      编辑角色
                    </button>
                    <button
                      className="text-button"
                      disabled={busy}
                      onClick={() => toggleAgent(agent)}
                      aria-label={`${agent.enabled ? "停用" : "启用"} ${agent.name} ${shortID(agent.id)}`}
                    >
                      {agent.enabled ? "停用" : "启用"}
                    </button>
                    <button
                      className="small-primary"
                      disabled={busy || !agent.enabled}
                      onClick={() => openConversation(undefined, agent)}
                    >
                      新建私聊
                    </button>
                  </div>
                </article>
              ))}
            </div>
          )}
        </div>
      ) : (
        <div className="conversation-workspace">
          <aside className="conversation-list" aria-label="会话列表">
            <div className="list-heading">
              <h2>我的会话</h2>
              <button
                className="text-button"
                disabled={!ready || busy}
                onClick={() => openConversation()}
              >
                ＋ 新建
              </button>
            </div>
            {workspace.conversations.length === 0 && (
              <p className="list-empty">
                创建私聊或多人会话，把伙伴邀请到同一张桌旁。
              </p>
            )}
            {workspace.conversations.map((c) => (
              <button
                className={`conversation-item ${selectedID === c.id ? "selected" : ""}`}
                key={c.id}
                onClick={() => {
                  setSelectedID(c.id);
                  setNotice("");
                  setError("");
                }}
              >
                <strong>{c.title}</strong>
                <span>
                  {c.kind === "private" ? "私聊" : actionNames[c.mode]} ·{" "}
                  {c.memberIDs.length} 位伙伴
                </span>
              </button>
            ))}
          </aside>
          {selected ? (
            <div className="saved-conversation">
              <header className="conversation-heading">
                <div>
                  <h2>{selected.conversation.title}</h2>
                  <p>
                    {selected.conversation.kind === "group"
                      ? selected.conversation.mode === "discussion"
                        ? "自由讨论 · 按话题接话，随时加入"
                        : `由${memberName(leadFor(selected.conversation))}组织讨论并总结`
                      : memberName(selected.conversation.leadAgentID)}
                  </p>
                </div>
                <button
                  className="text-button"
                  disabled={
                    busy ||
                    selected.runs.some(
                      (r) => r.status === "queued" || r.status === "running",
                    )
                  }
                  title="排队或执行期间不能修改会话配置"
                  onClick={() => openConversation(selected.conversation)}
                >
                  会话设置
                </button>
              </header>
              <div className="member-strip" aria-label="会话成员">
                {selected.conversation.memberIDs.map((id) => {
                  const agent = workspace.agents.find((a) => a.id === id);
                  return (
                    <span key={id}>
                      {memberName(id)}{" "}
                      <small>
                        {shortID(id)}
                        {agent?.enabled ? "" : " · 已停用"}
                      </small>
                    </span>
                  );
                })}
              </div>
              <div
                className="saved-messages"
                aria-label="会话消息"
                onScroll={(event) => {
                  const element = event.currentTarget;
                  followOutput.current =
                    element.scrollHeight -
                      element.scrollTop -
                      element.clientHeight <
                    80;
                }}
              >
                {selected.messages.length === 0 && (
                  <div className="conversation-empty">
                    <h3>会话已准备好。</h3>
                    <p>可以先记下目标或讨论要点。</p>
                  </div>
                )}
                {selected.messages.map((m) => (
                  <Fragment key={m.id}>
                    <article className={`saved-message ${m.senderType}`}>
                      <div>
                        <strong>
                          {m.senderName}
                          {m.targetAgentID && (
                            <span className="message-recipient">
                              {" "}
                              → {memberName(m.targetAgentID)}
                            </span>
                          )}
                        </strong>
                        <time>
                          {new Date(m.createdAt).toLocaleString("zh-CN", {
                            month: "2-digit",
                            day: "2-digit",
                            hour: "2-digit",
                            minute: "2-digit",
                          })}
                        </time>
                      </div>
                      <p>{m.content}</p>
                      {m.replyToMessageID && (
                        <small className="message-reference">
                          回复消息 · {shortID(m.replyToMessageID)}
                        </small>
                      )}
                    </article>
                    {selected.chains
                      .filter(
                        (c) => c.messageID === m.id && c.action !== "direct",
                      )
                      .map((chain) => (
                        <div className="chain-progress" key={chain.id}>
                          <strong>
                            {actionNames[chain.action]} · {chain.reserved}/6{" "}
                            {chain.action === "discussion"
                              ? "次发言机会"
                              : "次"}
                          </strong>
                          <span>
                            {chain.status === "active"
                              ? chain.action === "discussion"
                                ? "讨论中"
                                : "协作进行中"
                              : chain.status === "completed"
                                ? chain.action === "discussion"
                                  ? "讨论暂歇"
                                  : "本次已完成"
                                : chain.status === "stopped"
                                  ? "已停止"
                                  : "执行未完成"}
                          </span>
                          {chain.status === "active" && (
                            <button
                              type="button"
                              className="text-button"
                              onClick={() => void stopChain(chain.id)}
                            >
                              停止整次协作
                            </button>
                          )}
                          {(chain.status === "failed" ||
                            chain.status === "stopped") && (
                            <button
                              type="button"
                              className="text-button"
                              disabled={busy}
                              onClick={() => {
                                if (
                                  (drafts[selectedID] ?? "").trim() &&
                                  drafts[selectedID] !== m.content
                                ) {
                                  setNotice(
                                    "输入区已有草稿，请先发送或清空，再重新发起。",
                                  );
                                  return;
                                }
                                setDrafts((drafts) => ({
                                  ...drafts,
                                  [selectedID]: m.content,
                                }));
                                setNotice(
                                  selected.conversation.mode === "discussion"
                                    ? "已填入原问题，发送后开始新的自由讨论。"
                                    : "已填入原问题，发送后将由主要助手重新组织协作。",
                                );
                              }}
                            >
                              重新发起
                            </button>
                          )}
                          {chain.action === "round" && (
                            <small>
                              发言顺序：
                              {chain.participants.map(memberName).join(" → ")}
                            </small>
                          )}
                          {chain.reason && <small>{chain.reason}</small>}
                        </div>
                      ))}
                    {selected.runs
                      .filter(
                        (r) =>
                          r.messageID === m.id &&
                          r.kind !== "selector" &&
                          !r.silent,
                      )
                      .map((run) => (
                        <article
                          key={run.id}
                          className={`conversation-run ${run.status}`}
                          aria-label={`${run.agentName || memberName(run.agentID)}回复状态`}
                        >
                          <div className="run-heading">
                            <strong>
                              {run.agentName || memberName(run.agentID)}
                            </strong>
                            <span>
                              {
                                (
                                  {
                                    queued:
                                      run.previousRunID &&
                                      selected.runs.find(
                                        (r) => r.id === run.previousRunID,
                                      )?.status !== "completed"
                                        ? "等待前一位完成"
                                        : "排队中",
                                    running: "正在回复…",
                                    completed: "已完成",
                                    failed: "执行失败",
                                    cancelled: "已停止",
                                    interrupted: "已中断",
                                  } as Record<string, string>
                                )[run.status]
                              }
                            </span>
                            {(run.status === "running" ||
                              run.status === "queued") && (
                              <button
                                type="button"
                                className="text-button"
                                onClick={() => void stopRun(run.id)}
                              >
                                {run.status === "queued"
                                  ? selected.conversation.kind === "group"
                                    ? "停止整次协作"
                                    : "取消排队"
                                  : selected.conversation.kind === "group"
                                    ? "停止整次协作"
                                    : "停止回复"}
                              </button>
                            )}
                          </div>
                          {canRetry(run) && (
                            <button
                              type="button"
                              className="text-button retry-run"
                              disabled={busy}
                              onClick={() => void retryRun(run.id)}
                            >
                              重试此任务（占用剩余额度）
                            </button>
                          )}
                          {run.retryOf && (
                            <small>重试来源 · {shortID(run.retryOf)}</small>
                          )}
                          {run.tools.length > 0 && (
                            <small>已调用：{run.tools.join(" → ")}</small>
                          )}
                          {run.status !== "completed" &&
                            (run.text || run.status === "running") && (
                              <p>
                                <StreamingText
                                  text={run.text}
                                  streaming={run.status === "running"}
                                  onReveal={followReply}
                                />
                              </p>
                            )}
                          {run.error && (
                            <p className="run-error">{run.error}</p>
                          )}
                        </article>
                      ))}
                  </Fragment>
                ))}
                <div ref={end} />
              </div>
              <form className="message-compose" onSubmit={post}>
                {running && (
                  <div className="compose-running" role="status">
                    <span>
                      {running.kind === "selector"
                        ? "正在看看谁适合接话…"
                        : `${running.agentName}正在回复…`}
                    </span>
                    <button
                      type="button"
                      className="text-button"
                      onClick={() => void stopRun(running.id)}
                    >
                      {selected.conversation.kind === "group"
                        ? "停止整次协作"
                        : "停止当前回复"}
                    </button>
                  </div>
                )}
                <label htmlFor="conversation-message">
                  {selected.conversation.kind === "private"
                    ? "发送给角色"
                    : "发送消息"}
                </label>
                <textarea
                  id="conversation-message"
                  placeholder="写下这次想一起完成的事…"
                  value={drafts[selectedID] ?? ""}
                  maxLength={8000}
                  disabled={busy}
                  onChange={(e) =>
                    setDrafts((values) => ({
                      ...values,
                      [selectedID]: e.target.value,
                    }))
                  }
                />
                <div>
                  <p>
                    {selected.conversation.kind === "private"
                      ? "回复期间可继续发送，消息会依次处理。"
                      : selected.conversation.mode === "discussion"
                        ? "伙伴按话题自由接话；你发送新消息会打断旧讨论。"
                        : "主要助手会自动邀请伙伴参与，并汇总结论。"}
                  </p>
                  <button
                    className="primary"
                    disabled={busy || !(drafts[selectedID] ?? "").trim()}
                  >
                    {busy ? "正在发送…" : "发送"}
                  </button>
                </div>
              </form>
            </div>
          ) : (
            <div className="workspace-empty conversation-placeholder">
              <div className="empty-seat" aria-hidden="true">
                ◎
              </div>
              <h2>{selectedID ? "正在读取会话…" : "选一张桌子，开始交流。"}</h2>
              <p>选择已有会话，或邀请伙伴新建一个。</p>
              <button
                className="text-button"
                disabled={!ready}
                onClick={() => openConversation()}
              >
                新建会话 →
              </button>
            </div>
          )}
        </div>
      )}
      <dialog
        className="workspace-dialog"
        ref={dialog}
        aria-labelledby="editor-title"
        onCancel={(event) => {
          event.preventDefault();
          closeEditor();
        }}
      >
        <form onSubmit={saveEditor}>
          <header>
            <div>
              <span className="eyebrow">
                {agentDraft ? "角色配置" : "会话配置"}
              </span>
              <h2 id="editor-title">
                {agentDraft
                  ? agentDraft.id
                    ? "编辑角色"
                    : "邀请一位新伙伴"
                  : conversationDraft?.id
                    ? "编辑会话"
                    : "开一张新桌"}
              </h2>
            </div>
          </header>
          {formError && (
            <div className="banner error" role="alert">
              {formError}
              {agentDraft && "。其他输入已保留；如曾填写新密钥，请重新填写。"}
            </div>
          )}
          <fieldset disabled={busy} className="editor-fields">
            {agentDraft && (
              <>
                <label>
                  角色名称
                  <input
                    required
                    maxLength={60}
                    value={agentDraft.name}
                    onChange={(e) =>
                      setAgentDraft({ ...agentDraft, name: e.target.value })
                    }
                    placeholder="例如：写作助手"
                    autoFocus
                  />
                </label>
                <label>
                  角色简介
                  <input
                    maxLength={300}
                    value={agentDraft.description}
                    onChange={(e) =>
                      setAgentDraft({
                        ...agentDraft,
                        description: e.target.value,
                      })
                    }
                    placeholder="一句话说明这位伙伴擅长什么"
                  />
                </label>
                <label>
                  角色指令
                  <textarea
                    required
                    rows={4}
                    maxLength={8000}
                    value={agentDraft.instruction}
                    onChange={(e) =>
                      setAgentDraft({
                        ...agentDraft,
                        instruction: e.target.value,
                      })
                    }
                    placeholder="说明职责、工作方式和回复要求…"
                  />
                </label>
                <div className="editor-divider">模型连接</div>
                <label>
                  Base URL
                  <input
                    type="url"
                    required
                    value={agentDraft.baseURL}
                    onChange={(e) =>
                      setAgentDraft({ ...agentDraft, baseURL: e.target.value })
                    }
                    placeholder="https://api.kimi.com/coding/v1"
                  />
                </label>
                <label>
                  模型名称
                  <input
                    required
                    value={agentDraft.model}
                    onChange={(e) =>
                      setAgentDraft({ ...agentDraft, model: e.target.value })
                    }
                    placeholder="kimi-for-coding"
                  />
                </label>
                <label>
                  API Key
                  <input
                    type="password"
                    autoComplete="new-password"
                    value={agentDraft.apiKey}
                    onChange={(e) =>
                      setAgentDraft({ ...agentDraft, apiKey: e.target.value })
                    }
                    placeholder={
                      agentDraft.id
                        ? "留空保留该角色的密钥"
                        : "同一地址可留空，沿用默认连接密钥"
                    }
                  />
                </label>
                <p className="field-help">
                  更换服务地址时需填写新密钥。密钥只保存在系统凭据存储中。
                </p>
                <label className="choice-row">
                  <input
                    type="checkbox"
                    checked={agentDraft.tools.includes("count_characters")}
                    onChange={(e) =>
                      setAgentDraft({
                        ...agentDraft,
                        tools: e.target.checked ? ["count_characters"] : [],
                      })
                    }
                  />
                  <span>
                    字符统计<small>统计文本的 Unicode 字符数</small>
                  </span>
                </label>
              </>
            )}
            {conversationDraft && (
              <>
                <label>
                  会话名称
                  <input
                    required
                    autoFocus
                    maxLength={100}
                    value={conversationDraft.title}
                    onChange={(e) =>
                      setConversationDraft({
                        ...conversationDraft,
                        title: e.target.value,
                      })
                    }
                    placeholder="例如：活动文案讨论"
                  />
                </label>
                <div className="editor-columns">
                  <label>
                    会话类型
                    <select
                      value={conversationDraft.kind}
                      onChange={(e) => {
                        const kind = e.target.value;
                        const memberIDs =
                          kind === "private"
                            ? conversationDraft.memberIDs.slice(0, 1)
                            : conversationDraft.memberIDs;
                        setConversationDraft({
                          ...conversationDraft,
                          kind,
                          memberIDs,
                          mode: "lead",
                          leadAgentID:
                            kind === "private"
                              ? (memberIDs[0] ?? "")
                              : conversationDraft.leadAgentID,
                        });
                      }}
                    >
                      <option value="private">私聊</option>
                      <option value="group">多人会话</option>
                    </select>
                  </label>
                  {conversationDraft.kind === "group" && (
                    <label>
                      群聊方式
                      <select
                        value={conversationDraft.mode}
                        onChange={(e) =>
                          setConversationDraft({
                            ...conversationDraft,
                            mode: e.target.value,
                            leadAgentID:
                              e.target.value === "discussion"
                                ? ""
                                : leadFor(conversationDraft),
                          })
                        }
                      >
                        <option value="lead">主要助手带队</option>
                        <option value="discussion">自由讨论</option>
                      </select>
                    </label>
                  )}
                </div>
                <div className="editor-divider">
                  选择伙伴 ·{" "}
                  {conversationDraft.kind === "private" ? "一位" : "至少两位"}
                </div>
                {workspace.agents.length === 0 && (
                  <p className="field-help">
                    还没有角色。请先关闭此窗口，在「角色」中创建伙伴。
                  </p>
                )}
                <div className="member-choices">
                  {workspace.agents.map((a) => (
                    <label className="choice-row" key={a.id}>
                      <input
                        type="checkbox"
                        checked={conversationDraft.memberIDs.includes(a.id)}
                        disabled={
                          !a.enabled &&
                          !conversationDraft.memberIDs.includes(a.id)
                        }
                        onChange={(e) => changeMembers(a.id, e.target.checked)}
                      />
                      <span>
                        {a.name}{" "}
                        <small>
                          {shortID(a.id)} ·{" "}
                          {a.enabled ? a.description || a.model : "已停用"}
                        </small>
                      </span>
                    </label>
                  ))}
                </div>
                {conversationDraft.kind === "group" &&
                  conversationDraft.mode === "lead" && (
                    <label>
                      主要助手
                      <select
                        required
                        value={conversationDraft.leadAgentID}
                        onChange={(e) =>
                          setConversationDraft({
                            ...conversationDraft,
                            leadAgentID: e.target.value,
                          })
                        }
                      >
                        <option value="">请选择会话中的一位伙伴</option>
                        {conversationDraft.memberIDs.map((id) => (
                          <option value={id} key={id}>
                            {memberName(id)} · {shortID(id)}
                          </option>
                        ))}
                      </select>
                    </label>
                  )}
                <p className="field-help">
                  {conversationDraft.kind === "private"
                    ? "私聊由当前角色回复。"
                    : conversationDraft.mode === "discussion"
                      ? "没有固定主持人，伙伴根据话题接话；没有新内容时自然暂停，不强制总结。"
                      : "主要助手自动分工，组织伙伴发言并汇总结论。"}
                  排队或执行期间不能修改会话配置。
                </p>
              </>
            )}
          </fieldset>
          <footer>
            <button
              className="text-button"
              type="button"
              disabled={busy}
              onClick={closeEditor}
            >
              放弃修改
            </button>
            <button className="primary" disabled={busy}>
              {busy ? "正在保存…" : agentDraft ? "保存角色" : "保存会话"}
            </button>
          </footer>
        </form>
      </dialog>
    </section>
  );
}
