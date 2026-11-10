package httpapi

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"docfinder/core/internal/auth"
	"docfinder/core/internal/blank"
	engineclient "docfinder/core/internal/engine"
	"docfinder/core/internal/layout"
	"docfinder/core/internal/livestore"
	"docfinder/core/internal/ollama"
	"docfinder/core/internal/source"
	"docfinder/core/internal/texts"
)

type liveAPI struct {
	store       *livestore.LiveStore
	legalEngine *engineclient.Client
	modelClient *ollama.Client
	legacyDOU   bool
	chatter     interface {
		Complete_Chat(messages []ollama.ChatMessage) (string, error)
	}
	layoutFormat layout.Format
}

func register_LiveRoutes(store *livestore.LiveStore, modelClient *ollama.Client, legalEngine *engineclient.Client, layoutFormat layout.Format, legacyDOU bool) *http.ServeMux {
	api := &liveAPI{store: store, chatter: modelClient, modelClient: modelClient, legalEngine: legalEngine, layoutFormat: layoutFormat, legacyDOU: legacyDOU}
	routeMux := http.NewServeMux()
	routeMux.HandleFunc("POST /auth/login", api.serve_Login)
	routeMux.HandleFunc("POST /auth/logout", api.serve_Logout)
	routeMux.HandleFunc("GET /auth/config", api.serve_AuthConfig)
	routeMux.HandleFunc("POST /auth/signup", api.serve_Signup)
	routeMux.HandleFunc("POST /auth/ldap", api.serve_LdapLogin)
	routeMux.HandleFunc("POST /auth/kerberos", api.serve_KerberosLogin)
	routeMux.HandleFunc("GET /auth/admin/details", api.serve_AdminDetails)
	routeMux.HandleFunc("GET /auth/admin/config", api.serve_SignupSettings)
	routeMux.HandleFunc("POST /auth/admin/config", api.serve_SaveSignupSettings)
	routeMux.HandleFunc("GET /auth/admin/config/ldap", api.serve_LdapToggle)
	routeMux.HandleFunc("POST /auth/admin/config/ldap", api.serve_SaveLdapToggle)
	routeMux.HandleFunc("GET /auth/admin/config/ldap/server", api.serve_LdapServer)
	routeMux.HandleFunc("POST /auth/admin/config/ldap/server", api.serve_SaveLdapServer)
	routeMux.HandleFunc("GET /me", api.serve_CurrentUser)
	routeMux.HandleFunc("GET /permissions", api.serve_ListPermissions)
	routeMux.HandleFunc("GET /roles", api.serve_ListRoles)
	routeMux.HandleFunc("POST /roles", api.serve_CreateRole)
	routeMux.HandleFunc("PATCH /roles/{roleId}", api.serve_UpdateRole)
	routeMux.HandleFunc("DELETE /roles/{roleId}", api.serve_DeleteRole)
	routeMux.HandleFunc("POST /sessions", api.serve_CreateSession)
	routeMux.HandleFunc("GET /sessions", api.serve_ListSessions)
	routeMux.HandleFunc("GET /sessions/{sessionId}", api.serve_SessionHistory)
	routeMux.HandleFunc("PATCH /sessions/{sessionId}", api.serve_RenameSession)
	routeMux.HandleFunc("DELETE /sessions/{sessionId}", api.serve_DeleteSession)
	routeMux.HandleFunc("POST /sessions/{sessionId}/messages", api.serve_SessionMessage)
	routeMux.HandleFunc("POST /sessions/{sessionId}/attachments", api.serve_SessionAttachment)
	routeMux.HandleFunc("GET /documents", api.serve_ListDocuments)
	routeMux.HandleFunc("POST /documents", api.serve_UploadDocument)
	routeMux.HandleFunc("POST /documents/import", api.serve_ImportDocuments)
	routeMux.HandleFunc("GET /processes", api.serve_ListProcessCards)
	routeMux.HandleFunc("POST /processes", api.serve_CreateProcess)
	routeMux.HandleFunc("PATCH /processes/{processId}", api.serve_PatchProcess)
	routeMux.HandleFunc("DELETE /processes/{processId}", api.serve_DeleteProcess)
	routeMux.HandleFunc("GET /documents/{documentId}/file", api.serve_DocumentFile)
	routeMux.HandleFunc("GET /documents/{documentId}/structure", api.serve_DocumentStructure)
	routeMux.HandleFunc("GET /documents/{documentId}/editions", api.serve_ListEditions)
	routeMux.HandleFunc("GET /documents/{documentId}/editions/{editionId}/file", api.serve_EditionFile)
	routeMux.HandleFunc("GET /blanks/{blankId}/file", api.serve_BlankFile)
	routeMux.HandleFunc("GET /drafts/{draftId}/file", api.serve_DraftFile)
	routeMux.HandleFunc("DELETE /documents/{documentId}", api.serve_DeleteDocument)
	routeMux.HandleFunc("GET /users", api.serve_ListUsers)
	routeMux.HandleFunc("POST /users", api.serve_CreateUser)
	routeMux.HandleFunc("PATCH /users/{userId}", api.serve_PatchUser)
	routeMux.HandleFunc("DELETE /users/{userId}", api.serve_DeleteUser)
	routeMux.HandleFunc("GET /audit", api.serve_Audit)
	routeMux.HandleFunc("GET /audit/permissions", api.serve_PermissionAudit)
	routeMux.HandleFunc("POST /drafts/check", api.serve_DraftCheck)
	routeMux.HandleFunc("POST /drafts/fix", api.serve_DraftFix)
	return routeMux
}

