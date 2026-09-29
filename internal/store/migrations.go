package store

var migrations = []string{
	`CREATE TABLE model_settings (id INTEGER PRIMARY KEY CHECK(id=1), base_url TEXT NOT NULL, model TEXT NOT NULL, key_ref TEXT NOT NULL);
	CREATE TABLE probe_runs (id TEXT PRIMARY KEY, source TEXT NOT NULL, prompt TEXT NOT NULL, status TEXT NOT NULL, text TEXT NOT NULL, error TEXT NOT NULL, tools TEXT NOT NULL, started_at TEXT NOT NULL, finished_at TEXT NOT NULL, revision INTEGER NOT NULL);`,
	`CREATE TABLE agents (
		id TEXT PRIMARY KEY, name TEXT NOT NULL, description TEXT NOT NULL, instruction TEXT NOT NULL,
		base_url TEXT NOT NULL, model TEXT NOT NULL, key_ref TEXT NOT NULL, tools TEXT NOT NULL,
		enabled INTEGER NOT NULL CHECK(enabled IN (0,1)), version INTEGER NOT NULL,
		created_at TEXT NOT NULL, updated_at TEXT NOT NULL
	);
	CREATE TABLE conversations (
		id TEXT PRIMARY KEY, title TEXT NOT NULL, kind TEXT NOT NULL CHECK(kind IN ('private','group')),
		mode TEXT NOT NULL CHECK(mode IN ('lead','discussion')), lead_agent_id TEXT REFERENCES agents(id),
		created_at TEXT NOT NULL, updated_at TEXT NOT NULL
	);
	CREATE TABLE conversation_members (
		conversation_id TEXT NOT NULL REFERENCES conversations(id), agent_id TEXT NOT NULL REFERENCES agents(id),
		position INTEGER NOT NULL, PRIMARY KEY(conversation_id,agent_id), UNIQUE(conversation_id,position)
	);
	CREATE TABLE messages (
		id TEXT PRIMARY KEY, conversation_id TEXT NOT NULL REFERENCES conversations(id), sequence INTEGER NOT NULL,
		sender_type TEXT NOT NULL CHECK(sender_type IN ('user','agent')), sender_id TEXT REFERENCES agents(id),
		sender_name TEXT NOT NULL, content TEXT NOT NULL, created_at TEXT NOT NULL,
		attachments TEXT NOT NULL DEFAULT '[]',
		UNIQUE(conversation_id,sequence), CHECK((sender_type='user' AND sender_id IS NULL) OR (sender_type='agent' AND sender_id IS NOT NULL))
	);
	CREATE TABLE runs (
		id TEXT PRIMARY KEY, conversation_id TEXT NOT NULL REFERENCES conversations(id), agent_id TEXT NOT NULL REFERENCES agents(id),
		message_id TEXT NOT NULL REFERENCES messages(id), status TEXT NOT NULL CHECK(status IN ('queued','running','completed','failed','cancelled','interrupted')),
		error TEXT NOT NULL, created_at TEXT NOT NULL
	);
	CREATE INDEX runs_conversation ON runs(conversation_id,created_at);`,
	`ALTER TABLE runs ADD COLUMN text TEXT NOT NULL DEFAULT '';
	ALTER TABLE runs ADD COLUMN tools TEXT NOT NULL DEFAULT '[]';
	ALTER TABLE runs ADD COLUMN started_at TEXT NOT NULL DEFAULT '';
	ALTER TABLE runs ADD COLUMN finished_at TEXT NOT NULL DEFAULT '';
	ALTER TABLE runs ADD COLUMN revision INTEGER NOT NULL DEFAULT 1;
	ALTER TABLE runs ADD COLUMN agent_name TEXT NOT NULL DEFAULT '';
	ALTER TABLE runs ADD COLUMN config TEXT NOT NULL DEFAULT '{}';
	ALTER TABLE runs ADD COLUMN transcript TEXT NOT NULL DEFAULT '[]';
	CREATE TABLE deliveries (
		request_id TEXT PRIMARY KEY, message_id TEXT NOT NULL UNIQUE REFERENCES messages(id),
		run_id TEXT NOT NULL UNIQUE REFERENCES runs(id)
	);
	CREATE INDEX runs_queue ON runs(status);
	CREATE INDEX runs_history ON runs(agent_id,conversation_id,status);`,
	`CREATE TABLE chains (
		id TEXT PRIMARY KEY, conversation_id TEXT NOT NULL REFERENCES conversations(id),
		message_id TEXT NOT NULL REFERENCES messages(id), mode TEXT NOT NULL, action TEXT NOT NULL,
		lead_agent_id TEXT NOT NULL, participants TEXT NOT NULL, reserved INTEGER NOT NULL CHECK(reserved BETWEEN 0 AND 6),
		status TEXT NOT NULL DEFAULT 'active', reason TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL
	);
	ALTER TABLE runs ADD COLUMN chain_id TEXT REFERENCES chains(id);
	ALTER TABLE runs ADD COLUMN parent_run_id TEXT REFERENCES runs(id);
	ALTER TABLE runs ADD COLUMN previous_run_id TEXT REFERENCES runs(id);
	ALTER TABLE runs ADD COLUMN input_messages TEXT NOT NULL DEFAULT '[]';
	ALTER TABLE runs ADD COLUMN read_from INTEGER NOT NULL DEFAULT 0;
	ALTER TABLE runs ADD COLUMN read_upper INTEGER NOT NULL DEFAULT 0;
	ALTER TABLE messages ADD COLUMN target_agent_id TEXT REFERENCES agents(id);
	ALTER TABLE messages ADD COLUMN reply_to_message_id TEXT REFERENCES messages(id);
	ALTER TABLE messages ADD COLUMN source_run_id TEXT REFERENCES runs(id);
	ALTER TABLE deliveries RENAME TO deliveries_v3;
	CREATE TABLE deliveries (
		request_id TEXT PRIMARY KEY, message_id TEXT NOT NULL UNIQUE REFERENCES messages(id),
		run_id TEXT UNIQUE REFERENCES runs(id), chain_id TEXT REFERENCES chains(id), payload TEXT NOT NULL DEFAULT ''
	);
	INSERT INTO deliveries(request_id,message_id,run_id) SELECT request_id,message_id,run_id FROM deliveries_v3;
	DROP TABLE deliveries_v3;
	CREATE TABLE tool_deliveries (
		run_id TEXT NOT NULL REFERENCES runs(id), tool_call_id TEXT NOT NULL,
		message_id TEXT NOT NULL REFERENCES messages(id), receiver_run_id TEXT NOT NULL REFERENCES runs(id),
		payload TEXT NOT NULL, PRIMARY KEY(run_id,tool_call_id)
	);
	CREATE TABLE member_cursors (
		conversation_id TEXT NOT NULL REFERENCES conversations(id), agent_id TEXT NOT NULL REFERENCES agents(id),
		sequence INTEGER NOT NULL, PRIMARY KEY(conversation_id,agent_id)
	);
	CREATE INDEX runs_chain ON runs(chain_id);
	CREATE INDEX runs_dependency ON runs(previous_run_id);`,
	`ALTER TABLE runs ADD COLUMN retry_of TEXT REFERENCES runs(id);
	CREATE UNIQUE INDEX runs_retry_once ON runs(retry_of) WHERE retry_of IS NOT NULL;
	CREATE TABLE retry_requests(request_id TEXT PRIMARY KEY, run_id TEXT NOT NULL REFERENCES runs(id));
	ALTER TABLE conversations ADD COLUMN revision INTEGER NOT NULL DEFAULT 1;
	ALTER TABLE chains ADD COLUMN conversation_revision INTEGER NOT NULL DEFAULT 1;`,
	`ALTER TABLE runs ADD COLUMN kind TEXT NOT NULL DEFAULT 'reply' CHECK(kind IN ('reply','selector'));
	ALTER TABLE runs ADD COLUMN silent INTEGER NOT NULL DEFAULT 0 CHECK(silent IN (0,1));`,
	`CREATE TABLE chains_v7 (
		id TEXT PRIMARY KEY, conversation_id TEXT NOT NULL REFERENCES conversations(id),
		message_id TEXT NOT NULL REFERENCES messages(id), mode TEXT NOT NULL, action TEXT NOT NULL,
		lead_agent_id TEXT NOT NULL, participants TEXT NOT NULL, reserved INTEGER NOT NULL CHECK(reserved BETWEEN 0 AND 13),
		status TEXT NOT NULL DEFAULT 'active', reason TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL,
		conversation_revision INTEGER NOT NULL DEFAULT 1,
		lead_policy INTEGER NOT NULL DEFAULT 0 CHECK(lead_policy IN (0,1)),
		work TEXT NOT NULL DEFAULT 'null', stalled INTEGER NOT NULL DEFAULT 0
	);
	INSERT INTO chains_v7(id,conversation_id,message_id,mode,action,lead_agent_id,participants,reserved,status,reason,created_at,conversation_revision)
		SELECT id,conversation_id,message_id,mode,action,lead_agent_id,participants,reserved,status,reason,created_at,conversation_revision FROM chains;
	DROP TABLE chains;
	ALTER TABLE chains_v7 RENAME TO chains;
	CREATE TABLE lead_steps (
		run_id TEXT PRIMARY KEY REFERENCES runs(id), payload TEXT NOT NULL,
		step TEXT NOT NULL, next_run_id TEXT REFERENCES runs(id)
	);`,
	`CREATE TABLE model_configs (
		id TEXT PRIMARY KEY, name TEXT NOT NULL, provider TEXT NOT NULL,
		base_url TEXT NOT NULL, model TEXT NOT NULL, key_ref TEXT NOT NULL,
		version INTEGER NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
		context_tokens INTEGER NOT NULL DEFAULT 32768, token_ratio REAL NOT NULL DEFAULT 1
	);
	INSERT INTO model_configs(id,name,provider,base_url,model,key_ref,version,created_at,updated_at)
	SELECT MIN(id),model,CASE
		WHEN base_url LIKE 'https://api.kimi.com/coding/%' THEN 'kimi-code'
		WHEN base_url LIKE 'https://api.moonshot.cn/%' THEN 'kimi'
		WHEN base_url LIKE 'https://api.deepseek.com%' THEN 'deepseek'
		WHEN base_url LIKE 'https://open.bigmodel.cn/%' THEN 'glm'
		ELSE 'compatible' END,
		base_url,model,key_ref,1,strftime('%Y-%m-%dT%H:%M:%fZ','now'),strftime('%Y-%m-%dT%H:%M:%fZ','now')
	FROM (
		SELECT 'model-default' AS id,base_url,model,key_ref FROM model_settings WHERE base_url<>'' AND model<>''
		UNION ALL SELECT 'model-'||id,base_url,model,key_ref FROM agents
	) GROUP BY base_url,model,key_ref;
	CREATE TABLE agents_v8 (
		id TEXT PRIMARY KEY, name TEXT NOT NULL, description TEXT NOT NULL, instruction TEXT NOT NULL,
		model_id TEXT NOT NULL REFERENCES model_configs(id), tools TEXT NOT NULL,
		enabled INTEGER NOT NULL CHECK(enabled IN (0,1)), version INTEGER NOT NULL,
		created_at TEXT NOT NULL, updated_at TEXT NOT NULL
	);
	INSERT INTO agents_v8 SELECT a.id,a.name,a.description,a.instruction,m.id,a.tools,a.enabled,a.version,a.created_at,a.updated_at
		FROM agents a JOIN model_configs m ON m.base_url=a.base_url AND m.model=a.model AND m.key_ref=a.key_ref;
	DROP TABLE agents;
	ALTER TABLE agents_v8 RENAME TO agents;`,
	`CREATE TABLE work_tasks (
		id TEXT PRIMARY KEY, conversation_id TEXT NOT NULL REFERENCES conversations(id),
		title TEXT NOT NULL, goal TEXT NOT NULL, current_chain_id TEXT NOT NULL REFERENCES chains(id),
		latest_version_id TEXT REFERENCES work_versions(id), revision INTEGER NOT NULL DEFAULT 1,
		created_at TEXT NOT NULL, updated_at TEXT NOT NULL
	);
	CREATE TABLE work_versions (
		id TEXT PRIMARY KEY, task_id TEXT NOT NULL REFERENCES work_tasks(id), number INTEGER NOT NULL,
		chain_id TEXT NOT NULL UNIQUE REFERENCES chains(id), base_version_id TEXT REFERENCES work_versions(id),
		step TEXT NOT NULL, request TEXT NOT NULL, summary TEXT NOT NULL, created_at TEXT NOT NULL,
		UNIQUE(task_id,number)
	);
	ALTER TABLE chains ADD COLUMN task_id TEXT REFERENCES work_tasks(id);
	ALTER TABLE chains ADD COLUMN base_version_id TEXT REFERENCES work_versions(id);
	ALTER TABLE chains ADD COLUMN task_revision INTEGER NOT NULL DEFAULT 0;
	ALTER TABLE chains ADD COLUMN basis TEXT NOT NULL DEFAULT 'null';
	ALTER TABLE chains ADD COLUMN revision_policy INTEGER NOT NULL DEFAULT 0;
	INSERT INTO work_tasks(id,conversation_id,title,goal,current_chain_id,created_at,updated_at)
		SELECT c.id,c.conversation_id,substr(m.content,1,60),m.content,c.id,c.created_at,c.created_at
		FROM chains c JOIN messages m ON m.id=c.message_id WHERE c.lead_policy=1;
	UPDATE chains SET task_id=id,task_revision=1 WHERE lead_policy=1;
	INSERT INTO work_versions(id,task_id,number,chain_id,step,request,summary,created_at)
		SELECT 'version_'||c.id,c.id,1,c.id,c.work,m.content,'升级前已交付成果',c.created_at
		FROM chains c JOIN messages m ON m.id=c.message_id
		WHERE c.lead_policy=1 AND c.status='completed' AND json_extract(c.work,'$.action')='complete';
	UPDATE work_tasks SET latest_version_id=(SELECT id FROM work_versions v WHERE v.task_id=work_tasks.id);
	CREATE INDEX work_tasks_conversation ON work_tasks(conversation_id,updated_at);`,
	`CREATE TABLE sources (
		id TEXT PRIMARY KEY, conversation_id TEXT NOT NULL REFERENCES conversations(id),
		name TEXT NOT NULL, kind TEXT NOT NULL, format TEXT NOT NULL, url TEXT NOT NULL,
		hash TEXT NOT NULL, size INTEGER NOT NULL, segments INTEGER NOT NULL, characters INTEGER NOT NULL,
		note TEXT NOT NULL, created_at TEXT NOT NULL,
		UNIQUE(conversation_id,kind,url,name,hash)
	);
	CREATE TABLE source_segments (
		source_id TEXT NOT NULL REFERENCES sources(id), number INTEGER NOT NULL,
		location TEXT NOT NULL, content TEXT NOT NULL, PRIMARY KEY(source_id,number)
	);
	CREATE TABLE source_reads (
		run_id TEXT NOT NULL REFERENCES runs(id), source_id TEXT NOT NULL REFERENCES sources(id), segment INTEGER NOT NULL,
		location TEXT NOT NULL, content TEXT NOT NULL,
		PRIMARY KEY(run_id,source_id,segment,content)
	);
	CREATE INDEX sources_conversation ON sources(conversation_id);`,
	`ALTER TABLE conversations ADD COLUMN work_dir TEXT NOT NULL DEFAULT '';
	CREATE TRIGGER conversations_work_dir_fixed BEFORE UPDATE OF work_dir ON conversations
	WHEN OLD.work_dir <> '' AND NEW.work_dir <> OLD.work_dir
	BEGIN SELECT RAISE(ABORT, 'work directory is immutable'); END;`,
	`CREATE TABLE skills (
		id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE, enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
		version INTEGER NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
	);
	CREATE TABLE skill_versions (
		skill_id TEXT NOT NULL REFERENCES skills(id), version INTEGER NOT NULL,
		name TEXT NOT NULL, description TEXT NOT NULL, content TEXT NOT NULL,
		resources TEXT NOT NULL, note TEXT NOT NULL, created_at TEXT NOT NULL,
		PRIMARY KEY(skill_id,version)
	);
	CREATE TABLE agent_skills (
		agent_id TEXT NOT NULL REFERENCES agents(id), skill_id TEXT NOT NULL REFERENCES skills(id),
		PRIMARY KEY(agent_id,skill_id)
	);
	CREATE TABLE skill_scopes (id TEXT PRIMARY KEY);
	CREATE TABLE skill_scope_bindings (
		scope_id TEXT NOT NULL REFERENCES skill_scopes(id), agent_id TEXT NOT NULL REFERENCES agents(id),
		skill_id TEXT NOT NULL, version INTEGER NOT NULL,
		PRIMARY KEY(scope_id,agent_id,skill_id), FOREIGN KEY(skill_id,version) REFERENCES skill_versions(skill_id,version)
	);
	CREATE TABLE skill_uses (
		run_id TEXT NOT NULL REFERENCES runs(id), skill_id TEXT NOT NULL, version INTEGER NOT NULL, created_at TEXT NOT NULL,
		PRIMARY KEY(run_id,skill_id), FOREIGN KEY(skill_id,version) REFERENCES skill_versions(skill_id,version)
	);`,
	`CREATE TABLE skills_v13 (
  id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE, description TEXT NOT NULL,
  enabled INTEGER NOT NULL CHECK(enabled IN (0,1)), content TEXT NOT NULL,
  resources TEXT NOT NULL, note TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
 );
 INSERT INTO skills_v13 SELECT s.id,s.name,v.description,s.enabled,v.content,v.resources,v.note,s.created_at,s.updated_at
  FROM skills s JOIN skill_versions v ON v.skill_id=s.id AND v.version=s.version;
 CREATE TABLE skill_uses_v13 (
  run_id TEXT NOT NULL REFERENCES runs(id), skill_id TEXT NOT NULL REFERENCES skills(id),
  name TEXT NOT NULL, created_at TEXT NOT NULL, PRIMARY KEY(run_id,skill_id)
 );
 INSERT INTO skill_uses_v13 SELECT u.run_id,u.skill_id,v.name,u.created_at FROM skill_uses u
  JOIN skill_versions v ON v.skill_id=u.skill_id AND v.version=u.version;
 DROP TABLE skill_uses;
 ALTER TABLE skill_uses_v13 RENAME TO skill_uses;
 DROP TABLE skill_scope_bindings;
 DROP TABLE skill_scopes;
 DROP TABLE skill_versions;
 DROP TABLE skills;
 ALTER TABLE skills_v13 RENAME TO skills;`,
	`ALTER TABLE skills ADD COLUMN scripts TEXT NOT NULL DEFAULT '[]';
 CREATE TABLE skill_script_runs (
  id TEXT PRIMARY KEY, run_id TEXT NOT NULL REFERENCES runs(id), skill_id TEXT NOT NULL REFERENCES skills(id),
  name TEXT NOT NULL, path TEXT NOT NULL, args TEXT NOT NULL, status TEXT NOT NULL,
  exit_code INTEGER NOT NULL DEFAULT -1, stdout TEXT NOT NULL DEFAULT '', stderr TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT '', files TEXT NOT NULL DEFAULT '[]',
  started_at TEXT NOT NULL, finished_at TEXT NOT NULL DEFAULT ''
 );`,
	`CREATE TABLE artifacts (
 id TEXT PRIMARY KEY, conversation_id TEXT NOT NULL REFERENCES conversations(id),
 run_id TEXT NOT NULL REFERENCES runs(id), script_run_id TEXT NOT NULL REFERENCES skill_script_runs(id),
 name TEXT NOT NULL, path TEXT NOT NULL, format TEXT NOT NULL, hash TEXT NOT NULL, size INTEGER NOT NULL,
 source_id TEXT NOT NULL REFERENCES sources(id), created_at TEXT NOT NULL, data BLOB NOT NULL,
 UNIQUE(script_run_id,path)
 );
 CREATE INDEX artifacts_conversation ON artifacts(conversation_id);
 CREATE TABLE context_states (
  conversation_id TEXT NOT NULL REFERENCES conversations(id), agent_id TEXT NOT NULL REFERENCES agents(id),
  kind TEXT NOT NULL, through_run INTEGER NOT NULL, public_upper INTEGER NOT NULL,
  messages TEXT NOT NULL, updated_at TEXT NOT NULL,
  PRIMARY KEY(conversation_id,agent_id,kind)
 );
 CREATE TABLE context_tool_results (
  id TEXT PRIMARY KEY, conversation_id TEXT NOT NULL REFERENCES conversations(id),
  agent_id TEXT NOT NULL REFERENCES agents(id), content TEXT NOT NULL
 );`,
}
