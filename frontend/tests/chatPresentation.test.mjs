import assert from "node:assert/strict";
import { test } from "node:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import MarkdownText, { externalURL } from "../src/MarkdownText.tsx";
import VersionDiff from "../src/VersionDiff.tsx";
import {
  LeadMessage,
  AcceptanceDetails,
  ResultDialog,
  PendingQuestions,
} from "../src/LeadPresentation.tsx";
import {
  isDelivered,
  messageLeadStep,
  runActivity,
} from "../src/chatPresentation.ts";

const step = {
  action: "complete",
  result: "## 完整成果\n正文",
  reason: "已完成检查",
  checks: [{ criterion: "有正文", status: "met", evidence: "全文见成果" }],
};
const chain = {
  status: "completed",
  leadPolicy: 1,
  work: step,
  createdAt: "2026-09-28T00:00:00Z",
};
const render = (component, props) =>
  renderToStaticMarkup(createElement(component, props));

test("only completed, fully checked lead work is delivered", () => {
  assert.equal(isDelivered(chain), true);
  for (const status of ["active", "failed", "stopped", "incomplete"]) {
    assert.equal(isDelivered({ ...chain, status }), false);
    assert.match(
      render(ResultDialog, {
        chain: { ...chain, status },
        question: "原要求",
        onClose() {},
      }),
      /当前草稿/,
    );
  }
  for (const work of [
    undefined,
    { ...step, action: "pause" },
    { ...step, result: " " },
    { ...step, checks: [] },
    { ...step, checks: [{ status: "pending" }] },
  ]) {
    assert.ok(!isDelivered({ ...chain, work }));
  }
  assert.ok(!isDelivered({ ...chain, leadPolicy: 0 }));
});

test("invitations stay intact even when they share a run with an assessment", () => {
  const message = {
    senderType: "agent",
    sourceRunID: "lead",
    targetAgentID: "",
  };
  assert.equal(messageLeadStep(message, { lead: step }), step);
  assert.equal(
    messageLeadStep({ ...message, targetAgentID: "member" }, { lead: step }),
    undefined,
  );
  assert.equal(
    messageLeadStep({ ...message, senderType: "user" }, { lead: step }),
    undefined,
  );
  assert.equal(messageLeadStep(message, {}), undefined);
});

test("markdown renders tables, lists, code and copy actions without executing output", () => {
  const html = render(MarkdownText, {
    text: '# 标题\n\n**重点**\n\n- 条目\n\n| 项目 | 金额 |\n| --- | --- |\n| 纸张 | 7 元 |\n\n```js\nconst value = "<tag>";\n```',
  });
  for (const pattern of [
    /<h1>标题<\/h1>/,
    /<strong>重点<\/strong>/,
    /<li>条目<\/li>/,
    /<table>/,
    /<td>7 元<\/td>/,
    /language-js/,
    /复制代码/,
    /&lt;tag&gt;/,
  ])
    assert.match(html, pattern);
  const unsafe = render(MarkdownText, {
    text: "<script>alert(1)</script>\n\n[坏链接](javascript:alert%281%29)\n\n[文件](file:///etc/passwd)\n\n![外部图](https://example.test/tracking.png)\n\n[官网](https://example.test)",
  });
  assert.doesNotMatch(unsafe, /<script|javascript:|file:|<img|tracking.png/);
  assert.match(unsafe, /href="https:\/\/example.test\/"/);
  assert.match(unsafe, /rel="noopener noreferrer"/);
  for (const url of [
    "data:text/html,x",
    "//example.test",
    "/relative",
    "#fragment",
    "wails://x",
    "mailto:a@b.test",
  ])
    assert.equal(externalURL(url), "");
});

test("drafts and checks are folded, final content remains immediately readable", () => {
  const draft = render(LeadMessage, {
    step: { ...step, action: "delegate" },
    delivered: false,
  });
  assert.match(draft, /<details class="draft-details"><summary>查看本轮草稿/);
  assert.doesNotMatch(draft, /<details[^>]*open/);
  const result = render(LeadMessage, { step, delivered: true });
  assert.match(result, /成果已交付/);
  assert.match(result, /<h2>完整成果<\/h2>/);
  assert.doesNotMatch(result, /draft-details/);
  assert.match(render(AcceptanceDetails, { step }), /1\/1 项满足/);
});

test("progress distinguishes lead review, discussion selection and dependency waiting", () => {
  const detail = {
    chains: [{ id: "chain", leadPolicy: 1, leadAgentID: "lead" }],
    runs: [{ id: "previous", status: "running", agentName: "写作" }],
  };
  assert.match(
    runActivity(
      {
        status: "running",
        agentName: "写作",
        chainID: "chain",
        agentID: "lead",
      },
      detail,
    ),
    /检查并整理成果/,
  );
  assert.match(
    runActivity({ status: "running", kind: "selector" }, detail),
    /选择合适/,
  );
  assert.match(
    runActivity(
      { status: "queued", agentName: "评审", previousRunID: "previous" },
      detail,
    ),
    /评审等待写作完成/,
  );
  assert.match(
    runActivity({ status: "queued", agentName: "评审" }, detail),
    /评审等待执行/,
  );
});

test("compression replaces normal activity only while the run is processing context", () => {
  const detail = { chains: [], runs: [] };
  const run = { status: "running", agentName: "写作助手", contextCompacting: true };
  assert.equal(runActivity(run, detail), "写作助手正在整理聊天上下文…");
  assert.match(runActivity({ ...run, kind: "selector" }, detail), /整理聊天上下文/);
  assert.equal(runActivity({ ...run, contextCompacting: false }, detail), "写作助手正在回复…");
  assert.doesNotMatch(runActivity({ ...run, status: "queued" }, detail), /整理聊天上下文/);
});

