import { isValidElement, memo } from "react";
import ReactMarkdown from "react-markdown";
import type { Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import { BrowserOpenURL } from "../wailsjs/runtime/runtime";
import { isDesktop } from "./api";
import CopyButton from "./CopyButton";
import { SourceLink, sourceTarget } from "./Sources";

// Model output cannot navigate the app or load remote resources implicitly.
export function externalURL(url: string) {
  try {
    const parsed = new URL(url);
    return ["https:", "http:"].includes(parsed.protocol) ? parsed.href : "";
  } catch {
    return "";
  }
}

const components: Components = {
  a({ href, children }) {
    if (sourceTarget(href ?? ""))
      return <SourceLink url={href!}>{children}</SourceLink>;
    const url = externalURL(href ?? "");
    if (!url) return <span>{children}</span>;
    return (
      <a
        href={url}
        target="_blank"
        rel="noopener noreferrer"
        onClick={(event) => {
          if (isDesktop()) {
            event.preventDefault();
            BrowserOpenURL(url);
          }
        }}
      >
        {children}
      </a>
    );
  },
  img({ alt }) {
    return <span className="markdown-image">[图片：{alt || "未加载"}]</span>;
  },
  table({ children }) {
    return (
      <div
        className="markdown-table"
        tabIndex={0}
        role="region"
        aria-label="表格，可横向滚动"
      >
        <table>{children}</table>
      </div>
    );
  },
  pre({ children }) {
    const code = isValidElement<{ children?: string; className?: string }>(
      children,
    )
      ? children.props
      : null;
    const text = typeof code?.children === "string" ? code.children : "";
    const language = code?.className?.replace(/^language-/, "") || "代码";
    return (
      <div className="markdown-code">
        <div className="code-toolbar">
          <span>{language}</span>
          <CopyButton text={text} label="复制代码" />
        </div>
        <pre>{children}</pre>
      </div>
    );
  },
};

export default memo(function MarkdownText({ text }: { text: string }) {
  return (
    <div className="markdown-body">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={components}
        urlTransform={(url) => (sourceTarget(url) ? url : externalURL(url))}
        skipHtml
      >
        {text}
      </ReactMarkdown>
    </div>
  );
});
