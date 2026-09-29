import { useMemo } from "react";
import { diffLines } from "diff";

export default function VersionDiff({
  before,
  after,
}: {
  before: string;
  after: string;
}) {
  const changes = useMemo(
    () => diffLines(before, after, { timeout: 100 }),
    [before, after],
  );
  if (!changes)
    return (
      <div className="version-comparison">
        <section>
          <h3>修改前</h3>
          <pre>{before}</pre>
        </section>
        <section>
          <h3>修改后</h3>
          <pre>{after}</pre>
        </section>
      </div>
    );
  if (before === after) return <p className="lead-reason">正文没有变化。</p>;
  return (
    <div className="version-diff" aria-label="正文改动对比">
      <p className="diff-legend">− 删除的内容　＋ 新增的内容</p>
      {changes.map((part, index) => (
        <pre
          key={index}
          className={
            part.added
              ? "diff-added"
              : part.removed
                ? "diff-removed"
                : "diff-same"
          }
        >
          <span aria-hidden="true">
            {part.added ? "+ " : part.removed ? "− " : "  "}
          </span>
          {part.value}
        </pre>
      ))}
    </div>
  );
}
