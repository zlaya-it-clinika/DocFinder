package httpapi

import (
	"context"
	"strconv"
	"strings"
	"time"

	"docfinder/core/internal/auth"
	"docfinder/core/internal/livestore"
	"docfinder/core/internal/source"
	"docfinder/core/internal/texts"
)

type stubSourceItem struct {
	documentID string
	checksum   string
}

func sourceItemKey(sourceID, externalID string) string {
	return sourceID + "\n" + externalID
}

func (stubStore *stub) List_Sources(_ context.Context, token string) ([]livestore.DocumentSource, error) {
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	if err := stubStore.deny_UnlessSources(token); err != nil {
		return nil, err
	}
	lines := make([]livestore.DocumentSource, 0, len(stubStore.sources))
	for _, item := range stubStore.sources {
		lines = append(lines, copy_Source(*item))
	}
	return lines, nil
}

func (stubStore *stub) Create_Source(_ context.Context, token string, input livestore.SourceCreate) (livestore.DocumentSource, error) {
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	if err := stubStore.deny_UnlessSources(token); err != nil {
		return livestore.DocumentSource{}, err
	}
	if !source.Known_Kind(input.Kind) {
		return livestore.DocumentSource{}, source.ErrUnknownKind
	}
	processID, ok := stubStore.resolve_UploadProcess(input.ProcessID)
	if !ok {
		return livestore.DocumentSource{}, livestore.ErrInvalidBody
	}
	if !stubStore.known_RoleIDs(input.RoleIDs) {
		return livestore.DocumentSource{}, livestore.ErrInvalidBody
	}
	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = input.Kind
	}
	interval := source.Normalize_Interval(input.IntervalMinutes)
	if input.IntervalMinutes == 0 {
		interval = source.DefaultIntervalMinutes
	}
	now := time.Now().UTC()
	record := &livestore.DocumentSource{
		SourceID:        "source-" + strconv.Itoa(stubStore.nextSource),
		Kind:            input.Kind,
		Title:           title,
		Enabled:         false,
		IntervalMinutes: interval,
		ProcessID:       processID,
		VisibleToAll:    input.VisibleToAll,
		AccessLevel:     input.AccessLevel,
		RoleIDs:         append([]string{}, input.RoleIDs...),
		Config:          input.Config,
		LastStatus:      texts.SourceStatusIdle,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	stubStore.nextSource++
	stubStore.sources = append(stubStore.sources, record)
	stubStore.sourcesByID[record.SourceID] = record
	return copy_Source(*record), nil
}

func (stubStore *stub) Patch_Source(_ context.Context, token, sourceID string, patch livestore.SourcePatch) (livestore.DocumentSource, error) {
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	if err := stubStore.deny_UnlessSources(token); err != nil {
		return livestore.DocumentSource{}, err
	}
	record := stubStore.sourcesByID[sourceID]
	if record == nil {
		return livestore.DocumentSource{}, livestore.ErrNotFound
	}
	if patch.Title != nil {
		title := strings.TrimSpace(*patch.Title)
		if title == "" {
			return livestore.DocumentSource{}, livestore.ErrInvalidBody
		}
		record.Title = title
	}
	if patch.IntervalMinutes != nil {
		record.IntervalMinutes = source.Normalize_Interval(*patch.IntervalMinutes)
	}
	if patch.ProcessID != nil {
		processID, ok := stubStore.resolve_UploadProcess(*patch.ProcessID)
		if !ok {
			return livestore.DocumentSource{}, livestore.ErrInvalidBody
		}
		record.ProcessID = processID
	}
	if patch.VisibleToAll != nil {
		record.VisibleToAll = *patch.VisibleToAll
	}
	if patch.AccessLevel != nil {
		record.AccessLevel = *patch.AccessLevel
	}
	if patch.RoleIDsSet {
		if !stubStore.known_RoleIDs(patch.RoleIDs) {
			return livestore.DocumentSource{}, livestore.ErrInvalidBody
		}
		record.RoleIDs = append([]string{}, patch.RoleIDs...)
	}
	if patch.ConfigSet {
		record.Config = source.Merge_Config(record.Config, patch.Config)
	}
	if patch.Enabled != nil {
		if *patch.Enabled {
			if err := source.Check_CanEnable(record.Kind, record.Config); err != nil {
				return livestore.DocumentSource{}, err
			}
		}
		record.Enabled = *patch.Enabled
	}
	record.UpdatedAt = time.Now().UTC()
	return copy_Source(*record), nil
}

