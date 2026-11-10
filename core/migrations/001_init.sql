CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS users (
	user_id text PRIMARY KEY,
	login text UNIQUE NOT NULL,
	password_hash text NOT NULL,
	display_name text NOT NULL,
	access_level int NOT NULL,
	pending boolean NOT NULL DEFAULT false,
	created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS auth_tokens (
	token text PRIMARY KEY,
	user_id text NOT NULL REFERENCES users (user_id)
);

CREATE TABLE IF NOT EXISTS sessions (
	session_id text PRIMARY KEY,
	owner_user_id text NOT NULL REFERENCES users (user_id),
	template_step int NOT NULL DEFAULT 0,
	template_state jsonb NOT NULL DEFAULT '{}'::jsonb,
	title text NOT NULL DEFAULT '',
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS session_messages (
	message_id bigserial PRIMARY KEY,
	session_id text NOT NULL REFERENCES sessions (session_id),
	position int NOT NULL,
	body jsonb NOT NULL
);

CREATE TABLE IF NOT EXISTS documents (
	document_id text PRIMARY KEY,
	title text NOT NULL,
	edition text NOT NULL,
	access_level int NOT NULL,
	scope text NOT NULL DEFAULT 'global',
	owner_id text NOT NULL DEFAULT '',
	uploaded_at timestamptz NOT NULL DEFAULT now(),
	file_name text NOT NULL,
	content_type text NOT NULL,
	body bytea NOT NULL
);

CREATE TABLE IF NOT EXISTS chunks (
	chunk_id bigserial PRIMARY KEY,
	document_id text NOT NULL REFERENCES documents (document_id) ON DELETE CASCADE,
	section text NOT NULL DEFAULT '',
	paragraph text NOT NULL DEFAULT '',
	appendix text NOT NULL DEFAULT '',
	access_level int NOT NULL,
	scope text NOT NULL DEFAULT 'global',
	owner_id text NOT NULL DEFAULT '',
	content text NOT NULL,
	embedding vector(768) NOT NULL
);

CREATE INDEX IF NOT EXISTS chunks_access_idx ON chunks (access_level);
CREATE INDEX IF NOT EXISTS chunks_embedding_idx ON chunks USING hnsw (embedding vector_cosine_ops);

CREATE TABLE IF NOT EXISTS audit_entries (
	entry_id text PRIMARY KEY,
	user_id text NOT NULL,
	display_name text NOT NULL,
	question text NOT NULL,
	created_at timestamptz NOT NULL,
	documents jsonb NOT NULL
);

CREATE TABLE IF NOT EXISTS id_counters (
	name text PRIMARY KEY,
	value int NOT NULL
);

CREATE TABLE IF NOT EXISTS draft_files (
	draft_id text PRIMARY KEY,
	owner_user_id text NOT NULL REFERENCES users (user_id),
	file_name text NOT NULL,
	content_type text NOT NULL,
	body bytea NOT NULL
);

INSERT INTO id_counters (name, value) VALUES
	('user', 1),
	('document', 1),
	('session', 1),
	('audit', 1),
	('draft', 1)
ON CONFLICT (name) DO NOTHING;

-- Настройки входа из админ-панели: регистрация, роль по умолчанию, LDAP. Ключ - раздел, значение - JSON
CREATE TABLE IF NOT EXISTS app_settings (
	key text PRIMARY KEY,
	value jsonb NOT NULL,
	updated_at timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE users ADD COLUMN IF NOT EXISTS pending boolean NOT NULL DEFAULT false;
ALTER TABLE users ADD COLUMN IF NOT EXISTS created_at timestamptz NOT NULL DEFAULT now();

ALTER TABLE sessions ADD COLUMN IF NOT EXISTS title text NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS created_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS updated_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS template_state jsonb NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE documents ADD COLUMN IF NOT EXISTS scope text NOT NULL DEFAULT 'global';
ALTER TABLE documents ADD COLUMN IF NOT EXISTS owner_id text NOT NULL DEFAULT '';
ALTER TABLE documents ADD COLUMN IF NOT EXISTS uploaded_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE documents DROP CONSTRAINT IF EXISTS documents_title_key;

ALTER TABLE chunks ADD COLUMN IF NOT EXISTS scope text NOT NULL DEFAULT 'global';
ALTER TABLE chunks ADD COLUMN IF NOT EXISTS owner_id text NOT NULL DEFAULT '';

CREATE UNIQUE INDEX IF NOT EXISTS documents_global_title_idx ON documents (title) WHERE scope = 'global';
CREATE UNIQUE INDEX IF NOT EXISTS documents_personal_title_idx ON documents (owner_id, title) WHERE scope = 'personal';
