package httpapi

import (
	"net/http"
	"time"

	"docfinder/core/internal/texts"
)

func (api *liveAPI) serve_RenameSession(responseWriter http.ResponseWriter, request *http.Request) {
	title, ok := read_SessionTitle(responseWriter, request)
	if !ok {
		return
	}
	token, found := read_BearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	sessionID := request.PathValue("sessionId")
	renamed, err := api.store.Rename_Session(request.Context(), token, sessionID, title)
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	write_JSONResponse(responseWriter, http.StatusOK, sessionLine{
		SessionID: renamed.SessionID,
		Title:     renamed.Title,
		CreatedAt: renamed.CreatedAt.UTC().Format(timeLayout),
		UpdatedAt: renamed.UpdatedAt.UTC().Format(timeLayout),
		CanRename: true,
		CanDelete: true,
	})
}

func (api *liveAPI) serve_DeleteSession(responseWriter http.ResponseWriter, request *http.Request) {
	token, found := read_BearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	if err := api.store.Delete_Session(request.Context(), token, request.PathValue("sessionId")); err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	responseWriter.WriteHeader(http.StatusNoContent)
}

func (stubStore *stub) serve_RenameSession(responseWriter http.ResponseWriter, request *http.Request) {
	title, ok := read_SessionTitle(responseWriter, request)
	if !ok {
		return
	}
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	session, found := stubStore.find_OwnedSession(responseWriter, request)
	if !found {
		return
	}
	session.title = title
	session.updatedAt = time.Now().UTC()
	write_JSONResponse(responseWriter, http.StatusOK, session.line())
}

func (stubStore *stub) serve_DeleteSession(responseWriter http.ResponseWriter, request *http.Request) {
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	session, found := stubStore.find_OwnedSession(responseWriter, request)
	if !found {
		return
	}
	delete(stubStore.sessions, session.id)
	responseWriter.WriteHeader(http.StatusNoContent)
}

type sessionList struct {
	Sessions  []sessionLine `json:"sessions"`
	CanRename bool          `json:"canRename"`
	CanDelete bool          `json:"canDelete"`
}

// Свои чаты пользователь всегда может переименовать и удалить. Флаги дублируются
// рядом со списком: так клиент знает права, даже пока чатов нет
func build_SessionList(lines []sessionLine) sessionList {
	return sessionList{Sessions: lines, CanRename: true, CanDelete: true}
}

func read_SessionTitle(responseWriter http.ResponseWriter, request *http.Request) (string, bool) {
	var body struct {
		Title string `json:"title"`
	}
	if err := read_JSONBody(responseWriter, request, &body); err != nil {
		return "", false
	}
	title := build_SessionTitle(body.Title)
	if title == "" {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return "", false
	}
	return title, true
}
