-- Стандартный набор документов компании (source/docs): загружается при запуске, удалить его нельзя.
-- standard_hash - SHA-256 файла набора, который загружен последним: новая версия файла в наборе заменяет документ,
-- а редакция, загруженная администратором поверх, при перезапуске остается
ALTER TABLE documents ADD COLUMN IF NOT EXISTS standard boolean NOT NULL DEFAULT false;
ALTER TABLE documents ADD COLUMN IF NOT EXISTS standard_hash text NOT NULL DEFAULT '';

-- Архив старых редакций: новая редакция документа с другим содержимым переносит прежнюю сюда.
-- В поиске участвует только текущая редакция, архивные доступны для скачивания
CREATE TABLE IF NOT EXISTS document_editions (
	edition_id bigserial PRIMARY KEY,
	document_id text NOT NULL REFERENCES documents (document_id) ON DELETE CASCADE,
	edition text NOT NULL,
	file_name text NOT NULL,
	content_type text NOT NULL,
	body bytea NOT NULL,
	uploaded_at timestamptz NOT NULL,
	archived_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS document_editions_document_idx ON document_editions (document_id, archived_at DESC);

-- Фрагменты стандартного набора сохраняются при запуске без вектора, векторы досчитываются в фоне
ALTER TABLE chunks ALTER COLUMN embedding DROP NOT NULL;