func (api *liveAPI) serve_Login(responseWriter http.ResponseWriter, request *http.Request) {
	loginName, password, ok := read_LoginRequest(responseWriter, request)
	if !ok {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	token, err := create_RandomToken()
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	result, err := api.store.Login_User(request.Context(), loginName, password, token)
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	write_JSONResponse(responseWriter, http.StatusOK, build_LiveLoginResponse(result))
}

func build_LiveLoginResponse(result livestore.LoginResult) loginResponse {
	return loginResponse{
		Token:       result.Token,
		UserID:      result.UserID,
		Login:       result.Login,
		DisplayName: result.DisplayName,
		AccessLevel: result.AccessLevel,
		Role:        auth.Role_ForAccess(result.AccessLevel, result.Pending),
	}
}

func build_LiveAccountResponse(account livestore.Account) accountResponse {
	return accountResponse{
		UserID:      account.UserID,
		Login:       account.Login,
		DisplayName: account.DisplayName,
		AccessLevel: account.AccessLevel,
		Role:        auth.Role_ForAccess(account.AccessLevel, account.Pending),
		CreatedAt:   account.CreatedAt.UTC().Format(timeLayout),
		RoleIDs:     copy_Strings(account.RoleIDs),
	}
}

func (api *liveAPI) serve_AuthConfig(responseWriter http.ResponseWriter, request *http.Request) {
	publicConfig, err := api.store.Read_PublicAuthConfig(request.Context())
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	write_JSONResponse(responseWriter, http.StatusOK, publicAuthConfigResponse{
		Onboarding:   publicConfig.Onboarding,
		EnableSignup: publicConfig.EnableSignup,
		EnableLdap:   publicConfig.EnableLdap,
		LdapLabel:    publicConfig.LdapLabel,
	})
}

func (api *liveAPI) serve_Signup(responseWriter http.ResponseWriter, request *http.Request) {
	body, errorCode := read_SignupRequest(responseWriter, request)
	if errorCode != "" {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, errorCode)
		return
	}
	token, err := create_RandomToken()
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	result, err := api.store.Signup_User(request.Context(), body.Email, body.Password, body.DisplayName, token)
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	write_JSONResponse(responseWriter, http.StatusOK, build_LiveLoginResponse(result))
}

