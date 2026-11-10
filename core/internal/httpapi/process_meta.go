package httpapi

import (
	"context"
	"net/http"
	"strings"

	"docfinder/core/internal/livestore"
	"docfinder/core/internal/texts"
)

// Тело правки процесса. Поле без значения не меняется
type processBody struct {
	Title         *string `json:"title"`
	Description   *string `json:"description"`
	Color         *string `json:"color"`
	SelectPrompt  *string `json:"selectPrompt"`
	AnswerPrompt  *string `json:"answerPrompt"`
	ClarifyPrompt *string `json:"clarifyPrompt"`
}

func (body processBody) input() livestore.ProcessInput {
	return livestore.ProcessInput{
		Title:         body.Title,
		Description:   body.Description,
		Color:         body.Color,
		SelectPrompt:  body.SelectPrompt,
		AnswerPrompt:  body.AnswerPrompt,
		ClarifyPrompt: body.ClarifyPrompt,
	}
}

func (body processBody) check_Valid() bool {
	if body.Title != nil && strings.TrimSpace(*body.Title) == "" {
		return false
	}
	if body.Color != nil {
		if _, ok := livestore.Normalize_ProcessColor(*body.Color); !ok {
			return false
		}
	}
	return true
}

func build_ProcessLine(card livestore.ProcessCard, canManage bool) processLine {
	line := processLine{
		ProcessID:     card.ProcessID,
		Title:         card.Title,
		Description:   card.Description,
		Color:         card.Color,
		DocumentCount: card.DocumentCount,
	}
	if canManage {
		selectPrompt, answerPrompt, clarifyPrompt := card.SelectPrompt, card.AnswerPrompt, card.ClarifyPrompt
		line.SelectPrompt, line.AnswerPrompt, line.ClarifyPrompt = &selectPrompt, &answerPrompt, &clarifyPrompt
	}
	if line.Color == "" {
		line.Color = texts.ProcessDefaultColor
	}
	return line
}

// «Карта» темы: название, описание и перечень документов. Модель видит, в какой области отвечает,
// и честнее говорит, что в этой теме ответа нет
func format_ProcessBrief(title, description string, documentTitles []string) string {
	var builder strings.Builder
	builder.WriteString(texts.LiveProcessLabel)
	builder.WriteString(": ")
	builder.WriteString(title)
	if description != "" {
		builder.WriteString(". ")
		builder.WriteString(description)
	}
	if len(documentTitles) > 0 {
		builder.WriteString("\n")
		builder.WriteString(texts.LiveProcessContents)
		builder.WriteString(": ")
		builder.WriteString(strings.Join(documentTitles, "; "))
	}
	return builder.String()
}

func (prompts replyPrompts) with_Brief(brief string) replyPrompts {
	if brief == "" {
		return prompts
	}
	return replyPrompts{
		Select:  prompts.Select + "\n\n" + brief,
		Answer:  prompts.Answer + "\n\n" + brief,
		Clarify: prompts.Clarify + "\n\n" + brief,
	}
}

// Процесс отмечается только в ответах поиска: обычный ответ, уточнение, отказ, нет доступа
func reply_WithProcess(reply assistantReply, ref *processRef) assistantReply {
	switch reply.Kind {
	case texts.KindAnswer, texts.KindClarify, texts.KindFallback, texts.KindAccessDenied:
		reply.Process = ref
	}
	return reply
}

// Процессы для сообщения: из тела запроса, иначе сохранённые в сессии. Коды через запятую.
// Неизвестный процесс в запросе - ошибка. Сохранённый процесс мог быть удалён - он выпадает из выбора
func (api *liveAPI) resolve_MessageProcess(ctx context.Context, sessionID string, requested *string) (string, error) {
	if requested != nil {
		for _, id := range split_ProcessSelection(*requested) {
			if _, findError := api.store.Find_Process(ctx, id); findError != nil {
				return "", livestore.ErrInvalidBody
			}
		}
		return join_ProcessSelection([]string{*requested}), nil
	}
	saved, err := api.store.Read_SessionProcess(ctx, sessionID)
	if err != nil {
		return "", err
	}
	known := []string{}
	for _, id := range split_ProcessSelection(saved) {
		if _, findError := api.store.Find_Process(ctx, id); findError == nil {
			known = append(known, id)
		}
	}
	return join_ProcessSelection(known), nil
}

