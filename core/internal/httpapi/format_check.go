package httpapi

import (
	"net/http"
	"path/filepath"
	"strings"

	"docfinder/core/internal/auth"
	"docfinder/core/internal/draftcheck"
	"docfinder/core/internal/formatcheck"
	"docfinder/core/internal/layout"
	"docfinder/core/internal/texts"
)

// Проверка оформления внешнего документа в «Проверить черновик»: поля, шрифт, интервалы, выравнивание,
// номер страницы, даты, инициалы и остатки шаблона по правилам Инструкции по ДОУ и ГОСТ Р 7.0.97-2025.
// Замечание ссылается на пункт правила. Пункт Инструкции ведет в ее файл, если Инструкция видна сотруднику.
// false - оформление не проверялось: у PDF и текста поля, шрифт и интервалы не видны
func format_Recommendations(fileName string, body []byte, source draftcheck.Source, format layout.Format) ([]draftRecommendation, bool) {
	if !strings.EqualFold(filepath.Ext(fileName), ".docx") {
		return []draftRecommendation{}, false
	}
	findings, err := formatcheck.Check_Docx(body, format)
	if err != nil {
		return []draftRecommendation{}, false
	}
	recommendations := make([]draftRecommendation, 0, len(findings))
	for _, finding := range findings {
		item := citation{Document: finding.Profile, Paragraph: finding.Clause, Quote: finding.Ref}
		if strings.Contains(strings.ToLower(finding.Profile), texts.FormatInstructionMark) && source.DocumentID != "" {
			item.DocumentID = source.DocumentID
			item.Edition = source.Edition
			item.Scope = source.Scope
			if finding.Clause != "" {
				item.OriginalLink = build_OriginalFileLink(source.DocumentID, finding.Clause)
			}
		}
		recommendations = append(recommendations, draftRecommendation{Text: finding.Text, Level: finding.Level, Citation: item})
	}
	return recommendations, true
}

// Ответ «Проверить черновик»: замечания по оформлению и рекомендации по тексту
type draftCheckResponse struct {
	Recommendations []draftRecommendation `json:"recommendations"`
	// false - файл не .docx, оформление не проверялось, только текст
	FormatChecked bool `json:"formatChecked"`
	// Каких реквизитов нет и кого предложить: вопросы перед исправлением
	Requisites *draftRequisites `json:"requisites,omitempty"`
}

// Ответ «Исправить оформление»: исправленный .docx в черновиках, что исправлено и что осталось вручную
type draftFixResponse struct {
	FileName  string   `json:"fileName"`
	DraftLink string   `json:"draftLink"`
	Applied   []string `json:"applied"`
	// Замечания, которые остались после исправления: содержание документа правит человек
	Recommendations []draftRecommendation `json:"recommendations"`
}

// Исправление оформления .docx. ok false - файл не .docx или не читается
func fix_DraftFormat(fileName string, body []byte, source draftcheck.Source, format layout.Format, options formatcheck.FixOptions) ([]byte, draftFixResponse, bool) {
	if !strings.EqualFold(filepath.Ext(fileName), ".docx") {
		return nil, draftFixResponse{}, false
	}
	result, err := formatcheck.Fix_Docx(body, format, options)
	if err != nil {
		return nil, draftFixResponse{}, false
	}
	remaining, _ := format_Recommendations(fileName, result.Body, source, format)
	applied := result.Applied
	if applied == nil {
		applied = []string{}
	}
	stem := strings.TrimSuffix(filepath.Base(fileName), filepath.Ext(fileName))
	return result.Body, draftFixResponse{
		FileName:        stem + texts.FormatFixFileSuffix,
		Applied:         applied,
		Recommendations: remaining,
	}, true
}

func (api *liveAPI) serve_DraftFix(responseWriter http.ResponseWriter, request *http.Request) {
	fileName, body, _, err := read_UploadedFile(responseWriter, request)
	if err != nil {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	token, found := read_BearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	account, err := api.store.Find_AccountByToken(request.Context(), token)
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	if !account.Has(texts.PermissionDraftsCheck) {
		write_ErrorResponse(responseWriter, http.StatusForbidden, texts.ErrorAccessDenied)
		return
	}
	lines, err := api.store.List_VisibleDocuments(request.Context(), token, "", "")
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	organization, err := api.store.Read_Organization(request.Context(), token)
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	options := read_FixOptions(request, organization.Officials, api.layoutFormat, api.chatter)
	fixed, response, ok := fix_DraftFormat(fileName, body, instruction_Source(lines), api.layoutFormat, options)
	if !ok {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	draftID, err := api.store.Save_DraftFile(request.Context(), account.UserID, response.FileName, fixed)
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	response.DraftLink = draft_FileLink(draftID)
	write_JSONResponse(responseWriter, http.StatusOK, response)
}

func (stubStore *stub) serve_DraftFix(responseWriter http.ResponseWriter, request *http.Request) {
	fileName, body, _, err := read_UploadedFile(responseWriter, request)
	if err != nil {
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
	if !auth.Has_Permission(stubStore.permissions_Of(record), texts.PermissionDraftsCheck) {
		write_ErrorResponse(responseWriter, http.StatusForbidden, texts.ErrorAccessDenied)
		return
	}
	fixed, response, ok := fix_DraftFormat(fileName, body, draftcheck.Source{
		DocumentID: texts.SeedInstructionID,
		Title:      texts.SeedInstructionTitle,
		Edition:    texts.EditionDefault,
		Scope:      texts.ScopeGlobal,
	}, stubStore.layoutFormat, read_FixOptions(request, stubStore.organization.officials_Snapshot(), stubStore.layoutFormat, nil))
	if !ok {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	response.DraftLink = draft_FileLink(stubStore.save_DraftFile(record.id, response.FileName, fixed))
	write_JSONResponse(responseWriter, http.StatusOK, response)
}
