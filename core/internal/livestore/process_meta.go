package livestore

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"docfinder/core/internal/texts"
)

// Цвета меток процессов: клиент рисует метку по имени цвета
var processColors = map[string]bool{
	"gray": true, "blue": true, "green": true, "amber": true, "red": true, "violet": true, "teal": true,
}

// Карточка процесса для списка: описание, цвет и сколько документов видит сотрудник
type ProcessCard struct {
	Process
	Description   string
	Color         string
	DocumentCount int
}

// Поля правки процесса. nil - поле не меняется
type ProcessInput struct {
	Title         *string
	Description   *string
	Color         *string
	SelectPrompt  *string
	AnswerPrompt  *string
	ClarifyPrompt *string
}

func Normalize_ProcessColor(raw string) (string, bool) {
	color := strings.ToLower(strings.TrimSpace(raw))
	if color == "" {
		return texts.ProcessDefaultColor, true
	}
	return color, processColors[color]
}

// Список процессов с карточками. Подсказки модели отдаются только тем, кто управляет базой знаний
func (store *LiveStore) List_ProcessCards(ctx context.Context, token string) ([]ProcessCard, Account, error) {
	account, err := store.Find_AccountByToken(ctx, token)
	if err != nil {
		return nil, Account{}, err
	}
	rows, err := store.pool.Query(ctx, `
		SELECT p.process_id, p.title, p.description, p.color, p.select_prompt, p.answer_prompt, p.clarify_prompt,
			(SELECT count(*) FROM documents d
				WHERE d.process_id = p.process_id AND d.scope = 'global' AND `+global_VisibleSQL("$1")+`)
		FROM processes p
		ORDER BY p.process_id = $2 DESC, p.title`, role_Filter(account.RoleIDs), texts.ProcessDocuments)
	if err != nil {
		return nil, Account{}, err
	}
	defer rows.Close()
	cards := []ProcessCard{}
	for rows.Next() {
		var card ProcessCard
		if err := rows.Scan(&card.ProcessID, &card.Title, &card.Description, &card.Color, &card.SelectPrompt, &card.AnswerPrompt, &card.ClarifyPrompt, &card.DocumentCount); err != nil {
			return nil, Account{}, err
		}
		cards = append(cards, card)
	}
	return cards, account, rows.Err()
}

// Описание и цвет нового процесса записываются сразу после создания
func (store *LiveStore) Set_ProcessMeta(ctx context.Context, processID, description, color string) error {
	normalized, ok := Normalize_ProcessColor(color)
	if !ok {
		return ErrInvalidBody
	}
	_, err := store.pool.Exec(ctx, `UPDATE processes SET description = $2, color = $3 WHERE process_id = $1`,
		processID, strings.TrimSpace(description), normalized)
	return err
}

func (store *LiveStore) Update_Process(ctx context.Context, token, processID string, input ProcessInput) (ProcessCard, error) {
	account, err := store.Find_AccountByToken(ctx, token)
	if err != nil {
		return ProcessCard{}, err
	}
	if !account.Has(texts.PermissionGlobalManage) {
		return ProcessCard{}, ErrAccessDenied
	}
	card, err := store.Find_ProcessCard(ctx, processID)
	if err != nil {
		return ProcessCard{}, err
	}
	if input.Title != nil {
		card.Title = strings.TrimSpace(*input.Title)
	}
	if input.Description != nil {
		card.Description = strings.TrimSpace(*input.Description)
	}
	if input.Color != nil {
		color, ok := Normalize_ProcessColor(*input.Color)
		if !ok {
			return ProcessCard{}, ErrInvalidBody
		}
		card.Color = color
	}
	// Пустая подсказка возвращает стандартную, чтобы модель не осталась без правил
	if input.SelectPrompt != nil {
		card.SelectPrompt = prompt_OrFallback(*input.SelectPrompt, texts.LiveSelectPrompt)
	}
	if input.AnswerPrompt != nil {
		card.AnswerPrompt = prompt_OrFallback(*input.AnswerPrompt, texts.LiveAnswerPrompt)
	}
	if input.ClarifyPrompt != nil {
		card.ClarifyPrompt = prompt_OrFallback(*input.ClarifyPrompt, texts.LiveClarifyPrompt)
	}
	if card.Title == "" {
		return ProcessCard{}, ErrInvalidBody
	}
	_, err = store.pool.Exec(ctx, `
		UPDATE processes
		SET title = $2, description = $3, color = $4, select_prompt = $5, answer_prompt = $6, clarify_prompt = $7
		WHERE process_id = $1`,
		card.ProcessID, card.Title, card.Description, card.Color, card.SelectPrompt, card.AnswerPrompt, card.ClarifyPrompt)
	if err != nil {
		return ProcessCard{}, err
	}
	return card, nil
}

