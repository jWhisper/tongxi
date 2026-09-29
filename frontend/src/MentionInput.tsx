import { useEffect, useRef, useState } from "react";
import type { Agent } from "./api";
import { mentionQuery } from "./mentions";

export default function MentionInput({ value, onChange, members, recipient, onRecipient, disabled, placeholder }: {
  value: string;
  onChange: (value: string) => void;
  members: Agent[];
  recipient: string;
  onRecipient: (id: string) => void;
  disabled: boolean;
  placeholder: string;
}) {
  const input = useRef<HTMLTextAreaElement>(null);
  const [caret, setCaret] = useState(0);
  const [dismissed, setDismissed] = useState(true);
  const [active, setActive] = useState(0);
  const query = !dismissed && members.length ? mentionQuery(value, caret) : null;
  const matches = query ? members.filter(a => a.name.toLocaleLowerCase().includes(query.query.toLocaleLowerCase())) : [];
  const index = Math.min(active, Math.max(0, matches.length - 1));
  const member = members.find(a => a.id === recipient);
  useEffect(() => {
    if (query && matches[index]) document.getElementById(`mention-${matches[index].id}`)?.scrollIntoView({ block: "nearest" });
  }, [query?.query, index]);
  function choose(id: string) {
    if (!query) return;
    const next = value.slice(0, query.start) + value.slice(query.end);
    onChange(next);
    onRecipient(id);
    setDismissed(true);
    requestAnimationFrame(() => {
      input.current?.focus();
      input.current?.setSelectionRange(query.start, query.start);
    });
  }
  return <div className="mention-input">
    {recipient && <div className="compose-recipient">
      <span>@{member?.name || "成员已不可用"}<small>仅这位伙伴回复</small></span>
      <button type="button" className="text-button" aria-label="取消点名" disabled={disabled}
        onClick={() => { onRecipient(""); input.current?.focus(); }}>×</button>
    </div>}
    {query && <div className="mention-menu" role="listbox" id="mention-members" aria-label="选择回复的伙伴">
      <p>让谁回复这条消息？</p>
      {matches.map((a, i) => <button type="button" role="option" aria-selected={i === index}
        id={`mention-${a.id}`} key={a.id} className={i === index ? "active" : ""}
        onMouseDown={e => e.preventDefault()} onMouseEnter={() => setActive(i)} onClick={() => choose(a.id)}>
        <span className="mention-avatar" aria-hidden="true">{Array.from(a.name)[0]}</span>
        <span><strong>{a.name}</strong>{a.description && <small>{a.description}</small>}</span>
      </button>)}
      {!matches.length && <span className="mention-empty">没有匹配的会话成员</span>}
    </div>}
    <label className="visually-hidden" htmlFor="conversation-message">发送消息</label>
    <textarea ref={input} id="conversation-message" placeholder={placeholder} rows={2}
      value={value} maxLength={8000} disabled={disabled}
      aria-autocomplete={members.length ? "list" : undefined}
      aria-controls={query ? "mention-members" : undefined}
      aria-activedescendant={query && matches[index] ? `mention-${matches[index].id}` : undefined}
      onChange={e => { onChange(e.target.value); setCaret(e.target.selectionStart); setActive(0); setDismissed(false); }}
      onSelect={e => setCaret(e.currentTarget.selectionStart)}
      onBlur={() => setDismissed(true)}
      onKeyDown={e => {
        if (e.nativeEvent.isComposing || e.keyCode === 229) return;
        if (!query) return;
        if (e.key === "Escape") { e.preventDefault(); setDismissed(true); }
        if ((e.key === "ArrowDown" || e.key === "ArrowUp") && matches.length) {
          e.preventDefault(); setActive((index + (e.key === "ArrowDown" ? 1 : -1) + matches.length) % matches.length);
        }
        if ((e.key === "Enter" || e.key === "Tab") && matches[index]) { e.preventDefault(); choose(matches[index].id); }
      }} />
  </div>;
}
