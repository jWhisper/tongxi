// Support Chinese text immediately before @ while leaving email addresses alone.
export function mentionQuery(text: string, caret: number) {
  const match = /(?:^|[^A-Za-z0-9._%+@-])@([^@\n]{0,60})$/.exec(text.slice(0, caret));
  return match ? { start: caret - match[1].length - 1, end: caret, query: match[1].trim() } : null;
}

export function messageRoute(kind: string, mode: string, recipient: string) {
  return kind === "private"
    ? { action: "direct", agentIDs: [] as string[] }
    : recipient
      ? { action: "mention", agentIDs: [recipient] }
      : { action: mode === "discussion" ? "discussion" : "lead", agentIDs: [] as string[] };
}
