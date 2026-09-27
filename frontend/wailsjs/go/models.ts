export namespace app {
	
	export class AgentInput {
	    id: string;
	    name: string;
	    description: string;
	    instruction: string;
	    baseURL: string;
	    model: string;
	    apiKey: string;
	    tools: string[];
	    version: number;
	
	    static createFrom(source: any = {}) {
	        return new AgentInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.instruction = source["instruction"];
	        this.baseURL = source["baseURL"];
	        this.model = source["model"];
	        this.apiKey = source["apiKey"];
	        this.tools = source["tools"];
	        this.version = source["version"];
	    }
	}
	export class ConversationDetail {
	    chains: store.Chain[];
	    conversation: store.Conversation;
	    messages: store.Message[];
	    runs: store.ConversationRun[];
	
	    static createFrom(source: any = {}) {
	        return new ConversationDetail(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
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
	    agents: store.Agent[];
	    conversations: store.Conversation[];
	
	    static createFrom(source: any = {}) {
	        return new Workspace(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
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

export namespace store {
	
	export class Agent {
	    id: string;
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
	        this.id = source["id"];
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
	export class Chain {
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
	
	    static createFrom(source: any = {}) {
	        return new Chain(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
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
	    }
	}
	export class Conversation {
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
	export class Message {
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
	        this.requestID = source["requestID"];
	        this.conversationID = source["conversationID"];
	        this.content = source["content"];
	        this.action = source["action"];
	        this.agentIDs = source["agentIDs"];
	    }
	}

}

