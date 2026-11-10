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

func TestProcessMapDocumentsAndSessionProcess(t *testing.T) {
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
	processID := "process-test-" + suffix
	openID := "doc-open-proc-" + suffix
	closedID := "doc-closed-proc-" + suffix
	otherID := "doc-other-proc-" + suffix
	userID := "user-proc-" + suffix
	sessionID := "session-proc-" + suffix
	if _, err := store.pool.Exec(ctx, `
		INSERT INTO processes (process_id, title, select_prompt, answer_prompt, clarify_prompt, description, color)
		VALUES ($1, 'Продажи', 's', 'a', 'c', 'Скидки', 'green')`, processID); err != nil {
		t.Fatal(err)
	}
	insertTitledDocument(t, ctx, store, openID, "Регламент скидок "+suffix, texts.ScopeGlobal, "", 1)
	insertTitledDocument(t, ctx, store, closedID, "Закрытый прайс "+suffix, texts.ScopeGlobal, "", 1)
	insertTitledDocument(t, ctx, store, otherID, "Чужая тема "+suffix, texts.ScopeGlobal, "", 1)
	if _, err := store.pool.Exec(ctx, `UPDATE documents SET process_id = $1 WHERE document_id IN ($2, $3)`, processID, openID, closedID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE documents SET visible_to_all = false WHERE document_id = $1`, closedID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `
		INSERT INTO users (user_id, login, password_hash, display_name, access_level)
		VALUES ($1, $2, 'x', 'Тест', 1)`, userID, userID+"@test.local"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO sessions (session_id, owner_user_id) VALUES ($1, $2)`, sessionID, userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		background := context.Background()
		_, _ = store.pool.Exec(background, `DELETE FROM sessions WHERE session_id = $1`, sessionID)
		_, _ = store.pool.Exec(background, `DELETE FROM users WHERE user_id = $1`, userID)
		_, _ = store.pool.Exec(background, `DELETE FROM documents WHERE document_id IN ($1, $2, $3)`, openID, closedID, otherID)
		_, _ = store.pool.Exec(background, `DELETE FROM processes WHERE process_id = $1`, processID)
	})

	// В «карту» темы попадают только видимые сотруднику документы этого процесса
	titles, err := store.List_ProcessDocumentTitles(ctx, processID, []string{}, 40)
	if err != nil {
		t.Fatal(err)
	}
	if len(titles) != 1 || titles[0] != "Регламент скидок "+suffix {
		t.Fatalf("titles %+v", titles)
	}

	processes, err := store.Read_DocumentProcesses(ctx, []string{openID, otherID})
	if err != nil {
		t.Fatal(err)
	}
	if processes[openID] != processID || processes[otherID] != texts.ProcessDocuments {
		t.Fatalf("document processes %+v", processes)
	}

	card, err := store.Find_ProcessCard(ctx, processID)
	if err != nil || card.Description != "Скидки" || card.Color != "green" {
		t.Fatalf("card %+v err %v", card, err)
	}

	if saved, err := store.Read_SessionProcess(ctx, sessionID); err != nil || saved != "" {
		t.Fatalf("empty session process %q err %v", saved, err)
	}
	if err := store.Save_SessionProcess(ctx, sessionID, processID); err != nil {
		t.Fatal(err)
	}
	if saved, err := store.Read_SessionProcess(ctx, sessionID); err != nil || saved != processID {
		t.Fatalf("saved session process %q err %v", saved, err)
	}
	if _, err := store.Read_SessionProcess(ctx, "session-missing-"+suffix); err != ErrNotFound {
		t.Fatalf("missing session err %v", err)
	}
}