func (api *liveAPI) serve_LdapLogin(responseWriter http.ResponseWriter, request *http.Request) {
	body, ok := read_LdapLoginRequest(responseWriter, request)
	if !ok {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	token, err := create_RandomToken()
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	result, err := api.store.Login_LdapUser(request.Context(), body.Login, body.Password, token)
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	write_JSONResponse(responseWriter, http.StatusOK, build_LiveLoginResponse(result))
}

func (api *liveAPI) serve_AdminDetails(responseWriter http.ResponseWriter, request *http.Request) {
	token, found := read_BearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	details, err := api.store.Read_AdminDetails(request.Context(), token)
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	write_JSONResponse(responseWriter, http.StatusOK, adminDetailsResponse{Name: details.Name, Email: details.Email})
}

func (api *liveAPI) serve_SignupSettings(responseWriter http.ResponseWriter, request *http.Request) {
	token, found := read_BearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	settings, err := api.store.Read_SignupSettings(request.Context(), token)
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	write_JSONResponse(responseWriter, http.StatusOK, settings)
}

func (api *liveAPI) serve_SaveSignupSettings(responseWriter http.ResponseWriter, request *http.Request) {
	var body signupSettingsRequest
	if err := read_JSONBody(responseWriter, request, &body); err != nil {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	token, found := read_BearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	current, err := api.store.Read_SignupSettings(request.Context(), token)
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	updated, ok := apply_SignupSettingsRequest(current, body)
	if !ok {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	saved, err := api.store.Save_SignupSettings(request.Context(), token, updated)
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	write_JSONResponse(responseWriter, http.StatusOK, saved)
}

func (api *liveAPI) serve_LdapToggle(responseWriter http.ResponseWriter, request *http.Request) {
	token, found := read_BearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	settings, err := api.store.Read_LdapSettings(request.Context(), token)
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	write_JSONResponse(responseWriter, http.StatusOK, ldapToggleResponse{EnableLdap: settings.EnableLdap})
}

func (api *liveAPI) serve_SaveLdapToggle(responseWriter http.ResponseWriter, request *http.Request) {
	var body ldapToggleBody
	if err := read_JSONBody(responseWriter, request, &body); err != nil || body.EnableLdap == nil {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	token, found := read_BearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	settings, err := api.store.Read_LdapSettings(request.Context(), token)
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	settings.EnableLdap = *body.EnableLdap
	saved, err := api.store.Save_LdapSettings(request.Context(), token, settings)
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	write_JSONResponse(responseWriter, http.StatusOK, ldapToggleResponse{EnableLdap: saved.EnableLdap})
}

func (api *liveAPI) serve_LdapServer(responseWriter http.ResponseWriter, request *http.Request) {
	token, found := read_BearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	settings, err := api.store.Read_LdapSettings(request.Context(), token)
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	write_JSONResponse(responseWriter, http.StatusOK, build_LdapServerResponse(settings))
}

func (api *liveAPI) serve_SaveLdapServer(responseWriter http.ResponseWriter, request *http.Request) {
	var body ldapServerRequest
	if err := read_JSONBody(responseWriter, request, &body); err != nil {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	token, found := read_BearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	current, err := api.store.Read_LdapSettings(request.Context(), token)
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	updated, ok := apply_LdapServerRequest(current, body)
	if !ok {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	saved, err := api.store.Save_LdapSettings(request.Context(), token, updated)
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	write_JSONResponse(responseWriter, http.StatusOK, build_LdapServerResponse(saved))
}

func (api *liveAPI) serve_Logout(responseWriter http.ResponseWriter, request *http.Request) {
	token, found := read_BearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	if err := api.store.Delete_AuthToken(request.Context(), token); err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	responseWriter.WriteHeader(http.StatusNoContent)
}

func (api *liveAPI) serve_CurrentUser(responseWriter http.ResponseWriter, request *http.Request) {
	token, found := read_BearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	// Профиль отдаём и ожидающему подтверждения: по роли pending клиент показывает экран ожидания
	account, err := api.store.Find_ProfileByToken(request.Context(), token)
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	write_JSONResponse(responseWriter, http.StatusOK, profileResponse{
		UserID:      account.UserID,
		Login:       account.Login,
		DisplayName: account.DisplayName,
		AccessLevel: account.AccessLevel,
		Role:        auth.Role_ForAccess(account.AccessLevel, account.Pending),
		Roles:       profile_Roles(account.RoleIDs, account.RoleTitles),
		Permissions: copy_Strings(account.Permissions),
	})
}

func (api *liveAPI) serve_CreateSession(responseWriter http.ResponseWriter, request *http.Request) {
	token, found := read_BearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	sessionID, err := api.store.Create_Session(request.Context(), token)
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	write_JSONResponse(responseWriter, http.StatusOK, struct {
		SessionID string `json:"sessionId"`
	}{SessionID: sessionID})
}

func (api *liveAPI) serve_ListSessions(responseWriter http.ResponseWriter, request *http.Request) {
	token, found := read_BearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	sessions, err := api.store.List_Sessions(request.Context(), token)
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	lines := make([]sessionLine, 0, len(sessions))
	for _, session := range sessions {
		lines = append(lines, sessionLine{
			SessionID: session.SessionID,
			Title:     session.Title,
			CreatedAt: session.CreatedAt.UTC().Format(timeLayout),
			UpdatedAt: session.UpdatedAt.UTC().Format(timeLayout),
			CanRename: true,
			CanDelete: true,
		})
	}
	write_JSONResponse(responseWriter, http.StatusOK, build_SessionList(lines))
}

func (api *liveAPI) serve_SessionHistory(responseWriter http.ResponseWriter, request *http.Request) {
	token, found := read_BearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	messages, err := api.store.List_SessionMessages(request.Context(), token, request.PathValue("sessionId"))
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	rawMessages := make([]json.RawMessage, 0, len(messages))
	for _, message := range messages {
		rawMessages = append(rawMessages, json.RawMessage(message))
	}
	processID, err := api.resolve_MessageProcess(request.Context(), request.PathValue("sessionId"), nil)
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	savedMode, err := api.store.Read_SessionMode(request.Context(), request.PathValue("sessionId"))
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	write_JSONResponse(responseWriter, http.StatusOK, struct {
		SessionID  string            `json:"sessionId"`
		ProcessID  string            `json:"processId"`
		ProcessIDs []string          `json:"processIds"`
		Mode       string            `json:"mode"`
		Messages   []json.RawMessage `json:"messages"`
	}{SessionID: request.PathValue("sessionId"), ProcessID: processID, ProcessIDs: split_ProcessSelection(processID), Mode: saved_ChatMode(savedMode), Messages: rawMessages})
}

func (api *liveAPI) serve_SessionMessage(responseWriter http.ResponseWriter, request *http.Request) {
	var body struct {
		Text string `json:"text"`
		// nil - процесс, сохранённый в сессии, пустая строка - все процессы
		ProcessID *string `json:"processId"`
		// Несколько процессов сразу, важнее processId. Пустой список - все процессы
		ProcessIDs    *[]string `json:"processIds"`
		AttachmentIDs []string  `json:"attachmentIds"`
		// nil - режим, сохраненный в сессии: read или plan
		Mode *string `json:"mode"`
		// Ответы на вопросы документа пачкой. text - их запись для ленты
		Answers []fieldAnswer `json:"answers"`
	}
	if err := read_JSONBody(responseWriter, request, &body); err != nil || strings.TrimSpace(body.Text) == "" {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	token, found := read_BearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	account, _, templateState, err := api.store.Load_SessionForMessage(request.Context(), token, request.PathValue("sessionId"))
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	requestedProcess := read_RequestedProcesses(body.ProcessID, body.ProcessIDs)
	processID, err := api.resolve_MessageProcess(request.Context(), request.PathValue("sessionId"), requestedProcess)
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	prompts, promptError := api.store.Load_ReplyPrompts(request.Context(), reply_PromptProcess(processID))
	if promptError != nil {
		write_LiveError(responseWriter, promptError)
		return
	}
	// Выбранная тема: её подсказки модели плюс «карта» темы - название, описание и перечень документов
	brief, searchedProcess, briefError := api.build_ProcessBrief(request.Context(), processID, account.RoleIDs)
	if briefError != nil {
		write_LiveError(responseWriter, briefError)
		return
	}
	replyPrompt := replyPrompts{Select: prompts.Select, Answer: prompts.Answer, Clarify: prompts.Clarify}.with_Brief(brief)
	savedMode, err := api.store.Read_SessionMode(request.Context(), request.PathValue("sessionId"))
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	mode, modeOK := resolve_ChatMode(body.Mode, savedMode)
	if !modeOK {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	session := &sessionRecord{blankFill: decode_FillState(templateState), layoutFill: decode_LayoutState(templateState), planChoice: decode_PlanChoice(templateState)}
	var reply assistantReply
	var documents []shownDocument
	var turnAttachments []messageAttachment
	// «План»: документ по просьбе сотрудника. «Чтение» и сотрудник без флага шаблонов - только база знаний.
	// С вложениями черновика реплика - правка черновика, а не новый документ
	var plan planResult
	if api.legacyDOU && mode == texts.ChatModePlan && account.Has(texts.PermissionTemplatesUse) && account.Has(texts.PermissionChatUse) {
		if len(body.AttachmentIDs) == 0 {
			var planError error
			plan, planError = advance_PlanTurn(session, body.Text, body.Answers, func() (layout.Organization, error) {
				return api.load_LayoutOrganization(request.Context(), token)
			}, api.layoutFormat, api.chatter, time.Now())
			if planError != nil {
				write_LiveError(responseWriter, planError)
				return
			}
		}
	} else {
		clear_DocumentStates(session)
	}
	layoutHandled := plan.Layout != nil
	var layoutStep layout.Step
	if layoutHandled {
		layoutStep = *plan.Layout
	}
	step, handled := blank.Step{}, plan.Blank != nil
	if handled {
		step = *plan.Blank
	}
	var source livestore.RevisionSource
	revising := false
	if api.legacyDOU && !handled && !layoutHandled && plan.Reply == nil {
		var sourceError error
		source, revising, sourceError = api.store.Find_RevisionSource(request.Context(), request.PathValue("sessionId"), body.AttachmentIDs)
		if sourceError != nil {
			write_LiveError(responseWriter, sourceError)
			return
		}
	}
	// Вопрос, который называет документ целиком, - справка о документе и кнопка скачивания
	var overview overviewResult
	if api.legacyDOU && plan.Reply == nil && !layoutHandled && !handled && !revising && account.Has(texts.PermissionChatUse) {
		var overviewError error
		overview, overviewError = api.find_DocumentOverview(request.Context(), token, body.Text, processID)
		if overviewError != nil {
			write_LiveError(responseWriter, overviewError)
			return
		}
	}
	if plan.Reply != nil {
		reply = *plan.Reply
		documents = []shownDocument{}
	} else if overview.Found {
		reply = reply_WithProcess(overview.Reply, searchedProcess)
		documents = overview.Documents
		if citationError := api.fill_CitationProcesses(request.Context(), &reply); citationError != nil {
			write_LiveError(responseWriter, citationError)
			return
		}
	} else if layoutHandled {
		reply = reply_FromLayout(layoutStep)
		documents = []shownDocument{}
		if layoutStep.Ready && len(layoutStep.Docx) > 0 {
			draftID, saveError := api.store.Save_DraftFile(request.Context(), account.UserID, layoutStep.FileName, layoutStep.Docx)
			if saveError != nil {
				write_LiveError(responseWriter, saveError)
				return
			}
			reply.Template.DraftLink = draft_FileLink(draftID)
		}
	} else if handled {
		if !account.Has(texts.PermissionChatUse) {
			write_ErrorResponse(responseWriter, http.StatusForbidden, texts.ErrorAccessDenied)
			return
		}
		apply_BlankStep(session, step)
		reply = reply_FromBlank(step)
		documents = []shownDocument{}
		if step.Ready && len(step.DraftDocx) > 0 && reply.Template != nil {
			draftID, saveError := api.store.Save_DraftFile(request.Context(), account.UserID, step.BlankID+".docx", step.DraftDocx)
			if saveError != nil {
				write_LiveError(responseWriter, saveError)
				return
			}
			reply.Template.DraftLink = draft_FileLink(draftID)
		}
	} else if revising {
		if !account.Has(texts.PermissionDraftsAssist) {
			write_ErrorResponse(responseWriter, http.StatusForbidden, texts.ErrorAccessDenied)
			return
		}
		var reviseError error
		reply, documents, reviseError = api.revise_LiveDraft(request.Context(), account, request.PathValue("sessionId"), body.Text, processID, source)
		if reviseError != nil {
			write_LiveError(responseWriter, reviseError)
			return
		}
		if storeError := api.store.Store_RevisionText(request.Context(), request.PathValue("sessionId"), source.AttachmentID, reply.Draft.DraftText); storeError != nil {
			write_LiveError(responseWriter, storeError)
			return
		}
		turnAttachments = message_Attachments(source.Files)
	} else if api.legacyDOU && strings.Contains(strings.ToLower(body.Text), texts.PhrasePrepare) {
		if !account.Has(texts.PermissionChatUse) {
			write_ErrorResponse(responseWriter, http.StatusForbidden, texts.ErrorAccessDenied)
			return
		}
		reply = reply_WithContacts(build_PlainReply(texts.KindFallback, texts.FallbackText), nil)
		documents = []shownDocument{}
		if embedding, embedError := api.store.Embed_Question(body.Text); embedError == nil {
			contactChunk, found, contactError := api.store.Find_ContactChunk(request.Context(), embedding, account.UserID, account.RoleIDs, processID)
			if contactError != nil {
				write_LiveError(responseWriter, contactError)
				return
			}
			if found {
				reply = reply_WithContacts(reply, contacts_ForText(contactChunk.Content, body.Text))
			}
		}
	} else {
		if !account.Has(texts.PermissionChatUse) {
			write_ErrorResponse(responseWriter, http.StatusForbidden, texts.ErrorAccessDenied)
			return
		}
		historyRaw, historyError := api.store.List_SessionMessages(request.Context(), token, request.PathValue("sessionId"))
		if historyError != nil {
			write_LiveError(responseWriter, historyError)
			return
		}
		history := read_ChatHistory(historyRaw)
		answeredByEngine := false
		if api.legalEngine != nil {
			engineReply, engineDocuments, engineError := api.answer_WithLegalEngine(request.Context(), body.Text, history)
			if engineError == nil {
				reply, documents, answeredByEngine = engineReply, engineDocuments, true
			} else {
				log.Printf("legal engine answer: %v; using core fallback", engineError)
			}
		}
		if !answeredByEngine {
			// Ответ на уточнение ищется вместе с исходным вопросом
			question := build_ClarifiedQuestion(history, body.Text)
			embedding, embedError := api.store.Embed_Question(question)
			if embedError != nil {
				write_LiveError(responseWriter, embedError)
				return
			}
			visible, closed, searchError := api.store.Find_ReplyChunks(request.Context(), embedding, account.UserID, account.RoleIDs, processID)
			if searchError != nil {
				write_LiveError(responseWriter, searchError)
				return
			}
			var replyError error
			reply, documents, replyError = build_LiveAssistantReply(api.chatter, question, history, visible, closed, replyPrompt)
			if replyError != nil {
				write_LiveError(responseWriter, replyError)
				return
			}
			if citationError := api.fill_CitationProcesses(request.Context(), &reply); citationError != nil {
				write_LiveError(responseWriter, citationError)
				return
			}
			reply = reply_WithProcess(reply, searchedProcess)
			if reply.Kind == texts.KindAnswer || reply.Kind == texts.KindFallback {
				contactChunk, found, contactError := api.store.Find_ContactChunk(request.Context(), embedding, account.UserID, account.RoleIDs, processID)
				if contactError != nil {
					write_LiveError(responseWriter, contactError)
					return
				}
				contactList := []replyContact{}
				if found {
					contactList = contacts_ForText(contactChunk.Content, body.Text)
				}
				reply = reply_WithContacts(reply, contactList)
			}
		}
	}
	if api.legacyDOU {
		reply = apply_ModeHint(reply, mode, body.Text)
	}
	userBody, err := json.Marshal(userLine{Role: "user", Text: body.Text, Attachments: turnAttachments, Answers: plan.Answered})
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	stored := reply
	stored.Role = "assistant"
	assistantBody, err := json.Marshal(stored)
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	if documents == nil {
		documents = []shownDocument{}
	}
	auditDocuments, err := json.Marshal(documents)
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	err = api.store.Save_SessionExchange(request.Context(), request.PathValue("sessionId"), session.templateStep, encode_SessionState(session), userBody, assistantBody, auditDocuments, account.UserID, account.DisplayName, body.Text, build_SessionTitle(body.Text))
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	if requestedProcess != nil {
		if err := api.store.Save_SessionProcess(request.Context(), request.PathValue("sessionId"), processID); err != nil {
			write_LiveError(responseWriter, err)
			return
		}
	}
	if body.Mode != nil {
		if err := api.store.Save_SessionMode(request.Context(), request.PathValue("sessionId"), mode); err != nil {
			write_LiveError(responseWriter, err)
			return
		}
	}
	write_JSONResponse(responseWriter, http.StatusOK, reply)
}

func (api *liveAPI) serve_ListDocuments(responseWriter http.ResponseWriter, request *http.Request) {
	scopeFilter, scopeOK := read_ListScope(request.URL.Query().Get("scope"))
	if !scopeOK {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	token, found := read_BearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	processFilter := strings.TrimSpace(request.URL.Query().Get("process"))
	if processFilter != "" {
		if _, processError := api.store.Require_UploadProcess(request.Context(), processFilter); processError != nil {
			write_LiveError(responseWriter, processError)
			return
		}
	}
	account, err := api.store.Find_AccountByToken(request.Context(), token)
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	lines, err := api.store.List_VisibleDocuments(request.Context(), token, scopeFilter, processFilter)
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	visible := make([]documentResponse, 0, len(lines))
	for _, line := range lines {
		visible = append(visible, documentResponse{
			DocumentID:   line.DocumentID,
			Title:        line.Title,
			Edition:      line.Edition,
			AccessLevel:  line.AccessLevel,
			Scope:        line.Scope,
			ProcessID:    line.ProcessID,
			OwnerID:      line.OwnerID,
			OwnerName:    line.OwnerName,
			UploadedAt:   line.UploadedAt.UTC().Format(time.RFC3339),
			VisibleToAll: line.VisibleToAll,
			RoleIDs:      copy_Strings(line.RoleIDs),
			// Стандартный набор удалить нельзя, только заменить новой редакцией
			CanDelete:        check_CanDeleteDocument(account.UserID, account.Permissions, line.Scope, line.OwnerID) && !line.Standard,
			Standard:         line.Standard,
			ArchivedEditions: line.ArchivedEditions,
		})
	}
	write_JSONResponse(responseWriter, http.StatusOK, struct {
		CanUploadGlobal   bool               `json:"canUploadGlobal"`
		CanUploadPersonal bool               `json:"canUploadPersonal"`
		Documents         []documentResponse `json:"documents"`
	}{
		CanUploadGlobal:   account.Has(texts.PermissionGlobalManage),
		CanUploadPersonal: account.Has(texts.PermissionPersonalUpload),
		Documents:         visible,
	})
}

func (api *liveAPI) serve_UploadDocument(responseWriter http.ResponseWriter, request *http.Request) {
	fileName, body, contentType, err := read_UploadedFile(responseWriter, request)
	if err != nil {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	title := strings.TrimSpace(request.FormValue("title"))
	if title == "" {
		title = strings.TrimSpace(fileName)
	}
	if title == "" {
		title = texts.DefaultDocumentTitle
	}
	edition := strings.TrimSpace(request.FormValue("edition"))
	if edition == "" {
		edition = texts.EditionDefault
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
	scope, scopeOK := read_UploadScope(request.FormValue("scope"), account.Has(texts.PermissionGlobalManage))
	if !scopeOK {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	if !allow_Upload(account.Permissions, scope) {
		write_ErrorResponse(responseWriter, http.StatusForbidden, texts.ErrorAccessDenied)
		return
	}
	accessLevel, visibleToAll, roleIDs, audienceOK := read_UploadAudience(request.Form["roleIds"], request.FormValue("accessLevel"), account.AccessLevel, scope == texts.ScopePersonal)
	if !audienceOK {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	ownerID := ""
	if scope == texts.ScopePersonal {
		ownerID = account.UserID
	}
	processID, processError := api.store.Require_UploadProcess(request.Context(), request.FormValue("process"))
	if processError != nil {
		write_LiveError(responseWriter, processError)
		return
	}
	cleanName := filepath.Base(fileName)
	if cleanName == "" || cleanName == "." {
		cleanName = "document.bin"
	}
	result, err := api.store.Index_UploadedDocument(request.Context(), token, livestore.UploadInput{
		FileName:     cleanName,
		ContentType:  contentType,
		Body:         body,
		Title:        title,
		Edition:      edition,
		AccessLevel:  accessLevel,
		Scope:        scope,
		OwnerID:      ownerID,
		ProcessID:    processID,
		VisibleToAll: visibleToAll,
		RoleIDs:      roleIDs,
	})
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	api.start_EngineReindex()
	write_JSONResponse(responseWriter, http.StatusOK, uploadResponse{
		DocumentID: result.DocumentID,
		Status:     texts.StatusAccepted,
		Edition:    result.Edition,
		Scope:      result.Scope,
		ProcessID:  processID,
	})
}

func (api *liveAPI) serve_ImportDocuments(responseWriter http.ResponseWriter, request *http.Request) {
	request.Body = http.MaxBytesReader(responseWriter, request.Body, maxUploadBytes)
	if err := request.ParseMultipartForm(maxUploadBytes); err != nil {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	listing, err := source.Read_Folder(request.FormValue("path"))
	if err != nil {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	edition := strings.TrimSpace(request.FormValue("edition"))
	if edition == "" {
		edition = texts.EditionDefault
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
	scope, scopeOK := read_UploadScope(request.FormValue("scope"), account.Has(texts.PermissionGlobalManage))
	if !scopeOK {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	if !allow_Upload(account.Permissions, scope) {
		write_ErrorResponse(responseWriter, http.StatusForbidden, texts.ErrorAccessDenied)
		return
	}
	accessLevel, visibleToAll, roleIDs, audienceOK := read_UploadAudience(request.Form["roleIds"], request.FormValue("accessLevel"), account.AccessLevel, scope == texts.ScopePersonal)
	if !audienceOK {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	ownerID := ""
	if scope == texts.ScopePersonal {
		ownerID = account.UserID
	}
	processID, processError := api.store.Require_UploadProcess(request.Context(), request.FormValue("process"))
	if processError != nil {
		write_LiveError(responseWriter, processError)
		return
	}
	imported := make([]uploadResponse, 0, len(listing.Files))
	for _, file := range listing.Files {
		cleanName := filepath.Base(file.Name)
		result, indexError := api.store.Index_UploadedDocument(request.Context(), token, livestore.UploadInput{
			FileName:     cleanName,
			ContentType:  contentType_ForFile(cleanName),
			Body:         file.Body,
			Title:        cleanName,
			Edition:      edition,
			AccessLevel:  accessLevel,
			Scope:        scope,
			OwnerID:      ownerID,
			ProcessID:    processID,
			VisibleToAll: visibleToAll,
			RoleIDs:      roleIDs,
		})
		if indexError != nil {
			write_LiveError(responseWriter, indexError)
			return
		}
		imported = append(imported, uploadResponse{
			DocumentID: result.DocumentID,
			Status:     texts.StatusAccepted,
			Edition:    result.Edition,
			Scope:      result.Scope,
			ProcessID:  processID,
		})
	}
	api.start_EngineReindex()
	write_JSONResponse(responseWriter, http.StatusOK, importResponse{Documents: imported, Skipped: listing.Skipped})
}

func (api *liveAPI) serve_CreateProcess(responseWriter http.ResponseWriter, request *http.Request) {
	var body struct {
		Title         string `json:"title"`
		Description   string `json:"description"`
		Color         string `json:"color"`
		SelectPrompt  string `json:"selectPrompt"`
		AnswerPrompt  string `json:"answerPrompt"`
		ClarifyPrompt string `json:"clarifyPrompt"`
	}
	if err := read_JSONBody(responseWriter, request, &body); err != nil {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	color, colorOK := livestore.Normalize_ProcessColor(body.Color)
	if !colorOK {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	token, found := read_BearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	created, err := api.store.Create_Process(request.Context(), token, body.Title, body.SelectPrompt, body.AnswerPrompt, body.ClarifyPrompt)
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	if err := api.store.Set_ProcessMeta(request.Context(), created.ProcessID, body.Description, color); err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	write_JSONResponse(responseWriter, http.StatusOK, processDetail{
		ProcessID:     created.ProcessID,
		Title:         created.Title,
		Description:   strings.TrimSpace(body.Description),
		Color:         color,
		SelectPrompt:  created.SelectPrompt,
		AnswerPrompt:  created.AnswerPrompt,
		ClarifyPrompt: created.ClarifyPrompt,
	})
}

func (api *liveAPI) serve_BlankFile(responseWriter http.ResponseWriter, request *http.Request) {
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
	if !account.Has(texts.PermissionTemplatesUse) {
		write_ErrorResponse(responseWriter, http.StatusForbidden, texts.ErrorAccessDenied)
		return
	}
	fileName, body, err := blank.Read_ModuleFile(request.PathValue("blankId"))
	if err != nil {
		write_ErrorResponse(responseWriter, http.StatusNotFound, texts.ErrorNotFound)
		return
	}
	write_FileResponse(responseWriter, fileName, texts.DocxContentType, body)
}

func (api *liveAPI) serve_DraftFile(responseWriter http.ResponseWriter, request *http.Request) {
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
	file, err := api.store.Read_DraftFile(request.Context(), account.UserID, request.PathValue("draftId"))
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	write_FileResponse(responseWriter, file.FileName, file.ContentType, file.Body)
}

func (api *liveAPI) serve_DocumentFile(responseWriter http.ResponseWriter, request *http.Request) {
	token, found := read_BearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	file, err := api.store.Read_DocumentFile(request.Context(), token, request.PathValue("documentId"))
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	write_FileResponse(responseWriter, file.FileName, file.ContentType, file.Body)
}

func (api *liveAPI) serve_DocumentStructure(responseWriter http.ResponseWriter, request *http.Request) {
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
	if !account.Has(texts.PermissionChatUse) {
		write_ErrorResponse(responseWriter, http.StatusForbidden, texts.ErrorAccessDenied)
		return
	}
	if api.legalEngine == nil {
		write_ErrorResponse(responseWriter, http.StatusServiceUnavailable, texts.ErrorEngineUnavailable)
		return
	}
	raw, err := api.legalEngine.Structure(request.Context(), request.PathValue("documentId"))
	if errors.Is(err, engineclient.ErrNotFound) {
		write_ErrorResponse(responseWriter, http.StatusNotFound, texts.ErrorNotFound)
		return
	}
	if err != nil {
		write_ErrorResponse(responseWriter, http.StatusServiceUnavailable, texts.ErrorEngineUnavailable)
		return
	}
	responseWriter.Header().Set("Content-Type", "application/json; charset=utf-8")
	responseWriter.WriteHeader(http.StatusOK)
	_, _ = responseWriter.Write(raw)
}

func (api *liveAPI) serve_ListEditions(responseWriter http.ResponseWriter, request *http.Request) {
	token, found := read_BearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	documentID := request.PathValue("documentId")
	editions, err := api.store.List_DocumentEditions(request.Context(), token, documentID)
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	lines := make([]editionResponse, 0, len(editions))
	for _, edition := range editions {
		lines = append(lines, build_EditionResponse(documentID, edition.EditionID, edition.Edition, edition.FileName, edition.UploadedAt, edition.ArchivedAt))
	}
	write_JSONResponse(responseWriter, http.StatusOK, editionsResponse{Editions: lines})
}

func (api *liveAPI) serve_EditionFile(responseWriter http.ResponseWriter, request *http.Request) {
	token, found := read_BearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	file, err := api.store.Read_EditionFile(request.Context(), token, request.PathValue("documentId"), request.PathValue("editionId"))
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	write_FileResponse(responseWriter, file.FileName, file.ContentType, file.Body)
}

func (api *liveAPI) serve_DeleteDocument(responseWriter http.ResponseWriter, request *http.Request) {
	token, found := read_BearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	err := api.store.Delete_Document(request.Context(), token, request.PathValue("documentId"))
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	api.start_EngineReindex()
	responseWriter.WriteHeader(http.StatusNoContent)
}

func (api *liveAPI) serve_ListUsers(responseWriter http.ResponseWriter, request *http.Request) {
	token, found := read_BearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	accounts, err := api.store.List_Accounts(request.Context(), token)
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	lines := make([]accountResponse, 0, len(accounts))
	for _, account := range accounts {
		lines = append(lines, build_LiveAccountResponse(account))
	}
	write_JSONResponse(responseWriter, http.StatusOK, struct {
		Users []accountResponse `json:"users"`
	}{Users: lines})
}

func (api *liveAPI) serve_CreateUser(responseWriter http.ResponseWriter, request *http.Request) {
	created, errorCode := read_CreateUserRequest(responseWriter, request)
	if errorCode != "" {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, errorCode)
		return
	}
	token, found := read_BearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	account, err := api.store.Create_Account(request.Context(), token, created.login, created.password, created.displayName, created.accessLevel, created.pending)
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	write_JSONResponse(responseWriter, http.StatusOK, build_LiveAccountResponse(account))
}

func (api *liveAPI) serve_PatchUser(responseWriter http.ResponseWriter, request *http.Request) {
	patch, errorCode := read_PatchUserRequest(responseWriter, request)
	if errorCode != "" {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, errorCode)
		return
	}
	token, found := read_BearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	account, err := api.store.Patch_Account(request.Context(), token, request.PathValue("userId"), patch)
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	write_JSONResponse(responseWriter, http.StatusOK, build_LiveAccountResponse(account))
}

func (api *liveAPI) serve_DeleteUser(responseWriter http.ResponseWriter, request *http.Request) {
	token, found := read_BearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	if err := api.store.Delete_Account(request.Context(), token, request.PathValue("userId")); err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	responseWriter.WriteHeader(http.StatusNoContent)
}

func (api *liveAPI) serve_Audit(responseWriter http.ResponseWriter, request *http.Request) {
	token, found := read_BearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	entries, err := api.store.List_Audit(request.Context(), token)
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	lines := make([]auditEntry, 0, len(entries))
	for _, entry := range entries {
		documents := []shownDocument{}
		if len(entry.Documents) > 0 {
			if err := json.Unmarshal(entry.Documents, &documents); err != nil {
				write_LiveError(responseWriter, err)
				return
			}
		}
		lines = append(lines, auditEntry{
			EntryID:     entry.EntryID,
			UserID:      entry.UserID,
			DisplayName: entry.DisplayName,
			Question:    entry.Question,
			CreatedAt:   entry.CreatedAt.UTC().Format(timeLayout),
			Documents:   documents,
		})
	}
	write_JSONResponse(responseWriter, http.StatusOK, struct {
		Entries []auditEntry `json:"entries"`
	}{Entries: lines})
}

func (api *liveAPI) serve_DraftCheck(responseWriter http.ResponseWriter, request *http.Request) {
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
	recommendations, err := recommendations_ForDraft(fileName, body, instruction_Source(lines), api.layoutFormat)
	if err != nil {
		write_DraftError(responseWriter, err)
		return
	}
	organization, err := api.store.Read_Organization(request.Context(), token)
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	recommendations.Requisites = requisites_ForDraft(fileName, body, organization.Officials)
	write_JSONResponse(responseWriter, http.StatusOK, recommendations)
}

func read_BearerToken(request *http.Request) (string, bool) {
	header := request.Header.Get("Authorization")
	token, found := strings.CutPrefix(header, "Bearer ")
	if !found || token == "" || token != strings.TrimSpace(token) {
		return "", false
	}
	return token, true
}

func write_LiveError(responseWriter http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, livestore.ErrInvalidCredentials):
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorInvalidCredentials)
	case errors.Is(err, livestore.ErrUnauthorized):
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
	case errors.Is(err, livestore.ErrAccessDenied):
		write_ErrorResponse(responseWriter, http.StatusForbidden, texts.ErrorAccessDenied)
	case errors.Is(err, livestore.ErrNotFound):
		write_ErrorResponse(responseWriter, http.StatusNotFound, texts.ErrorNotFound)
	case errors.Is(err, livestore.ErrDirectoryAccountConflict):
		write_ErrorResponse(responseWriter, http.StatusConflict, texts.ErrorDirectoryAccountConflict)
	case errors.Is(err, livestore.ErrLoginTaken):
		write_ErrorResponse(responseWriter, http.StatusConflict, texts.ErrorLoginTaken)
	case errors.Is(err, livestore.ErrStandardDocument):
		write_ErrorResponse(responseWriter, http.StatusConflict, texts.ErrorStandardDocument)
	case errors.Is(err, livestore.ErrRoleInUse):
		write_ErrorResponse(responseWriter, http.StatusConflict, texts.ErrorRoleInUse)
	case errors.Is(err, livestore.ErrUnsupportedFile), errors.Is(err, livestore.ErrInvalidBody):
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
	case errors.Is(err, livestore.ErrEmbedSize):
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorEmbedSize)
	case errors.Is(err, livestore.ErrEmbedUnavailable):
		write_ErrorResponse(responseWriter, http.StatusBadGateway, texts.ErrorEmbedUnavailable)
	case errors.Is(err, livestore.ErrSignupDisabled):
		write_ErrorResponse(responseWriter, http.StatusForbidden, texts.ErrorSignupDisabled)
	case errors.Is(err, livestore.ErrAccountPending):
		write_ErrorResponse(responseWriter, http.StatusForbidden, texts.ErrorAccountPending)
	case errors.Is(err, livestore.ErrLdapDisabled):
		write_ErrorResponse(responseWriter, http.StatusForbidden, texts.ErrorLdapDisabled)
	case errors.Is(err, livestore.ErrKerberosDisabled):
		write_ErrorResponse(responseWriter, http.StatusForbidden, texts.ErrorKerberosDisabled)
	case errors.Is(err, auth.ErrLdapEmailMissing):
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorLdapEmailMissing)
	case errors.Is(err, auth.ErrLdapUnavailable):
		// В тексте ошибки адрес сервера и ответ каталога, пароля там нет
		log.Printf("ldap error: %v", err)
		write_ErrorResponse(responseWriter, http.StatusBadGateway, texts.ErrorLdapUnavailable)
	default:
		log.Printf("store error: %v", err)
		write_ErrorResponse(responseWriter, http.StatusInternalServerError, texts.ErrorStore)
	}
}

const timeLayout = "2006-01-02T15:04:05Z07:00"
