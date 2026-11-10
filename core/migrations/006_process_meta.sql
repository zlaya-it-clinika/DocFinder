-- Карточка процесса в админке: описание и цвет метки
ALTER TABLE processes ADD COLUMN IF NOT EXISTS description text NOT NULL DEFAULT '';
ALTER TABLE processes ADD COLUMN IF NOT EXISTS color text NOT NULL DEFAULT 'gray';

-- Процесс, выбранный в чате. Пустая строка - все процессы
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS process_id text NOT NULL DEFAULT '';
