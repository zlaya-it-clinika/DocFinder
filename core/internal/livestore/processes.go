package livestore

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"docfinder/core/internal/texts"
)

const counterProcess = "process"

type Process struct {
	ProcessID     string
	Title         string
	SelectPrompt  string
	AnswerPrompt  string
	ClarifyPrompt string
}

type ReplyPrompts struct {
	Select  string
	Answer  string
	Clarify string
}

func (store *LiveStore) seed_DefaultProcess(ctx context.Context) error {
	_, err := store.pool.Exec(ctx, `
		INSERT INTO processes (process_id, title, select_prompt, answer_prompt, clarify_prompt)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (process_id) DO NOTHING`,
		texts.ProcessDocuments, texts.ProcessTitleDocuments, texts.LiveSelectPrompt, texts.LiveAnswerPrompt, texts.LiveClarifyPrompt)
	return err
}

func (store *LiveStore) List_Processes(ctx context.Context, token string) ([]Process, error) {
	if _, err := store.Find_AccountByToken(ctx, token); err != nil {
		return nil, err
	}
	rows, err := store.pool.Query(ctx, `
		SELECT process_id, title FROM processes ORDER BY process_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	found := []Process{}
	for rows.Next() {
		var process Process
		if err := rows.Scan(&process.ProcessID, &process.Title); err != nil {
			return nil, err
		}
		found = append(found, process)
	}
	return found, rows.Err()
}

func (store *LiveStore) Create_Process(ctx context.Context, token, title, selectPrompt, answerPrompt, clarifyPrompt string) (Process, error) {
	account, err := store.Find_AccountByToken(ctx, token)
	if err != nil {
		return Process{}, err
	}
	// Процессы - структура базы знаний, ими управляет тот, кто управляет общими документами
	if !account.Has(texts.PermissionGlobalManage) {
		return Process{}, ErrAccessDenied
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return Process{}, ErrInvalidBody
	}
	fallback, err := store.Find_Process(ctx, texts.ProcessDocuments)
	if errors.Is(err, ErrNotFound) {
		fallback = Process{
			SelectPrompt:  texts.LiveSelectPrompt,
			AnswerPrompt:  texts.LiveAnswerPrompt,
			ClarifyPrompt: texts.LiveClarifyPrompt,
		}
	} else if err != nil {
		return Process{}, err
	}
	number, err := store.next_ID(ctx, counterProcess)
	if err != nil {
		return Process{}, err
	}
	created := Process{
		ProcessID:     "process-" + strconv.Itoa(number),
		Title:         title,
		SelectPrompt:  prompt_OrFallback(selectPrompt, fallback.SelectPrompt, texts.LiveSelectPrompt),
		AnswerPrompt:  prompt_OrFallback(answerPrompt, fallback.AnswerPrompt, texts.LiveAnswerPrompt),
		ClarifyPrompt: prompt_OrFallback(clarifyPrompt, fallback.ClarifyPrompt, texts.LiveClarifyPrompt),
	}
	_, err = store.pool.Exec(ctx, `
		INSERT INTO processes (process_id, title, select_prompt, answer_prompt, clarify_prompt)
		VALUES ($1, $2, $3, $4, $5)`,
		created.ProcessID, created.Title, created.SelectPrompt, created.AnswerPrompt, created.ClarifyPrompt)
	if err != nil {
		return Process{}, err
	}
	return created, nil
}

func (store *LiveStore) Find_Process(ctx context.Context, processID string) (Process, error) {
	var process Process
	err := store.pool.QueryRow(ctx, `
		SELECT process_id, title, select_prompt, answer_prompt, clarify_prompt
		FROM processes WHERE process_id = $1`, processID).Scan(
		&process.ProcessID, &process.Title, &process.SelectPrompt, &process.AnswerPrompt, &process.ClarifyPrompt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Process{}, ErrNotFound
	}
	if err != nil {
		return Process{}, err
	}
	return process, nil
}

// Пустой processID оставляет поиск по всем процессам, а промпт берется у документооборота.
func (store *LiveStore) Load_ReplyPrompts(ctx context.Context, processID string) (ReplyPrompts, error) {
	lookupID := strings.TrimSpace(processID)
	if lookupID == "" {
		lookupID = texts.ProcessDocuments
	}
	process, err := store.Find_Process(ctx, lookupID)
	if err != nil {
		if errors.Is(err, ErrNotFound) && strings.TrimSpace(processID) != "" {
			return ReplyPrompts{}, ErrInvalidBody
		}
		if lookupID == texts.ProcessDocuments && errors.Is(err, ErrNotFound) {
			return default_ReplyPrompts(), nil
		}
		return ReplyPrompts{}, err
	}
	return ReplyPrompts{
		Select:  prompt_OrFallback(process.SelectPrompt, texts.LiveSelectPrompt),
		Answer:  prompt_OrFallback(process.AnswerPrompt, texts.LiveAnswerPrompt),
		Clarify: prompt_OrFallback(process.ClarifyPrompt, texts.LiveClarifyPrompt),
	}, nil
}

func (store *LiveStore) Require_UploadProcess(ctx context.Context, raw string) (string, error) {
	processID := strings.TrimSpace(raw)
	if processID == "" {
		return texts.ProcessDocuments, nil
	}
	if _, err := store.Find_Process(ctx, processID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return "", ErrInvalidBody
		}
		return "", err
	}
	return processID, nil
}

func default_ReplyPrompts() ReplyPrompts {
	return ReplyPrompts{
		Select:  texts.LiveSelectPrompt,
		Answer:  texts.LiveAnswerPrompt,
		Clarify: texts.LiveClarifyPrompt,
	}
}

func prompt_OrFallback(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
