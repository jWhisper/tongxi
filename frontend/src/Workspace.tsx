import { ImageAttachmentInput } from "./Images";
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
  WorkspaceData,
  WorkVersion,
  Source,
} from "./api";
import "./workspace.css";
import StreamingText from "./StreamingText";
import MarkdownText from "./MarkdownText";
import CopyButton from "./CopyButton";
import { LeadMessage, ResultDialog } from "./LeadPresentation";
import {
  isDelivered,
  messageLeadStep,
  runActivity,
  toolLabel,
} from "./chatPresentation";
import "./chat.css";
import "./sources.css";
import { AddFilesButton, FileAttachments, SourceProvider } from "./Sources";
import { MessageAvatar, ReplyQuote } from "./MessageContext";
import ConversationDetails from "./ConversationDetails";
import TokenUsage from "./TokenUsage";
import MentionInput from "./MentionInput";
import { messageRoute } from "./mentions";
import ArtifactCards from "./ArtifactCards";
import "./skills.css";

type Page = "agents" | "conversations" | "settings" | "skills";
const emptyWorkspace: WorkspaceData = {
  agents: [],
  conversations: [],
  models: [],
  skills: [],
};
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
  navigate,
}: {
  page: Page;
  navigate: (page: Page) => void;
}) {
  const [workspace, setWorkspace] = useState<WorkspaceData>(emptyWorkspace);
  const [ready, setReady] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [selectedID, setSelectedID] = useState("");
  const [resultID, setResultID] = useState("");
  const [detailsID, setDetailsID] = useState("");
  const [detail, setDetail] = useState<ConversationDetail | null>(null);
  const [revisionBases, setRevisionBases] = useState<
    Record<string, WorkVersion>
  >({});
  const [drafts, setDrafts] = useState<Record<string, string>>({});
  const [draftFiles, setDraftFiles] = useState<Record<string, Source[]>>({});
  const [recipients, setRecipients] = useState<Record<string, string>>({});
  const [pendingImports, setPendingImports] = useState(0);
  const importing = pendingImports > 0;
  const [busy, setBusy] = useState(false);
  const [agentDraft, setAgentDraft] = useState<AgentInput | null>(null);
  const [conversationDraft, setConversationDraft] =
    useState<Conversation | null>(null);
  const [formError, setFormError] = useState("");
  const dialog = useRef<HTMLDialogElement>(null);
  const detailsTrigger = useRef<HTMLButtonElement | null>(null);
  const refreshID = useRef(0);
  const detailID = useRef(0);
  const followOutput = useRef(true);
  const runCache = useRef(new Map<string, ConversationRun>());
  const deliveries = useRef<Record<string, { text: string; id: string }>>({});
  const retryRequests = useRef<Record<string, string>>({});
  const end = useRef<HTMLDivElement>(null);
  const messageList = useRef<HTMLDivElement>(null);
  const visible = page === "agents" || page === "conversations";
  const selected = detail?.conversation.id === selectedID ? detail : null;
  const editing = agentDraft !== null || conversationDraft !== null;
  const running = selected?.runs.find((run) => run.status === "running");
  const queued = selected?.runs.filter((run) => run.status === "queued") ?? [];
  const activeRun = running ?? queued[0];
  const currentTask = selected?.tasks.at(-1);
  const latestLead =
    selected?.chains.find(
      (chain) => chain.id === currentTask?.currentChainID,
    ) ?? selected?.chains.filter((chain) => chain.leadPolicy === 1).at(-1);
  const selectedBase = revisionBases[selectedID];
  const resultChain = selected?.chains.find((chain) => chain.id === resultID);
  const followReply = useCallback(() => {
    if (page === "conversations" && followOutput.current)
      end.current?.scrollIntoView({ block: "end" });
  }, [page]);

  function jumpToMessage(id: string) {
    const target = document.getElementById(`message-${selectedID}-${id}`);
    if (!target || !messageList.current?.contains(target)) return;
    followOutput.current = false;
    target.scrollIntoView({ block: "center" });
    target.focus({ preventScroll: true });
  }

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
    setResultID("");
  }, [selectedID, page]);
  useEffect(() => {
    if (selected?.messages.length) followReply();
  }, [selected, followReply]);

  function openAgent(agent?: Agent) {
    setFormError("");
    setAgentDraft(
      agent
        ? { ...agent }
        : {
            id: "",
            name: "",
            description: "",
            instruction: "",
            modelID: workspace.models[0]?.id ?? "",
            tools: ["count_characters"],
            skillIDs: [],
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
            timeBudgetMinutes: 20,
            tokenBudget: 0,
            workDir: "",
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
  async function chooseWorkDirectory() {
    setBusy(true); setFormError("");
    try {
      const path = await api.chooseWorkspaceDirectory();
      if (path) setConversationDraft(current => current ? { ...current, workDir: path } : current);
    } catch (err) { setFormError(String(err)); }
    finally { setBusy(false); }
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
    const files = draftFiles[id] ?? [];
    if ((!text.trim() && !files.length) || busy || importing || !selected) return;
    setNotice("");
    setBusy(true);
    setError("");
    try {
      const selection = messageRoute(selected.conversation.kind, selected.conversation.mode, recipients[id] ?? "");
      const attachmentIDs = files.map(file => file.id);
      const baseVersionID =
        selection.action === "lead" ? revisionBases[id]?.id : undefined;
      const fingerprint = JSON.stringify([text, selection, baseVersionID, attachmentIDs]);
      if (deliveries.current[id]?.text !== fingerprint)
        deliveries.current[id] = { text: fingerprint, id: crypto.randomUUID() };
      await api.schedule({
        conversationID: id,
        content: text,
        attachmentIDs,
        requestID: deliveries.current[id].id,
        baseVersionID,
        ...selection,
      });
      delete deliveries.current[id];
      if (selection.action === "lead") setRevisionBases((values) => {
        const next = { ...values };
        delete next[id];
        return next;
      });
      followOutput.current = true;
      setDrafts((values) => ({ ...values, [id]: "" }));
      setDraftFiles(values => ({ ...values, [id]: [] }));
      setRecipients(values => ({ ...values, [id]: "" }));
      await refresh();
    } catch (err) {
      setError(String(err));
    } finally {
      setBusy(false);
    }
  }
  function useVersion(version: WorkVersion) {
    setRevisionBases((values) => ({ ...values, [selectedID]: version }));
    setResultID("");
    setNotice(
      `已选择 V${version.number}，请在输入框写下修改要求；发送后生成新版本，旧版保留。`,
    );
    requestAnimationFrame(() =>
      document.getElementById("conversation-message")?.focus(),
    );
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
  function automaticRun(run: ConversationRun) {
    return selected?.chains.some(chain => chain.id === run.chainID && ["lead", "discussion"].includes(chain.action)) ?? false;
  }
  function canRetry(run: ConversationRun) {
    const chain = selected?.chains.find((c) => c.id === run.chainID);
    return (
      (run.status === "failed" || run.status === "interrupted") &&
      chain?.status === "failed" &&
      ["lead", "direct", "mention"].includes(chain.action) &&
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
              <h1>我的角色</h1>
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
          {workspace.models.length === 0 && (
            <p className="connection-note">
              先添加一个模型，角色就可以直接选用。
              <button
                className="text-button"
                onClick={() => navigate("settings")}
              >
                添加模型 →
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
                    </div>
                    <span className="role-status">
                      {agent.enabled ? "已启用" : "已停用"}
                    </span>
                  </div>
                  <p className="role-description">
                    {agent.description || "尚未填写角色简介"}
                  </p>
                  <p className="role-model">
                    {agent.modelName}
                    {agent.skillIDs.length ? ` · ${agent.skillIDs.length} 个技能` : ""}
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
                  setDetailsID("");
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
            <SourceProvider
              key={selectedID}
              conversationID={selectedID}
              sources={selected.sources}
            >
              <div className="saved-conversation">
                <header className="conversation-heading">
                  <div>
                    <h2>{selected.conversation.title}</h2>
                    <p>
                      {selected.conversation.kind === "group"
                        ? selected.conversation.mode === "discussion"
                          ? `自由讨论 · ${selected.conversation.memberIDs.length} 位伙伴`
                          : `${memberName(leadFor(selected.conversation))}带队 · ${selected.conversation.memberIDs.length} 位伙伴`
                        : memberName(selected.conversation.leadAgentID)}
                    </p>
                  </div>
                  <div className="conversation-actions">
                    {latestLead && (
                      <button
                        type="button"
                        className="result-entry"
                        onClick={() => setResultID(latestLead.id)}
                      >
                        {isDelivered(latestLead)
                          ? "最新成果"
                          : "当前草稿"}
                      </button>
                    )}
                    <TokenUsage key={selectedID} runs={selected.runs} />
                    <button type="button" className="text-button"
                      aria-haspopup="dialog" onClick={(event) => {
                        detailsTrigger.current = event.currentTarget;
                        setDetailsID(selectedID);
                      }}>
                      会话详情
                    </button>
                  </div>
                </header>
                <div
                  ref={messageList}
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
                  {selected.messages.map((m) => {
                    const step = messageLeadStep(m, selected.leadSteps);
                    const sourceRun = selected.runs.find(
                      (run) => run.id === m.sourceRunID,
                    );
                    const chain = selected.chains.find(
                      (chain) => chain.id === sourceRun?.chainID,
                    );
                    return (
                      <Fragment key={m.id}>
                        <article className={`saved-message ${m.senderType}`}
                          id={`message-${selectedID}-${m.id}`} tabIndex={-1}
                          aria-label={`${m.senderName}的消息`}>
                          <MessageAvatar id={m.senderID} name={m.senderName} user={m.senderType === "user"} />
                          <div className="message-body">
                            <div className="message-heading">
                              <strong>
                                {m.senderName}
                                {m.targetAgentID && (
                                  <span className="message-recipient">
                                    {" "}
                                    {m.senderType === "user" ? "发给" : "邀请"} {memberName(m.targetAgentID)}
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
                              <CopyButton text={m.content} label="复制消息" />
                            </div>
                            <ReplyQuote message={selected.messages.find((item) => item.id === m.replyToMessageID)}
                              onJump={jumpToMessage} />
                            <FileAttachments files={m.attachments ?? []} />
                            {step ? (
                              <LeadMessage
                                step={step}
                                delivered={Boolean(chain && isDelivered(chain))}
                              />
                            ) : (
                              <MarkdownText text={m.content} />
                            )}
                            {!m.targetAgentID && <ArtifactCards conversationID={selectedID}
                              artifacts={step ? selected.artifacts : selected.artifacts.filter(a => a.runID === m.sourceRunID)}
                              files={step ? step.files ?? [] : undefined}
                              delivered={Boolean(step && chain && isDelivered(chain))} />}
                            {step && chain && step.action !== "delegate" && (
                              <button
                                type="button"
                                className="text-button message-result-link"
                                onClick={() => setResultID(chain.id)}
                              >
                                单独查看{isDelivered(chain) ? "成果" : "草稿"} →
                              </button>
                            )}
                            {!!sourceRun?.tools.length && (
                              <details className="message-reference">
                                <summary>使用记录 · {sourceRun.tools.length}</summary>
                                <p>{sourceRun.tools.map(toolLabel).join(" → ")}</p>
                                {selected.imageReads.filter(read => read.runID === sourceRun.id).map(read => <p key={`${read.sourceID}-${read.hash}`}>读取图片：{read.name} · {read.width} × {read.height}</p>)}
                              </details>
                            )}
                          </div>
                        </article>
                        {selected.chains
                          .filter(
                            (c) =>
                              c.messageID === m.id && c.action !== "direct" && c.action !== "mention",
                          )
                          .map((chain) => (
                            <div className="chain-progress" key={chain.id}>
                              <strong>{actionNames[chain.action]}</strong>
                              <span>
                                {chain.status === "active"
                                  ? chain.action === "discussion"
                                    ? "讨论中"
                                    : "协作进行中"
                                  : chain.status === "completed"
                                    ? chain.action === "discussion"
                                      ? "讨论暂歇"
                                      : chain.leadPolicy === 1
                                        ? "已交付 · 主要助手自检通过"
                                        : "本次已完成"
                                    : chain.status === "stopped"
                                      ? "已停止"
                                      : chain.status === "incomplete"
                                        ? chain.action === "discussion" ? "已暂停" : "尚未完成"
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
                                chain.status === "incomplete" ||
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
                                      selected.conversation.mode ===
                                        "discussion"
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
                                  {chain.participants
                                    .map(memberName)
                                    .join(" → ")}
                                </small>
                              )}
                              {chain.leadPolicy === 1 && (
                                <button
                                  type="button"
                                  className="text-button"
                                  onClick={() => setResultID(chain.id)}
                                >
                                  {isDelivered(chain)
                                    ? "查看成果"
                                    : "查看进展与草稿"}
                                </button>
                              )}
                              {chain.reason && <details className="chain-reason">
                                <summary>协作记录</summary>
                                <p>{chain.reason}</p>
                              </details>}
                            </div>
                          ))}
                        {selected.runs
                          .filter(
                            (r) =>
                              r.messageID === m.id &&
                              r.kind !== "selector" &&
                              r.status !== "completed" &&
                              !r.silent,
                          )
                          .map((run) => (
                            <article
                              key={run.id}
                              className={`conversation-run ${run.status}`}
                              aria-label={`${run.agentName || memberName(run.agentID)}回复状态`}
                            >
                              <div className="run-heading">
                                <MessageAvatar id={run.agentID} name={run.agentName || memberName(run.agentID)} />
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
                                        running: run.contextCompacting ? "正在整理聊天上下文…" : "正在回复…",
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
                                      ? automaticRun(run)
                                        ? "停止整次协作"
                                        : "取消排队"
                                      : automaticRun(run)
                                        ? "停止整次协作"
                                        : "停止回复"}
                                  </button>
                                )}
                              </div>
                              <ReplyQuote message={selected.messages.find((item) => item.id === run.messageID)}
                                onJump={jumpToMessage} />
                              {canRetry(run) && (
                                <button
                                  type="button"
                                  className="text-button retry-run"
                                  disabled={busy}
                                  onClick={() => void retryRun(run.id)}
                                >
                                  重试回复
                                </button>
                              )}
                              {run.retryOf && (
                                <small>重试来源 · {shortID(run.retryOf)}</small>
                              )}
                              {run.tools.length > 0 && (
                                <small>
                                  已调用：
                                  {run.tools.map(toolLabel).join(" → ")}
                                </small>
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
                    );
                  })}
                  <div ref={end} />
                </div>
                <form className="message-compose" onSubmit={post}>
                  {selected.conversation.kind === "group" &&
                    selected.conversation.mode === "lead" &&
                    selectedBase && !recipients[selectedID] && (
                      <div className="revision-context">
                        <span>基于 V{selectedBase.number} 继续修改</span>
                        <button
                          type="button"
                          className="text-button"
                          onClick={() =>
                            setRevisionBases((values) => {
                              const next = { ...values };
                              delete next[selectedID];
                              return next;
                            })
                          }
                        >
                          取消选版
                        </button>
                      </div>
                    )}
                  {activeRun && (
                    <div className="compose-running" role="status">
                      <div>
                        <strong>{runActivity(activeRun, selected)}</strong>
                        {running && queued.length > 0 && (
                          <small>
                            {runActivity(queued[0], selected)}
                            {queued.length > 1
                              ? `，另有 ${queued.length - 1} 项排队`
                              : ""}
                          </small>
                        )}
                      </div>
                      <button
                        type="button"
                        className="text-button"
                        onClick={() => void stopRun(activeRun.id)}
                      >
                        {automaticRun(activeRun)
                          ? "停止整次协作"
                          : "停止当前回复"}
                      </button>
                    </div>
                  )}
                  <ImageAttachmentInput key={selectedID} conversationID={selectedID} disabled={busy || importing}
                    onBusy={loading => setPendingImports(count => count + (loading ? 1 : -1))}
                    onChanged={files => {
                      const id = selectedID;
                      setDraftFiles(values => {
                        const merged = new Map((values[id] ?? []).map(file => [file.id, file]));
                        files.forEach(file => merged.set(file.id, file));
                        return { ...values, [id]: [...merged.values()] };
                      });
                      void refresh();
                    }}>
                    <FileAttachments files={draftFiles[selectedID] ?? []} disabled={busy}
                      onRemove={id => setDraftFiles(values => ({ ...values, [selectedID]: (values[selectedID] ?? []).filter(file => file.id !== id) }))} />
                    <MentionInput key={selectedID} value={drafts[selectedID] ?? ""}
                      onChange={value => setDrafts(values => ({ ...values, [selectedID]: value }))}
                      members={selected.conversation.kind === "group" ? workspace.agents.filter(a => a.enabled && selected.conversation.memberIDs.includes(a.id)) : []}
                      recipient={recipients[selectedID] ?? ""}
                      onRecipient={id => setRecipients(values => ({ ...values, [selectedID]: id }))}
                      disabled={busy}
                      placeholder={selected.conversation.kind === "group" ? "发送消息，@ 点名伙伴…" : "补充要求，或继续聊…"} />
                    {(draftFiles[selectedID]?.length ?? 0) > 10 && <p className="form-error" role="alert">一条消息最多添加10个文件，请移除多余附件。</p>}
                    <div className="compose-toolbar">
                      <AddFilesButton key={selectedID} conversationID={selectedID} disabled={busy}
                        onBusy={loading => setPendingImports(count => count + (loading ? 1 : -1))} onChanged={files => {
                          setDraftFiles(values => {
                            const merged = new Map((values[selectedID] ?? []).map(file => [file.id, file]));
                            files.forEach(file => merged.set(file.id, file));
                            return { ...values, [selectedID]: [...merged.values()] };
                          });
                          void refresh();
                        }} />
                      <button
                        className="primary"
                        disabled={busy || importing || (!((drafts[selectedID] ?? "").trim()) && !(draftFiles[selectedID]?.length)) || (draftFiles[selectedID]?.length ?? 0) > 10}
                      >
                        {busy ? "正在发送…" : "发送"}
                      </button>
                    </div>
                  </ImageAttachmentInput>
                </form>
                {detailsID === selectedID && (
                  <ConversationDetails detail={selected} agents={workspace.agents}
                    active={busy || !!activeRun} onChanged={() => void refresh()}
                    onSettings={() => { setDetailsID(""); openConversation(selected.conversation); }}
                    onClose={() => {
                      setDetailsID("");
                      requestAnimationFrame(() => detailsTrigger.current?.focus());
                    }} />
                )}
                {resultChain && (
                  <ResultDialog
                    key={resultChain.id}
                    chain={resultChain}
                    conversationID={selectedID}
                    artifacts={selected.artifacts}
                    question={
                      selected.messages.find(
                        (message) => message.id === resultChain.messageID,
                      )?.content ?? ""
                    }
                    task={selected.tasks.find(
                      (task) => task.id === resultChain.taskID,
                    )}
                    versions={selected.versions.filter(
                      (version) => version.taskID === resultChain.taskID,
                    )}
                    onUseVersion={
                      selected.conversation.kind === "group" &&
                      selected.conversation.mode === "lead"
                        ? useVersion
                        : undefined
                    }
                    onClose={() => setResultID("")}
                  />
                )}
              </div>
            </SourceProvider>
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
                <label>
                  使用模型
                  <select
                    required
                    value={agentDraft.modelID}
                    onChange={(e) =>
                      setAgentDraft({ ...agentDraft, modelID: e.target.value })
                    }
                  >
                    <option value="" disabled>
                      请选择已保存的模型
                    </option>
                    {workspace.models.map((m) => (
                      <option key={m.id} value={m.id}>
                        {m.name}
                        {m.name !== m.model ? ` · ${m.model}` : ""}
                      </option>
                    ))}
                  </select>
                </label>
                <p className="field-help">
                  {workspace.models.length
                    ? "连接地址和密钥统一在模型设置中管理。"
                    : "请先到模型设置添加一个模型，再回来创建角色。"}
                </p>
                <div className="skill-bindings">
                  <h3>绑定技能</h3>
                  <p className="field-help">遇到匹配任务时自动加载，无需在每个会话重复选择。停用的技能不会用于新一轮协作。</p>
                  {workspace.skills.map(skill => <label className="choice-row" key={skill.id}>
                    <input type="checkbox" checked={agentDraft.skillIDs.includes(skill.id)} onChange={e => setAgentDraft({...agentDraft, skillIDs:e.target.checked ? [...agentDraft.skillIDs,skill.id] : agentDraft.skillIDs.filter(id=>id!==skill.id)})} />
                    <span>{skill.name}{skill.enabled ? "" : " · 已停用"}<small>{skill.description}</small></span>
                  </label>)}
                  {!workspace.skills.length && <p className="field-help">尚无技能，请先在左侧“技能”页面创建或导入。</p>}
                </div>
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
                {conversationDraft.kind === "group" && <section className="collaboration-budget-fields">
                  <h3>单次协作预算</h3>
                  <div className="editor-columns">
                    <label>时长（分钟）
                      <input type="number" min="0" max="525600" step="1" required
                        value={conversationDraft.timeBudgetMinutes}
                        onChange={e => setConversationDraft({ ...conversationDraft, timeBudgetMinutes: Number(e.target.value) })} />
                    </label>
                    <label>Token 上限
                      <input type="number" min="0" max="1000000000" step="1" required
                        value={conversationDraft.tokenBudget}
                        onChange={e => setConversationDraft({ ...conversationDraft, tokenBudget: Number(e.target.value) })} />
                    </label>
                  </div>
                  <p className="field-help">任一到限即暂停；0 表示不限。Token 为本次协作所有模型调用的输入与输出之和，缓存不重复相加。重试沿用原预算，发送新消息开始新预算。</p>
                </section>}
                <div className="directory-choice">
                  <strong>工作目录</strong>
                  <p>{conversationDraft.workDir || (conversationDraft.id ? "尚未绑定，可选择一次。" : "保存时自动创建独立目录，也可以选择已有文件夹。")}</p>
                  {workspace.conversations.find(c => c.id === conversationDraft.id)?.workDir ? (
                    <small>此会话的目录已固定，不能更换。要使用其他目录，请新建会话。</small>
                  ) : <>
                    <button type="button" className="text-button" disabled={busy} onClick={() => void chooseWorkDirectory()}>选择文件夹</button>
                    <small>保存会话后不可更换或清空。所有成员默认可读取目录文件，用到的片段会发送给所选模型。</small>
                  </>}
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
                      : "主要助手先明确验收条件，每次邀请一位伙伴，检查后继续完善或交付成果。"}
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
