package livestore

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"docfinder/core/internal/auth"
	"docfinder/core/internal/texts"
)

func TestFind_ChunksBySimilarityFiltersScopeAndLevel(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is empty")
	}
	store, err := Open_LiveStore(databaseURL, 768, nil, auth.Settings{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)

	ctx := context.Background()
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	openID := "doc-open-" + suffix
	closedID := "doc-closed-" + suffix
	ownID := "doc-own-" + suffix
	foreignID := "doc-foreign-" + suffix
	ownerID := "user-1"
	foreignOwnerID := "user-foreign-" + suffix

	insertDocument(t, ctx, store, openID, texts.ScopeGlobal, "", 1)
	insertDocument(t, ctx, store, closedID, texts.ScopeGlobal, "", 2)
	insertDocument(t, ctx, store, ownID, texts.ScopePersonal, ownerID, 1)
	insertDocument(t, ctx, store, foreignID, texts.ScopePersonal, foreignOwnerID, 1)
	t.Cleanup(func() {
		_, _ = store.pool.Exec(context.Background(), `DELETE FROM documents WHERE document_id IN ($1, $2, $3, $4)`, openID, closedID, ownID, foreignID)
	})

	near := make([]float32, 768)
	near[2+int(time.Now().UnixNano()%700)] = 1
	insertChunk(t, ctx, store, openID, texts.ScopeGlobal, "", "открытый общий", near)
	insertChunk(t, ctx, store, closedID, texts.ScopeGlobal, "", "закрытый общий", near)
	insertChunk(t, ctx, store, ownID, texts.ScopePersonal, ownerID, "свой личный", near)
	insertChunk(t, ctx, store, foreignID, texts.ScopePersonal, foreignOwnerID, "чужой личный", near)

	visible, err := store.Find_ChunksBySimilarity(ctx, near, ownerID, []string{texts.RoleIDUser}, "", 5)
	if err != nil {
		t.Fatal(err)
	}
	if !hasAllContents(visible, "открытый общий", "свой личный") || hasAnyContents(visible, "закрытый общий", "чужой личный") {
		t.Fatalf("employee visible %+v", visible)
	}

	otherVisible, err := store.Find_ChunksBySimilarity(ctx, near, foreignOwnerID, []string{texts.RoleIDUser}, "", 5)
	if err != nil {
		t.Fatal(err)
	}
	if !hasAllContents(otherVisible, "открытый общий", "чужой личный") || hasAnyContents(otherVisible, "свой личный", "закрытый общий") {
		t.Fatalf("other user visible %+v", otherVisible)
	}

	adminVisible, err := store.Find_ChunksBySimilarity(ctx, near, ownerID, []string{texts.RoleIDAdmin}, "", 5)
	if err != nil {
		t.Fatal(err)
	}
	if !hasAllContents(adminVisible, "открытый общий", "закрытый общий", "свой личный") || hasAnyContents(adminVisible, "чужой личный") {
		t.Fatalf("raised level %+v", adminVisible)
	}

	closed, err := store.Find_ClosedGlobalChunks(ctx, near, []string{texts.RoleIDUser}, "", 5)
	if err != nil {
		t.Fatal(err)
	}
	if !hasAllContents(closed, "закрытый общий") || hasAnyContents(closed, "открытый общий", "свой личный", "чужой личный") {
		t.Fatalf("closed global %+v", closed)
	}

	replyVisible, replyClosed, err := store.Find_ReplyChunks(ctx, near, ownerID, []string{texts.RoleIDUser}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !hasAllContents(replyVisible, "открытый общий", "свой личный") || hasAnyContents(replyVisible, "чужой личный", "закрытый общий") || len(replyClosed) != 0 {
		t.Fatalf("reply visible %+v closed %+v", replyVisible, replyClosed)
	}

	if _, err := store.pool.Exec(ctx, `DELETE FROM documents WHERE document_id = $1`, closedID); err != nil {
		t.Fatal(err)
	}
	var chunkCount int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM chunks WHERE document_id = $1`, closedID).Scan(&chunkCount); err != nil {
		t.Fatal(err)
	}
	if chunkCount != 0 {
		t.Fatalf("chunks left %d", chunkCount)
	}
	closed, err = store.Find_ClosedGlobalChunks(ctx, near, []string{texts.RoleIDUser}, "", 5)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range closed {
		if row.DocumentID == closedID {
			t.Fatalf("deleted closed chunk %+v", closed)
		}
	}
}

func insertDocument(t *testing.T, ctx context.Context, store *LiveStore, documentID, scope, ownerID string, accessLevel int) {
	t.Helper()
	insertTitledDocument(t, ctx, store, documentID, documentID, scope, ownerID, accessLevel)
}

func TestFind_ContactChunkStaysOutOfAnswerSearch(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is empty")
	}
	store, err := Open_LiveStore(databaseURL, 768, nil, auth.Settings{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)

	ctx := context.Background()
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	instructionID := "doc-instr-" + suffix
	contactID := "doc-contact-" + suffix
	foreignID := "doc-foreign-contact-" + suffix
	ownerID := "user-1"
	foreignOwnerID := "user-foreign-" + suffix
	insertTitledDocument(t, ctx, store, instructionID, "Инструкция по документообороту", texts.ScopeGlobal, "", 1)
	insertTitledDocument(t, ctx, store, contactID, "Контакты подразделений", texts.ScopeGlobal, "", 1)
	insertTitledDocument(t, ctx, store, foreignID, "Контакты сотрудника", texts.ScopePersonal, foreignOwnerID, 1)
	t.Cleanup(func() {
		_, _ = store.pool.Exec(context.Background(), `DELETE FROM documents WHERE document_id IN ($1, $2, $3)`, instructionID, contactID, foreignID)
	})

	near := make([]float32, 768)
	near[2+int(time.Now().UnixNano()%700)] = 1
	insertChunk(t, ctx, store, instructionID, texts.ScopeGlobal, "", "открытый пункт", near)
	contactBody := "Центр документационного обеспечения\nтелефон 1-10-20"
	insertChunk(t, ctx, store, contactID, texts.ScopeGlobal, "", contactBody, near)
	insertChunk(t, ctx, store, foreignID, texts.ScopePersonal, foreignOwnerID, "чужой телефон", near)

	visible, err := store.Find_ChunksBySimilarity(ctx, near, ownerID, []string{texts.RoleIDUser}, "", 5)
	if err != nil {
		t.Fatal(err)
	}
	if !hasAllContents(visible, "открытый пункт") || hasAnyContents(visible, contactBody, "чужой телефон") {
		t.Fatalf("answer search %+v", visible)
	}

	contact, found, err := store.Find_ContactChunk(ctx, near, ownerID, []string{texts.RoleIDUser}, "")
	if err != nil || !found || contact.DocumentID != contactID || contact.Content != contactBody {
		t.Fatalf("contact %+v found %v err %v", contact, found, err)
	}

	far := make([]float32, 768)
	far[4] = 1
	_, weakFound, err := store.Find_ContactChunk(ctx, far, ownerID, []string{texts.RoleIDUser}, "")
	if err != nil || weakFound {
		t.Fatalf("weak contact found %v err %v", weakFound, err)
	}
}

func insertTitledDocument(t *testing.T, ctx context.Context, store *LiveStore, documentID, title, scope, ownerID string, accessLevel int) {
	t.Helper()
	visibleToAll := scope == texts.ScopeGlobal && accessLevel < texts.SeedAdminAccessLevel
	_, err := store.pool.Exec(ctx, `
		INSERT INTO documents (document_id, title, edition, access_level, scope, owner_id, file_name, content_type, body, visible_to_all)
		VALUES ($1, $2, '1.0', $3, $4, $5, 'probe.txt', 'text/plain', $6, $7)`,
		documentID, title, accessLevel, scope, ownerID, []byte("probe"), visibleToAll)
	if err != nil {
		t.Fatal(err)
	}
	if scope == texts.ScopeGlobal && !visibleToAll {
		for _, roleID := range []string{texts.RoleIDAdmin, texts.RoleIDChief} {
			if _, err := store.pool.Exec(ctx, `INSERT INTO document_roles (document_id, role_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, documentID, roleID); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func insertChunk(t *testing.T, ctx context.Context, store *LiveStore, documentID, scope, ownerID, content string, embedding []float32) {
	t.Helper()
	_, err := store.pool.Exec(ctx, `
		INSERT INTO chunks (document_id, section, paragraph, appendix, access_level, scope, owner_id, content, embedding)
		VALUES ($1, '', '', '', 1, $2, $3, $4, $5::vector)`,
		documentID, scope, ownerID, content, format_Vector(embedding))
	if err != nil {
		t.Fatal(err)
	}
}

func hasAllContents(rows []SimilarChunk, contents ...string) bool {
	seen := map[string]bool{}
	for _, row := range rows {
		seen[row.Content] = true
	}
	for _, content := range contents {
		if !seen[content] {
			return false
		}
	}
	return true
}

func hasAnyContents(rows []SimilarChunk, contents ...string) bool {
	seen := map[string]bool{}
	for _, row := range rows {
		seen[row.Content] = true
	}
	for _, content := range contents {
		if seen[content] {
			return true
		}
	}
	return false
}

func TestFind_ChunksBySimilarityFiltersProcess(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is empty")
	}
	store, err := Open_LiveStore(databaseURL, 768, nil, auth.Settings{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)

	ctx := context.Background()
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	recordsID := "doc-proc-records-" + suffix
	staffID := "doc-proc-staff-" + suffix
	insertTitledDocument(t, ctx, store, recordsID, "регламент документооборота", texts.ScopeGlobal, "", 1)
	insertTitledDocument(t, ctx, store, staffID, "регламент кадров", texts.ScopeGlobal, "", 1)
	if _, err := store.pool.Exec(ctx, `UPDATE documents SET process_id = $2 WHERE document_id = $1`, staffID, "process-hr"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = store.pool.Exec(context.Background(), `DELETE FROM documents WHERE document_id IN ($1, $2)`, recordsID, staffID)
	})

	near := make([]float32, 768)
	near[1+int(time.Now().UnixNano()%700)] = 1
	insertChunk(t, ctx, store, recordsID, texts.ScopeGlobal, "", "текст документооборота", near)
	insertChunk(t, ctx, store, staffID, texts.ScopeGlobal, "", "текст кадров", near)

	staffOnly, err := store.Find_ChunksBySimilarity(ctx, near, "user-1", []string{texts.RoleIDUser}, "process-hr", 5)
	if err != nil {
		t.Fatal(err)
	}
	if !hasAllContents(staffOnly, "текст кадров") || hasAnyContents(staffOnly, "текст документооборота") {
		t.Fatalf("process filter %+v", staffOnly)
	}
	all, err := store.Find_ChunksBySimilarity(ctx, near, "user-1", []string{texts.RoleIDUser}, "", 5)
	if err != nil {
		t.Fatal(err)
	}
	if !hasAllContents(all, "текст кадров", "текст документооборота") {
		t.Fatalf("all processes %+v", all)
	}
}
