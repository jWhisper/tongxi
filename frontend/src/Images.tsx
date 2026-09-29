import { useEffect, useState } from "react";
import type { ReactNode } from "react";
import { api } from "./api";
import type { Source } from "./api";

export const isImage = (format: string) => ["png", "jpg", "jpeg", "webp"].includes(format.toLowerCase());

export function ImagePreview({ conversationID, id, thumbnail = false, name }: {
  conversationID: string; id: string; thumbnail?: boolean; name: string;
}) {
  const [image, setImage] = useState<{ dataURL: string; width: number; height: number } | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    let ignore = false;
    setImage(null); setError("");
    api.imagePreview(conversationID, id, thumbnail).then(value => {
      if (!ignore) setImage(value);
    }).catch(err => { if (!ignore) setError(String(err)); });
    return () => { ignore = true; };
  }, [conversationID, id, thumbnail]);
  if (error) return <span className="image-error" role="status" title={error}>{thumbnail ? "图片不可用" : error}</span>;
  if (!image) return <span className="image-loading">加载图片…</span>;
  return <span className={thumbnail ? "image-thumbnail" : "image-preview"}>
    <img src={image.dataURL} alt={name} />
    {!thumbnail && <small>{image.width} × {image.height} · 当前文件</small>}
  </span>;
}

const encodedImage = (file: File) => new Promise<string>((resolve, reject) => {
  const reader = new FileReader();
  reader.onerror = () => reject(new Error("图片读取失败，请重新添加"));
  reader.onload = () => resolve(String(reader.result).split(",")[1]);
  reader.readAsDataURL(file);
});

export function ImageAttachmentInput({ conversationID, disabled, onChanged, onBusy, children }: {
  conversationID: string; disabled: boolean; onChanged: (files: Source[]) => void;
  onBusy: (busy: boolean) => void; children: ReactNode;
}) {
  const [dragging, setDragging] = useState(false);
  const [error, setError] = useState("");
  async function add(files: File[]) {
    if (disabled) return;
    setError("");
    if (files.length > 10) { setError("每次最多添加10张图片"); return; }
    onBusy(true);
    const added: Source[] = [], errors: string[] = [];
    try {
      for (const file of files) {
        if (!isImage(file.name.split(".").at(-1) || "")) { errors.push(`${file.name}：请使用 PNG、JPEG 或 WebP 图片`); continue; }
        if (file.size > 20 * 1024 * 1024) { errors.push(`${file.name}：图片超过20MB`); continue; }
        try { added.push(await api.importImage(conversationID, file.name, await encodedImage(file))); }
        catch (err) { errors.push(String(err)); }
      }
      if (added.length) onChanged(added);
      setError(errors.join("；"));
    } finally { onBusy(false); }
  }
  return <div className={`compose-box ${dragging ? "image-drop-active" : ""}`}
    onPasteCapture={event => {
      const files = Array.from(event.clipboardData.files).filter(file => file.type.startsWith("image/"));
      if (files.length) { event.preventDefault(); void add(files); }
    }}
    onDragOver={event => { if (event.dataTransfer.types.includes("Files")) { event.preventDefault(); setDragging(true); } }}
    onDragLeave={event => { if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setDragging(false); }}
    onDrop={event => { event.preventDefault(); setDragging(false); void add(Array.from(event.dataTransfer.files)); }}>
    {dragging && <div className="image-drop-hint">松开以添加图片</div>}
    {children}
    {error && <p className="image-import-error" role="alert">{error}</p>}
  </div>;
}