func (stubStore *stub) Delete_Source(_ context.Context, token, sourceID string) error {
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	if err := stubStore.deny_UnlessSources(token); err != nil {
		return err
	}
	if stubStore.sourcesByID[sourceID] == nil {
		return livestore.ErrNotFound
	}
	for _, document := range stubStore.documents {
		if document.sourceID == sourceID {
			document.sourceID = ""
			document.sourceExternalID = ""
		}
	}
	for key := range stubStore.sourceItems {
		if strings.HasPrefix(key, sourceID+"\n") {
			delete(stubStore.sourceItems, key)
		}
	}
	delete(stubStore.sourcesByID, sourceID)
	remaining := stubStore.sources[:0]
	for _, item := range stubStore.sources {
		if item.SourceID != sourceID {
			remaining = append(remaining, item)
		}
	}
	stubStore.sources = remaining
	return nil
}

func (stubStore *stub) Verify_Source(ctx context.Context, token, sourceID string) error {
	stubStore.mu.Lock()
	record, err := stubStore.source_ForManager(token, sourceID)
	config := source.ConnectorConfig{}
	kind := ""
	if err == nil {
		config = record.Config
		kind = record.Kind
	}
	stubStore.mu.Unlock()
	if err != nil {
		return err
	}
	connector, connectorError := source.Connector_ByKind(kind)
	if connectorError != nil {
		return connectorError
	}
	return connector.Verify_Access(ctx, config)
}

func (stubStore *stub) Sync_Source(ctx context.Context, token, sourceID string) (livestore.SourceRun, error) {
	stubStore.mu.Lock()
	if err := stubStore.deny_UnlessSources(token); err != nil {
		stubStore.mu.Unlock()
		return livestore.SourceRun{}, err
	}
	record := stubStore.sourcesByID[sourceID]
	if record == nil {
		stubStore.mu.Unlock()
		return livestore.SourceRun{}, livestore.ErrNotFound
	}
	if stubStore.sourceRunning[sourceID] {
		stubStore.mu.Unlock()
		return livestore.SourceRun{}, livestore.ErrSourceSyncRunning
	}
	stubStore.sourceRunning[sourceID] = true
	snapshot := copy_Source(*record)
	stubStore.mu.Unlock()
	defer func() {
		stubStore.mu.Lock()
		delete(stubStore.sourceRunning, sourceID)
		stubStore.mu.Unlock()
	}()
	connector, err := source.Connector_ByKind(snapshot.Kind)
	if err != nil {
		stubStore.note_SourceError(sourceID, err)
		return livestore.SourceRun{SourceID: sourceID, Status: texts.SourceStatusError, Error: err.Error()}, err
	}
	files, nextCursor, err := source.Pull_Documents(ctx, connector, snapshot.LastCursor, snapshot.Config)
	if err != nil {
		stubStore.note_SourceError(sourceID, err)
		return livestore.SourceRun{SourceID: sourceID, Status: texts.SourceStatusError, Error: err.Error()}, err
	}
	indexed := 0
	skipped := 0
	for _, file := range files {
		changed, indexError := stubStore.index_SourceFile(snapshot, file)
		if indexError != nil {
			stubStore.note_SourceError(sourceID, indexError)
			return livestore.SourceRun{SourceID: sourceID, Status: texts.SourceStatusError, Error: indexError.Error()}, indexError
		}
		if changed {
			indexed++
		} else {
			skipped++
		}
	}
	stubStore.mu.Lock()
	if current := stubStore.sourcesByID[sourceID]; current != nil {
		current.LastStatus = texts.SourceStatusOK
		current.LastError = ""
		current.LastCursor = nextCursor
		current.LastSyncAt = time.Now().UTC()
		current.UpdatedAt = current.LastSyncAt
		snapshot = copy_Source(*current)
	}
	stubStore.mu.Unlock()
	return livestore.SourceRun{SourceID: sourceID, Status: texts.SourceStatusOK, SyncedAt: snapshot.LastSyncAt, Indexed: indexed, Skipped: skipped}, nil
}

