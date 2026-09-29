import type { Message } from "./api";

export function MessageAvatar({ id, name, user = false }: {
  id: string;
  name: string;
  user?: boolean;
}) {
  const tone = Array.from(id).reduce((sum, char) => sum + char.codePointAt(0)!, 0) % 4;
  return <span className={`message-avatar ${user ? "self" : `tone-${tone}`}`} aria-hidden="true">
    {user ? "我" : Array.from(name)[0] || "助"}
  </span>;
}

export function ReplyQuote({ message, onJump }: {
  message?: Message;
  onJump: (id: string) => void;
}) {
  if (!message) return null;
  const content = message.content.replace(/\s+/g, " ").trim();
  const characters = Array.from(content);
  const excerpt = characters.slice(0, 100).join("") + (characters.length > 100 ? "…" : "");
  return <button type="button" className="reply-quote"
    aria-label={`查看${message.senderName}的原消息：${excerpt}`}
    onClick={() => onJump(message.id)}>
    <span className="reply-quote-author">回复 {message.senderName}</span>
    <span className="reply-quote-text">{excerpt || "查看原消息"}</span>
    <span className="reply-quote-arrow" aria-hidden="true">↗</span>
  </button>;
}
