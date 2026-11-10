-- Реквизиты организации для документов: должностные лица и изображения (герб, штамп)
CREATE TABLE IF NOT EXISTS officials (
	official_id text PRIMARY KEY,
	position text NOT NULL,
	full_name text NOT NULL,
	department text NOT NULL DEFAULT '',
	chief boolean NOT NULL DEFAULT false,
	valid_from text NOT NULL DEFAULT '',
	updated_at timestamptz NOT NULL DEFAULT now()
);

-- Генеральный директор в организации один
CREATE UNIQUE INDEX IF NOT EXISTS officials_single_chief ON officials (chief) WHERE chief;

-- Вид изображения: emblem (герб) или stamp (штамп), у каждого вида одно текущее изображение
CREATE TABLE IF NOT EXISTS organization_images (
	kind text PRIMARY KEY,
	file_name text NOT NULL,
	content_type text NOT NULL,
	body bytea NOT NULL,
	updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO id_counters (name, value) VALUES ('official', 1) ON CONFLICT (name) DO NOTHING;
