import type {
  Chain,
  ConversationDetail,
  ConversationRun,
  LeadStep,
  Message,
} from "./api";

export function messageLeadStep(
  message: Message,
  steps: Record<string, LeadStep>,
) {
  // An invitation and the lead's own published reply can share one source run.
  return message.senderType === "agent" && !message.targetAgentID
    ? steps[message.sourceRunID]
    : undefined;
}

export function isDelivered(chain: Chain) {
  return (
    chain.status === "completed" &&
    chain.leadPolicy === 1 &&
    chain.work?.action === "complete" &&
    Boolean(chain.work.result.trim()) &&
    chain.work.checks.length > 0 &&
    chain.work.checks.every((check) => check.status === "met")
  );
}

export function runActivity(run: ConversationRun, detail: ConversationDetail) {
  if (run.status === "running") {
    if (run.contextCompacting) return `${run.agentName || "助手"}正在整理聊天上下文…`;
    if (run.activeScript) return `${run.agentName}正在执行 ${run.activeScript}…`;
    if (run.kind === "selector") return "正在选择合适的伙伴接话…";
    const chain = detail.chains.find((chain) => chain.id === run.chainID);
    return chain?.leadPolicy === 1 && chain.leadAgentID === run.agentID
      ? `${run.agentName}正在检查并整理成果…`
      : `${run.agentName}正在回复…`;
  }
  const previous = detail.runs.find(
    (candidate) => candidate.id === run.previousRunID,
  );
  return previous && ["queued", "running"].includes(previous.status)
    ? `${run.agentName}等待${previous.agentName}完成`
    : `${run.agentName || "任务"}等待执行`;
}

export function toolLabel(name: string) {
  return (
    (
      {
        advance_work: "验收与安排",
        load_skill: "加载技能",
        list_artifacts: "查看成果文件",
    read_artifact: "核对成果文件",
    run_skill_script: "执行技能脚本",
        read_skill_resource: "读取技能参考",
        count_characters: "字符统计",
        list_workspace_files: "浏览工作目录",
    read_workspace_file: "读取目录文件",
    list_sources: "查看资料目录",
        read_source: "读取资料",
        read_image: "读取图片",
        search_web: "搜索网页",
        read_web: "读取网页",
      } as Record<string, string>
    )[name] || name
  );
}
