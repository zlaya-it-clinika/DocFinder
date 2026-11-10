package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"docfinder/core/internal/auth"
	"docfinder/core/internal/livestore"
	"docfinder/core/internal/source"
	"docfinder/core/internal/texts"
)

type sourceBackend interface {
	List_Sources(ctx context.Context, token string) ([]livestore.DocumentSource, error)
	Create_Source(ctx context.Context, token string, input livestore.SourceCreate) (livestore.DocumentSource, error)
	Patch_Source(ctx context.Context, token, sourceID string, patch livestore.SourcePatch) (livestore.DocumentSource, error)
	Delete_Source(ctx context.Context, token, sourceID string) error
	Verify_Source(ctx context.Context, token, sourceID string) error
	Sync_Source(ctx context.Context, token, sourceID string) (livestore.SourceRun, error)
	Read_SourceRun(ctx context.Context, token, sourceID string) (livestore.SourceRun, error)
}

type sourcesAPI struct {
	store sourceBackend
}

type sourceResponse struct {
	SourceID        string              `json:"sourceId"`
	Kind            string              `json:"kind"`
	Title           string              `json:"title"`
	Enabled         bool                `json:"enabled"`
	IntervalMinutes int                 `json:"intervalMinutes"`
	ProcessID       string              `json:"processId"`
	VisibleToAll    bool                `json:"visibleToAll"`
	AccessLevel     int                 `json:"accessLevel"`
	RoleIDs         []string            `json:"roleIds"`
	Config          source.PublicConfig `json:"config"`
	LastSyncAt      string              `json:"lastSyncAt,omitempty"`
	LastStatus      string              `json:"lastStatus"`
	LastError       string              `json:"lastError,omitempty"`
}

type sourceRunResponse struct {
	SourceID string `json:"sourceId"`
	Status   string `json:"status"`
	Error    string `json:"error,omitempty"`
	SyncedAt string `json:"syncedAt,omitempty"`
	Indexed  int    `json:"indexed"`
	Skipped  int    `json:"skipped"`
}

type sourceWriteRequest struct {
	Kind            string                 `json:"kind"`
	Title           string                 `json:"title"`
	Enabled         *bool                  `json:"enabled"`
	IntervalMinutes *int                   `json:"intervalMinutes"`
	ProcessID       string                 `json:"processId"`
	VisibleToAll    *bool                  `json:"visibleToAll"`
	AccessLevel     *int                   `json:"accessLevel"`
	RoleIDs         []string               `json:"roleIds"`
	Config          source.ConnectorConfig `json:"config"`
}

func register_SourceRoutes(routeMux *http.ServeMux, store sourceBackend) {
	api := sourcesAPI{store: store}
	routeMux.HandleFunc("GET /sources", api.serve_List)
	routeMux.HandleFunc("GET /sources/kinds", api.serve_Kinds)
	routeMux.HandleFunc("POST /sources", api.serve_Create)
	routeMux.HandleFunc("PATCH /sources/{sourceId}", api.serve_Patch)
	routeMux.HandleFunc("DELETE /sources/{sourceId}", api.serve_Delete)
	routeMux.HandleFunc("POST /sources/{sourceId}/verify", api.serve_Verify)
	routeMux.HandleFunc("POST /sources/{sourceId}/sync", api.serve_Sync)
	routeMux.HandleFunc("GET /sources/{sourceId}/runs", api.serve_Run)
}

func (api sourcesAPI) serve_Kinds(responseWriter http.ResponseWriter, request *http.Request) {
	if _, ok := require_Bearer(responseWriter, request); !ok {
		return
	}
	write_JSONResponse(responseWriter, http.StatusOK, struct {
		Kinds []source.KindInfo `json:"kinds"`
	}{Kinds: source.Kind_Catalog()})
}

