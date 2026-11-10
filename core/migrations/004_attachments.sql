-- Черновик сессии не индексируется и удаляется вместе со строкой сессии
CREATE TABLE IF NOT EXISTS session_attachments (
	attachment_id text PRIMARY KEY,
	session_id text NOT NULL REFERENCES sessions (session_id) ON DELETE CASCADE,
	owner_user_id text NOT NULL REFERENCES users (user_id),
	file_name text NOT NULL,
	plain_text text NOT NULL,
	characters int NOT NULL,
	created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS session_attachments_session_id_idx ON session_attachments (session_id);

ALTER TABLE sessions ADD COLUMN IF NOT EXISTS draft_attachment_id text;

-- Пустой session_id у бланка фазы 9. Файл правки черновика привязан к сессии и уходит вместе с ней
ALTER TABLE draft_files ADD COLUMN IF NOT EXISTS session_id text REFERENCES sessions (session_id) ON DELETE CASCADE;

CREATE INDEX IF NOT EXISTS draft_files_session_id_idx ON draft_files (session_id);

INSERT INTO id_counters (name, value) VALUES ('attachment', 1)
ON CONFLICT (name) DO NOTHING;
