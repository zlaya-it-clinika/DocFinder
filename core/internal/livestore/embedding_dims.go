package livestore

import (
	"context"
	"fmt"
	"log"
)

// Размерность векторов в базе следует за OLLAMA_EMBED_DIMENSIONS. Новая таблица сразу создается нужной размерности,
// а у существующей базы при смене модели эмбеддингов (nomic-embed-text 768 -> bge-m3 1024) столбец перестраивается:
// векторы старой модели с новыми несравнимы, поэтому они стираются, фрагменты остаются,
// а векторы пересчитываются новой моделью в фоне (Start_PendingReindex). Без этого каждый вопрос падал
// на сравнении векторов разной длины, а сотрудник видел «База данных ядра недоступна»
func (store *LiveStore) sync_EmbeddingDimensions(ctx context.Context) error {
	current, err := read_EmbeddingDimensions(ctx, store.pool)
	if err != nil {
		return err
	}
	if current == store.dimensions {
		return nil
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	statements := []string{
		// Индекс HNSW строится под размерность столбца: старый удаляется, новый создается на пустом столбце
		`DROP INDEX IF EXISTS chunks_embedding_idx`,
		`ALTER TABLE chunks ALTER COLUMN embedding DROP NOT NULL`,
		fmt.Sprintf(`ALTER TABLE chunks ALTER COLUMN embedding TYPE vector(%d) USING NULL`, store.dimensions),
		`CREATE INDEX chunks_embedding_idx ON chunks USING hnsw (embedding vector_cosine_ops)`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(ctx, statement); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	log.Printf("embedding dimensions changed %d -> %d: vectors cleared, they will be rebuilt by the current embed model", current, store.dimensions)
	return nil
}

// Размерность столбца chunks.embedding. У типа vector из pgvector модификатор типа - это и есть размерность
func read_EmbeddingDimensions(ctx context.Context, querier queryRower) (int, error) {
	var dimensions int
	err := querier.QueryRow(ctx, `
		SELECT atttypmod FROM pg_attribute
		WHERE attrelid = 'chunks'::regclass AND attname = 'embedding' AND NOT attisdropped`).Scan(&dimensions)
	return dimensions, err
}
