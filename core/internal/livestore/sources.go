package livestore

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"docfinder/core/internal/auth"
	"docfinder/core/internal/chunk"
	"docfinder/core/internal/source"
	"docfinder/core/internal/texts"
)

const settingsKeySourcesPermission = "permission_sources_manage"

type sourceSyncState struct {
	mu      sync.Mutex
	running map[string]bool
}

type DocumentSource struct {
	SourceID        string
	Kind            string
	Title           string
	Enabled         bool
	IntervalMinutes int
	ProcessID       string
	VisibleToAll    bool
	AccessLevel     int
	RoleIDs         []string
	Config          source.ConnectorConfig
	LastSyncAt      time.Time
	LastStatus      string
	LastError       string
	LastCursor      string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type SourceCreate struct {
	Kind            string
	Title           string
	IntervalMinutes int
	ProcessID       string
	VisibleToAll    bool
	AccessLevel     int
	RoleIDs         []string
	Config          source.ConnectorConfig
}

type SourcePatch struct {
	Title           *string
	Enabled         *bool
	IntervalMinutes *int
	ProcessID       *string
	VisibleToAll    *bool
	AccessLevel     *int
	RoleIDs         []string
	RoleIDsSet      bool
	Config          source.ConnectorConfig
	ConfigSet       bool
}

type SourceRun struct {
	SourceID string
	Status   string
	Error    string
	SyncedAt time.Time
	Indexed  int
	Skipped  int
}

func (store *LiveStore) List_Sources(ctx context.Context, token string) ([]DocumentSource, error) {
	if _, err := store.require_Permission(ctx, token, texts.PermissionSourcesManage); err != nil {
		return nil, err
	}
	rows, err := store.pool.Query(ctx, `
		SELECT s.source_id, s.kind, s.title, s.enabled, s.interval_minutes, s.process_id, s.visible_to_all, s.access_level,
			s.config, s.last_sync_at, s.last_status, s.last_error, s.last_cursor, s.created_at, s.updated_at,
			COALESCE((SELECT array_agg(sr.role_id ORDER BY sr.role_id) FROM document_source_roles sr WHERE sr.source_id = s.source_id), '{}')
		FROM document_sources s
		ORDER BY s.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	found := []DocumentSource{}
	for rows.Next() {
		line, scanError := scan_Source(rows)
		if scanError != nil {
			return nil, scanError
		}
		found = append(found, line)
	}
	return found, rows.Err()
}

func (store *LiveStore) Read_Source(ctx context.Context, token, sourceID string) (DocumentSource, error) {
	if _, err := store.require_Permission(ctx, token, texts.PermissionSourcesManage); err != nil {
		return DocumentSource{}, err
	}
	return store.read_Source(ctx, sourceID)
}

func (store *LiveStore) Create_Source(ctx context.Context, token string, input SourceCreate) (DocumentSource, error) {
	if _, err := store.require_Permission(ctx, token, texts.PermissionSourcesManage); err != nil {
		return DocumentSource{}, err
	}
	if !source.Known_Kind(input.Kind) {
		return DocumentSource{}, source.ErrUnknownKind
	}
	processID, err := store.Require_UploadProcess(ctx, input.ProcessID)
	if err != nil {
		return DocumentSource{}, err
	}
	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = input.Kind
	}
	interval := source.Normalize_Interval(input.IntervalMinutes)
	if input.IntervalMinutes == 0 {
		interval = source.DefaultIntervalMinutes
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return DocumentSource{}, err
	}
	defer tx.Rollback(ctx)
	if err := check_RoleIDs(ctx, tx, input.RoleIDs); err != nil {
		return DocumentSource{}, err
	}
	number, err := next_IDTx(ctx, tx, counterSource)
	if err != nil {
		return DocumentSource{}, err
	}
	sourceID := "source-" + strconv.Itoa(number)
	configRaw, err := json.Marshal(input.Config)
	if err != nil {
		return DocumentSource{}, ErrInvalidBody
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO document_sources (source_id, kind, title, enabled, interval_minutes, process_id, visible_to_all, access_level, config, last_status)
		VALUES ($1, $2, $3, false, $4, $5, $6, $7, $8::jsonb, $9)`,
		sourceID, input.Kind, title, interval, processID, input.VisibleToAll, input.AccessLevel, configRaw, texts.SourceStatusIdle)
	if err != nil {
		return DocumentSource{}, err
	}
	if err := replace_SourceRoles(ctx, tx, sourceID, input.RoleIDs); err != nil {
		return DocumentSource{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return DocumentSource{}, err
	}
	return store.read_Source(ctx, sourceID)
}

func (store *LiveStore) Patch_Source(ctx context.Context, token, sourceID string, patch SourcePatch) (DocumentSource, error) {
	if _, err := store.require_Permission(ctx, token, texts.PermissionSourcesManage); err != nil {
		return DocumentSource{}, err
	}
	current, err := store.read_Source(ctx, sourceID)
	if err != nil {
		return DocumentSource{}, err
	}
	title := current.Title
	if patch.Title != nil {
		title = strings.TrimSpace(*patch.Title)
		if title == "" {
			return DocumentSource{}, ErrInvalidBody
		}
	}
	interval := current.IntervalMinutes
	if patch.IntervalMinutes != nil {
		interval = source.Normalize_Interval(*patch.IntervalMinutes)
	}
	processID := current.ProcessID
	if patch.ProcessID != nil {
		processID, err = store.Require_UploadProcess(ctx, *patch.ProcessID)
		if err != nil {
			return DocumentSource{}, err
		}
	}
	visibleToAll := current.VisibleToAll
	if patch.VisibleToAll != nil {
		visibleToAll = *patch.VisibleToAll
	}
	accessLevel := current.AccessLevel
	if patch.AccessLevel != nil {
		accessLevel = *patch.AccessLevel
	}
	roleIDs := current.RoleIDs
	if patch.RoleIDsSet {
		roleIDs = patch.RoleIDs
	}
	config := current.Config
	if patch.ConfigSet {
		config = source.Merge_Config(current.Config, patch.Config)
	}
	enabled := current.Enabled
	if patch.Enabled != nil {
		enabled = *patch.Enabled
	}
	if enabled {
		if err := source.Check_CanEnable(current.Kind, config); err != nil {
			return DocumentSource{}, err
		}
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return DocumentSource{}, err
	}
	defer tx.Rollback(ctx)
	if err := check_RoleIDs(ctx, tx, roleIDs); err != nil {
		return DocumentSource{}, err
	}
	configRaw, err := json.Marshal(config)
	if err != nil {
		return DocumentSource{}, ErrInvalidBody
	}
	_, err = tx.Exec(ctx, `
		UPDATE document_sources
		SET title = $2, enabled = $3, interval_minutes = $4, process_id = $5, visible_to_all = $6, access_level = $7, config = $8::jsonb, updated_at = now()
		WHERE source_id = $1`,
		sourceID, title, enabled, interval, processID, visibleToAll, accessLevel, configRaw)
	if err != nil {
		return DocumentSource{}, err
	}
	if err := replace_SourceRoles(ctx, tx, sourceID, roleIDs); err != nil {
		return DocumentSource{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return DocumentSource{}, err
	}
	return store.read_Source(ctx, sourceID)
}

func (store *LiveStore) Delete_Source(ctx context.Context, token, sourceID string) error {
	if _, err := store.require_Permission(ctx, token, texts.PermissionSourcesManage); err != nil {
		return err
	}
	if _, err := store.read_Source(ctx, sourceID); err != nil {
		return err
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE documents SET source_id = '', source_external_id = '' WHERE source_id = $1`, sourceID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM document_sources WHERE source_id = $1`, sourceID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (store *LiveStore) Verify_Source(ctx context.Context, token, sourceID string) error {
	if _, err := store.require_Permission(ctx, token, texts.PermissionSourcesManage); err != nil {
		return err
	}
	record, err := store.read_Source(ctx, sourceID)
	if err != nil {
		return err
	}
	connector, err := source.Connector_ByKind(record.Kind)
	if err != nil {
		return err
	}
	return connector.Verify_Access(ctx, record.Config)
}

func (store *LiveStore) Sync_Source(ctx context.Context, token, sourceID string) (SourceRun, error) {
	if _, err := store.require_Permission(ctx, token, texts.PermissionSourcesManage); err != nil {
		return SourceRun{}, err
	}
	return store.run_SourceSync(ctx, sourceID)
}

func (store *LiveStore) Read_SourceRun(ctx context.Context, token, sourceID string) (SourceRun, error) {
	if _, err := store.require_Permission(ctx, token, texts.PermissionSourcesManage); err != nil {
		return SourceRun{}, err
	}
	record, err := store.read_Source(ctx, sourceID)
	if err != nil {
		return SourceRun{}, err
	}
	return SourceRun{SourceID: record.SourceID, Status: record.LastStatus, Error: record.LastError, SyncedAt: record.LastSyncAt}, nil
}

func (store *LiveStore) Start_SourceLoop() {
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			store.sync_DueSources(context.Background())
		}
	}()
}

func (store *LiveStore) sync_DueSources(ctx context.Context) {
	rows, err := store.pool.Query(ctx, `
		SELECT source_id FROM document_sources
		WHERE enabled AND (last_sync_at IS NULL OR last_sync_at + make_interval(mins => interval_minutes) <= now())`)
	if err != nil {
		log.Printf("source loop: %v", err)
		return
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var sourceID string
		if err := rows.Scan(&sourceID); err != nil {
			log.Printf("source loop: %v", err)
			return
		}
		ids = append(ids, sourceID)
	}
	for _, sourceID := range ids {
		if _, err := store.run_SourceSync(ctx, sourceID); err != nil && !errors.Is(err, ErrSourceSyncRunning) {
			log.Printf("source %s: %v", sourceID, err)
		}
	}
}

func (store *LiveStore) run_SourceSync(ctx context.Context, sourceID string) (SourceRun, error) {
	if !store.mark_SourceRunning(sourceID) {
		return SourceRun{}, ErrSourceSyncRunning
	}
	defer store.clear_SourceRunning(sourceID)
	record, err := store.read_Source(ctx, sourceID)
	if err != nil {
		return SourceRun{}, err
	}
	_ = store.write_SourceStatus(ctx, sourceID, texts.SourceStatusRunning, "", record.LastCursor)
	connector, err := source.Connector_ByKind(record.Kind)
	if err != nil {
		_ = store.write_SourceStatus(ctx, sourceID, texts.SourceStatusError, err.Error(), record.LastCursor)
		return SourceRun{SourceID: sourceID, Status: texts.SourceStatusError, Error: err.Error()}, err
	}
	files, nextCursor, err := source.Pull_Documents(ctx, connector, record.LastCursor, record.Config)
	if err != nil {
		_ = store.write_SourceStatus(ctx, sourceID, texts.SourceStatusError, err.Error(), record.LastCursor)
		return SourceRun{SourceID: sourceID, Status: texts.SourceStatusError, Error: err.Error()}, err
	}
	indexed := 0
	skipped := 0
	for _, file := range files {
		changed, indexError := store.Index_SourceDocument(ctx, sourceID, record, file)
		if indexError != nil {
			_ = store.write_SourceStatus(ctx, sourceID, texts.SourceStatusError, indexError.Error(), record.LastCursor)
			return SourceRun{SourceID: sourceID, Status: texts.SourceStatusError, Error: indexError.Error()}, indexError
		}
		if changed {
			indexed++
		} else {
			skipped++
		}
	}
	if err := store.write_SourceStatus(ctx, sourceID, texts.SourceStatusOK, "", nextCursor); err != nil {
		return SourceRun{}, err
	}
	return SourceRun{SourceID: sourceID, Status: texts.SourceStatusOK, SyncedAt: time.Now().UTC(), Indexed: indexed, Skipped: skipped}, nil
}

func (store *LiveStore) Index_SourceDocument(ctx context.Context, sourceID string, record DocumentSource, file source.FetchedFile) (bool, error) {
	if strings.TrimSpace(file.ExternalID) == "" || len(file.Body) == 0 {
		return false, nil
	}
	var storedChecksum string
	var documentID string
	err := store.pool.QueryRow(ctx, `
		SELECT document_id, checksum FROM document_source_items WHERE source_id = $1 AND external_id = $2`,
		sourceID, file.ExternalID).Scan(&documentID, &storedChecksum)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, err
	}
	if storedChecksum != "" && storedChecksum == file.Checksum {
		return false, nil
	}
	title := strings.TrimSpace(file.Title)
	if title == "" {
		title = file.FileName
	}
	edition := strings.TrimSpace(file.Edition)
	if edition == "" {
		edition = texts.EditionDefault
	}
	pieces, err := chunk.Extract_DocumentChunks(file.FileName, file.Body)
	if errors.Is(err, chunk.ErrUnsupportedFile) {
		return false, ErrUnsupportedFile
	}
	if err != nil {
		return false, ErrUnsupportedFile
	}
	contents := make([]string, 0, len(pieces))
	for _, piece := range pieces {
		contents = append(contents, chunk_EmbedText(title, piece.Section, piece.Paragraph, piece.Appendix, piece.Content))
	}
	vectors, err := store.embed_Texts(contents)
	if err != nil {
		return false, err
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if documentID == "" {
		number, nextError := next_IDTx(ctx, tx, counterDocument)
		if nextError != nil {
			return false, nextError
		}
		documentID = "doc-" + strconv.Itoa(number)
		_, err = tx.Exec(ctx, `
			INSERT INTO documents (document_id, title, edition, access_level, scope, owner_id, process_id, uploaded_at, file_name, content_type, body, visible_to_all, source_id, source_external_id)
			VALUES ($1, $2, $3, $4, 'global', '', $5, now(), $6, $7, $8, $9, $10, $11)`,
			documentID, title, edition, record.AccessLevel, upload_ProcessID(record.ProcessID), file.FileName, file.ContentType, file.Body, record.VisibleToAll, sourceID, file.ExternalID)
	} else {
		_, err = tx.Exec(ctx, `
			UPDATE documents
			SET title = $2, edition = $3, access_level = $4, process_id = $5, file_name = $6, content_type = $7, body = $8, uploaded_at = now(), visible_to_all = $9
			WHERE document_id = $1`,
			documentID, title, edition, record.AccessLevel, upload_ProcessID(record.ProcessID), file.FileName, file.ContentType, file.Body, record.VisibleToAll)
	}
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM document_roles WHERE document_id = $1`, documentID); err != nil {
		return false, err
	}
	for _, roleID := range record.RoleIDs {
		if _, err := tx.Exec(ctx, `INSERT INTO document_roles (document_id, role_id) VALUES ($1, $2)`, documentID, roleID); err != nil {
			return false, err
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM chunks WHERE document_id = $1`, documentID); err != nil {
		return false, err
	}
	for index, piece := range pieces {
		_, err = tx.Exec(ctx, `
			INSERT INTO chunks (document_id, section, paragraph, appendix, access_level, scope, owner_id, content, embedding)
			VALUES ($1, $2, $3, $4, $5, 'global', '', $6, $7::vector)`,
			documentID, piece.Section, piece.Paragraph, piece.Appendix, record.AccessLevel, piece.Content, format_Vector(vectors[index]))
		if err != nil {
			return false, err
		}
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO document_source_items (source_id, external_id, document_id, checksum, fetched_at)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (source_id, external_id) DO UPDATE SET document_id = EXCLUDED.document_id, checksum = EXCLUDED.checksum, fetched_at = now()`,
		sourceID, file.ExternalID, documentID, file.Checksum); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (store *LiveStore) read_Source(ctx context.Context, sourceID string) (DocumentSource, error) {
	row := store.pool.QueryRow(ctx, `
		SELECT s.source_id, s.kind, s.title, s.enabled, s.interval_minutes, s.process_id, s.visible_to_all, s.access_level,
			s.config, s.last_sync_at, s.last_status, s.last_error, s.last_cursor, s.created_at, s.updated_at,
			COALESCE((SELECT array_agg(sr.role_id ORDER BY sr.role_id) FROM document_source_roles sr WHERE sr.source_id = s.source_id), '{}')
		FROM document_sources s WHERE s.source_id = $1`, sourceID)
	line, err := scan_Source(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return DocumentSource{}, ErrNotFound
	}
	return line, err
}

type sourceRow interface {
	Scan(dest ...any) error
}

func scan_Source(row sourceRow) (DocumentSource, error) {
	var line DocumentSource
	var configRaw []byte
	var lastSync *time.Time
	err := row.Scan(&line.SourceID, &line.Kind, &line.Title, &line.Enabled, &line.IntervalMinutes, &line.ProcessID, &line.VisibleToAll, &line.AccessLevel,
		&configRaw, &lastSync, &line.LastStatus, &line.LastError, &line.LastCursor, &line.CreatedAt, &line.UpdatedAt, &line.RoleIDs)
	if err != nil {
		return DocumentSource{}, err
	}
	if lastSync != nil {
		line.LastSyncAt = *lastSync
	}
	if line.RoleIDs == nil {
		line.RoleIDs = []string{}
	}
	if len(configRaw) > 0 {
		_ = json.Unmarshal(configRaw, &line.Config)
	}
	return line, nil
}

func (store *LiveStore) write_SourceStatus(ctx context.Context, sourceID, status, message, cursor string) error {
	_, err := store.pool.Exec(ctx, `
		UPDATE document_sources
		SET last_status = $2, last_error = $3, last_cursor = $4, last_sync_at = CASE WHEN $2 = $5 THEN last_sync_at ELSE now() END, updated_at = now()
		WHERE source_id = $1`,
		sourceID, status, message, cursor, texts.SourceStatusRunning)
	return err
}

func replace_SourceRoles(ctx context.Context, tx pgx.Tx, sourceID string, roleIDs []string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM document_source_roles WHERE source_id = $1`, sourceID); err != nil {
		return err
	}
	for _, roleID := range roleIDs {
		if _, err := tx.Exec(ctx, `INSERT INTO document_source_roles (source_id, role_id) VALUES ($1, $2)`, sourceID, roleID); err != nil {
			return err
		}
	}
	return nil
}

func (store *LiveStore) mark_SourceRunning(sourceID string) bool {
	store.sourceSync.mu.Lock()
	defer store.sourceSync.mu.Unlock()
	if store.sourceSync.running == nil {
		store.sourceSync.running = map[string]bool{}
	}
	if store.sourceSync.running[sourceID] {
		return false
	}
	store.sourceSync.running[sourceID] = true
	return true
}

func (store *LiveStore) clear_SourceRunning(sourceID string) {
	store.sourceSync.mu.Lock()
	defer store.sourceSync.mu.Unlock()
	delete(store.sourceSync.running, sourceID)
}

func Audience_ForSource(roleIDs []string, accessLevel int) auth.DocumentAudience {
	if len(roleIDs) > 0 {
		return auth.Audience_FromRoleIDs(roleIDs)
	}
	return auth.Audience_FromLevel(accessLevel)
}
