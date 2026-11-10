package livestore

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"docfinder/core/internal/auth"
	"docfinder/core/internal/chunk"
	"docfinder/core/internal/contacts"
	"docfinder/core/internal/texts"
)

// EngineDocument - полный общий документ для синхронизации отдельного поискового движка.
// Текст собирается из уже разобранных фрагментов: так двоичные DOCX/PDF не передаются как строка.
type EngineDocument struct {
	Text     string
	FileName string
	Category string
}

func (store *LiveStore) Export_EngineDocuments(ctx context.Context) ([]EngineDocument, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT d.file_name, d.process_id,
			COALESCE(string_agg(
				concat_ws(E'\n', NULLIF(c.section, ''), NULLIF(c.paragraph, ''), NULLIF(c.appendix, ''), c.content),
				E'\n\n' ORDER BY c.chunk_id
			), '')
		FROM documents d
		LEFT JOIN chunks c ON c.document_id = d.document_id
		WHERE d.scope = 'global' AND d.visible_to_all
		GROUP BY d.document_id, d.file_name, d.process_id
		ORDER BY d.document_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	documents := []EngineDocument{}
	for rows.Next() {
		var document EngineDocument
		var processID string
		if err := rows.Scan(&document.FileName, &processID, &document.Text); err != nil {
			return nil, err
		}
		if strings.TrimSpace(document.Text) == "" {
			continue
		}
		foldedName := strings.ToLower(document.FileName)
		switch {
		case strings.HasPrefix(foldedName, "законы__"), processID == "laws", processID == "law":
			document.Category = "law"
		default:
			document.Category = "company"
		}
		documents = append(documents, document)
	}
	return documents, rows.Err()
}

type UploadInput struct {
	FileName     string
	ContentType  string
	Body         []byte
	Title        string
	Edition      string
	AccessLevel  int
	Scope        string
	OwnerID      string
	ProcessID    string
	VisibleToAll bool
	RoleIDs      []string
	// Документ стандартного набора и SHA-256 его файла в наборе
	Standard     bool
	StandardHash string
}

type UploadResult struct {
	DocumentID string
	Edition    string
	Scope      string
}

func (store *LiveStore) Index_UploadedDocument(ctx context.Context, token string, input UploadInput) (UploadResult, error) {
	if _, err := store.Find_AccountByToken(ctx, token); err != nil {
		return UploadResult{}, err
	}
	return store.index_Document(ctx, input, true)
}