test("historical result opens its own saved version and offers a non-destructive continuation", () => {
  const versions = [
    { id: "v1", taskID: "task", chainID: "old", number: 1, step: { ...step, result: "旧版预算100元" }, request: "初稿", summary: "初版", createdAt: chain.createdAt },
    { id: "v2", taskID: "task", chainID: "new", number: 2, baseVersionID: "v1", step: { ...step, result: "新版预算200元" }, request: "预算改为200元", summary: "预算上限变更", createdAt: chain.createdAt },
  ];
  const html = render(ResultDialog, { chain: { ...chain, id: "old", taskID: "task" }, question: "初稿", versions, onUseVersion() {}, onClose() {} });
  assert.match(html, /交付成果 · V1/);
  assert.match(html, /旧版预算100元/);
  assert.doesNotMatch(html, /新版预算200元/);
  assert.match(html, /基于 V1 继续修改/);
  const newer = render(ResultDialog, { chain: { ...chain, id: "new", taskID: "task" }, question: "改预算", versions, onClose() {} });
  assert.match(newer, /比较改动/);
  assert.match(newer, /预算上限变更/);
});

test("version diff preserves unchanged content and escapes added text", () => {
  const html = render(VersionDiff, { before: "人数12人\n预算100元\n", after: "人数12人\n预算200元\n<script>不可执行</script>" });
  assert.match(html, /diff-same/);
  assert.match(html, /diff-removed[^>]*>.*预算100元/s);
  assert.match(html, /diff-added[^>]*>.*预算200元/s);
  assert.doesNotMatch(html, /<script>/);
  assert.match(render(VersionDiff, { before: "相同", after: "相同" }), /正文没有变化/);
});

test("missing information has a persistent, readable continuation prompt", () => {
  assert.match(render(PendingQuestions, { step: { ...step, action: "pause", questions: ["实际报名人数是多少？"] } }), /等待你补充.*实际报名人数是多少？/s);
  assert.equal(render(PendingQuestions, { step }), "");
});

test("source citations open a scoped reader and invalid schemes remain inert", async () => {
  const { SourceProvider, sourceTarget, SourceReferences } = await import("../src/Sources.tsx");
  const id = "a".repeat(32);
  assert.deepEqual(sourceTarget(`source://${id}/12`), { id, segment: 12 });
  for (const value of ["source://bad/1", `source://${id}/0`, `source://${id}/1/extra`, "file:///etc/passwd"]) assert.equal(sourceTarget(value), null);
  const html = renderToStaticMarkup(createElement(SourceProvider, { conversationID: "one", sources: [] },
    createElement(MarkdownText, { text: `[原文](source://${id}/1) [坏链接](javascript:alert(1))` })));
  assert.match(html, /source-citation/);
  assert.doesNotMatch(html, /href="source:|javascript:/);
  const refs = render(SourceReferences, { citations: [{ source_id: id, segment: 1, quote: "<script>untrusted</script>" }] });
  assert.match(refs, /&lt;script&gt;/);
  assert.match(refs, /引用依据/);
});

test("export targets the historical version selected in the result dialog", () => {
  const versions = [1, 2].map(number => ({ id: `v${number}`, number, chainID: `c${number}`, step, createdAt: "2026-09-28T00:00:00Z", request: "要求" }));
  const html = render(ResultDialog, { chain: { ...chain, id: "c1" }, versions, conversationID: "chat", question: "要求", onClose() {} });
  assert.match(html, /导出 V1/);
  assert.doesNotMatch(html, /导出 V2/);
  assert.match(html, /Word/);
  assert.match(html, /Markdown/);
});

test("work directory remains fixed while legacy conversations can bind once", async () => {
  const { WorkDirectoryPanel } = await import("../src/Sources.tsx");
  const props = { conversationID: "chat", directory: "/projects/活动", active: false, onBind() {}, onChanged() {} };
  const bound = render(WorkDirectoryPanel, props);
  assert.match(bound, /已固定/);
  assert.match(bound, /浏览文件/);
  assert.match(bound, /打开文件夹/);
  assert.doesNotMatch(bound, /选择工作目录|更换目录/);
  const legacy = render(WorkDirectoryPanel, { ...props, directory: "", active: true });
  assert.match(legacy, /disabled=""[^>]*>选择工作目录/);
});

test("historical result only shows its attached files and escapes names", async () => {
  const { SourceProvider } = await import("../src/Sources.tsx");
  const artifacts = [
    {id:"old-file",name:"old<script>.csv",format:"csv",size:25,sourceID:"a".repeat(32)},
    {id:"new-file",name:"new.csv",format:"csv",size:40,sourceID:"b".repeat(32)},
  ];
  const versions = [
    {id:"v1",chainID:"c1",number:1,step:{...step,files:[{artifact_id:"old-file",evidence:"旧数据已核对"}]},createdAt:"2026-09-28",request:"old"},
    {id:"v2",chainID:"c2",number:2,step:{...step,files:[{artifact_id:"new-file",evidence:"新数据已核对"}]},createdAt:"2026-09-28",request:"new"},
  ];
  const html=renderToStaticMarkup(createElement(SourceProvider,{conversationID:"c",sources:[]},
    createElement(ResultDialog,{chain:{...chain,id:"c1"},conversationID:"c",versions,artifacts,question:"old",onClose(){}})));
  assert.match(html,/old&lt;script&gt;\.csv/);
  assert.doesNotMatch(html,/new\.csv|<script>/);
  assert.match(html,/预览内容/);
  assert.match(html,/打开文件夹/);
  assert.match(html,/旧数据已核对/);
});