func (api sourcesAPI) serve_List(responseWriter http.ResponseWriter, request *http.Request) {
	token, ok := require_Bearer(responseWriter, request)
	if !ok {
		return
	}
	lines, err := api.store.List_Sources(request.Context(), token)
	if err != nil {
		write_SourceError(responseWriter, err)
		return
	}
	payload := make([]sourceResponse, 0, len(lines))
	for _, line := range lines {
		payload = append(payload, source_ToResponse(line))
	}
	write_JSONResponse(responseWriter, http.StatusOK, struct {
		Sources []sourceResponse `json:"sources"`
	}{Sources: payload})
}

func (api sourcesAPI) serve_Create(responseWriter http.ResponseWriter, request *http.Request) {
	var body sourceWriteRequest
	if err := read_JSONBody(responseWriter, request, &body); err != nil {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	token, ok := require_Bearer(responseWriter, request)
	if !ok {
		return
	}
	audience := livestore.Audience_ForSource(auth.Unique_IDs(body.RoleIDs), default_SourceAccess(body.AccessLevel))
	interval := source.DefaultIntervalMinutes
	if body.IntervalMinutes != nil {
		interval = *body.IntervalMinutes
	}
	created, err := api.store.Create_Source(request.Context(), token, livestore.SourceCreate{
		Kind:            strings.TrimSpace(body.Kind),
		Title:           body.Title,
		IntervalMinutes: interval,
		ProcessID:       body.ProcessID,
		VisibleToAll:    audience.VisibleToAll,
		AccessLevel:     audience.AccessLevel,
		RoleIDs:         audience.RoleIDs,
		Config:          body.Config,
	})
	if err != nil {
		write_SourceError(responseWriter, err)
		return
	}
	write_JSONResponse(responseWriter, http.StatusOK, source_ToResponse(created))
}

func (api sourcesAPI) serve_Patch(responseWriter http.ResponseWriter, request *http.Request) {
	var body sourceWriteRequest
	if err := read_JSONBody(responseWriter, request, &body); err != nil {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	token, ok := require_Bearer(responseWriter, request)
	if !ok {
		return
	}
	patch := livestore.SourcePatch{
		Enabled:   body.Enabled,
		ProcessID: optional_String(body.ProcessID),
		Config:    body.Config,
		ConfigSet: true,
	}
	if strings.TrimSpace(body.Title) != "" {
		title := strings.TrimSpace(body.Title)
		patch.Title = &title
	}
	if body.IntervalMinutes != nil {
		patch.IntervalMinutes = body.IntervalMinutes
	}
	if body.RoleIDs != nil {
		patch.RoleIDs = auth.Unique_IDs(body.RoleIDs)
		patch.RoleIDsSet = true
		audience := livestore.Audience_ForSource(patch.RoleIDs, default_SourceAccess(body.AccessLevel))
		patch.VisibleToAll = &audience.VisibleToAll
		patch.AccessLevel = &audience.AccessLevel
		patch.RoleIDs = audience.RoleIDs
	} else if body.AccessLevel != nil {
		audience := livestore.Audience_ForSource(nil, *body.AccessLevel)
		patch.VisibleToAll = &audience.VisibleToAll
		patch.AccessLevel = &audience.AccessLevel
		patch.RoleIDs = audience.RoleIDs
		patch.RoleIDsSet = true
	} else if body.VisibleToAll != nil {
		patch.VisibleToAll = body.VisibleToAll
	}
	updated, err := api.store.Patch_Source(request.Context(), token, request.PathValue("sourceId"), patch)
	if err != nil {
		write_SourceError(responseWriter, err)
		return
	}
	write_JSONResponse(responseWriter, http.StatusOK, source_ToResponse(updated))
}

func (api sourcesAPI) serve_Delete(responseWriter http.ResponseWriter, request *http.Request) {
	token, ok := require_Bearer(responseWriter, request)
	if !ok {
		return
	}
	if err := api.store.Delete_Source(request.Context(), token, request.PathValue("sourceId")); err != nil {
		write_SourceError(responseWriter, err)
		return
	}
	responseWriter.WriteHeader(http.StatusNoContent)
}

func (api sourcesAPI) serve_Verify(responseWriter http.ResponseWriter, request *http.Request) {
	token, ok := require_Bearer(responseWriter, request)
	if !ok {
		return
	}
	if err := api.store.Verify_Source(request.Context(), token, request.PathValue("sourceId")); err != nil {
		write_SourceError(responseWriter, err)
		return
	}
	write_JSONResponse(responseWriter, http.StatusOK, struct {
		OK bool `json:"ok"`
	}{OK: true})
}

func (api sourcesAPI) serve_Sync(responseWriter http.ResponseWriter, request *http.Request) {
	token, ok := require_Bearer(responseWriter, request)
	if !ok {
		return
	}
	run, err := api.store.Sync_Source(request.Context(), token, request.PathValue("sourceId"))
	if err != nil {
		write_SourceError(responseWriter, err)
		return
	}
	write_JSONResponse(responseWriter, http.StatusOK, run_ToResponse(run))
}

func (api sourcesAPI) serve_Run(responseWriter http.ResponseWriter, request *http.Request) {
	token, ok := require_Bearer(responseWriter, request)
	if !ok {
		return
	}
	run, err := api.store.Read_SourceRun(request.Context(), token, request.PathValue("sourceId"))
	if err != nil {
		write_SourceError(responseWriter, err)
		return
	}
	write_JSONResponse(responseWriter, http.StatusOK, run_ToResponse(run))
}

func require_Bearer(responseWriter http.ResponseWriter, request *http.Request) (string, bool) {
	token, found := read_BearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return "", false
	}
	return token, true
}

func source_ToResponse(line livestore.DocumentSource) sourceResponse {
	response := sourceResponse{
		SourceID:        line.SourceID,
		Kind:            line.Kind,
		Title:           line.Title,
		Enabled:         line.Enabled,
		IntervalMinutes: line.IntervalMinutes,
		ProcessID:       line.ProcessID,
		VisibleToAll:    line.VisibleToAll,
		AccessLevel:     line.AccessLevel,
		RoleIDs:         line.RoleIDs,
		Config:          line.Config.Public(),
		LastStatus:      line.LastStatus,
		LastError:       line.LastError,
	}
	if !line.LastSyncAt.IsZero() {
		response.LastSyncAt = line.LastSyncAt.Format(timeLayout)
	}
	return response
}

func run_ToResponse(run livestore.SourceRun) sourceRunResponse {
	response := sourceRunResponse{SourceID: run.SourceID, Status: run.Status, Error: run.Error, Indexed: run.Indexed, Skipped: run.Skipped}
	if !run.SyncedAt.IsZero() {
		response.SyncedAt = run.SyncedAt.Format(time.RFC3339)
	}
	return response
}

func default_SourceAccess(level *int) int {
	if level == nil {
		return texts.SeedDocumentAccessOpen
	}
	return *level
}

func optional_String(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func write_SourceError(responseWriter http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, source.ErrUnknownKind):
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorSourceKindUnknown)
	case errors.Is(err, source.ErrNotConfigured):
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorSourceNotConfigured)
	case errors.Is(err, source.ErrLicenseDenied):
		write_ErrorResponse(responseWriter, http.StatusForbidden, texts.ErrorSourceLicense)
	case errors.Is(err, source.ErrUnsafePath):
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
	case errors.Is(err, source.ErrSourceUnreachable):
		write_ErrorResponse(responseWriter, http.StatusBadGateway, texts.ErrorSourceUnavailable)
	case errors.Is(err, livestore.ErrSourceSyncRunning):
		write_ErrorResponse(responseWriter, http.StatusConflict, texts.ErrorSourceSyncRunning)
	default:
		write_LiveError(responseWriter, err)
	}
}
