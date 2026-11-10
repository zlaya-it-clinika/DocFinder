CREATE TABLE IF NOT EXISTS processes (
	process_id text PRIMARY KEY,
	title text NOT NULL,
	select_prompt text NOT NULL,
	answer_prompt text NOT NULL,
	clarify_prompt text NOT NULL
);

ALTER TABLE documents ADD COLUMN IF NOT EXISTS process_id text NOT NULL DEFAULT 'documents';

INSERT INTO id_counters (name, value) VALUES ('process', 1)
ON CONFLICT (name) DO NOTHING;
