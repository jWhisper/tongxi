import {
  Snapshot,
  SaveSettings,
  StartProbe,
  StopProbe,
  Workspace,
  SaveAgent,
  SetAgentEnabled,
  SaveConversation,
  Conversation,
  PostMessage,
  SendMessage,
  StopRun,
  StopChain,
  RetryRun,
  Schedule,
} from "../wailsjs/go/main/App";
import { EventsOn } from "../wailsjs/runtime/runtime";

export type Run = {
  id: string;
  source: string;
  prompt: string;
  status: string;
  text: string;
  error: string;
  tools: string[];
  startedAt: string;
  finishedAt: string;
  revision: number;
};
export type Settings = { baseURL: string; model: string; hasKey: boolean };
export const emptySettings: Settings = {
  baseURL: "",
  model: "",
  hasKey: false,
};
export const isDesktop = () =>
  Boolean((window as unknown as { go?: unknown }).go);
export const api = {
  snapshot: Snapshot,
  saveSettings: SaveSettings,
  start: StartProbe,
  stop: StopProbe,
  workspace: Workspace,
  saveAgent: SaveAgent,
  setAgentEnabled: SetAgentEnabled,
  saveConversation: SaveConversation,
  conversation: Conversation,
  postMessage: PostMessage,
  sendMessage: SendMessage,
  stopRun: StopRun,
  stopChain: StopChain,
  retryRun: RetryRun,
  schedule: Schedule,
};
export const subscribe = (callback: (run: Run) => void) =>
  EventsOn("probe:updated", callback);

// Full snapshots plus monotonic revisions avoid lost deltas and late-event rollback.
export function mergeRuns(previous: Run[], incoming: Run[]): Run[] {
  const map = new Map(previous.map((run) => [run.id, run]));
  for (const run of incoming) {
    if (!map.has(run.id) || map.get(run.id)!.revision < run.revision)
      map.set(run.id, run);
  }
  return [...map.values()].sort((a, b) =>
    a.startedAt.localeCompare(b.startedAt),
  );
}

import type { app, store } from "../wailsjs/go/models";
export type Agent = store.Agent;
export type AgentInput = app.AgentInput;
export type Conversation = store.Conversation;
export type ConversationDetail = Pick<
  app.ConversationDetail,
  "conversation" | "messages" | "runs" | "chains"
>;
export type ConversationRun = store.ConversationRun;
export type WorkspaceData = Pick<app.Workspace, "agents" | "conversations">;
export const subscribeWorkspace = (callback: () => void) =>
  EventsOn("workspace:changed", callback);

export const subscribeConversationRuns = (
  callback: (run: ConversationRun) => void,
) => EventsOn("conversation:run", callback);
export function mergeConversationRuns(
  previous: ConversationRun[],
  incoming: ConversationRun[],
) {
  const map = new Map(previous.map((run) => [run.id, run]));
  for (const run of incoming) {
    if (!map.has(run.id) || map.get(run.id)!.revision < run.revision)
      map.set(run.id, run);
  }
  return [...map.values()].sort((a, b) =>
    a.createdAt.localeCompare(b.createdAt),
  );
}
