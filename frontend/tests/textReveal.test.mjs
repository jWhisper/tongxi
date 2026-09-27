import assert from "node:assert/strict";
import { test } from "node:test";
import { TextReveal } from "../src/textReveal.ts";

test("burst output grows across frames and catches up without a long typing tail", () => {
  const buffer = new TextReveal("");
  const text = "同席".repeat(1000);
  buffer.update(text, true);
  let elapsed = 0;
  let previous = "";
  let frames = 0;
  while (buffer.pending && elapsed < 1000) {
    const visible = buffer.advance(1000 / 60);
    assert.ok(visible.startsWith(previous));
    assert.ok(text.startsWith(visible));
    if (frames === 0)
      assert.ok(visible.length > 0 && visible.length < text.length);
    previous = visible;
    elapsed += 1000 / 60;
    frames++;
  }
  assert.equal(buffer.visible, text);
  assert.ok(frames > 10 && elapsed < 1000);
});

test("new batches retain progress; long provider gaps add no invented content", () => {
  const buffer = new TextReveal("");
  buffer.update("第一段".repeat(10), true);
  buffer.advance(16);
  const prefix = buffer.visible;
  buffer.update("第一段".repeat(10) + "第二段".repeat(10), true);
  assert.ok(buffer.advance(16).startsWith(prefix));
  for (let i = 0; i < 100; i++) buffer.advance(16);
  const caughtUp = buffer.visible;
  assert.equal(buffer.advance(2600), caughtUp);
  assert.equal(buffer.pending, false);
});

test("completion, cancel and reduced motion flush exactly and stop revealing", () => {
  for (const text of ["最终回复", "保留已收到的部分内容", "无需动画"]) {
    const buffer = new TextReveal("");
    buffer.update(text, true);
    buffer.advance(16);
    buffer.update(text, false);
    assert.equal(buffer.visible, text);
    assert.equal(buffer.pending, false);
    assert.equal(buffer.advance(1000), text);
  }
});

test("existing history is immediate and a replacement never leaks old text", () => {
  const buffer = new TextReveal("已有历史");
  buffer.update("已有历史", false);
  assert.equal(buffer.visible, "已有历史");
  buffer.update("已有历史加新回复", true);
  assert.equal(buffer.visible, "已有历史");
  buffer.update("校正后的文本", true);
  assert.equal(buffer.visible, "校正后的文本");
  assert.equal(buffer.pending, false);
});

test("reveals complete graphemes, including emoji and combining marks", () => {
  const buffer = new TextReveal("");
  const characters = ["👨‍👩‍👧‍👦", "e\u0301", "🪑", "中"];
  buffer.update(characters.join(""), true);
  const prefixes = new Set([
    "",
    ...characters.map((_, i) => characters.slice(0, i + 1).join("")),
  ]);
  while (buffer.pending) assert.ok(prefixes.has(buffer.advance(8)));
  assert.equal(buffer.visible, characters.join(""));
});
