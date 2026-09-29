import {
	Skills,
	SaveSkill,
	SkillContent,
	ImportSkill,
  Models,
  SaveModel,
  DeleteModel,
  TestModel,
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
  ImportSources,
  ReadSource,
  AddWebSource,
  ExportVersion,
  ChooseWorkspaceDirectory,
  OpenWorkspaceDirectory,
  ListWorkspaceFiles,
  ReadWorkspaceFile,
  OpenArtifact,
  OpenSourceFile,
  ImportImage,
  ImagePreview,
} from "../wailsjs/go/main/App";
import { EventsOn } from "../wailsjs/runtime/runtime";

export const isDesktop = () =>
  Boolean((window as unknown as { go?: unknown }).go);
export const api = {
	skills: Skills,
  saveSkill: (input: SkillInput) => SaveSkill(new app.SkillInput(input)),
	skillContent: SkillContent,
	importSkill: ImportSkill,
  models: Models,
  saveModel: SaveModel,
  deleteModel: DeleteModel,
  testModel: TestModel,
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
  importSources: ImportSources,
  readSource: ReadSource,
  addWebSource: AddWebSource,
  exportVersion: ExportVersion,
  chooseWorkspaceDirectory: ChooseWorkspaceDirectory,
  openWorkspaceDirectory: OpenWorkspaceDirectory,
  listWorkspaceFiles: ListWorkspaceFiles,
  readWorkspaceFile: ReadWorkspaceFile,
  openArtifact: OpenArtifact,
  openSourceFile: OpenSourceFile,
  importImage: ImportImage,
  imagePreview: ImagePreview,
};
import { app } from "../wailsjs/go/models";
import type { store, material } from "../wailsjs/go/models";
export type WorkspacePage = material.WorkspacePage;
export type Skill = store.Skill;
export type SkillInput = Pick<app.SkillInput, "id" | "updatedAt" | "enabled" | "content" | "resources" | "scripts" | "note">;
export type SkillUse = store.SkillUse;
export type ScriptRun = store.ScriptRun;
export type Artifact = store.Artifact;
export type ArtifactDelivery = store.ArtifactDelivery;
export type ModelConfig = store.ModelConfig;
export type ModelInput = app.ModelInput;
export type Agent = store.Agent;
export type AgentInput = app.AgentInput;
export type Conversation = store.Conversation;
export type ConversationDetail = Pick<
  app.ConversationDetail,
  | "conversation"
  | "messages"
  | "runs"
  | "chains"
  | "leadSteps"
  | "tasks"
  | "versions"
  | "sources"
  | "skillUses"
  | "scriptRuns"
  | "artifacts"
  | "imageReads"
>;
export type Source = store.Source;
export type SourcePage = store.SourcePage;
export type Citation = store.Citation;
export type WorkTask = store.WorkTask;
export type WorkVersion = store.WorkVersion;
export type Chain = store.Chain;
export type LeadStep = store.LeadStep;
export type Message = store.Message;
export type ConversationRun = store.ConversationRun;
export type WorkspaceData = Pick<
  app.Workspace,
  "agents" | "conversations" | "models" | "skills"
>;
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
