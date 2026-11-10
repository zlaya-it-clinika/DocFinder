-- Режим чата: read - ответы по базе знаний, plan - подготовка документов. Старые сессии - read
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS chat_mode text NOT NULL DEFAULT 'read';
