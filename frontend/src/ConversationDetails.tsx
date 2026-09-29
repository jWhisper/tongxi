import { useEffect, useRef } from "react";
import type { Agent, ConversationDetail } from "./api";
import ArtifactCards from "./ArtifactCards";
import SkillHistory from "./SkillHistory";
import { SourcesPanel, WorkDirectoryPanel } from "./Sources";

export default function ConversationDetails({
  detail, agents, active, onChanged, onSettings, onClose,
}: {
  detail: ConversationDetail;
  agents: Agent[];
  active: boolean;
  onChanged: () => void;
  onSettings: () => void;
  onClose: () => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const { conversation } = detail;
  const task = detail.tasks.at(-1);
  useEffect(() => { dialog.current?.showModal(); }, []);
  return (
    <dialog ref={dialog} className="conversation-details" aria-labelledby="conversation-details-title"
      onCancel={onClose} onClick={(event) => { if (event.target === event.currentTarget) onClose(); }}>
      <div className="conversation-details-content">
        <header>
          <div><h2 id="conversation-details-title">会话详情</h2><p>{conversation.title}</p></div>
          <button type="button" className="text-button" onClick={onClose} autoFocus>关闭</button>
        </header>
        <div className="conversation-details-body">
          <SourcesPanel conversationID={conversation.id}
            sources={detail.sources} onChanged={onChanged} />
          <WorkDirectoryPanel conversationID={conversation.id} directory={conversation.workDir}
            active={active} onBind={onSettings} onChanged={onChanged} />
          {detail.artifacts.length > 0 && <details className="artifact-history">
            <summary>文件成果 · {detail.artifacts.length}</summary>
            <ArtifactCards conversationID={conversation.id} artifacts={detail.artifacts} />
          </details>}
          <section className="conversation-members">
            <div className="details-section-heading">
              <h3>会话成员 · {conversation.memberIDs.length}</h3>
              <button type="button" className="text-button" onClick={onSettings} disabled={active}
                title={active ? "协作结束后可修改会话设置" : undefined}>会话设置</button>
            </div>
            <ul>{conversation.memberIDs.map((id) => {
              const agent = agents.find((item) => item.id === id);
              return <li key={id}><span>{agent?.name || "未命名角色"}</span>
                <small>{!agent?.enabled ? "已停用" : conversation.kind === "group" && conversation.mode === "lead" && id === conversation.leadAgentID ? "主要助手" : ""}</small>
              </li>;
            })}</ul>
            <p>{conversation.kind === "private" ? "回复期间可继续发送，消息会依次处理。"
              : conversation.mode === "discussion" ? "伙伴按话题自由接话；新消息会打断旧讨论。"
              : "主要助手按需邀请伙伴，检查验收要求后交付。直接补充要求即可继续修改。"}</p>
          </section>
          {task && <details className="conversation-task"><summary>当前任务</summary><p>{task.title}</p></details>}
          <SkillHistory uses={detail.skillUses} scripts={detail.scriptRuns} runs={detail.runs} />
        </div>
      </div>
    </dialog>
  );
}
