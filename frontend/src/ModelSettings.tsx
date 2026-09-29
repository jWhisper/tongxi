import { useState } from "react";
import type { FormEvent } from "react";
import { api } from "./api";
import type { ModelConfig, ModelInput } from "./api";
import { modelPresets, presetFor } from "./modelPresets";
import "./models.css";

const blankModel = (): ModelInput => ({
  id: "",
  name: "",
  provider: "compatible",
  baseURL: "",
  model: "",
  apiKey: "",
  version: 0,
  contextTokens: 32768,
});
const editModel = (m: ModelConfig): ModelInput => ({
  id: m.id,
  name: m.name,
  provider: m.provider,
  baseURL: m.baseURL,
  model: m.model,
  apiKey: "",
  version: m.version,
  contextTokens: m.contextTokens,
});
export default function ModelSettings({
  models,
  ready,
  onModelsChange,
}: {
  models: ModelConfig[];
  ready: boolean;
  onModelsChange: (models: ModelConfig[]) => void;
}) {
  const [draft, setDraft] = useState<ModelInput>(() =>
    models[0] ? editModel(models[0]) : blankModel(),
  );
  const [busy, setBusy] = useState<"" | "save" | "test" | "delete">("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [test, setTest] = useState<{ ok: boolean; text: string } | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const saved = models.find((m) => m.id === draft.id);
  const preset = presetFor(draft.provider);
  const valid = Boolean(
    draft.baseURL.trim() &&
      draft.model.trim() &&
      (draft.apiKey.trim() ||
        (saved?.hasKey &&
          saved.baseURL === draft.baseURL.trim().replace(/\/+$/, ""))),
  );

  function update(change: Partial<ModelInput>) {
    setDraft((d) => ({ ...d, ...change }));
    setTest(null);
    setNotice("");
    setError("");
    setConfirmDelete(false);
  }
  function select(m?: ModelConfig) {
    setDraft(m ? editModel(m) : blankModel());
    setTest(null);
    setNotice("");
    setError("");
    setConfirmDelete(false);
  }
  async function save(event: FormEvent) {
    event.preventDefault();
    setBusy("save");
    setError("");
    setNotice("");
    try {
      const m = await api.saveModel(draft);
      onModelsChange(
        draft.id
          ? models.map((old) => (old.id === m.id ? m : old))
          : [...models, m],
      );
      setDraft(editModel(m));
      setNotice("模型已保存，角色可以直接选用。");
    } catch (err) {
      setError(String(err));
    } finally {
      setBusy("");
    }
  }
  async function testConnection() {
    setBusy("test");
    setTest(null);
    setError("");
    try {
      const result = await api.testModel(draft);
      setTest({
        ok: true,
        text: `连接成功 · ${(result.latencyMS / 1000).toFixed(1)} 秒`,
      });
    } catch (err) {
      setTest({ ok: false, text: String(err) });
    } finally {
      setBusy("");
    }
  }
  async function remove() {
    if (!saved) return;
    setBusy("delete");
    setError("");
    try {
      await api.deleteModel(saved.id, saved.version);
      const remaining = models.filter((m) => m.id !== saved.id);
      onModelsChange(remaining);
      select(remaining[0]);
      setNotice("模型已删除。");
    } catch (err) {
      setError(String(err));
    } finally {
      setBusy("");
    }
  }

  return (
    <section className="models-page">
      <header className="models-heading">
        <div>
          <h1>模型设置</h1>
          <p>保存不同的模型，再为每位角色选择合适的一个。</p>
        </div>
        <button
          className="primary"
          disabled={!ready || !!busy}
          onClick={() => select()}
        >
          ＋ 添加模型
        </button>
      </header>
      <div className="models-layout">
        <aside className="model-list" aria-label="已保存的模型">
          <div className="model-list-title">
            我的模型 <span>{models.length}</span>
          </div>
          {models.map((m) => (
            <button
              key={m.id}
              className={`model-item ${m.id === draft.id ? "selected" : ""}`}
              disabled={!!busy}
              onClick={() => select(m)}
            >
              <span className="provider-mark" aria-hidden="true">
                {presetFor(m.provider).mark}
              </span>
              <span>
                <strong>{m.name}</strong>
                <small>{presetFor(m.provider).name}</small>
                <small>
                  {m.agentCount ? `${m.agentCount} 位角色使用` : "尚未分配角色"}
                </small>
              </span>
            </button>
          ))}
          {models.length === 0 && (
            <p className="model-list-empty">
              在右侧添加第一个模型。保存后，创建角色时就能选到它。
            </p>
          )}
          {!draft.id && (
            <div className="model-new-label">＋ 正在添加新模型</div>
          )}
        </aside>
        <form className="model-editor" onSubmit={save}>
          <div className="model-editor-heading">
            <h2>{draft.id ? "编辑模型" : "添加模型"}</h2>
            <span>
              {saved?.agentCount
                ? `更改将用于 ${saved.agentCount} 位角色的后续回复`
                : "配置保存在本机"}
            </span>
          </div>
          <fieldset disabled={!ready || !!busy}>
            <label>
              服务商
              <select
                value={draft.provider}
                onChange={(e) => {
                  const p = presetFor(e.target.value);
                  update({
                    provider: p.id,
                    baseURL: p.baseURL,
                    model: p.model,
                    apiKey: "",
                  });
                }}
              >
                {modelPresets.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
                  </option>
                ))}
              </select>
            </label>
            <p className="field-help">{preset.help}</p>
            <div className="model-field-row">
              <label>
                模型 ID
                <input
                  required
                  maxLength={200}
                  value={draft.model}
                  autoComplete="off"
                  onChange={(e) => update({ model: e.target.value })}
                  placeholder="服务商提供的模型名称"
                />
              </label>
              <label>
                显示名称 <span className="optional">可选</span>
                <input
                  maxLength={80}
                  value={draft.name}
                  autoComplete="off"
                  onChange={(e) => update({ name: e.target.value })}
                  placeholder={draft.model || "例如：日常写作"}
                />
              </label>
            </div>
            <label>
              Base URL
              <input
                className="model-url"
                type="url"
                required
                value={draft.baseURL}
                autoComplete="off"
                onChange={(e) => update({ baseURL: e.target.value })}
                placeholder="https://your-provider.example/v1"
              />
            </label>
            <p className="field-help">
              已预填的地址可以修改，无需包含 /chat/completions。
            </p>
            <label>
              API Key
              <input
                type="password"
                autoComplete="new-password"
                value={draft.apiKey}
                onChange={(e) => update({ apiKey: e.target.value })}
                placeholder={
                  saved?.hasKey
                    ? "已保存 · 留空保留密钥"
                    : "粘贴该服务商的 API Key"
                }
              />
            </label>
            <p className="field-help">
              密钥保存在系统钥匙串中。更换地址时需重新填写。
            </p>
            <details className="model-context-settings">
              <summary>高级设置</summary>
              <label>
                上下文容量（Tokens）
                <input type="number" min={8192} max={2000000} step={1} required
                  value={draft.contextTokens}
                  onChange={(e) => update({ contextTokens: Number(e.target.value) })} />
              </label>
              <p className="field-help">填写当前模型支持的容量。不确定时保留 32768；接近容量会自动整理历史，并预留回答空间。</p>
            </details>
            <div
              className={`connection-test ${test ? (test.ok ? "passed" : "failed") : ""}`}
            >
              <div>
                <strong>连接测试</strong>
                <p>发送一条简短请求，确认当前配置能够响应。</p>
              </div>
              <button
                type="button"
                className="secondary"
                disabled={!valid || !!busy}
                onClick={() => void testConnection()}
              >
                {busy === "test" ? "正在测试…" : "测试连接"}
              </button>
              {test && (
                <p
                  className="connection-result"
                  role={test.ok ? "status" : "alert"}
                >
                  {test.ok ? "✓ " : ""}
                  {test.text}
                </p>
              )}
            </div>
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
            <div className="model-actions">
              <button className="primary" disabled={!valid || !!busy}>
                {busy === "save" ? "正在保存…" : "保存模型"}
              </button>
              {saved && (
                <button
                  type="button"
                  className="text-button"
                  disabled={saved.agentCount > 0}
                  onClick={() => setConfirmDelete(true)}
                >
                  删除模型
                </button>
              )}
            </div>
            {saved && saved.agentCount > 0 && (
              <p className="field-help">
                仍有角色使用此模型，切换角色的模型后才能删除。
              </p>
            )}
            {confirmDelete && (
              <div className="model-delete-confirm" role="alert">
                <span>删除「{saved?.name}」及其连接配置？</span>
                <button
                  type="button"
                  className="text-button"
                  onClick={() => void remove()}
                >
                  确认删除
                </button>
                <button
                  type="button"
                  className="text-button"
                  onClick={() => setConfirmDelete(false)}
                >
                  取消
                </button>
              </div>
            )}
          </fieldset>
        </form>
      </div>
    </section>
  );
}
