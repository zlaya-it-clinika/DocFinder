package livestore

import (
	"context"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"docfinder/core/internal/auth"
)

// Модель эмбеддингов для теста: векторы нужной длины без Ollama
type sizedEmbedder struct {
	dimensions int
}

func (embedder sizedEmbedder) Embed_Texts(inputs []string) ([][]float32, error) {
	vectors := make([][]float32, len(inputs))
	for index := range inputs {
		vector := make([]float32, embedder.dimensions)
		vector[0] = 1
		vectors[index] = vector
	}
	return vectors, nil
}

// Смена OLLAMA_EMBED_DIMENSIONS у существующей базы: столбец перестраивается, поиск не падает,
// а векторы пересчитываются новой моделью. Тест идет в отдельной временной базе, рабочие данные не трогаются
func TestEmbeddingDimensionsFollowSettings(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is empty")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	name := "docfinder_dims_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	if _, err := admin.Exec(ctx, `CREATE DATABASE `+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`)
	})
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Path = "/" + name
	testURL := parsed.String()

	old, err := Open_LiveStore(testURL, 768, sizedEmbedder{768}, auth.Settings{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.pool.Exec(ctx, `
		INSERT INTO documents (document_id, title, edition, access_level, file_name, content_type, body)
		VALUES ('doc-dims', 'Инструкция', '1', 1, 'doc.txt', 'text/plain', 'x')`); err != nil {
		t.Fatal(err)
	}
	oldVector := make([]float32, 768)
	oldVector[0] = 1
	if _, err := old.pool.Exec(ctx, `
		INSERT INTO chunks (document_id, access_level, content, embedding) VALUES ('doc-dims', 1, 'Текст пункта', $1::vector)`, format_Vector(oldVector)); err != nil {
		t.Fatal(err)
	}
	old.Close()

	store, err := Open_LiveStore(testURL, 1024, sizedEmbedder{1024}, auth.Settings{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	if dimensions, err := read_EmbeddingDimensions(ctx, store.pool); err != nil || dimensions != 1024 {
		t.Fatalf("column dimensions %d %v", dimensions, err)
	}
	question := make([]float32, 1024)
	question[0] = 1
	// Пока векторы не пересчитаны, поиск отвечает пусто, а не ошибкой базы
	found, err := store.Find_ChunksBySimilarity(ctx, question, "user-dims", nil, "", 5)
	if err != nil || len(found) != 0 {
		t.Fatalf("search before reindex %d %v", len(found), err)
	}
	if !store.Start_PendingReindex() {
		t.Fatal("pending reindex did not start")
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		store.reindex.mu.Lock()
		status := store.reindex.status
		store.reindex.mu.Unlock()
		if !status.Running {
			if status.Failed || status.Done != 1 || status.Total != 1 {
				t.Fatalf("reindex status %+v", status)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("reindex did not finish")
		}
		time.Sleep(50 * time.Millisecond)
	}
	found, err = store.Find_ChunksBySimilarity(ctx, question, "user-dims", nil, "", 5)
	if err != nil || len(found) != 1 || found[0].Content != "Текст пункта" {
		t.Fatalf("search after reindex %+v %v", found, err)
	}
	// Тот же размер при следующем запуске: столбец и векторы не трогаются
	again, err := Open_LiveStore(testURL, 1024, sizedEmbedder{1024}, auth.Settings{})
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if again.Start_PendingReindex() {
		t.Fatal("reindex started without missing vectors")
	}
}
