-- Автозагрузка из внешних источников: каталог, лицензионный API. Ручная загрузка не перезаписывает эти документы
ALTER TABLE documents ADD COLUMN IF NOT EXISTS source_id text NOT NULL DEFAULT '';
ALTER TABLE documents ADD COLUMN IF NOT EXISTS source_external_id text NOT NULL DEFAULT '';

DROP INDEX IF EXISTS documents_global_title_idx;
DROP INDEX IF EXISTS documents_personal_title_idx;
CREATE UNIQUE INDEX IF NOT EXISTS documents_global_title_idx ON documents (title) WHERE scope = 'global' AND source_id = '';
CREATE UNIQUE INDEX IF NOT EXISTS documents_personal_title_idx ON documents (owner_id, title) WHERE scope = 'personal' AND source_id = '';
CREATE UNIQUE INDEX IF NOT EXISTS documents_source_item_idx ON documents (source_id, source_external_id) WHERE source_id <> '' AND source_external_id <> '';

CREATE TABLE IF NOT EXISTS document_sources (
	source_id text PRIMARY KEY,
	kind text NOT NULL,
	title text NOT NULL,
	enabled boolean NOT NULL DEFAULT false,
	interval_minutes int NOT NULL DEFAULT 60,
	process_id text NOT NULL DEFAULT 'documents',
	visible_to_all boolean NOT NULL DEFAULT true,
	access_level int NOT NULL DEFAULT 1,
	config jsonb NOT NULL DEFAULT '{}'::jsonb,
	last_sync_at timestamptz,
	last_status text NOT NULL DEFAULT '',
	last_error text NOT NULL DEFAULT '',
	last_cursor text NOT NULL DEFAULT '',
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS document_source_roles (
	source_id text NOT NULL REFERENCES document_sources (source_id) ON DELETE CASCADE,
	role_id text NOT NULL REFERENCES roles (role_id),
	PRIMARY KEY (source_id, role_id)
);

CREATE TABLE IF NOT EXISTS document_source_items (
	source_id text NOT NULL REFERENCES document_sources (source_id) ON DELETE CASCADE,
	external_id text NOT NULL,
	document_id text NOT NULL REFERENCES documents (document_id) ON DELETE CASCADE,
	checksum text NOT NULL DEFAULT '',
	fetched_at timestamptz NOT NULL DEFAULT now(),
	PRIMARY KEY (source_id, external_id)
);

INSERT INTO id_counters (name, value) VALUES ('source', 1) ON CONFLICT (name) DO NOTHING;
