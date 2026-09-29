import { useEffect, useState } from "react";
import { api, isDesktop, subscribeWorkspace } from "./api";
import type { ModelConfig } from "./api";
import Workspace from "./Workspace";
import ModelSettings from "./ModelSettings";
import SkillsPage from "./SkillsPage";

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

type Page = "agents" | "conversations" | "settings" | "skills";
const pageNames = {
  agents: "角色",
  conversations: "会话",
  settings: "模型设置",
  skills: "技能",
};

function NavIcon({ page }: { page: Page }) {
  return (
    <svg className="nav-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      {page === "agents" && <><circle cx="12" cy="8" r="3.5" /><path d="M5 20v-2a7 7 0 0 1 14 0v2" /></>}
      {page === "conversations" && <path d="M20 14a3 3 0 0 1-3 3h-7l-5 4v-4a3 3 0 0 1-2-3V6a3 3 0 0 1 3-3h11a3 3 0 0 1 3 3z" />}
      {page === "skills" && <path d="m12 3 3 6 6 3-6 3-3 6-3-6-6-3 6-3z" />}
      {page === "settings" && <><path d="M4 7h7m4 0h5M4 17h3m4 0h9" /><circle cx="13" cy="7" r="2" /><circle cx="9" cy="17" r="2" /></>}
    </svg>
  );
}

export default function App() {
  const [page, setPage] = useState<Page>("agents");
  const [models, setModels] = useState<ModelConfig[]>([]);
  const [ready, setReady] = useState(false);
  const [error, setError] = useState("");
  const desktop = isDesktop();

  useEffect(() => {
    if (!isDesktop()) return;
    let disposed = false;
    let request = 0;
    const refresh = async () => {
      const id = ++request;
      try {
        const data = await api.models();
        if (disposed || id !== request) return;
        setModels(data);
        setReady(true);
        setError("");
      } catch (err) {
        if (!disposed && id === request) setError(String(err));
      }
    };
    void refresh();
    const off = subscribeWorkspace(() => void refresh());
    return () => {
      disposed = true;
      off();
    };
  }, []);

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
        <nav aria-label="工作区导航">
          {(["agents", "conversations", "skills", "settings"] as const).map((item) => (
            <button
              key={item}
              className={`nav-item ${page === item ? "selected" : ""}`}
              aria-current={page === item ? "page" : undefined}
              onClick={() => setPage(item)}
            >
              <NavIcon page={item} />
              <span>{pageNames[item]}</span>
            </button>
          ))}
        </nav>
        <footer className="sidebar-footer">
          <span className="version">0.1.19</span>
        </footer>
      </aside>
      <main className="main">
        <header className="topbar">
          <div>
            <span className="breadcrumb">我的工作区</span>
            <span className="slash">/</span>
            <strong>{pageNames[page]}</strong>
          </div>
        </header>
        {!desktop && (
          <div className="banner">
            当前为浏览器界面预览。请通过桌面应用保存配置和调用模型。
          </div>
        )}
        {error && (
          <div className="banner error" role="alert">
            {error}
          </div>
        )}
        <Workspace page={page} navigate={setPage} />
        {page === "skills" && <SkillsPage />}
        {page === "settings" && (
          <ModelSettings
            models={models}
            ready={ready}
            onModelsChange={setModels}
          />
        )}
      </main>
    </div>
  );
}
