import { useEffect, useState } from "react";
import type { FormEvent } from "react";
import { api, isDesktop, subscribeWorkspace } from "./api";
import type { Skill, SkillInput } from "./api";
import "./models.css";
import "./skills.css";

const blank = (): SkillInput => ({
  id: "", updatedAt: "", enabled: true, resources: [], scripts: [], note: "",
  content: '---\nname: my-skill\ndescription: 描述这个技能能做什么，以及什么时候使用。\n---\n\n# 执行步骤\n\n1. 明确用户目标。\n2. 按实际资料完成任务。\n3. 检查结果是否满足要求。\n',
});

export default function SkillsPage() {
  const [skills, setSkills] = useState<Skill[]>([]);
  const [draft, setDraft] = useState<SkillInput>(blank);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const selected = skills.find(s => s.id === draft.id);

  useEffect(() => {
    if (!isDesktop()) return;
    let disposed = false;
    const refresh = async () => {
      try { const data = await api.skills(); if (!disposed) setSkills(data); }
      catch (e) { if (!disposed) setError(String(e)); }
    };
    void refresh();
    const off = subscribeWorkspace(() => void refresh());
    return () => { disposed = true; off(); };
  }, []);

  async function select(s: Skill) {
    setBusy(true); setError(""); setNotice("");
    try {
      const v = await api.skillContent(s.id);
      setDraft({id:v.id, updatedAt:v.updatedAt, enabled:v.enabled, content:v.content, resources:v.resources, scripts:v.scripts, note:v.note});
    } catch (e) { setError(String(e)); }
    finally { setBusy(false); }
  }
  async function importDirectory() {
    setBusy(true); setError(""); setNotice("");
    try {
      const p = await api.importSkill();
      if (p.content) { setDraft(d => ({...d, ...p})); setNotice("已读入技能内容，保存后可为角色绑定。"); }
    } catch (e) { setError(String(e)); }
    finally { setBusy(false); }
  }
  async function save(e: FormEvent) {
    e.preventDefault(); setBusy(true); setError(""); setNotice("");
    try {
      const v = await api.saveSkill(draft);
      setDraft(d => ({...d, id:v.id, updatedAt:v.updatedAt}));
      setSkills(await api.skills());
      setNotice("已保存，下次加载技能时使用最新内容。");
    } catch (e) { setError(String(e)); }
    finally { setBusy(false); }
  }

  return <section className="models-page skills-page">
    <header className="models-heading">
      <div><h1>我的技能</h1><p>为角色绑定技能，遇到适合的任务时自动使用。</p></div>
      <button className="primary" disabled={busy || !isDesktop()} onClick={() => {setDraft(blank());setError("");setNotice("");}}>＋ 创建技能</button>
    </header>
    <div className="models-layout">
      <aside className="model-list" aria-label="技能列表">
        <div className="model-list-title">我的技能 <span>{skills.length}</span></div>
        {skills.map(s => <button key={s.id} className={`model-item ${s.id===draft.id ? "selected" : ""}`} disabled={busy} onClick={() => void select(s)}>
          <span><strong>{s.name}</strong><small>{s.enabled ? "已启用" : "已停用"} · {s.agentNames.length} 位角色绑定</small><small>{s.description}</small></span>
        </button>)}
        {!skills.length && <p className="model-list-empty">导入一个包含 SKILL.md 的文件夹，或在右侧填写第一个技能。</p>}
      </aside>
      <form className="model-editor skill-editor" onSubmit={save}>
        <div className="model-editor-heading"><h2>{draft.id ? selected?.name ?? "编辑技能" : "创建技能"}</h2><span>{draft.id ? "每次加载使用最新内容" : "保存后前往角色页绑定"}</span></div>
        {error && <p className="banner error" role="alert">{error}</p>}
        {notice && <p className="banner" role="status">{notice}</p>}
        <fieldset disabled={busy || !isDesktop()}>
          <button type="button" className="text-button" onClick={() => void importDirectory()}>{draft.id ? "从文件夹更新内容" : "导入技能文件夹"}</button>
          <label>技能说明 · SKILL.md<textarea className="skill-content" aria-label="技能说明" spellCheck={false} rows={18} value={draft.content} onChange={e => setDraft(d => ({...d,content:e.target.value}))} /></label>
          <p className="field-help">顶部填写 name 和 description，正文写明适用场景、执行步骤和检查要求。按需加载时会发送给角色所选模型。</p>
          <details className="skill-resources"><summary>配套参考文件 · {draft.resources.length}</summary>
            {draft.resources.map(r => <details key={r.path}><summary>{r.path}</summary><pre>{r.content}</pre></details>)}
            {!draft.resources.length && <p>可随目录导入 Markdown、TXT、CSV、JSON、YAML 文本参考。</p>}
          </details>
          <details className="skill-resources"><summary>可执行脚本 · {draft.scripts.length}</summary>
            {draft.scripts.map(r => <details key={r.path}><summary>{r.path}</summary><pre>{r.content}</pre></details>)}
            {!draft.scripts.length && <p>可随技能文件夹导入 scripts/ 中的 Python、Node.js、Shell 文件。</p>}
          </details>
          {draft.note && <p className="skill-note">{draft.note}</p>}
          <p className="field-help">支持说明、参考和脚本。绑定角色后可自动执行脚本；原文件只读，结果写入会话成果目录，不能联网。每次执行使用最新保存内容。</p>
          {selected && <p className="field-help">已绑定：{selected.agentNames.join("、") || "暂无，请在角色设置中勾选此技能"}</p>}
          <label className="skill-enabled"><input type="checkbox" checked={draft.enabled} onChange={e => setDraft(d => ({...d,enabled:e.target.checked}))} />启用此技能</label>
            <div className="model-actions"><button className="primary" type="submit">{busy ? "正在保存…" : "保存技能"}</button></div>
        </fieldset>
      </form>
    </div>
  </section>;
}