func (stubStore *stub) Read_SourceRun(_ context.Context, token, sourceID string) (livestore.SourceRun, error) {
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	record, err := stubStore.source_ForManager(token, sourceID)
	if err != nil {
		return livestore.SourceRun{}, err
	}
	return livestore.SourceRun{SourceID: record.SourceID, Status: record.LastStatus, Error: record.LastError, SyncedAt: record.LastSyncAt}, nil
}

func (stubStore *stub) index_SourceFile(record livestore.DocumentSource, file source.FetchedFile) (bool, error) {
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	key := sourceItemKey(record.SourceID, file.ExternalID)
	if item, found := stubStore.sourceItems[key]; found && item.checksum == file.Checksum {
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
	documentID := ""
	if item, found := stubStore.sourceItems[key]; found {
		documentID = item.documentID
		if document := stubStore.documentsByID[documentID]; document != nil {
			document.title = title
			document.edition = edition
			document.accessLevel = record.AccessLevel
			document.processID = record.ProcessID
			document.fileName = file.FileName
			document.contentType = file.ContentType
			document.body = file.Body
			document.visibleToAll = record.VisibleToAll
			document.roleIDs = append([]string{}, record.RoleIDs...)
			document.uploadedAt = time.Now().UTC()
			document.sourceID = record.SourceID
			document.sourceExternalID = file.ExternalID
		}
	} else {
		created := stubStore.store_Document(title, edition, record.AccessLevel, texts.ScopeGlobal, "", record.ProcessID, file.FileName, file.ContentType, file.Body, record.VisibleToAll, record.RoleIDs)
		documentID = created.DocumentID
		if document := stubStore.documentsByID[documentID]; document != nil {
			document.sourceID = record.SourceID
			document.sourceExternalID = file.ExternalID
		}
	}
	stubStore.sourceItems[key] = stubSourceItem{documentID: documentID, checksum: file.Checksum}
	return true, nil
}

func (stubStore *stub) deny_UnlessSources(token string) error {
	record := stubStore.usersByID[stubStore.tokens[token]]
	if record == nil {
		return livestore.ErrUnauthorized
	}
	if record.pending {
		return livestore.ErrAccountPending
	}
	if !auth.Has_Permission(stubStore.permissions_Of(record), texts.PermissionSourcesManage) {
		return livestore.ErrAccessDenied
	}
	return nil
}

func (stubStore *stub) source_ForManager(token, sourceID string) (*livestore.DocumentSource, error) {
	if err := stubStore.deny_UnlessSources(token); err != nil {
		return nil, err
	}
	record := stubStore.sourcesByID[sourceID]
	if record == nil {
		return nil, livestore.ErrNotFound
	}
	return record, nil
}

func (stubStore *stub) note_SourceError(sourceID string, err error) {
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	if current := stubStore.sourcesByID[sourceID]; current != nil {
		current.LastStatus = texts.SourceStatusError
		current.LastError = err.Error()
		current.LastSyncAt = time.Now().UTC()
		current.UpdatedAt = current.LastSyncAt
	}
}

func copy_Source(line livestore.DocumentSource) livestore.DocumentSource {
	line.RoleIDs = append([]string{}, line.RoleIDs...)
	return line
}
