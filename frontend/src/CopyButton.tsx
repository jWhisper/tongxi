import { useEffect, useRef, useState } from "react";
import { ClipboardSetText } from "../wailsjs/runtime/runtime";
import { isDesktop } from "./api";

export default function CopyButton({
  text,
  label = "复制",
}: {
  text: string;
  label?: string;
}) {
  const [status, setStatus] = useState("");
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => {
    setStatus("");
    return () => clearTimeout(timer.current);
  }, [text]);
  async function copy() {
    clearTimeout(timer.current);
    try {
      if (isDesktop()) {
        if (!(await ClipboardSetText(text)))
          throw new Error("clipboard unavailable");
      } else {
        await navigator.clipboard.writeText(text);
      }
      setStatus("已复制");
    } catch {
      setStatus("复制失败，请重试");
    }
    timer.current = setTimeout(() => setStatus(""), 2500);
  }
  return (
    <button
      type="button"
      className="text-button copy-button"
      onClick={() => void copy()}
      aria-label={status || label}
      aria-live="polite"
    >
      {status || label}
    </button>
  );
}