// Подсказки модели: у одного процесса - его собственные, у нескольких - общие «Документооборота»
func reply_PromptProcess(selection string) string {
	ids := split_ProcessSelection(selection)
	if len(ids) == 1 {
		return ids[0]
	}
	return ""
}

// «Карта» выбранных процессов для подсказки модели и отметка, где искали ответ
func (api *liveAPI) build_ProcessBrief(ctx context.Context, selection string, roleIDs []string) (string, *processRef, error) {
	ids := split_ProcessSelection(selection)
	if len(ids) == 0 {
		return "", nil, nil
	}
	briefs := make([]string, 0, len(ids))
	titles := make([]string, 0, len(ids))
	for _, id := range ids {
		card, err := api.store.Find_ProcessCard(ctx, id)
		if err != nil {
			return "", nil, err
		}
		documentTitles, err := api.store.List_ProcessDocumentTitles(ctx, id, roleIDs, texts.ProcessMapLimit)
		if err != nil {
			return "", nil, err
		}
		briefs = append(briefs, format_ProcessBrief(card.Title, card.Description, documentTitles))
		titles = append(titles, card.Title)
	}
	return strings.Join(briefs, "\n\n"), build_ProcessRef(ids, titles), nil
}

// Один процесс - как раньше, несколько - коды и названия через запятую
func build_ProcessRef(ids, titles []string) *processRef {
	return &processRef{
		ProcessID:  join_ProcessSelection(ids),
		Title:      strings.Join(titles, texts.ProcessTitleSeparator),
		ProcessIDs: append([]string{}, ids...),
	}
}

func (api *liveAPI) fill_CitationProcesses(ctx context.Context, reply *assistantReply) error {
	if len(reply.Citations) == 0 {
		return nil
	}
	ids := make([]string, 0, len(reply.Citations))
	for _, item := range reply.Citations {
		ids = append(ids, item.DocumentID)
	}
	processes, err := api.store.Read_DocumentProcesses(ctx, ids)
	if err != nil {
		return err
	}
	for index := range reply.Citations {
		reply.Citations[index].ProcessID = processes[reply.Citations[index].DocumentID]
	}
	return nil
}

func (api *liveAPI) serve_ListProcessCards(responseWriter http.ResponseWriter, request *http.Request) {
	token, found := read_BearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	cards, account, err := api.store.List_ProcessCards(request.Context(), token)
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	canManage := account.Has(texts.PermissionGlobalManage)
	lines := make([]processLine, 0, len(cards))
	for _, card := range cards {
		lines = append(lines, build_ProcessLine(card, canManage))
	}
	write_JSONResponse(responseWriter, http.StatusOK, struct {
		CanManage bool          `json:"canManage"`
		Processes []processLine `json:"processes"`
	}{CanManage: canManage, Processes: lines})
}

func (api *liveAPI) serve_PatchProcess(responseWriter http.ResponseWriter, request *http.Request) {
	var body processBody
	if err := read_JSONBody(responseWriter, request, &body); err != nil || !body.check_Valid() {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	token, found := read_BearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	card, err := api.store.Update_Process(request.Context(), token, request.PathValue("processId"), body.input())
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	write_JSONResponse(responseWriter, http.StatusOK, build_ProcessLine(card, true))
}

func (api *liveAPI) serve_DeleteProcess(responseWriter http.ResponseWriter, request *http.Request) {
	token, found := read_BearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	if err := api.store.Delete_Process(request.Context(), token, request.PathValue("processId")); err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	responseWriter.WriteHeader(http.StatusNoContent)
}