// Документооборот удалить нельзя: туда переезжают документы удалённого процесса,
// а чаты с этим процессом снова ищут по всем
func (store *LiveStore) Delete_Process(ctx context.Context, token, processID string) error {
	account, err := store.Find_AccountByToken(ctx, token)
	if err != nil {
		return err
	}
	if !account.Has(texts.PermissionGlobalManage) {
		return ErrAccessDenied
	}
	if processID == texts.ProcessDocuments {
		return ErrAccessDenied
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `DELETE FROM processes WHERE process_id = $1`, processID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec(ctx, `UPDATE documents SET process_id = $2 WHERE process_id = $1`, processID, texts.ProcessDocuments); err != nil {
		return err
	}
	// Удаленный процесс выпадает из выбора чатов, остальные выбранные процессы остаются
	if _, err := tx.Exec(ctx, `
		UPDATE sessions SET process_id = array_to_string(array_remove(string_to_array(process_id, ','), $1), ',')
		WHERE $1 = ANY(string_to_array(process_id, ','))`, processID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (store *LiveStore) Find_ProcessCard(ctx context.Context, processID string) (ProcessCard, error) {
	var card ProcessCard
	err := store.pool.QueryRow(ctx, `
		SELECT process_id, title, description, color, select_prompt, answer_prompt, clarify_prompt
		FROM processes WHERE process_id = $1`, processID).Scan(
		&card.ProcessID, &card.Title, &card.Description, &card.Color, &card.SelectPrompt, &card.AnswerPrompt, &card.ClarifyPrompt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ProcessCard{}, ErrNotFound
	}
	return card, err
}

// Процессы, выбранные в чате, через запятую. Пустая строка - все процессы
func (store *LiveStore) Read_SessionProcess(ctx context.Context, sessionID string) (string, error) {
	var processID string
	err := store.pool.QueryRow(ctx, `SELECT process_id FROM sessions WHERE session_id = $1`, sessionID).Scan(&processID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return processID, err
}

func (store *LiveStore) Save_SessionProcess(ctx context.Context, sessionID, processID string) error {
	_, err := store.pool.Exec(ctx, `UPDATE sessions SET process_id = $2 WHERE session_id = $1`, sessionID, processID)
	return err
}

// Названия документов процесса, видимых сотруднику, для «карты» темы в подсказке модели
func (store *LiveStore) List_ProcessDocumentTitles(ctx context.Context, processID string, roleIDs []string, limit int) ([]string, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT d.title FROM documents d
		WHERE d.process_id = $1 AND d.scope = 'global' AND `+global_VisibleSQL("$2")+`
		ORDER BY d.title LIMIT $3`, processID, role_Filter(roleIDs), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	titles := []string{}
	for rows.Next() {
		var title string
		if err := rows.Scan(&title); err != nil {
			return nil, err
		}
		titles = append(titles, title)
	}
	return titles, rows.Err()
}

// Процесс каждого документа цитат: для метки темы у источника
func (store *LiveStore) Read_DocumentProcesses(ctx context.Context, documentIDs []string) (map[string]string, error) {
	processes := map[string]string{}
	if len(documentIDs) == 0 {
		return processes, nil
	}
	rows, err := store.pool.Query(ctx, `SELECT document_id, process_id FROM documents WHERE document_id = ANY($1::text[])`, documentIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var documentID, processID string
		if err := rows.Scan(&documentID, &processID); err != nil {
			return nil, err
		}
		processes[documentID] = processID
	}
	return processes, rows.Err()
}
