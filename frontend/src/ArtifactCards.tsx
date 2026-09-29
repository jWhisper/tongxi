import { useState } from "react";
import { api } from "./api";
import type { Artifact, ArtifactDelivery } from "./api";
import { SourceLink } from "./Sources";

export default function ArtifactCards({ conversationID, artifacts, files, delivered = false }: {
  conversationID: string;
  artifacts: Artifact[];
  files?: ArtifactDelivery[];
  delivered?: boolean;
}) {
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const selected = files ? files.map(file => ({ artifact: artifacts.find(a => a.id === file.artifact_id), evidence: file.evidence }))
    : artifacts.map(artifact => ({ artifact, evidence: "" }));
  if (!selected.length) return null;
  async function open(id: string, folder: boolean) {
    setBusy(id); setError("");
    try { await api.openArtifact(conversationID, id, folder); }
    catch (e) { setError(String(e)); }
    finally { setBusy(""); }
  }
  return <section className="artifact-files" aria-label={delivered ? "本版交付文件" : "生成文件"}>
    <p className="artifact-caption">{delivered ? "本版交付文件" : "生成文件"}</p>
    {selected.map(({artifact: a, evidence}, index) => a ? <article className="artifact-card" key={a.id}>
      <span className="artifact-format">{a.format.toUpperCase()}</span>
      <div className="artifact-body">
        <strong>{a.name}</strong>
        <span className="artifact-meta">{a.size < 1024 ? `${a.size} B` : `${(a.size / 1024).toFixed(1)} KB`} · 已保存文件快照</span>
        {evidence && <p className="artifact-evidence">核对依据：{evidence}</p>}
        <div className="artifact-actions">
          <SourceLink url={`source://${a.sourceID}/1`}>预览内容</SourceLink>
          <button type="button" className="text-button" disabled={Boolean(busy)} onClick={() => open(a.id, false)}>{busy === a.id ? "正在打开…" : "打开文件"}</button>
          <button type="button" className="text-button" disabled={Boolean(busy)} onClick={() => open(a.id, true)}>打开文件夹</button>
        </div>
      </div>
    </article> : <p key={index} role="alert">这份成果文件的记录缺失，请检查本地数据。</p>)}
    {error && <p className="artifact-error" role="alert">{error}</p>}
  </section>;
}
