import type { ConversationRun, SkillUse, ScriptRun } from "./api";

const scriptStatus:Record<string,string> = {running:"执行中",completed:"已完成",failed:"执行失败",cancelled:"已停止",timed_out:"已超时",interrupted:"已中断"};
export default function SkillHistory({uses, scripts, runs}:{uses:SkillUse[];scripts:ScriptRun[];runs:ConversationRun[]}) {
  if (!uses.length && !scripts.length) return null;
  const agentName = (id:string) => runs.find(r=>r.id===id)?.agentName || "伙伴";
  return <details className="skill-use-history"><summary>技能使用记录 · {uses.length}{scripts.length ? ` · ${scripts.length} 次脚本执行` : ""}</summary>
    <p>记录实际加载和执行情况；加载记录不代表所有步骤均已完成。</p>
    {uses.map(u => <div className="skill-use-row" key={`${u.runID}:${u.skillID}`}>
      <span>{agentName(u.runID)} · {u.name}</span><time>{new Date(u.createdAt).toLocaleString("zh-CN")}</time>
    </div>)}
    {scripts.map(s => <details className="skill-script-run" key={s.id}>
      <summary>{agentName(s.runID)} · {s.path} · {scriptStatus[s.status] || s.status}</summary>
      <p>{new Date(s.startedAt).toLocaleString("zh-CN")} · 退出码 {s.exitCode}</p>
      {s.args.length>0 && <pre>参数：{JSON.stringify(s.args)}</pre>}
      {s.stdout && <pre>{s.stdout}</pre>}
      {s.stderr && <pre>错误输出：{s.stderr}</pre>}
      {s.error && <p>{s.error}</p>}
      {s.files.length>0 && <><p>生成文件（相对会话工作目录）：</p><ul>{s.files.map(f=><li key={f}>{f}</li>)}</ul></>}
    </details>)}
  </details>;
}
