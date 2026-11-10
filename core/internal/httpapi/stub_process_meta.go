package httpapi

import (
	"net/http"
	"strings"

	"docfinder/core/internal/auth"
	"docfinder/core/internal/livestore"
	"docfinder/core/internal/texts"
)

// Карточка процесса в заглушке: документы считаются только видимые сотруднику
func (stubStore *stub) process_Card(process *processRecord, viewer *userRecord) livestore.ProcessCard {
	card := livestore.ProcessCard{
		Process: livestore.Process{
			ProcessID:     process.id,
			Title:         process.title,
			SelectPrompt:  process.selectPrompt,
			AnswerPrompt:  process.answerPrompt,
			ClarifyPrompt: process.clarifyPrompt,
		},
		Description: process.description,
		Color:       process.color,
	}
	for _, document := range stubStore.documents {
		if document.scope == texts.ScopeGlobal && document.processID == process.id && auth.Sees_GlobalDocument(viewer.roleIDs, document.visibleToAll, document.roleIDs) {
			card.DocumentCount++
		}
	}
	return card
}

// Процесс сессии мог быть удалён: он выпадает из выбора, пустой выбор - поиск по всем
func (stubStore *stub) saved_SessionProcess(session *sessionRecord) string {
	known := []string{}
	for _, id := range split_ProcessSelection(session.processID) {
		if stubStore.processesByID[id] != nil {
			known = append(known, id)
		}
	}
	return join_ProcessSelection(known)
}

// Фиксированный ответ заглушки опирается на инструкцию документооборота.
// Если выбран другой процесс, в нём этого ответа нет - честный отказ
func (stubStore *stub) apply_StubProcess(reply assistantReply, processID string) assistantReply {
	if processID == "" {
		return reply
	}
	if reply.Kind == texts.KindAnswer {
		for _, item := range reply.Citations {
			if document := stubStore.documentsByID[item.DocumentID]; document != nil && !check_ProcessSelected(processID, document.processID) {
				reply = build_PlainReply(texts.KindFallback, texts.FallbackText)
				break
			}
		}
	}
	for index := range reply.Citations {
		if document := stubStore.documentsByID[reply.Citations[index].DocumentID]; document != nil {
			reply.Citations[index].ProcessID = document.processID
		}
	}
	ids := []string{}
	titles := []string{}
	for _, id := range split_ProcessSelection(processID) {
		if process := stubStore.processesByID[id]; process != nil {
			ids = append(ids, process.id)
			titles = append(titles, process.title)
		}
	}
	if len(ids) > 0 {
		reply = reply_WithProcess(reply, build_ProcessRef(ids, titles))
	}
	return reply
}

func (stubStore *stub) serve_PatchProcess(responseWriter http.ResponseWriter, request *http.Request) {
	var body processBody
	if err := read_JSONBody(responseWriter, request, &body); err != nil || !body.check_Valid() {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	record, found := stubStore.find_UserByBearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	if !auth.Has_Permission(stubStore.permissions_Of(record), texts.PermissionGlobalManage) {
		write_ErrorResponse(responseWriter, http.StatusForbidden, texts.ErrorAccessDenied)
		return
	}
	process := stubStore.processesByID[request.PathValue("processId")]
	if process == nil {
		write_ErrorResponse(responseWriter, http.StatusNotFound, texts.ErrorNotFound)
		return
	}
	if body.Title != nil {
		process.title = strings.TrimSpace(*body.Title)
	}
	if body.Description != nil {
		process.description = strings.TrimSpace(*body.Description)
	}
	if body.Color != nil {
		process.color, _ = livestore.Normalize_ProcessColor(*body.Color)
	}
	if body.SelectPrompt != nil {
		process.selectPrompt = prompt_OrText(*body.SelectPrompt, texts.LiveSelectPrompt)
	}
	if body.AnswerPrompt != nil {
		process.answerPrompt = prompt_OrText(*body.AnswerPrompt, texts.LiveAnswerPrompt)
	}
	if body.ClarifyPrompt != nil {
		process.clarifyPrompt = prompt_OrText(*body.ClarifyPrompt, texts.LiveClarifyPrompt)
	}
	write_JSONResponse(responseWriter, http.StatusOK, build_ProcessLine(stubStore.process_Card(process, record), true))
}

func (stubStore *stub) serve_DeleteProcess(responseWriter http.ResponseWriter, request *http.Request) {
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	record, found := stubStore.find_UserByBearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	if !auth.Has_Permission(stubStore.permissions_Of(record), texts.PermissionGlobalManage) {
		write_ErrorResponse(responseWriter, http.StatusForbidden, texts.ErrorAccessDenied)
		return
	}
	processID := request.PathValue("processId")
	if processID == texts.ProcessDocuments {
		write_ErrorResponse(responseWriter, http.StatusForbidden, texts.ErrorAccessDenied)
		return
	}
	if stubStore.processesByID[processID] == nil {
		write_ErrorResponse(responseWriter, http.StatusNotFound, texts.ErrorNotFound)
		return
	}
	delete(stubStore.processesByID, processID)
	kept := stubStore.processes[:0]
	for _, process := range stubStore.processes {
		if process.id != processID {
			kept = append(kept, process)
		}
	}
	stubStore.processes = kept
	for _, document := range stubStore.documents {
		if document.processID == processID {
			document.processID = texts.ProcessDocuments
		}
	}
	responseWriter.WriteHeader(http.StatusNoContent)
}