// withVectors false - фрагменты сохраняются без вектора, их досчитывает Start_PendingReindex.
// Так стандартный набор загружается при запуске, даже если Ollama еще недоступна
func (store *LiveStore) index_Document(ctx context.Context, input UploadInput, withVectors bool) (UploadResult, error) {
	// Таблица данных .docx (например, Матрица визирования) режется по строкам с подписями столбцов
	pieces, err := chunk.Extract_DocumentChunks(input.FileName, input.Body)
	if errors.Is(err, chunk.ErrUnsupportedFile) {
		return UploadResult{}, ErrUnsupportedFile
	}
	if err != nil {
		return UploadResult{}, ErrUnsupportedFile
	}
	vectors := make([][]float32, len(pieces))
	if withVectors {
		contents := make([]string, 0, len(pieces))
		for _, piece := range pieces {
			contents = append(contents, chunk_EmbedText(input.Title, piece.Section, piece.Paragraph, piece.Appendix, piece.Content))
		}
		vectors, err = store.embed_Texts(contents)
		if err != nil {
			return UploadResult{}, err
		}
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return UploadResult{}, err
	}
	defer tx.Rollback(ctx)

	var documentID string
	err = tx.QueryRow(ctx, `
		SELECT document_id FROM documents
		WHERE title = $1 AND scope = $2 AND source_id = '' AND (($2 = 'global') OR owner_id = $3)`,
		input.Title, input.Scope, input.OwnerID).Scan(&documentID)
	if err == nil {
		// Прежняя редакция с другим содержимым уходит в архив. Тот же файл повторно архив не засоряет
		if _, err := tx.Exec(ctx, `
			INSERT INTO document_editions (document_id, edition, file_name, content_type, body, uploaded_at)
			SELECT document_id, edition, file_name, content_type, body, uploaded_at
			FROM documents WHERE document_id = $1 AND body <> $2`, documentID, input.Body); err != nil {
			return UploadResult{}, err
		}
	}
	if errors.Is(err, pgx.ErrNoRows) {
		number, nextError := next_IDTx(ctx, tx, counterDocument)
		if nextError != nil {
			return UploadResult{}, nextError
		}
		documentID = "doc-" + strconv.Itoa(number)
		_, err = tx.Exec(ctx, `
			INSERT INTO documents (document_id, title, edition, access_level, scope, owner_id, process_id, uploaded_at, file_name, content_type, body, visible_to_all, standard, standard_hash)
			VALUES ($1, $2, $3, $4, $5, $6, $7, now(), $8, $9, $10, $11, $12, $13)`,
			documentID, input.Title, input.Edition, input.AccessLevel, input.Scope, input.OwnerID, upload_ProcessID(input.ProcessID), input.FileName, input.ContentType, input.Body, input.VisibleToAll, input.Standard, input.StandardHash)
	} else if err != nil {
		return UploadResult{}, err
	} else {
		// Новая редакция стандартного документа от администратора остается в наборе: флаг и хеш файла набора не меняются
		_, err = tx.Exec(ctx, `
			UPDATE documents
			SET edition = $2, access_level = $3, process_id = $4, file_name = $5, content_type = $6, body = $7, uploaded_at = now(), visible_to_all = $8,
				standard = standard OR $9, standard_hash = CASE WHEN $9 THEN $10 ELSE standard_hash END
			WHERE document_id = $1`,
			documentID, input.Edition, input.AccessLevel, upload_ProcessID(input.ProcessID), input.FileName, input.ContentType, input.Body, input.VisibleToAll, input.Standard, input.StandardHash)
	}
	if err != nil {
		if is_UniqueViolation(err) {
			return UploadResult{}, err
		}
		return UploadResult{}, err
	}
	if err := check_RoleIDs(ctx, tx, input.RoleIDs); err != nil {
		return UploadResult{}, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM document_roles WHERE document_id = $1`, documentID); err != nil {
		return UploadResult{}, err
	}
	for _, roleID := range input.RoleIDs {
		if _, err := tx.Exec(ctx, `INSERT INTO document_roles (document_id, role_id) VALUES ($1, $2)`, documentID, roleID); err != nil {
			return UploadResult{}, err
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM chunks WHERE document_id = $1`, documentID); err != nil {
		return UploadResult{}, err
	}
	for index, piece := range pieces {
		var embedding *string
		if len(vectors[index]) > 0 {
			formatted := format_Vector(vectors[index])
			embedding = &formatted
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO chunks (document_id, section, paragraph, appendix, access_level, scope, owner_id, content, embedding)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::vector)`,
			documentID, piece.Section, piece.Paragraph, piece.Appendix, input.AccessLevel, input.Scope, input.OwnerID, piece.Content, embedding)
		if err != nil {
			return UploadResult{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return UploadResult{}, err
	}
	return UploadResult{DocumentID: documentID, Edition: input.Edition, Scope: input.Scope}, nil
}

func upload_ProcessID(processID string) string {
	if strings.TrimSpace(processID) == "" {
		return texts.ProcessDocuments
	}
	return processID
}

// Текст для эмбеддинга фрагмента: «адрес» (документ, раздел, пункт, приложение) и сам текст.
// По адресу вопрос находит нужный пункт чаще, а вопрос не по теме уходит дальше от документов.
// В базе хранится только текст фрагмента, адрес нужен лишь для вектора
func chunk_EmbedText(title, section, paragraph, appendix, content string) string {
	var builder strings.Builder
	builder.WriteString(strings.TrimSpace(title))
	builder.WriteString(".")
	for _, part := range []struct{ label, value string }{
		{texts.LiveSectionLabel, section},
		{texts.LiveParagraphLabel, paragraph},
		{texts.LiveAppendixLabel, appendix},
	} {
		if strings.TrimSpace(part.value) != "" {
			builder.WriteString(" " + part.label + " " + strings.TrimSpace(part.value) + ".")
		}
	}
	builder.WriteString("\n")
	builder.WriteString(content)
	return builder.String()
}

func (store *LiveStore) embed_Texts(contents []string) ([][]float32, error) {
	if len(contents) == 0 {
		return nil, nil
	}
	if store.embedder == nil {
		return nil, ErrEmbedUnavailable
	}
	vectors, err := store.embedder.Embed_Texts(contents)
	if err != nil || len(vectors) != len(contents) {
		return nil, ErrEmbedUnavailable
	}
	for _, vector := range vectors {
		if len(vector) != store.dimensions {
			return nil, ErrEmbedSize
		}
	}
	return vectors, nil
}

func (store *LiveStore) List_VisibleDocuments(ctx context.Context, token, scopeFilter, processFilter string) ([]DocumentLine, error) {
	account, err := store.Find_AccountByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if scopeFilter == texts.ScopeAll && !account.Has(texts.PermissionPersonalViewAll) {
		return nil, ErrAccessDenied
	}
	visibleSQL := global_VisibleSQL("$3")
	rows, err := store.pool.Query(ctx, `
		SELECT d.document_id, d.title, d.edition, d.access_level, d.scope, d.owner_id,
			COALESCE(u.display_name, ''), d.uploaded_at, d.process_id, d.visible_to_all,
			COALESCE((SELECT array_agg(dr.role_id ORDER BY dr.role_id) FROM document_roles dr WHERE dr.document_id = d.document_id), '{}'),
			d.standard, (SELECT count(*) FROM document_editions e WHERE e.document_id = d.document_id)
		FROM documents d
		LEFT JOIN users u ON u.user_id = d.owner_id AND d.owner_id <> ''
		WHERE
			(
				($2 = 'all' AND (d.scope = 'personal' OR (d.scope = 'global' AND `+visibleSQL+`)))
				OR ($2 = 'global' AND d.scope = 'global' AND `+visibleSQL+`)
				OR ($2 = 'personal' AND d.scope = 'personal' AND d.owner_id = $1)
				OR ($2 = '' AND (
					(d.scope = 'global' AND `+visibleSQL+`)
					OR (d.scope = 'personal' AND d.owner_id = $1)
				))
			)
			AND ($4 = '' OR d.process_id = ANY(string_to_array($4, ',')))
		ORDER BY d.document_id`, account.UserID, scopeFilter, role_Filter(account.RoleIDs), processFilter)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	lines := []DocumentLine{}
	for rows.Next() {
		var line DocumentLine
		if err := rows.Scan(&line.DocumentID, &line.Title, &line.Edition, &line.AccessLevel, &line.Scope, &line.OwnerID, &line.OwnerName, &line.UploadedAt, &line.ProcessID, &line.VisibleToAll, &line.RoleIDs, &line.Standard, &line.ArchivedEditions); err != nil {
			return nil, err
		}
		if line.RoleIDs == nil {
			line.RoleIDs = []string{}
		}
		lines = append(lines, line)
	}
	return lines, rows.Err()
}

func (store *LiveStore) Read_DocumentFile(ctx context.Context, token, documentID string) (DocumentFile, error) {
	account, err := store.Find_AccountByToken(ctx, token)
	if err != nil {
		return DocumentFile{}, err
	}
	var file DocumentFile
	var scope string
	var ownerID string
	var visibleToAll bool
	var documentRoleIDs []string
	err = store.pool.QueryRow(ctx, `
		SELECT scope, owner_id, visible_to_all,
			COALESCE((SELECT array_agg(role_id) FROM document_roles WHERE document_id = $1), '{}'),
			file_name, content_type, body
		FROM documents WHERE document_id = $1`, documentID).Scan(&scope, &ownerID, &visibleToAll, &documentRoleIDs, &file.FileName, &file.ContentType, &file.Body)
	if errors.Is(err, pgx.ErrNoRows) {
		return DocumentFile{}, ErrNotFound
	}
	if err != nil {
		return DocumentFile{}, err
	}
	if status := document_ReadError(account, scope, ownerID, visibleToAll, documentRoleIDs); status != nil {
		return DocumentFile{}, status
	}
	return file, nil
}

func (store *LiveStore) Delete_Document(ctx context.Context, token, documentID string) error {
	account, err := store.Find_AccountByToken(ctx, token)
	if err != nil {
		return err
	}
	var scope string
	var ownerID string
	var visibleToAll bool
	var documentRoleIDs []string
	var standard bool
	err = store.pool.QueryRow(ctx, `
		SELECT scope, owner_id, visible_to_all,
			COALESCE((SELECT array_agg(role_id) FROM document_roles WHERE document_id = $1), '{}'), standard
		FROM documents WHERE document_id = $1`, documentID).Scan(&scope, &ownerID, &visibleToAll, &documentRoleIDs, &standard)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if status := document_ReadError(account, scope, ownerID, visibleToAll, documentRoleIDs); status != nil {
		return status
	}
	if !document_CanDelete(account, scope, ownerID) {
		return ErrAccessDenied
	}
	// Стандартный набор всегда в базе знаний: его можно заменить новой редакцией, но не удалить
	if standard {
		return ErrStandardDocument
	}
	_, err = store.pool.Exec(ctx, `DELETE FROM documents WHERE document_id = $1`, documentID)
	return err
}

func document_ReadError(account Account, scope, ownerID string, visibleToAll bool, documentRoleIDs []string) error {
	if scope == texts.ScopePersonal {
		if account.UserID == ownerID || account.Has(texts.PermissionPersonalViewAll) {
			return nil
		}
		return ErrNotFound
	}
	if auth.Sees_GlobalDocument(account.RoleIDs, visibleToAll, documentRoleIDs) {
		return nil
	}
	return ErrAccessDenied
}

func document_CanDelete(account Account, scope, ownerID string) bool {
	if scope == texts.ScopePersonal {
		return account.UserID == ownerID || account.Has(texts.PermissionPersonalViewAll)
	}
	return account.Has(texts.PermissionGlobalManage)
}

func global_VisibleSQL(roleParam string) string {
	return `(d.visible_to_all OR EXISTS (
		SELECT 1 FROM document_roles dr
		WHERE dr.document_id = d.document_id AND dr.role_id = ANY(` + roleParam + `::text[])))`
}

// Чужой личный документ не попадает в поиск, даже при documents.personal.view_all: этот флаг открывает список и удаление.
// Фрагменты без вектора (ждут пересчета после смены модели эмбеддингов) в поиск не попадают
func (store *LiveStore) Find_ChunksBySimilarity(ctx context.Context, embedding []float32, userID string, roleIDs []string, processID string, limit int) ([]SimilarChunk, error) {
	return store.query_Chunks(ctx, embedding, limit, `
		SELECT d.document_id, d.title, d.edition, c.section, c.paragraph, c.appendix, c.content, d.scope,
			c.embedding <=> $1::vector AS distance
		FROM chunks c
		JOIN documents d ON d.document_id = c.document_id
		WHERE ((d.scope = 'global' AND `+global_VisibleSQL("$2")+`)
			OR (d.scope = 'personal' AND d.owner_id = $3))
			AND NOT `+contacts.Title_SQLCondition()+`
			AND ($4 = '' OR d.process_id = ANY(string_to_array($4, ',')))
			AND c.embedding IS NOT NULL
		ORDER BY c.embedding <=> $1::vector
		LIMIT $5`, role_Filter(roleIDs), userID, processID)
}

func (store *LiveStore) Find_ClosedGlobalChunks(ctx context.Context, embedding []float32, roleIDs []string, processID string, limit int) ([]SimilarChunk, error) {
	return store.query_Chunks(ctx, embedding, limit, `
		SELECT d.document_id, d.title, d.edition, c.section, c.paragraph, c.appendix, c.content, d.scope,
			c.embedding <=> $1::vector AS distance
		FROM chunks c
		JOIN documents d ON d.document_id = c.document_id
		WHERE d.scope = 'global' AND NOT d.visible_to_all
			AND NOT EXISTS (
				SELECT 1 FROM document_roles dr
				WHERE dr.document_id = d.document_id AND dr.role_id = ANY($2::text[])
			)
			AND NOT `+contacts.Title_SQLCondition()+`
			AND ($3 = '' OR d.process_id = ANY(string_to_array($3, ',')))
			AND c.embedding IS NOT NULL
		ORDER BY c.embedding <=> $1::vector
		LIMIT $4`, role_Filter(roleIDs), processID)
}

func (store *LiveStore) Find_ContactChunk(ctx context.Context, embedding []float32, userID string, roleIDs []string, processID string) (SimilarChunk, bool, error) {
	found, err := store.query_Chunks(ctx, embedding, 1, `
		SELECT d.document_id, d.title, d.edition, c.section, c.paragraph, c.appendix, c.content, d.scope,
			c.embedding <=> $1::vector AS distance
		FROM chunks c
		JOIN documents d ON d.document_id = c.document_id
		WHERE ((d.scope = 'global' AND `+global_VisibleSQL("$2")+`)
			OR (d.scope = 'personal' AND d.owner_id = $3))
			AND `+contacts.Title_SQLCondition()+`
			AND ($4 = '' OR d.process_id = ANY(string_to_array($4, ',')))
			AND c.embedding IS NOT NULL
		ORDER BY c.embedding <=> $1::vector
		LIMIT $5`, role_Filter(roleIDs), userID, processID)
	if err != nil {
		return SimilarChunk{}, false, err
	}
	if len(found) == 0 || similarity_OfChunk(found[0]) < minReplySimilarity {
		return SimilarChunk{}, false, nil
	}
	return found[0], true, nil
}

func (store *LiveStore) query_Chunks(ctx context.Context, embedding []float32, limit int, query string, args ...any) ([]SimilarChunk, error) {
	if len(embedding) != store.dimensions {
		return nil, ErrEmbedSize
	}
	if limit <= 0 {
		limit = 5
	}
	queryArgs := []any{format_Vector(embedding)}
	queryArgs = append(queryArgs, args...)
	queryArgs = append(queryArgs, limit)
	rows, err := store.pool.Query(ctx, query, queryArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	found := []SimilarChunk{}
	for rows.Next() {
		var chunkRow SimilarChunk
		if err := rows.Scan(&chunkRow.DocumentID, &chunkRow.Title, &chunkRow.Edition, &chunkRow.Section, &chunkRow.Paragraph, &chunkRow.Appendix, &chunkRow.Content, &chunkRow.Scope, &chunkRow.Distance); err != nil {
			return nil, err
		}
		found = append(found, chunkRow)
	}
	return found, rows.Err()
}

func format_Vector(values []float32) string {
	var builder strings.Builder
	builder.WriteByte('[')
	for index, value := range values {
		if index > 0 {
			builder.WriteByte(',')
		}
		builder.WriteString(strconv.FormatFloat(float64(value), 'f', -1, 32))
	}
	builder.WriteByte(']')
	return builder.String()
}

type queryRower interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func next_IDTx(ctx context.Context, tx queryRower, name string) (int, error) {
	var value int
	err := tx.QueryRow(ctx, `UPDATE id_counters SET value = value + 1 WHERE name = $1 RETURNING value - 1`, name).Scan(&value)
	return value, err
}

// Начало документа для справки о нем: первые фрагменты по порядку и разделы в порядке появления.
// Права проверяет вызывающий: документ берется из списка видимых сотруднику
func (store *LiveStore) Read_DocumentOutline(ctx context.Context, documentID string, limit int) ([]SimilarChunk, []string, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT d.document_id, d.title, d.edition, c.section, c.paragraph, c.appendix, c.content, d.scope
		FROM chunks c
		JOIN documents d ON d.document_id = c.document_id
		WHERE c.document_id = $1
		ORDER BY c.chunk_id`, documentID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	opening := []SimilarChunk{}
	sections := []string{}
	seen := map[string]bool{}
	for rows.Next() {
		var chunkRow SimilarChunk
		if err := rows.Scan(&chunkRow.DocumentID, &chunkRow.Title, &chunkRow.Edition, &chunkRow.Section, &chunkRow.Paragraph, &chunkRow.Appendix, &chunkRow.Content, &chunkRow.Scope); err != nil {
			return nil, nil, err
		}
		if len(opening) < limit {
			opening = append(opening, chunkRow)
		}
		section := strings.TrimSpace(chunkRow.Section)
		if section != "" && !seen[section] {
			seen[section] = true
			sections = append(sections, section)
		}
	}
	return opening, sections, rows.Err()
}
