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
}
