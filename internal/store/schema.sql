CREATE TABLE IF NOT EXISTS schema_versions (version int PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE IF NOT EXISTS workspaces (id text PRIMARY KEY);
CREATE TABLE IF NOT EXISTS users (id text PRIMARY KEY, workspace_id text NOT NULL REFERENCES workspaces(id), invite_hash bytea NOT NULL UNIQUE, display_name text NOT NULL);
CREATE TABLE IF NOT EXISTS sessions (token_hash bytea PRIMARY KEY, user_id text NOT NULL REFERENCES users(id), expires_at timestamptz NOT NULL);
CREATE TABLE IF NOT EXISTS matters (
 workspace_id text NOT NULL REFERENCES workspaces(id), id text NOT NULL, title text NOT NULL, source text NOT NULL,
 category text NOT NULL CHECK(category IN ('life','travel','work')), status text NOT NULL CHECK(status IN ('ACTIVE','COMPLETED')),
 revision int NOT NULL DEFAULT 1 CHECK(revision>0), items jsonb NOT NULL DEFAULT '[]', deadline timestamptz, created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(workspace_id,id)
);
CREATE TABLE IF NOT EXISTS reminders (
 workspace_id text NOT NULL, id text NOT NULL, matter_id text NOT NULL, revision int NOT NULL DEFAULT 1,
 nominal_at timestamptz NOT NULL, due_at timestamptz NOT NULL, quiet boolean NOT NULL, enabled boolean NOT NULL DEFAULT true,
 sync_status text NOT NULL DEFAULT 'PENDING_SYNC', PRIMARY KEY(workspace_id,id), UNIQUE(workspace_id,matter_id),
 FOREIGN KEY(workspace_id,matter_id) REFERENCES matters(workspace_id,id)
);
CREATE TABLE IF NOT EXISTS runs (
 workspace_id text NOT NULL, id text NOT NULL, matter_id text NOT NULL, matter_revision int NOT NULL, snapshot_title text NOT NULL, snapshot_source text NOT NULL,
 status text NOT NULL CHECK(status IN ('QUEUED','RUNNING','SUCCEEDED','FAILED')), mode text NOT NULL, result jsonb,
 error_code text NOT NULL DEFAULT '', attempt_status text NOT NULL DEFAULT 'NONE', reserved_micro_cny bigint NOT NULL DEFAULT 0,
 charged_micro_cny bigint NOT NULL DEFAULT 0, input_tokens bigint NOT NULL DEFAULT 0, output_tokens bigint NOT NULL DEFAULT 0, budget_day date,
 model_profile text NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(workspace_id,id), FOREIGN KEY(workspace_id,matter_id) REFERENCES matters(workspace_id,id)
);
CREATE UNIQUE INDEX IF NOT EXISTS active_matter_run ON runs(workspace_id,matter_id) WHERE status IN ('QUEUED','RUNNING');
ALTER TABLE runs ADD COLUMN IF NOT EXISTS budget_day date;
UPDATE runs SET budget_day=(updated_at AT TIME ZONE 'Asia/Shanghai')::date WHERE budget_day IS NULL AND attempt_status<>'NONE';
CREATE TABLE IF NOT EXISTS outbox (
 id text PRIMARY KEY, workspace_id text NOT NULL, kind text NOT NULL, subject_id text NOT NULL, revision int NOT NULL DEFAULT 0, due_at timestamptz,
 state text NOT NULL DEFAULT 'PENDING', leased_until timestamptz, lease_token text, attempts int NOT NULL DEFAULT 0, available_at timestamptz NOT NULL DEFAULT now(), last_error text NOT NULL DEFAULT '',
 UNIQUE(workspace_id,kind,subject_id,revision)
);
CREATE INDEX IF NOT EXISTS pending_outbox ON outbox(available_at) WHERE state='PENDING';
CREATE TABLE IF NOT EXISTS commands (
 workspace_id text NOT NULL REFERENCES workspaces(id), key text NOT NULL, route text NOT NULL, body_hash bytea NOT NULL, response jsonb NOT NULL, status int NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(workspace_id,key)
);
CREATE TABLE IF NOT EXISTS notifications (
 workspace_id text NOT NULL, id text NOT NULL, reminder_id text NOT NULL, revision int NOT NULL, matter_id text NOT NULL,
 title text NOT NULL, status text NOT NULL CHECK(status IN ('AVAILABLE','OVERDUE')), created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(workspace_id,id), UNIQUE(workspace_id,reminder_id,revision), FOREIGN KEY(workspace_id,reminder_id) REFERENCES reminders(workspace_id,id)
);
CREATE TABLE IF NOT EXISTS business_events (
 sequence bigserial PRIMARY KEY, workspace_id text NOT NULL REFERENCES workspaces(id), kind text NOT NULL, subject_id text NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS scoped_events ON business_events(workspace_id,sequence);
INSERT INTO schema_versions(version) VALUES(1) ON CONFLICT DO NOTHING;
INSERT INTO schema_versions(version) VALUES(2) ON CONFLICT DO NOTHING;
ALTER TABLE reminders ADD COLUMN IF NOT EXISTS repeat text NOT NULL DEFAULT 'once' CHECK(repeat IN ('once','daily','weekdays','weekly'));
ALTER TABLE reminders ADD COLUMN IF NOT EXISTS repeat_until timestamptz;
CREATE TABLE IF NOT EXISTS memories (
 workspace_id text NOT NULL REFERENCES workspaces(id), id text NOT NULL, category text NOT NULL CHECK(category IN ('general','life','travel','work')),
 content text NOT NULL, revision int NOT NULL DEFAULT 1 CHECK(revision>0), updated_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(workspace_id,id)
);
ALTER TABLE runs ADD COLUMN IF NOT EXISTS memory_refs jsonb NOT NULL DEFAULT '[]';
ALTER TABLE runs ADD COLUMN IF NOT EXISTS used_memory_count int NOT NULL DEFAULT 0;
INSERT INTO schema_versions(version) VALUES(3) ON CONFLICT DO NOTHING;
CREATE TABLE IF NOT EXISTS connector_dispatches (
 workspace_id text NOT NULL REFERENCES workspaces(id), key text NOT NULL, connector_id text NOT NULL,
 body_hash bytea NOT NULL, status text NOT NULL CHECK(status IN ('SENDING','DELIVERED','REJECTED','UNKNOWN')),
 created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(workspace_id,key)
);
CREATE INDEX IF NOT EXISTS connector_dispatch_quota ON connector_dispatches(workspace_id,created_at);
INSERT INTO schema_versions(version) VALUES(4) ON CONFLICT DO NOTHING;
CREATE TABLE IF NOT EXISTS personal_models (
 workspace_id text NOT NULL REFERENCES workspaces(id), id text NOT NULL, label text NOT NULL, provider text NOT NULL, model text NOT NULL,
 input_price bigint NOT NULL CHECK(input_price>0 AND input_price<=1000000000), output_price bigint NOT NULL CHECK(output_price>0 AND output_price<=1000000000),
 credential bytea NOT NULL, revoked boolean NOT NULL DEFAULT false, verified_at timestamptz, created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(workspace_id,id)
);
CREATE TABLE IF NOT EXISTS workspace_model_settings (
 workspace_id text PRIMARY KEY REFERENCES workspaces(id), model_id text NOT NULL DEFAULT ''
);
ALTER TABLE runs ADD COLUMN IF NOT EXISTS model_config_id text NOT NULL DEFAULT '';
CREATE TABLE IF NOT EXISTS app_connections (
 workspace_id text NOT NULL REFERENCES workspaces(id), id text NOT NULL, label text NOT NULL, revision int NOT NULL CHECK(revision>0),
 credential bytea NOT NULL, enabled boolean NOT NULL DEFAULT true, verified_at timestamptz,
 PRIMARY KEY(workspace_id,id)
);
INSERT INTO schema_versions(version) VALUES(5) ON CONFLICT DO NOTHING;
ALTER TABLE matters ADD COLUMN IF NOT EXISTS origin_url text NOT NULL DEFAULT '';
ALTER TABLE matters ADD COLUMN IF NOT EXISTS origin_provider text NOT NULL DEFAULT '';
CREATE TABLE IF NOT EXISTS conversations (
 workspace_id text NOT NULL REFERENCES workspaces(id), id text NOT NULL, title text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(workspace_id,id)
);
ALTER TABLE runs ALTER COLUMN matter_id DROP NOT NULL;
ALTER TABLE runs ADD COLUMN IF NOT EXISTS kind text NOT NULL DEFAULT 'plan' CHECK(kind IN ('plan','chat'));
ALTER TABLE runs ADD COLUMN IF NOT EXISTS conversation_id text;
CREATE UNIQUE INDEX IF NOT EXISTS active_conversation_run ON runs(workspace_id,conversation_id) WHERE kind='chat' AND status IN ('QUEUED','RUNNING');
CREATE TABLE IF NOT EXISTS chat_turns (
 workspace_id text NOT NULL, conversation_id text NOT NULL, run_id text NOT NULL, position int NOT NULL CHECK(position>0), user_text text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(workspace_id,conversation_id,position), UNIQUE(workspace_id,run_id),
 FOREIGN KEY(workspace_id,conversation_id) REFERENCES conversations(workspace_id,id), FOREIGN KEY(workspace_id,run_id) REFERENCES runs(workspace_id,id)
);
INSERT INTO schema_versions(version) VALUES(6) ON CONFLICT DO NOTHING;
CREATE TABLE IF NOT EXISTS agent_steps (
 workspace_id text NOT NULL, run_id text NOT NULL, position int NOT NULL, kind text NOT NULL CHECK(kind IN ('MODEL','TOOL')),
 name text NOT NULL, status text NOT NULL CHECK(status IN ('CALLING','SUCCEEDED','FAILED','UNKNOWN')), error_code text NOT NULL DEFAULT '',
 input_tokens bigint NOT NULL DEFAULT 0, output_tokens bigint NOT NULL DEFAULT 0, charged_micro_cny bigint NOT NULL DEFAULT 0,
 created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(workspace_id,run_id,position), FOREIGN KEY(workspace_id,run_id) REFERENCES runs(workspace_id,id)
);
CREATE TABLE IF NOT EXISTS agent_actions (
 workspace_id text NOT NULL, id text NOT NULL, run_id text NOT NULL, kind text NOT NULL, payload jsonb NOT NULL,
 status text NOT NULL DEFAULT 'PENDING' CHECK(status IN ('PENDING','APPROVED','DECLINED')), result_id text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(workspace_id,id), FOREIGN KEY(workspace_id,run_id) REFERENCES runs(workspace_id,id)
);
INSERT INTO schema_versions(version) VALUES(7) ON CONFLICT DO NOTHING;
ALTER TABLE conversations ADD COLUMN IF NOT EXISTS context_summary text NOT NULL DEFAULT '';
ALTER TABLE conversations ADD COLUMN IF NOT EXISTS summary_through int NOT NULL DEFAULT 0 CHECK(summary_through>=0);
ALTER TABLE runs ADD COLUMN IF NOT EXISTS task_plan jsonb;
INSERT INTO schema_versions(version) VALUES(8) ON CONFLICT DO NOTHING;
CREATE TABLE IF NOT EXISTS workspace_files (
 workspace_id text NOT NULL REFERENCES workspaces(id), id text NOT NULL, name text NOT NULL,
 kind text NOT NULL CHECK(kind IN ('upload','artifact','web')), mime text NOT NULL, size bigint NOT NULL CHECK(size>0),
 origin_url text NOT NULL DEFAULT '', storage text NOT NULL CHECK(storage IN ('local','s3')), object_key text NOT NULL,
 deleted boolean NOT NULL DEFAULT false, created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(workspace_id,id)
);
CREATE TABLE IF NOT EXISTS chat_run_files (
 workspace_id text NOT NULL, run_id text NOT NULL, file_id text NOT NULL, role text NOT NULL CHECK(role IN ('input','output')),
 PRIMARY KEY(workspace_id,run_id,file_id), FOREIGN KEY(workspace_id,run_id) REFERENCES runs(workspace_id,id),
 FOREIGN KEY(workspace_id,file_id) REFERENCES workspace_files(workspace_id,id)
);
INSERT INTO schema_versions(version) VALUES(9) ON CONFLICT DO NOTHING;
ALTER TABLE matters DROP CONSTRAINT IF EXISTS matters_status_check;
ALTER TABLE matters ADD CONSTRAINT matters_status_check CHECK(status IN ('ACTIVE','COMPLETED','ARCHIVED'));
INSERT INTO schema_versions(version) VALUES(10) ON CONFLICT DO NOTHING;
