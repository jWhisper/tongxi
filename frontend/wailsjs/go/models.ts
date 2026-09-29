export namespace app {

	export class AgentInput {
	    skillIDs: string[];
	    id: string;
	    name: string;
	    description: string;
	    instruction: string;
	    modelID: string;
	    tools: string[];
	    version: number;

	    static createFrom(source: any = {}) {
	        return new AgentInput(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.skillIDs = source["skillIDs"];
	        this.id = source["id"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.instruction = source["instruction"];
	        this.modelID = source["modelID"];
	        this.tools = source["tools"];
	        this.version = source["version"];
	    }
	}
	export class ConnectionResult {
	    latencyMS: number;

	    static createFrom(source: any = {}) {
	        return new ConnectionResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.latencyMS = source["latencyMS"];
	    }
	}
	export class ConversationDetail {
	    imageReads: store.ImageRead[];
	    artifacts: store.Artifact[];
	    scriptRuns: store.ScriptRun[];
	    skillUses: store.SkillUse[];
	    sources: store.Source[];
	    tasks: store.WorkTask[];
	    versions: store.WorkVersion[];
	    leadSteps: Record<string, store.LeadStep>;
	    chains: store.Chain[];
	    conversation: store.Conversation;
	    messages: store.Message[];
	    runs: store.ConversationRun[];

	    static createFrom(source: any = {}) {
	        return new ConversationDetail(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.imageReads = this.convertValues(source["imageReads"], store.ImageRead);
	        this.artifacts = this.convertValues(source["artifacts"], store.Artifact);
	        this.scriptRuns = this.convertValues(source["scriptRuns"], store.ScriptRun);
	        this.skillUses = this.convertValues(source["skillUses"], store.SkillUse);
	        this.sources = this.convertValues(source["sources"], store.Source);
	        this.tasks = this.convertValues(source["tasks"], store.WorkTask);
	        this.versions = this.convertValues(source["versions"], store.WorkVersion);
	        this.leadSteps = this.convertValues(source["leadSteps"], store.LeadStep, true);
	        this.chains = this.convertValues(source["chains"], store.Chain);
	        this.conversation = this.convertValues(source["conversation"], store.Conversation);
	        this.messages = this.convertValues(source["messages"], store.Message);
	        this.runs = this.convertValues(source["runs"], store.ConversationRun);
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ImportResult {
	    sources: store.Source[];
	    errors: string[];

	    static createFrom(source: any = {}) {
	        return new ImportResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sources = this.convertValues(source["sources"], store.Source);
	        this.errors = source["errors"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ModelInput {
	    contextTokens: number;
	    id: string;
	    name: string;
	    provider: string;
	    baseURL: string;
	    model: string;
	    apiKey: string;
	    version: number;

	    static createFrom(source: any = {}) {
	        return new ModelInput(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.contextTokens = source["contextTokens"];
	        this.id = source["id"];
	        this.name = source["name"];
	        this.provider = source["provider"];
	        this.baseURL = source["baseURL"];
	        this.model = source["model"];
	        this.apiKey = source["apiKey"];
	        this.version = source["version"];
	    }
	}
	export class SettingsInput {
	    baseURL: string;
	    model: string;
	    apiKey: string;

	    static createFrom(source: any = {}) {
	        return new SettingsInput(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.baseURL = source["baseURL"];
	        this.model = source["model"];
	        this.apiKey = source["apiKey"];
	    }
	}
	export class SettingsView {
	    baseURL: string;
	    model: string;
	    hasKey: boolean;

	    static createFrom(source: any = {}) {
	        return new SettingsView(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.baseURL = source["baseURL"];
	        this.model = source["model"];
	        this.hasKey = source["hasKey"];
	    }
	}
	export class SkillInput {
	    id: string;
	    updatedAt: string;
	    enabled: boolean;
	    content: string;
	    resources: skill.Resource[];
	    scripts: skill.Resource[];
	    note: string;

	    static createFrom(source: any = {}) {
	        return new SkillInput(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.updatedAt = source["updatedAt"];
	        this.enabled = source["enabled"];
	        this.content = source["content"];
	        this.resources = this.convertValues(source["resources"], skill.Resource);
	        this.scripts = this.convertValues(source["scripts"], skill.Resource);
	        this.note = source["note"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Snapshot {
	    settings: SettingsView;
	    runs: store.ProbeRun[];

	    static createFrom(source: any = {}) {
	        return new Snapshot(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.settings = this.convertValues(source["settings"], SettingsView);
	        this.runs = this.convertValues(source["runs"], store.ProbeRun);
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Workspace {
	    skills: store.Skill[];
	    models: store.ModelConfig[];
	    agents: store.Agent[];
	    conversations: store.Conversation[];

	    static createFrom(source: any = {}) {
	        return new Workspace(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.skills = this.convertValues(source["skills"], store.Skill);
	        this.models = this.convertValues(source["models"], store.ModelConfig);
	        this.agents = this.convertValues(source["agents"], store.Agent);
	        this.conversations = this.convertValues(source["conversations"], store.Conversation);
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace material {

	export class ImagePreview {
	    dataURL: string;
	    width: number;
	    height: number;

	    static createFrom(source: any = {}) {
	        return new ImagePreview(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.dataURL = source["dataURL"];
	        this.width = source["width"];
	        this.height = source["height"];
	    }
	}
	export class WorkspaceFile {
	    name: string;
	    path: string;
	    directory: boolean;
	    size: number;

	    static createFrom(source: any = {}) {
	        return new WorkspaceFile(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.directory = source["directory"];
	        this.size = source["size"];
	    }
	}
	export class WorkspacePage {
	    path: string;
	    files: WorkspaceFile[];
	    next: number;

	    static createFrom(source: any = {}) {
	        return new WorkspacePage(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.files = this.convertValues(source["files"], WorkspaceFile);
	        this.next = source["next"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace skill {

	export class Resource {
	    path: string;
	    content: string;

	    static createFrom(source: any = {}) {
	        return new Resource(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.content = source["content"];
	    }
	}
	export class Bundle {
	    content: string;
	    resources: Resource[];
	    scripts: Resource[];
	    note: string;

	    static createFrom(source: any = {}) {
	        return new Bundle(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.content = source["content"];
	        this.resources = this.convertValues(source["resources"], Resource);
	        this.scripts = this.convertValues(source["scripts"], Resource);
	        this.note = source["note"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace store {

	export class AcceptanceCheck {
	    criterion: string;
	    status: string;
	    evidence: string;

	    static createFrom(source: any = {}) {
	        return new AcceptanceCheck(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.criterion = source["criterion"];
	        this.status = source["status"];
	        this.evidence = source["evidence"];
	    }
	}
	export class Agent {
	    skillIDs: string[];
	    id: string;
	    modelID: string;
	    modelName: string;
	    name: string;
	    description: string;
	    instruction: string;
	    baseURL: string;
	    model: string;
	    hasKey: boolean;
	    tools: string[];
	    enabled: boolean;
	    version: number;
	    createdAt: string;
	    updatedAt: string;

	    static createFrom(source: any = {}) {
	        return new Agent(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.skillIDs = source["skillIDs"];
	        this.id = source["id"];
	        this.modelID = source["modelID"];
	        this.modelName = source["modelName"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.instruction = source["instruction"];
	        this.baseURL = source["baseURL"];
	        this.model = source["model"];
	        this.hasKey = source["hasKey"];
	        this.tools = source["tools"];
	        this.enabled = source["enabled"];
	        this.version = source["version"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	    }
	}
	export class Artifact {
	    id: string;
	    conversationID: string;
	    runID: string;
	    scriptRunID: string;
	    name: string;
	    path: string;
	    format: string;
	    hash: string;
	    size: number;
	    sourceID: string;
	    createdAt: string;

	    static createFrom(source: any = {}) {
	        return new Artifact(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.conversationID = source["conversationID"];
	        this.runID = source["runID"];
	        this.scriptRunID = source["scriptRunID"];
	        this.name = source["name"];
	        this.path = source["path"];
	        this.format = source["format"];
	        this.hash = source["hash"];
	        this.size = source["size"];
	        this.sourceID = source["sourceID"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class ArtifactDelivery {
	    artifact_id: string;
	    evidence: string;

	    static createFrom(source: any = {}) {
	        return new ArtifactDelivery(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.artifact_id = source["artifact_id"];
	        this.evidence = source["evidence"];
	    }
	}
	export class RequirementChange {
	    index: number;
	    criterion: string;
	    quote: string;

	    static createFrom(source: any = {}) {
	        return new RequirementChange(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.index = source["index"];
	        this.criterion = source["criterion"];
	        this.quote = source["quote"];
	    }
	}
	export class Citation {
	    name?: string;
	    location?: string;
	    source_id: string;
	    segment: number;
	    quote: string;

	    static createFrom(source: any = {}) {
	        return new Citation(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.location = source["location"];
	        this.source_id = source["source_id"];
	        this.segment = source["segment"];
	        this.quote = source["quote"];
	    }
	}
	export class LeadStep {
	    files?: ArtifactDelivery[];
	    citations?: Citation[];
	    work_mode?: string;
	    title?: string;
	    brief?: string;
	    change_summary?: string;
	    changes?: RequirementChange[];
	    questions?: string[];
	    action: string;
	    checks: AcceptanceCheck[];
	    result: string;
	    reason: string;
	    next_agent_id: string;
	    task: string;

	    static createFrom(source: any = {}) {
	        return new LeadStep(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.files = this.convertValues(source["files"], ArtifactDelivery);
	        this.citations = this.convertValues(source["citations"], Citation);
	        this.work_mode = source["work_mode"];
	        this.title = source["title"];
	        this.brief = source["brief"];
	        this.change_summary = source["change_summary"];
	        this.changes = this.convertValues(source["changes"], RequirementChange);
	        this.questions = source["questions"];
	        this.action = source["action"];
	        this.checks = this.convertValues(source["checks"], AcceptanceCheck);
	        this.result = source["result"];
	        this.reason = source["reason"];
	        this.next_agent_id = source["next_agent_id"];
	        this.task = source["task"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class WorkTask {
	    id: string;
	    conversationID: string;
	    title: string;
	    goal: string;
	    currentChainID: string;
	    latestVersionID: string;
	    revision: number;
	    createdAt: string;
	    updatedAt: string;

	    static createFrom(source: any = {}) {
	        return new WorkTask(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.conversationID = source["conversationID"];
	        this.title = source["title"];
	        this.goal = source["goal"];
	        this.currentChainID = source["currentChainID"];
	        this.latestVersionID = source["latestVersionID"];
	        this.revision = source["revision"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	    }
	}
	export class WorkBasis {
	    task: WorkTask;
	    versionID: string;
	    versionNumber: number;
	    work?: LeadStep;
	    previousRequest: string;
	    explicit: boolean;

	    static createFrom(source: any = {}) {
	        return new WorkBasis(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.task = this.convertValues(source["task"], WorkTask);
	        this.versionID = source["versionID"];
	        this.versionNumber = source["versionNumber"];
	        this.work = this.convertValues(source["work"], LeadStep);
	        this.previousRequest = source["previousRequest"];
	        this.explicit = source["explicit"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Chain {
	    timeBudgetMinutes: number;
	    tokenBudget: number;
	    taskID: string;
	    baseVersionID: string;
	    taskRevision: number;
	    basis?: WorkBasis;
	    revisionPolicy: number;
	    id: string;
	    conversationID: string;
	    messageID: string;
	    mode: string;
	    action: string;
	    leadAgentID: string;
	    participants: string[];
	    reserved: number;
	    status: string;
	    reason: string;
	    createdAt: string;
	    leadPolicy: number;
	    work?: LeadStep;
	    stalled: number;

	    static createFrom(source: any = {}) {
	        return new Chain(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.timeBudgetMinutes = source["timeBudgetMinutes"];
	        this.tokenBudget = source["tokenBudget"];
	        this.taskID = source["taskID"];
	        this.baseVersionID = source["baseVersionID"];
	        this.taskRevision = source["taskRevision"];
	        this.basis = this.convertValues(source["basis"], WorkBasis);
	        this.revisionPolicy = source["revisionPolicy"];
	        this.id = source["id"];
	        this.conversationID = source["conversationID"];
	        this.messageID = source["messageID"];
	        this.mode = source["mode"];
	        this.action = source["action"];
	        this.leadAgentID = source["leadAgentID"];
	        this.participants = source["participants"];
	        this.reserved = source["reserved"];
	        this.status = source["status"];
	        this.reason = source["reason"];
	        this.createdAt = source["createdAt"];
	        this.leadPolicy = source["leadPolicy"];
	        this.work = this.convertValues(source["work"], LeadStep);
	        this.stalled = source["stalled"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

	export class Conversation {
	    timeBudgetMinutes: number;
	    tokenBudget: number;
	    workDir: string;
	    id: string;
	    title: string;
	    kind: string;
	    mode: string;
	    leadAgentID: string;
	    memberIDs: string[];
	    createdAt: string;
	    updatedAt: string;

	    static createFrom(source: any = {}) {
	        return new Conversation(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.timeBudgetMinutes = source["timeBudgetMinutes"];
	        this.tokenBudget = source["tokenBudget"];
	        this.workDir = source["workDir"];
	        this.id = source["id"];
	        this.title = source["title"];
	        this.kind = source["kind"];
	        this.mode = source["mode"];
	        this.leadAgentID = source["leadAgentID"];
	        this.memberIDs = source["memberIDs"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	    }
	}
	export class ConversationRun {
	    cachedTokens: number;
	    inputTokens: number;
	    outputTokens: number;
	    usageEstimated: boolean;
	    contextCompacting: boolean;
	    activeScript: string;
	    kind: string;
	    silent: boolean;
	    retryOf: string;
	    chainID: string;
	    parentRunID: string;
	    previousRunID: string;
	    id: string;
	    conversationID: string;
	    agentID: string;
	    messageID: string;
	    status: string;
	    error: string;
	    createdAt: string;
	    text: string;
	    tools: string[];
	    startedAt: string;
	    finishedAt: string;
	    revision: number;
	    agentName: string;

	    static createFrom(source: any = {}) {
	        return new ConversationRun(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.cachedTokens = source["cachedTokens"];
	        this.inputTokens = source["inputTokens"];
	        this.outputTokens = source["outputTokens"];
	        this.usageEstimated = source["usageEstimated"];
	        this.contextCompacting = source["contextCompacting"];
	        this.activeScript = source["activeScript"];
	        this.kind = source["kind"];
	        this.silent = source["silent"];
	        this.retryOf = source["retryOf"];
	        this.chainID = source["chainID"];
	        this.parentRunID = source["parentRunID"];
	        this.previousRunID = source["previousRunID"];
	        this.id = source["id"];
	        this.conversationID = source["conversationID"];
	        this.agentID = source["agentID"];
	        this.messageID = source["messageID"];
	        this.status = source["status"];
	        this.error = source["error"];
	        this.createdAt = source["createdAt"];
	        this.text = source["text"];
	        this.tools = source["tools"];
	        this.startedAt = source["startedAt"];
	        this.finishedAt = source["finishedAt"];
	        this.revision = source["revision"];
	        this.agentName = source["agentName"];
	    }
	}
	export class Delivery {
	    messageID: string;
	    chainID: string;
	    runs: ConversationRun[];

	    static createFrom(source: any = {}) {
	        return new Delivery(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.messageID = source["messageID"];
	        this.chainID = source["chainID"];
	        this.runs = this.convertValues(source["runs"], ConversationRun);
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ImageRead {
	    runID: string;
	    sourceID: string;
	    name: string;
	    hash: string;
	    width: number;
	    height: number;
	    createdAt: string;

	    static createFrom(source: any = {}) {
	        return new ImageRead(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.runID = source["runID"];
	        this.sourceID = source["sourceID"];
	        this.name = source["name"];
	        this.hash = source["hash"];
	        this.width = source["width"];
	        this.height = source["height"];
	        this.createdAt = source["createdAt"];
	    }
	}

	export class Source {
	    id: string;
	    conversationID: string;
	    name: string;
	    kind: string;
	    format: string;
	    url: string;
	    hash: string;
	    size: number;
	    segments: number;
	    characters: number;
	    note: string;
	    createdAt: string;

	    static createFrom(source: any = {}) {
	        return new Source(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.conversationID = source["conversationID"];
	        this.name = source["name"];
	        this.kind = source["kind"];
	        this.format = source["format"];
	        this.url = source["url"];
	        this.hash = source["hash"];
	        this.size = source["size"];
	        this.segments = source["segments"];
	        this.characters = source["characters"];
	        this.note = source["note"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class Message {
	    attachments: Source[];
	    targetAgentID: string;
	    replyToMessageID: string;
	    sourceRunID: string;
	    id: string;
	    conversationID: string;
	    sequence: number;
	    senderType: string;
	    senderID: string;
	    senderName: string;
	    content: string;
	    createdAt: string;

	    static createFrom(source: any = {}) {
	        return new Message(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.attachments = this.convertValues(source["attachments"], Source);
	        this.targetAgentID = source["targetAgentID"];
	        this.replyToMessageID = source["replyToMessageID"];
	        this.sourceRunID = source["sourceRunID"];
	        this.id = source["id"];
	        this.conversationID = source["conversationID"];
	        this.sequence = source["sequence"];
	        this.senderType = source["senderType"];
	        this.senderID = source["senderID"];
	        this.senderName = source["senderName"];
	        this.content = source["content"];
	        this.createdAt = source["createdAt"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ModelConfig {
	    contextTokens: number;
	    id: string;
	    name: string;
	    provider: string;
	    baseURL: string;
	    model: string;
	    hasKey: boolean;
	    version: number;
	    agentCount: number;

	    static createFrom(source: any = {}) {
	        return new ModelConfig(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.contextTokens = source["contextTokens"];
	        this.id = source["id"];
	        this.name = source["name"];
	        this.provider = source["provider"];
	        this.baseURL = source["baseURL"];
	        this.model = source["model"];
	        this.hasKey = source["hasKey"];
	        this.version = source["version"];
	        this.agentCount = source["agentCount"];
	    }
	}
	export class ProbeRun {
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

	    static createFrom(source: any = {}) {
	        return new ProbeRun(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.source = source["source"];
	        this.prompt = source["prompt"];
	        this.status = source["status"];
	        this.text = source["text"];
	        this.error = source["error"];
	        this.tools = source["tools"];
	        this.startedAt = source["startedAt"];
	        this.finishedAt = source["finishedAt"];
	        this.revision = source["revision"];
	    }
	}

	export class ScheduleRequest {
	    attachmentIDs?: string[];
	    baseVersionID?: string;
	    requestID: string;
	    conversationID: string;
	    content: string;
	    action: string;
	    agentIDs: string[];

	    static createFrom(source: any = {}) {
	        return new ScheduleRequest(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.attachmentIDs = source["attachmentIDs"];
	        this.baseVersionID = source["baseVersionID"];
	        this.requestID = source["requestID"];
	        this.conversationID = source["conversationID"];
	        this.content = source["content"];
	        this.action = source["action"];
	        this.agentIDs = source["agentIDs"];
	    }
	}
	export class ScriptRun {
	    id: string;
	    runID: string;
	    skillID: string;
	    name: string;
	    path: string;
	    args: string[];
	    status: string;
	    exitCode: number;
	    stdout: string;
	    stderr: string;
	    error: string;
	    files: string[];
	    startedAt: string;
	    finishedAt: string;

	    static createFrom(source: any = {}) {
	        return new ScriptRun(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.runID = source["runID"];
	        this.skillID = source["skillID"];
	        this.name = source["name"];
	        this.path = source["path"];
	        this.args = source["args"];
	        this.status = source["status"];
	        this.exitCode = source["exitCode"];
	        this.stdout = source["stdout"];
	        this.stderr = source["stderr"];
	        this.error = source["error"];
	        this.files = source["files"];
	        this.startedAt = source["startedAt"];
	        this.finishedAt = source["finishedAt"];
	    }
	}
	export class Skill {
	    id: string;
	    name: string;
	    description: string;
	    enabled: boolean;
	    updatedAt: string;
	    agentNames: string[];

	    static createFrom(source: any = {}) {
	        return new Skill(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.enabled = source["enabled"];
	        this.updatedAt = source["updatedAt"];
	        this.agentNames = source["agentNames"];
	    }
	}
	export class SkillContent {
	    id: string;
	    name: string;
	    description: string;
	    enabled: boolean;
	    content: string;
	    resources: skill.Resource[];
	    scripts: skill.Resource[];
	    note: string;
	    updatedAt: string;

	    static createFrom(source: any = {}) {
	        return new SkillContent(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.enabled = source["enabled"];
	        this.content = source["content"];
	        this.resources = this.convertValues(source["resources"], skill.Resource);
	        this.scripts = this.convertValues(source["scripts"], skill.Resource);
	        this.note = source["note"];
	        this.updatedAt = source["updatedAt"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SkillUse {
	    runID: string;
	    skillID: string;
	    name: string;
	    createdAt: string;

	    static createFrom(source: any = {}) {
	        return new SkillUse(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.runID = source["runID"];
	        this.skillID = source["skillID"];
	        this.name = source["name"];
	        this.createdAt = source["createdAt"];
	    }
	}

	export class SourceSegment {
	    link?: string;
	    number: number;
	    location: string;
	    content: string;

	    static createFrom(source: any = {}) {
	        return new SourceSegment(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.link = source["link"];
	        this.number = source["number"];
	        this.location = source["location"];
	        this.content = source["content"];
	    }
	}
	export class SourcePage {
	    source: Source;
	    segments: SourceSegment[];
	    next: number;

	    static createFrom(source: any = {}) {
	        return new SourcePage(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source = this.convertValues(source["source"], Source);
	        this.segments = this.convertValues(source["segments"], SourceSegment);
	        this.next = source["next"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}



	export class WorkVersion {
	    id: string;
	    taskID: string;
	    number: number;
	    chainID: string;
	    baseVersionID: string;
	    step: LeadStep;
	    request: string;
	    summary: string;
	    createdAt: string;

	    static createFrom(source: any = {}) {
	        return new WorkVersion(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.taskID = source["taskID"];
	        this.number = source["number"];
	        this.chainID = source["chainID"];
	        this.baseVersionID = source["baseVersionID"];
	        this.step = this.convertValues(source["step"], LeadStep);
	        this.request = source["request"];
	        this.summary = source["summary"];
	        this.createdAt = source["createdAt"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

