package httpapi

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"docfinder/core/internal/auth"
	"docfinder/core/internal/blank"
	"docfinder/core/internal/chunk"
	"docfinder/core/internal/contacts"
	"docfinder/core/internal/draftcheck"
	"docfinder/core/internal/layout"
	"docfinder/core/internal/livestore"
	"docfinder/core/internal/source"
	"docfinder/core/internal/texts"
)

const (
	maxUploadBytes    = 20 << 20
	adminAccessLevel  = 2
	templateStepIdle  = 0
	templateStepAsked = 1
)

type userRecord struct {
	id          string
	login       string
	password    string
	displayName string
	accessLevel int
	pending     bool
	createdAt   time.Time
	roleIDs     []string
	// Запись создал вход через каталог. Только в такие записи каталог и пускает
	directory bool
}

type documentRecord struct {
	id               string
	title            string
	edition          string
	accessLevel      int
	scope            string
	processID        string
	ownerID          string
	uploadedAt       time.Time
	fileName         string
	contentType      string
	body             []byte
	visibleToAll     bool
	roleIDs          []string
	sourceID         string
	sourceExternalID string
	// Документ стандартного набора: удалить нельзя
	standard bool
	// Прежние редакции, новые в конце
	editions []stubEdition
}

type stubEdition struct {
	id          string
	edition     string
	fileName    string
	contentType string
	body        []byte
	uploadedAt  time.Time
	archivedAt  time.Time
}

type processRecord struct {
	id            string
	title         string
	description   string
	color         string
	selectPrompt  string
	answerPrompt  string
	clarifyPrompt string
}

type sessionRecord struct {
	id                string
	ownerID           string
	title             string
	createdAt         time.Time
	updatedAt         time.Time
	messages          []any
	templateStep      int
	blankFill         blank.FillState
	layoutFill        layout.State
	attachmentsByID   map[string]*attachmentRecord
	draftAttachmentID string
	// Процесс, выбранный в чате. Пустая строка - все процессы
	processID string
	// Режим чата: read или plan. Пусто - read
	chatMode string
	// Список документов на выбор в режиме «План»
	planChoice *planChoice
}

type attachmentRecord struct {
	id         string
	fileName   string
	plainText  string
	characters int
}

type citation struct {
	Label        string `json:"label,omitempty"`
	Document     string `json:"document"`
	Edition      string `json:"edition"`
	Section      string `json:"section"`
	Paragraph    string `json:"paragraph"`
	Appendix     string `json:"appendix"`
	Quote        string `json:"quote"`
	OriginalLink string `json:"originalLink"`
	DocumentID   string `json:"documentId"`
	Scope        string `json:"scope"`
	Status       string `json:"status,omitempty"`
	// Процесс документа цитаты, для метки темы у источника
	ProcessID string `json:"processId,omitempty"`
}

type replyContact struct {
	Department string `json:"department"`
	Channel    string `json:"channel"`
}

type templateState struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Field      string `json:"field,omitempty"`
	FieldLabel string `json:"fieldLabel,omitempty"`
	DraftText  string `json:"draftText,omitempty"`
	FileLink   string `json:"fileLink,omitempty"`
	DraftLink  string `json:"draftLink,omitempty"`
	// Все поля без ответа, начиная с текущего: клиент задает их одной карточкой
	Questions []templateQuestion `json:"questions,omitempty"`
}

type draftFile struct {
	ownerID     string
	fileName    string
	contentType string
	body        []byte
}

type assistantReply struct {
	Role             string          `json:"role,omitempty"`
	Kind             string          `json:"kind"`
	Text             string          `json:"text"`
	RelevancePercent int             `json:"relevancePercent"`
	Citations        []citation      `json:"citations"`
	Contacts         *[]replyContact `json:"contacts,omitempty"`
	Template         *templateState  `json:"template,omitempty"`
	Draft            *draftState     `json:"draft,omitempty"`
	// Готовые ответы на вопрос ассистента: названия документов в уточнении (clarify) и в выборе документа (template_choice)
	Options []string `json:"options,omitempty"`
	// Где искали ответ: процесс, выбранный в чате. Пустой объект - все процессы
	Process *processRef `json:"process,omitempty"`
	// Подсказка под ответом: в режиме «Чтение» документы не готовятся
	Hint string `json:"hint,omitempty"`
	// Справка о документе: документ целиком для скачивания
	Download *replyDownload `json:"download,omitempty"`
	// Юридический движок сообщает, каких сведений нет в базе, и длительность ответа.
	Missing string  `json:"missing,omitempty"`
	Seconds float64 `json:"seconds,omitempty"`
}

type processRef struct {
	ProcessID string `json:"processId"`
	Title     string `json:"title"`
	// Все выбранные процессы, когда их несколько
	ProcessIDs []string `json:"processIds,omitempty"`
}

type messageAttachment struct {
	AttachmentID string `json:"attachmentId"`
	FileName     string `json:"fileName"`
}

type draftChange struct {
	Text     string    `json:"text"`
	Citation *citation `json:"citation,omitempty"`
}

type draftState struct {
	AttachmentID string        `json:"attachmentId"`
	Title        string        `json:"title"`
	DraftText    string        `json:"draftText"`
	Changes      []draftChange `json:"changes"`
	DraftLink    string        `json:"draftLink"`
}

type attachmentResponse struct {
	AttachmentID string `json:"attachmentId"`
	FileName     string `json:"fileName"`
	Characters   int    `json:"characters"`
}

type userLine struct {
	Role        string              `json:"role"`
	Text        string              `json:"text"`
	Attachments []messageAttachment `json:"attachments,omitempty"`
	// Ответы на вопросы документа пачкой: подпись поля и ответ
	Answers []answerLine `json:"answers,omitempty"`
}

type loginResponse struct {
	Token       string `json:"token"`
	UserID      string `json:"userId"`
	Login       string `json:"login"`
	DisplayName string `json:"displayName"`
	AccessLevel int    `json:"accessLevel"`
	Role        string `json:"role"`
}

type profileResponse struct {
	UserID      string        `json:"userId"`
	Login       string        `json:"login"`
	DisplayName string        `json:"displayName"`
	AccessLevel int           `json:"accessLevel"`
	Role        string        `json:"role"`
	Roles       []profileRole `json:"roles"`
	Permissions []string      `json:"permissions"`
}

type accountResponse struct {
	UserID      string   `json:"userId"`
	Login       string   `json:"login"`
	DisplayName string   `json:"displayName"`
	AccessLevel int      `json:"accessLevel"`
	Role        string   `json:"role"`
	RoleIDs     []string `json:"roleIds"`
	CreatedAt   string   `json:"createdAt"`
}

type sessionLine struct {
	SessionID string `json:"sessionId"`
	Title     string `json:"title"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
	CanRename bool   `json:"canRename"`
	CanDelete bool   `json:"canDelete"`
}

type documentResponse struct {
	DocumentID   string   `json:"documentId"`
	Title        string   `json:"title"`
	Edition      string   `json:"edition"`
	AccessLevel  int      `json:"accessLevel"`
	Scope        string   `json:"scope"`
	ProcessID    string   `json:"processId"`
	OwnerID      string   `json:"ownerId"`
	OwnerName    string   `json:"ownerName"`
	UploadedAt   string   `json:"uploadedAt"`
	VisibleToAll bool     `json:"visibleToAll"`
	RoleIDs      []string `json:"roleIds"`
	CanDelete    bool     `json:"canDelete"`
	// Документ стандартного набора компании: всегда в базе знаний, удалить нельзя
	Standard bool `json:"standard"`
	// Сколько прежних редакций в архиве
	ArchivedEditions int `json:"archivedEditions"`
}

type editionResponse struct {
	EditionID  string `json:"editionId"`
	Edition    string `json:"edition"`
	FileName   string `json:"fileName"`
	UploadedAt string `json:"uploadedAt"`
	ArchivedAt string `json:"archivedAt"`
	FileLink   string `json:"fileLink"`
}

type editionsResponse struct {
	Editions []editionResponse `json:"editions"`
}

func build_EditionResponse(documentID, editionID, edition, fileName string, uploadedAt, archivedAt time.Time) editionResponse {
	return editionResponse{
		EditionID:  editionID,
		Edition:    edition,
		FileName:   fileName,
		UploadedAt: uploadedAt.UTC().Format(time.RFC3339),
		ArchivedAt: archivedAt.UTC().Format(time.RFC3339),
		FileLink:   "/documents/" + documentID + "/editions/" + editionID + "/file",
	}
}

type uploadResponse struct {
	DocumentID string `json:"documentId"`
	Status     string `json:"status"`
	Edition    string `json:"edition"`
	Scope      string `json:"scope"`
	ProcessID  string `json:"processId"`
}

type importResponse struct {
	Documents []uploadResponse `json:"documents"`
	Skipped   []string         `json:"skipped"`
}

type processLine struct {
	ProcessID     string `json:"processId"`
	Title         string `json:"title"`
	Description   string `json:"description"`
	Color         string `json:"color"`
	DocumentCount int    `json:"documentCount"`
	// Подсказки модели видят только те, кто управляет базой знаний
	SelectPrompt  *string `json:"selectPrompt,omitempty"`
	AnswerPrompt  *string `json:"answerPrompt,omitempty"`
	ClarifyPrompt *string `json:"clarifyPrompt,omitempty"`
}

type processDetail struct {
	ProcessID     string `json:"processId"`
	Title         string `json:"title"`
	Description   string `json:"description"`
	Color         string `json:"color"`
	SelectPrompt  string `json:"selectPrompt"`
	AnswerPrompt  string `json:"answerPrompt"`
	ClarifyPrompt string `json:"clarifyPrompt"`
}

type shownDocument struct {
	DocumentID string `json:"documentId"`
	Title      string `json:"title"`
	Paragraph  string `json:"paragraph"`
	Scope      string `json:"scope"`
}

type auditEntry struct {
	EntryID     string          `json:"entryId"`
	UserID      string          `json:"userId"`
	DisplayName string          `json:"displayName"`
	Question    string          `json:"question"`
	CreatedAt   string          `json:"createdAt"`
	Documents   []shownDocument `json:"documents"`
}

type stub struct {
	mu                  sync.Mutex
	users               []*userRecord
	usersByID           map[string]*userRecord
	usersByLogin        map[string]*userRecord
	tokens              map[string]string
	documents           []*documentRecord
	documentsByID       map[string]*documentRecord
	sessions            map[string]*sessionRecord
	draftsByID          map[string]*draftFile
	processes           []*processRecord
	processesByID       map[string]*processRecord
	audit               []auditEntry
	roles               []*roleRecord
	rolesByID           map[string]*roleRecord
	permissionEvents    []permissionEvent
	nextUser            int
	nextDocument        int
	nextSession         int
	nextDraft           int
	nextAttachment      int
	nextProcess         int
	nextAudit           int
	nextRole            int
	nextPermissionEvent int
	nextSource          int
	sources             []*livestore.DocumentSource
	sourcesByID         map[string]*livestore.DocumentSource
	sourceItems         map[string]stubSourceItem
	sourceRunning       map[string]bool
	nextEdition         int
	// Настройки входа живут в памяти, как и остальные данные заглушки
	signupSettings auth.SignupSettings
	ldapSettings   auth.LdapSettings
	// Реквизиты организации для макетов документов в чате
	organization *stubOrganization
	// Правила оформления макетов из профилей internal/layout/formats
	layoutFormat layout.Format
}

// withoutUsers: старт без admin/admin и user/user, первая регистрация создаёт администратора
func create_StubStore(withoutUsers bool, authDefaults auth.Settings, layoutFormat layout.Format) *stub {
	stubStore := &stub{
		usersByID:           map[string]*userRecord{},
		usersByLogin:        map[string]*userRecord{},
		tokens:              map[string]string{},
		documentsByID:       map[string]*documentRecord{},
		sessions:            map[string]*sessionRecord{},
		draftsByID:          map[string]*draftFile{},
		processesByID:       map[string]*processRecord{},
		nextUser:            2,
		nextDocument:        3,
		nextSession:         1,
		nextDraft:           1,
		nextAttachment:      1,
		nextProcess:         1,
		nextAudit:           1,
		nextRole:            1,
		nextPermissionEvent: 1,
		nextSource:          1,
		rolesByID:           map[string]*roleRecord{},
		sourcesByID:         map[string]*livestore.DocumentSource{},
		sourceItems:         map[string]stubSourceItem{},
		sourceRunning:       map[string]bool{},
		signupSettings:      authDefaults.Signup,
		ldapSettings:        authDefaults.Ldap,
		layoutFormat:        layoutFormat,
	}
	stubStore.seed_Roles()
	if withoutUsers {
		stubStore.nextUser = 1
	} else {
		admin := stubStore.addUser(texts.SeedAdminID, texts.SeedAdminLogin, texts.SeedAdminPassword, texts.SeedAdminDisplayName, texts.SeedAdminAccessLevel)
		employee := stubStore.addUser(texts.SeedEmployeeID, texts.SeedEmployeeLogin, texts.SeedEmployeePassword, texts.SeedEmployeeDisplayName, texts.SeedEmployeeAccessLevel)
		admin.roleIDs = []string{texts.RoleIDChief}
		employee.roleIDs = []string{texts.RoleIDUser}
		stubStore.sync_Access(admin)
		stubStore.sync_Access(employee)
	}
	stubStore.add_DefaultProcess()
	stubStore.addDocument(texts.SeedInstructionID, texts.SeedInstructionTitle, texts.EditionDefault, texts.SeedDocumentAccessOpen, texts.SeedInstructionFileName, texts.PlainTextContentType, []byte(texts.SeedInstructionBody))
	// Инструкция заглушки - ее стандартный набор, как source/docs в боевом режиме
	stubStore.documentsByID[texts.SeedInstructionID].standard = true
	stubStore.addDocument(texts.SeedRestrictedID, texts.SeedRestrictedTitle, texts.EditionDefault, texts.SeedDocumentAccessClosed, texts.SeedRestrictedFileName, texts.PlainTextContentType, []byte(texts.SeedRestrictedBody))
	return stubStore
}

func (stubStore *stub) addUser(id, login, password, displayName string, accessLevel int) *userRecord {
	record := &userRecord{
		id:          id,
		login:       login,
		password:    password,
		displayName: displayName,
		accessLevel: accessLevel,
		createdAt:   time.Now().UTC(),
	}
	stubStore.users = append(stubStore.users, record)
	stubStore.usersByID[id] = record
	stubStore.usersByLogin[strings.ToLower(login)] = record
	return record
}

// Логин без учёта регистра, как в боевом режиме
func (stubStore *stub) find_UserByLogin(login string) *userRecord {
	return stubStore.usersByLogin[strings.ToLower(strings.TrimSpace(login))]
}

func (stubStore *stub) create_NextUser(login, password, displayName string, accessLevel int, pending bool) *userRecord {
	usersBefore := len(stubStore.users)
	userID := "user-" + strconv.Itoa(stubStore.nextUser)
	stubStore.nextUser++
	record := stubStore.addUser(userID, login, password, displayName, accessLevel)
	record.pending = pending
	record.roleIDs = []string{auth.RoleID_ForNewAccount(usersBefore, accessLevel, pending)}
	stubStore.sync_Access(record)
	stubStore.append_PermissionEvent(record.id, texts.PermissionActionAssign, record.roleIDs[0], record.id)
	return record
}

func (stubStore *stub) addDocument(id, title, edition string, accessLevel int, fileName, contentType string, body []byte) {
	record := &documentRecord{
		id:          id,
		title:       title,
		edition:     edition,
		accessLevel: accessLevel,
		scope:       texts.ScopeGlobal,
		processID:   texts.ProcessDocuments,
		uploadedAt:  time.Now().UTC(),
		fileName:    fileName,
		contentType: contentType,
		body:        body,
	}
	if accessLevel >= texts.SeedAdminAccessLevel {
		record.visibleToAll = false
		record.roleIDs = []string{texts.RoleIDAdmin, texts.RoleIDChief}
	} else {
		record.visibleToAll = true
		record.roleIDs = []string{}
	}
	stubStore.documents = append(stubStore.documents, record)
	stubStore.documentsByID[id] = record
}

func (stubStore *stub) add_DefaultProcess() {
	stubStore.store_Process(&processRecord{
		id:            texts.ProcessDocuments,
		title:         texts.ProcessTitleDocuments,
		color:         texts.SeedProcessColor,
		selectPrompt:  texts.LiveSelectPrompt,
		answerPrompt:  texts.LiveAnswerPrompt,
		clarifyPrompt: texts.LiveClarifyPrompt,
	})
}

func (stubStore *stub) store_Process(process *processRecord) {
	stubStore.processes = append(stubStore.processes, process)
	stubStore.processesByID[process.id] = process
}

func (stubStore *stub) resolve_UploadProcess(raw string) (string, bool) {
	processID := strings.TrimSpace(raw)
	if processID == "" {
		processID = texts.ProcessDocuments
	}
	if stubStore.processesByID[processID] == nil {
		return "", false
	}
	return processID, true
}

func (stubStore *stub) register_StubRoutes() *http.ServeMux {
	routeMux := http.NewServeMux()
	routeMux.HandleFunc("POST /auth/login", stubStore.serve_Login)
	routeMux.HandleFunc("POST /auth/logout", stubStore.serve_Logout)
	routeMux.HandleFunc("GET /auth/config", stubStore.serve_AuthConfig)
	routeMux.HandleFunc("POST /auth/signup", stubStore.serve_Signup)
	routeMux.HandleFunc("POST /auth/ldap", stubStore.serve_LdapLogin)
	routeMux.HandleFunc("POST /auth/kerberos", stubStore.serve_KerberosLogin)
	routeMux.HandleFunc("GET /auth/admin/details", stubStore.serve_AdminDetails)
	routeMux.HandleFunc("GET /auth/admin/config", stubStore.serve_SignupSettings)
	routeMux.HandleFunc("POST /auth/admin/config", stubStore.serve_SaveSignupSettings)
	routeMux.HandleFunc("GET /auth/admin/config/ldap", stubStore.serve_LdapToggle)
	routeMux.HandleFunc("POST /auth/admin/config/ldap", stubStore.serve_SaveLdapToggle)
	routeMux.HandleFunc("GET /auth/admin/config/ldap/server", stubStore.serve_LdapServer)
	routeMux.HandleFunc("POST /auth/admin/config/ldap/server", stubStore.serve_SaveLdapServer)
	routeMux.HandleFunc("GET /me", stubStore.serve_CurrentUser)
	routeMux.HandleFunc("GET /permissions", stubStore.serve_ListPermissions)
	routeMux.HandleFunc("GET /roles", stubStore.serve_ListRoles)
	routeMux.HandleFunc("POST /roles", stubStore.serve_CreateRole)
	routeMux.HandleFunc("PATCH /roles/{roleId}", stubStore.serve_UpdateRole)
	routeMux.HandleFunc("DELETE /roles/{roleId}", stubStore.serve_DeleteRole)
	routeMux.HandleFunc("POST /sessions", stubStore.serve_CreateSession)
	routeMux.HandleFunc("GET /sessions", stubStore.serve_ListSessions)
	routeMux.HandleFunc("GET /sessions/{sessionId}", stubStore.serve_SessionHistory)
	routeMux.HandleFunc("PATCH /sessions/{sessionId}", stubStore.serve_RenameSession)
	routeMux.HandleFunc("DELETE /sessions/{sessionId}", stubStore.serve_DeleteSession)
	routeMux.HandleFunc("POST /sessions/{sessionId}/messages", stubStore.serve_SessionMessage)
	routeMux.HandleFunc("POST /sessions/{sessionId}/attachments", stubStore.serve_SessionAttachment)
	routeMux.HandleFunc("GET /documents", stubStore.serve_ListDocuments)
	routeMux.HandleFunc("POST /documents", stubStore.serve_UploadDocument)
	routeMux.HandleFunc("POST /documents/import", stubStore.serve_ImportDocuments)
	routeMux.HandleFunc("GET /documents/{documentId}/structure", stubStore.serve_DocumentStructure)
	routeMux.HandleFunc("GET /processes", stubStore.serve_ListProcesses)
	routeMux.HandleFunc("POST /processes", stubStore.serve_CreateProcess)
	routeMux.HandleFunc("PATCH /processes/{processId}", stubStore.serve_PatchProcess)
	routeMux.HandleFunc("DELETE /processes/{processId}", stubStore.serve_DeleteProcess)
	routeMux.HandleFunc("GET /documents/{documentId}/file", stubStore.serve_DocumentFile)
	routeMux.HandleFunc("GET /documents/{documentId}/editions", stubStore.serve_ListEditions)
	routeMux.HandleFunc("GET /documents/{documentId}/editions/{editionId}/file", stubStore.serve_EditionFile)
	routeMux.HandleFunc("DELETE /documents/{documentId}", stubStore.serve_DeleteDocument)
	routeMux.HandleFunc("GET /blanks/{blankId}/file", stubStore.serve_BlankFile)
	routeMux.HandleFunc("GET /drafts/{draftId}/file", stubStore.serve_DraftFile)
	routeMux.HandleFunc("GET /users", stubStore.serve_ListUsers)
	routeMux.HandleFunc("POST /users", stubStore.serve_CreateUser)
	routeMux.HandleFunc("PATCH /users/{userId}", stubStore.serve_PatchUser)
	routeMux.HandleFunc("DELETE /users/{userId}", stubStore.serve_DeleteUser)
	routeMux.HandleFunc("GET /audit", stubStore.serve_Audit)
	routeMux.HandleFunc("GET /audit/permissions", stubStore.serve_PermissionAudit)
	routeMux.HandleFunc("POST /drafts/check", stubStore.serve_DraftCheck)
	routeMux.HandleFunc("POST /drafts/fix", stubStore.serve_DraftFix)
	return routeMux
}

func (stubStore *stub) serve_Login(responseWriter http.ResponseWriter, request *http.Request) {
	loginName, password, ok := read_LoginRequest(responseWriter, request)
	if !ok {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	record := stubStore.find_UserByLogin(loginName)
	if record == nil || !match_Password(record.password, password) {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorInvalidCredentials)
		return
	}
	stubStore.write_LoginForRecord(responseWriter, record)
}

// Новый токен и ответ входа. Вызывать под stubStore.mu
func (stubStore *stub) write_LoginForRecord(responseWriter http.ResponseWriter, record *userRecord) {
	token, err := create_RandomToken()
	if err != nil {
		write_ErrorResponse(responseWriter, http.StatusInternalServerError, texts.ErrorInvalidBody)
		return
	}
	stubStore.tokens[token] = record.id
	write_JSONResponse(responseWriter, http.StatusOK, loginResponse{
		Token:       token,
		UserID:      record.id,
		Login:       record.login,
		DisplayName: record.displayName,
		AccessLevel: record.accessLevel,
		Role:        record.role(),
	})
}

func (stubStore *stub) serve_Logout(responseWriter http.ResponseWriter, request *http.Request) {
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	token, found := read_BearerToken(request)
	if !found || stubStore.tokens[token] == "" {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	delete(stubStore.tokens, token)
	responseWriter.WriteHeader(http.StatusNoContent)
}

func (stubStore *stub) serve_CurrentUser(responseWriter http.ResponseWriter, request *http.Request) {
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	record, found := stubStore.find_UserByBearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	write_JSONResponse(responseWriter, http.StatusOK, stubStore.profile_Of(record))
}

func (stubStore *stub) serve_CreateSession(responseWriter http.ResponseWriter, request *http.Request) {
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	record, found := stubStore.find_UserByBearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	sessionID := "session-" + strconv.Itoa(stubStore.nextSession)
	stubStore.nextSession++
	now := time.Now().UTC()
	stubStore.sessions[sessionID] = &sessionRecord{
		id:              sessionID,
		ownerID:         record.id,
		createdAt:       now,
		updatedAt:       now,
		messages:        []any{},
		attachmentsByID: map[string]*attachmentRecord{},
	}
	write_JSONResponse(responseWriter, http.StatusOK, struct {
		SessionID string `json:"sessionId"`
	}{SessionID: sessionID})
}

func (stubStore *stub) serve_ListSessions(responseWriter http.ResponseWriter, request *http.Request) {
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	record, found := stubStore.find_UserByBearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	lines := []sessionLine{}
	for _, session := range stubStore.sessions {
		if session.ownerID != record.id {
			continue
		}
		lines = append(lines, session.line())
	}
	sort.Slice(lines, func(left, right int) bool {
		if lines[left].UpdatedAt == lines[right].UpdatedAt {
			return lines[left].SessionID > lines[right].SessionID
		}
		return lines[left].UpdatedAt > lines[right].UpdatedAt
	})
	write_JSONResponse(responseWriter, http.StatusOK, build_SessionList(lines))
}

func (stubStore *stub) serve_SessionHistory(responseWriter http.ResponseWriter, request *http.Request) {
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	session, found := stubStore.find_OwnedSession(responseWriter, request)
	if !found {
		return
	}
	write_JSONResponse(responseWriter, http.StatusOK, struct {
		SessionID  string   `json:"sessionId"`
		ProcessID  string   `json:"processId"`
		ProcessIDs []string `json:"processIds"`
		Messages   []any    `json:"messages"`
		Mode       string   `json:"mode"`
	}{SessionID: session.id, ProcessID: stubStore.saved_SessionProcess(session), ProcessIDs: split_ProcessSelection(stubStore.saved_SessionProcess(session)), Messages: session.messages, Mode: saved_ChatMode(session.chatMode)})
}

func (stubStore *stub) serve_SessionMessage(responseWriter http.ResponseWriter, request *http.Request) {
	var body struct {
		Text string `json:"text"`
		// nil - процесс, сохранённый в сессии, пустая строка - все процессы
		ProcessID *string `json:"processId"`
		// Несколько процессов сразу, важнее processId
		ProcessIDs    *[]string `json:"processIds"`
		AttachmentIDs []string  `json:"attachmentIds"`
		// nil - режим, сохраненный в сессии: read или plan
		Mode *string `json:"mode"`
		// Ответы на вопросы документа пачкой. text - их запись для ленты
		Answers []fieldAnswer `json:"answers"`
	}
	if err := read_JSONBody(responseWriter, request, &body); err != nil {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	if strings.TrimSpace(body.Text) == "" {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	session, found := stubStore.find_OwnedSession(responseWriter, request)
	if !found {
		return
	}
	record := stubStore.usersByID[session.ownerID]
	permissions := stubStore.permissions_Of(record)
	processID := stubStore.saved_SessionProcess(session)
	requestedProcess := read_RequestedProcesses(body.ProcessID, body.ProcessIDs)
	if requestedProcess != nil {
		processID = *requestedProcess
	}
	for _, id := range split_ProcessSelection(processID) {
		if stubStore.processesByID[id] == nil {
			write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
			return
		}
	}
	if requestedProcess != nil {
		session.processID = processID
	}
	mode, modeOK := resolve_ChatMode(body.Mode, session.chatMode)
	if !modeOK {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	session.chatMode = mode
	var reply assistantReply
	var step blank.Step
	var turnAttachments []messageAttachment
	// «План»: документ по просьбе. В заглушке нет модели: вид документа - по словам или из списка
	var plan planResult
	if mode == texts.ChatModePlan && auth.Has_Permission(permissions, texts.PermissionTemplatesUse) && auth.Has_Permission(permissions, texts.PermissionChatUse) {
		if len(body.AttachmentIDs) == 0 {
			plan, _ = advance_PlanTurn(session, body.Text, body.Answers, func() (layout.Organization, error) {
				return stubStore.organization.layout_Snapshot(), nil
			}, stubStore.layoutFormat, nil, time.Now())
		}
	} else {
		clear_DocumentStates(session)
	}
	layoutHandled := plan.Layout != nil
	var layoutStep layout.Step
	if layoutHandled {
		layoutStep = *plan.Layout
	}
	handled := plan.Blank != nil
	if handled {
		step = *plan.Blank
	}
	var overview overviewResult
	if plan.Reply == nil && !layoutHandled && !handled && len(body.AttachmentIDs) == 0 && auth.Has_Permission(permissions, texts.PermissionChatUse) {
		overview = stubStore.find_DocumentOverview(record, body.Text, processID)
	}
	if plan.Reply != nil {
		reply = *plan.Reply
	} else if overview.Found {
		reply = overview.Reply
	} else if layoutHandled {
		reply = reply_FromLayout(layoutStep)
		if layoutStep.Ready && len(layoutStep.Docx) > 0 {
			reply.Template.DraftLink = draft_FileLink(stubStore.save_DraftFile(record.id, layoutStep.FileName, layoutStep.Docx))
		}
	} else if handled {
		if !auth.Has_Permission(permissions, texts.PermissionChatUse) {
			write_ErrorResponse(responseWriter, http.StatusForbidden, texts.ErrorAccessDenied)
			return
		}
		apply_BlankStep(session, step)
		reply = reply_FromBlank(step)
	} else {
		source, revising, sourceError := session.revision_Source(body.AttachmentIDs)
		if sourceError != nil {
			write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
			return
		}
		if revising {
			if !auth.Has_Permission(permissions, texts.PermissionDraftsAssist) {
				write_ErrorResponse(responseWriter, http.StatusForbidden, texts.ErrorAccessDenied)
				return
			}
			var reviseError error
			reply, turnAttachments, reviseError = stubStore.finish_StubRevision(session, source)
			if reviseError != nil {
				write_ErrorResponse(responseWriter, http.StatusInternalServerError, texts.ErrorStore)
				return
			}
		} else {
			if !auth.Has_Permission(permissions, texts.PermissionChatUse) {
				write_ErrorResponse(responseWriter, http.StatusForbidden, texts.ErrorAccessDenied)
				return
			}
			var replyError error
			reply, step, replyError = build_AssistantReply(session, body.Text, false)
			if replyError != nil {
				write_DraftError(responseWriter, replyError)
				return
			}
		}
	}
	if step.Ready && len(step.DraftDocx) > 0 && reply.Template != nil {
		draftID := stubStore.save_DraftFile(record.id, step.BlankID+".docx", step.DraftDocx)
		reply.Template.DraftLink = draft_FileLink(draftID)
	}
	if plan.Reply == nil {
		reply = stubStore.apply_StubProcess(reply, processID)
		if reply.Kind == texts.KindAnswer || reply.Kind == texts.KindFallback {
			reply = reply_WithContacts(reply, stubStore.lookup_Contacts(record, body.Text))
		}
	}
	reply = apply_ModeHint(reply, mode, body.Text)
	if session.title == "" {
		session.title = build_SessionTitle(body.Text)
	}
	session.updatedAt = time.Now().UTC()
	session.messages = append(session.messages, userLine{Role: "user", Text: body.Text, Attachments: turnAttachments, Answers: plan.Answered})
	stored := reply
	stored.Role = "assistant"
	session.messages = append(session.messages, stored)
	stubStore.append_AuditEntry(record, body.Text)
	write_JSONResponse(responseWriter, http.StatusOK, reply)
}

func (stubStore *stub) serve_ListDocuments(responseWriter http.ResponseWriter, request *http.Request) {
	scopeFilter, scopeOK := read_ListScope(request.URL.Query().Get("scope"))
	if !scopeOK {
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
	if scopeFilter == texts.ScopeAll && !auth.Has_Permission(stubStore.permissions_Of(record), texts.PermissionPersonalViewAll) {
		write_ErrorResponse(responseWriter, http.StatusForbidden, texts.ErrorAccessDenied)
		return
	}
	processFilter := strings.TrimSpace(request.URL.Query().Get("process"))
	if processFilter != "" && stubStore.processesByID[processFilter] == nil {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	visible := make([]documentResponse, 0)
	for _, document := range stubStore.documents {
		if processFilter != "" && document.processID != processFilter {
			continue
		}
		if !document_MatchesList(scopeFilter, document.scope, document.ownerID, record.id, document.visibleToAll, document.roleIDs, record.roleIDs) {
			continue
		}
		visible = append(visible, document.line(record, stubStore.ownerName(document), stubStore.permissions_Of(record)))
	}
	write_JSONResponse(responseWriter, http.StatusOK, struct {
		CanUploadGlobal   bool               `json:"canUploadGlobal"`
		CanUploadPersonal bool               `json:"canUploadPersonal"`
		Documents         []documentResponse `json:"documents"`
	}{
		CanUploadGlobal:   auth.Has_Permission(stubStore.permissions_Of(record), texts.PermissionGlobalManage),
		CanUploadPersonal: auth.Has_Permission(stubStore.permissions_Of(record), texts.PermissionPersonalUpload),
		Documents:         visible,
	})
}

func (stubStore *stub) serve_UploadDocument(responseWriter http.ResponseWriter, request *http.Request) {
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
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	record, found := stubStore.find_UserByBearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	permissions := stubStore.permissions_Of(record)
	scope, scopeOK := read_UploadScope(request.FormValue("scope"), auth.Has_Permission(permissions, texts.PermissionGlobalManage))
	if !scopeOK {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	if !allow_Upload(permissions, scope) {
		write_ErrorResponse(responseWriter, http.StatusForbidden, texts.ErrorAccessDenied)
		return
	}
	accessLevel, visibleToAll, roleIDs, audienceOK := read_UploadAudience(request.Form["roleIds"], request.FormValue("accessLevel"), record.accessLevel, scope == texts.ScopePersonal)
	if !audienceOK || !stubStore.known_RoleIDs(roleIDs) {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	cleanName := filepath.Base(fileName)
	if cleanName == "" || cleanName == "." {
		cleanName = "document.bin"
	}
	ownerID := ""
	if scope == texts.ScopePersonal {
		ownerID = record.id
	}
	processID, processOK := stubStore.resolve_UploadProcess(request.FormValue("process"))
	if !processOK {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	write_JSONResponse(responseWriter, http.StatusOK, stubStore.store_Document(title, edition, accessLevel, scope, ownerID, processID, cleanName, contentType, body, visibleToAll, roleIDs))
}

func (stubStore *stub) store_Document(title, edition string, accessLevel int, scope, ownerID, processID, fileName, contentType string, body []byte, visibleToAll bool, roleIDs []string) uploadResponse {
	now := time.Now().UTC()
	for _, document := range stubStore.documents {
		if !same_DocumentTitle(document, title, scope, ownerID) {
			continue
		}
		// Прежняя редакция с другим содержимым уходит в архив
		if !bytes.Equal(document.body, body) {
			stubStore.nextEdition++
			document.editions = append(document.editions, stubEdition{
				id:          strconv.Itoa(stubStore.nextEdition),
				edition:     document.edition,
				fileName:    document.fileName,
				contentType: document.contentType,
				body:        document.body,
				uploadedAt:  document.uploadedAt,
				archivedAt:  now,
			})
		}
		document.edition = edition
		document.accessLevel = accessLevel
		document.processID = processID
		document.fileName = fileName
		document.contentType = contentType
		document.body = body
		document.uploadedAt = now
		document.visibleToAll = visibleToAll
		document.roleIDs = copy_Strings(roleIDs)
		return uploadResponse{
			DocumentID: document.id,
			Status:     texts.StatusAccepted,
			Edition:    document.edition,
			Scope:      document.scope,
			ProcessID:  document.processID,
		}
	}
	documentID := "doc-" + strconv.Itoa(stubStore.nextDocument)
	stubStore.nextDocument++
	created := &documentRecord{
		id:           documentID,
		title:        title,
		edition:      edition,
		accessLevel:  accessLevel,
		scope:        scope,
		processID:    processID,
		ownerID:      ownerID,
		uploadedAt:   now,
		fileName:     fileName,
		contentType:  contentType,
		body:         body,
		visibleToAll: visibleToAll,
		roleIDs:      copy_Strings(roleIDs),
	}
	stubStore.documents = append(stubStore.documents, created)
	stubStore.documentsByID[documentID] = created
	return uploadResponse{
		DocumentID: documentID,
		Status:     texts.StatusAccepted,
		Edition:    edition,
		Scope:      scope,
		ProcessID:  processID,
	}
}

func (stubStore *stub) serve_ImportDocuments(responseWriter http.ResponseWriter, request *http.Request) {
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
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	record, found := stubStore.find_UserByBearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	permissions := stubStore.permissions_Of(record)
	scope, scopeOK := read_UploadScope(request.FormValue("scope"), auth.Has_Permission(permissions, texts.PermissionGlobalManage))
	if !scopeOK {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	if !allow_Upload(permissions, scope) {
		write_ErrorResponse(responseWriter, http.StatusForbidden, texts.ErrorAccessDenied)
		return
	}
	accessLevel, visibleToAll, roleIDs, audienceOK := read_UploadAudience(request.Form["roleIds"], request.FormValue("accessLevel"), record.accessLevel, scope == texts.ScopePersonal)
	if !audienceOK || !stubStore.known_RoleIDs(roleIDs) {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	ownerID := ""
	if scope == texts.ScopePersonal {
		ownerID = record.id
	}
	processID, processOK := stubStore.resolve_UploadProcess(request.FormValue("process"))
	if !processOK {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	imported := make([]uploadResponse, 0, len(listing.Files))
	for _, file := range listing.Files {
		imported = append(imported, stubStore.store_Document(file.Name, edition, accessLevel, scope, ownerID, processID, file.Name, contentType_ForFile(file.Name), file.Body, visibleToAll, roleIDs))
	}
	write_JSONResponse(responseWriter, http.StatusOK, importResponse{Documents: imported, Skipped: listing.Skipped})
}

func (stubStore *stub) serve_ListProcesses(responseWriter http.ResponseWriter, request *http.Request) {
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	record, found := stubStore.find_UserByBearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	canManage := auth.Has_Permission(stubStore.permissions_Of(record), texts.PermissionGlobalManage)
	lines := make([]processLine, 0, len(stubStore.processes))
	for _, process := range stubStore.processes {
		lines = append(lines, build_ProcessLine(stubStore.process_Card(process, record), canManage))
	}
	write_JSONResponse(responseWriter, http.StatusOK, struct {
		CanManage bool          `json:"canManage"`
		Processes []processLine `json:"processes"`
	}{CanManage: canManage, Processes: lines})
}

func (stubStore *stub) serve_CreateProcess(responseWriter http.ResponseWriter, request *http.Request) {
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
	title := strings.TrimSpace(body.Title)
	if title == "" {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	fallback := stubStore.processesByID[texts.ProcessDocuments]
	selectPrompt := texts.LiveSelectPrompt
	answerPrompt := texts.LiveAnswerPrompt
	clarifyPrompt := texts.LiveClarifyPrompt
	if fallback != nil {
		selectPrompt = fallback.selectPrompt
		answerPrompt = fallback.answerPrompt
		clarifyPrompt = fallback.clarifyPrompt
	}
	created := &processRecord{
		id:            "process-" + strconv.Itoa(stubStore.nextProcess),
		title:         title,
		description:   strings.TrimSpace(body.Description),
		color:         color,
		selectPrompt:  prompt_OrText(body.SelectPrompt, selectPrompt),
		answerPrompt:  prompt_OrText(body.AnswerPrompt, answerPrompt),
		clarifyPrompt: prompt_OrText(body.ClarifyPrompt, clarifyPrompt),
	}
	stubStore.nextProcess++
	stubStore.store_Process(created)
	write_JSONResponse(responseWriter, http.StatusOK, processDetail{
		ProcessID:     created.id,
		Title:         created.title,
		Description:   created.description,
		Color:         created.color,
		SelectPrompt:  created.selectPrompt,
		AnswerPrompt:  created.answerPrompt,
		ClarifyPrompt: created.clarifyPrompt,
	})
}

func (stubStore *stub) serve_DocumentFile(responseWriter http.ResponseWriter, request *http.Request) {
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	record, found := stubStore.find_UserByBearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	document := stubStore.documentsByID[request.PathValue("documentId")]
	if document == nil {
		write_ErrorResponse(responseWriter, http.StatusNotFound, texts.ErrorNotFound)
		return
	}
	if status := document_ReadStatus(record.id, stubStore.permissions_Of(record), record.roleIDs, document.scope, document.ownerID, document.visibleToAll, document.roleIDs); status != http.StatusOK {
		if status == http.StatusNotFound {
			write_ErrorResponse(responseWriter, status, texts.ErrorNotFound)
			return
		}
		write_ErrorResponse(responseWriter, status, texts.ErrorAccessDenied)
		return
	}
	write_FileResponse(responseWriter, document.fileName, document.contentType, document.body)
}

// Структурированный документ нужен демо-интерфейсу и в stub-режиме. В live-режиме
// ту же схему формирует юридический engine после полноценного разбора нормативного акта.
func (stubStore *stub) serve_DocumentStructure(responseWriter http.ResponseWriter, request *http.Request) {
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	document, found := stubStore.find_ReadableDocument(responseWriter, request)
	if !found {
		return
	}
	text := strings.TrimSpace(string(document.body))
	chunks := []map[string]any{{
		"path":    "Положение 1",
		"section": document.title,
		"text":    text,
		"notes":   []string{},
		"kind":    "provision",
	}}
	write_JSONResponse(responseWriter, http.StatusOK, map[string]any{
		"doc_id":         document.id,
		"file_name":      document.fileName,
		"category":       "company",
		"category_label": "Документы компании",
		"doc_type":       "Документ",
		"title":          document.title,
		"display_name":   document.title,
		"status_note":    "Действующая редакция",
		"chunks":         chunks,
		"removed_lines":  0,
	})
}

// Документ, который сотрудник видит, или код ошибки для ответа
func (stubStore *stub) find_ReadableDocument(responseWriter http.ResponseWriter, request *http.Request) (*documentRecord, bool) {
	record, found := stubStore.find_UserByBearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return nil, false
	}
	document := stubStore.documentsByID[request.PathValue("documentId")]
	if document == nil {
		write_ErrorResponse(responseWriter, http.StatusNotFound, texts.ErrorNotFound)
		return nil, false
	}
	if status := document_ReadStatus(record.id, stubStore.permissions_Of(record), record.roleIDs, document.scope, document.ownerID, document.visibleToAll, document.roleIDs); status != http.StatusOK {
		if status == http.StatusNotFound {
			write_ErrorResponse(responseWriter, status, texts.ErrorNotFound)
			return nil, false
		}
		write_ErrorResponse(responseWriter, status, texts.ErrorAccessDenied)
		return nil, false
	}
	return document, true
}

func (stubStore *stub) serve_ListEditions(responseWriter http.ResponseWriter, request *http.Request) {
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	document, found := stubStore.find_ReadableDocument(responseWriter, request)
	if !found {
		return
	}
	lines := make([]editionResponse, 0, len(document.editions))
	for index := len(document.editions) - 1; index >= 0; index-- {
		edition := document.editions[index]
		lines = append(lines, build_EditionResponse(document.id, edition.id, edition.edition, edition.fileName, edition.uploadedAt, edition.archivedAt))
	}
	write_JSONResponse(responseWriter, http.StatusOK, editionsResponse{Editions: lines})
}

func (stubStore *stub) serve_EditionFile(responseWriter http.ResponseWriter, request *http.Request) {
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	document, found := stubStore.find_ReadableDocument(responseWriter, request)
	if !found {
		return
	}
	for _, edition := range document.editions {
		if edition.id == request.PathValue("editionId") {
			write_FileResponse(responseWriter, edition.fileName, edition.contentType, edition.body)
			return
		}
	}
	write_ErrorResponse(responseWriter, http.StatusNotFound, texts.ErrorNotFound)
}

func (stubStore *stub) serve_DeleteDocument(responseWriter http.ResponseWriter, request *http.Request) {
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	record, found := stubStore.find_UserByBearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	documentID := request.PathValue("documentId")
	document := stubStore.documentsByID[documentID]
	if document == nil {
		write_ErrorResponse(responseWriter, http.StatusNotFound, texts.ErrorNotFound)
		return
	}
	if status := document_ReadStatus(record.id, stubStore.permissions_Of(record), record.roleIDs, document.scope, document.ownerID, document.visibleToAll, document.roleIDs); status != http.StatusOK {
		if status == http.StatusNotFound {
			write_ErrorResponse(responseWriter, status, texts.ErrorNotFound)
			return
		}
		write_ErrorResponse(responseWriter, status, texts.ErrorAccessDenied)
		return
	}
	if !check_CanDeleteDocument(record.id, stubStore.permissions_Of(record), document.scope, document.ownerID) {
		write_ErrorResponse(responseWriter, http.StatusForbidden, texts.ErrorAccessDenied)
		return
	}
	if document.standard {
		write_ErrorResponse(responseWriter, http.StatusConflict, texts.ErrorStandardDocument)
		return
	}
	delete(stubStore.documentsByID, documentID)
	remaining := stubStore.documents[:0]
	for _, item := range stubStore.documents {
		if item.id != documentID {
			remaining = append(remaining, item)
		}
	}
	stubStore.documents = remaining
	responseWriter.WriteHeader(http.StatusNoContent)
}

func same_DocumentTitle(document *documentRecord, title, scope, ownerID string) bool {
	if document.sourceID != "" {
		return false
	}
	if document.title != title || document.scope != scope {
		return false
	}
	if scope == texts.ScopePersonal {
		return document.ownerID == ownerID
	}
	return true
}

func (stubStore *stub) ownerName(document *documentRecord) string {
	if document.scope != texts.ScopePersonal {
		return ""
	}
	owner := stubStore.usersByID[document.ownerID]
	if owner == nil {
		return ""
	}
	return owner.displayName
}

func (stubStore *stub) serve_ListUsers(responseWriter http.ResponseWriter, request *http.Request) {
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	if _, found := stubStore.require_AdminUser(responseWriter, request); !found {
		return
	}
	accounts := make([]accountResponse, 0, len(stubStore.users))
	for _, record := range stubStore.users {
		accounts = append(accounts, record.account())
	}
	write_JSONResponse(responseWriter, http.StatusOK, struct {
		Users []accountResponse `json:"users"`
	}{Users: accounts})
}

func (stubStore *stub) serve_CreateUser(responseWriter http.ResponseWriter, request *http.Request) {
	created, errorCode := read_CreateUserRequest(responseWriter, request)
	if errorCode != "" {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, errorCode)
		return
	}
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	if _, found := stubStore.require_AdminUser(responseWriter, request); !found {
		return
	}
	if stubStore.find_UserByLogin(created.login) != nil {
		write_ErrorResponse(responseWriter, http.StatusConflict, texts.ErrorLoginTaken)
		return
	}
	record := stubStore.create_NextUser(created.login, created.password, created.displayName, created.accessLevel, created.pending)
	write_JSONResponse(responseWriter, http.StatusOK, record.account())
}

func (stubStore *stub) serve_PatchUser(responseWriter http.ResponseWriter, request *http.Request) {
	patch, errorCode := read_PatchUserRequest(responseWriter, request)
	if errorCode != "" {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, errorCode)
		return
	}
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	actor, found := stubStore.require_AdminUser(responseWriter, request)
	if !found {
		return
	}
	record := stubStore.usersByID[request.PathValue("userId")]
	if record == nil {
		write_ErrorResponse(responseWriter, http.StatusNotFound, texts.ErrorNotFound)
		return
	}
	// Свою роль администратор не меняет, как в боевом режиме
	levelChanges := patch.AccessLevel != nil && *patch.AccessLevel != record.accessLevel
	pendingChanges := patch.Pending != nil && *patch.Pending != record.pending
	if actor.id == record.id && (levelChanges || pendingChanges || patch.RoleIDs != nil) {
		write_ErrorResponse(responseWriter, http.StatusForbidden, texts.ErrorAccessDenied)
		return
	}
	if patch.Login != nil {
		owner := stubStore.find_UserByLogin(*patch.Login)
		if owner != nil && owner.id != record.id {
			write_ErrorResponse(responseWriter, http.StatusConflict, texts.ErrorLoginTaken)
			return
		}
		delete(stubStore.usersByLogin, strings.ToLower(record.login))
		record.login = *patch.Login
		stubStore.usersByLogin[strings.ToLower(record.login)] = record
	}
	if patch.DisplayName != nil {
		record.displayName = *patch.DisplayName
	}
	if patch.Password != nil {
		record.password = *patch.Password
	}
	if patch.AccessLevel != nil {
		record.accessLevel = *patch.AccessLevel
	}
	if patch.Pending != nil {
		record.pending = *patch.Pending
	}
	if patch.RoleIDs != nil || levelChanges || pendingChanges {
		next := []string{auth.RoleID_ForNewAccount(1, record.accessLevel, record.pending)}
		if patch.RoleIDs != nil {
			next = *patch.RoleIDs
		}
		if !stubStore.known_RoleIDs(next) {
			write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
			return
		}
		if stubStore.chiefs_After(record.id, next) == 0 {
			write_ErrorResponse(responseWriter, http.StatusForbidden, texts.ErrorAccessDenied)
			return
		}
		oldRoles := copy_Strings(record.roleIDs)
		record.roleIDs = copy_Strings(next)
		stubStore.sync_Access(record)
		stubStore.record_RoleDiff(actor.id, record.id, oldRoles, record.roleIDs)
	}
	write_JSONResponse(responseWriter, http.StatusOK, record.account())
}

// Удаление учетной записи по тем же правилам, что в боевом режиме: себя нельзя,
// главного администратора - только с roles.manage и если останется другой.
// Вместе с ней уходят токены, сессии, черновики и личные документы
func (stubStore *stub) serve_DeleteUser(responseWriter http.ResponseWriter, request *http.Request) {
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	actor, found := stubStore.require_AdminUser(responseWriter, request)
	if !found {
		return
	}
	record := stubStore.usersByID[request.PathValue("userId")]
	if record == nil {
		write_ErrorResponse(responseWriter, http.StatusNotFound, texts.ErrorNotFound)
		return
	}
	if actor.id == record.id {
		write_ErrorResponse(responseWriter, http.StatusForbidden, texts.ErrorAccessDenied)
		return
	}
	if contains_Role(record.roleIDs, texts.RoleIDChief) {
		if !auth.Has_Permission(stubStore.permissions_Of(actor), texts.PermissionRolesManage) || stubStore.chiefs_After(record.id, nil) == 0 {
			write_ErrorResponse(responseWriter, http.StatusForbidden, texts.ErrorAccessDenied)
			return
		}
	}
	stubStore.remove_UserData(record.id)
	delete(stubStore.usersByID, record.id)
	delete(stubStore.usersByLogin, strings.ToLower(record.login))
	remaining := stubStore.users[:0]
	for _, item := range stubStore.users {
		if item.id != record.id {
			remaining = append(remaining, item)
		}
	}
	stubStore.users = remaining
	stubStore.append_PermissionEvent(actor.id, texts.PermissionActionDeleteUser, "", record.id)
	responseWriter.WriteHeader(http.StatusNoContent)
}

// Всё, что принадлежит только этому пользователю. Общие документы и журнал вопросов остаются
func (stubStore *stub) remove_UserData(userID string) {
	for token, ownerID := range stubStore.tokens {
		if ownerID == userID {
			delete(stubStore.tokens, token)
		}
	}
	for sessionID, session := range stubStore.sessions {
		if session.ownerID == userID {
			delete(stubStore.sessions, sessionID)
		}
	}
	for draftID, file := range stubStore.draftsByID {
		if file.ownerID == userID {
			delete(stubStore.draftsByID, draftID)
		}
	}
	documents := stubStore.documents[:0]
	for _, document := range stubStore.documents {
		if document.scope == texts.ScopePersonal && document.ownerID == userID {
			delete(stubStore.documentsByID, document.id)
			continue
		}
		documents = append(documents, document)
	}
	stubStore.documents = documents
}

func (stubStore *stub) serve_Audit(responseWriter http.ResponseWriter, request *http.Request) {
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	if _, found := stubStore.require_Permission(responseWriter, request, texts.PermissionAuditView); !found {
		return
	}
	entries := stubStore.audit
	if entries == nil {
		entries = []auditEntry{}
	}
	write_JSONResponse(responseWriter, http.StatusOK, struct {
		Entries []auditEntry `json:"entries"`
	}{Entries: entries})
}

func (stubStore *stub) serve_DraftCheck(responseWriter http.ResponseWriter, request *http.Request) {
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
	recommendations, err := recommendations_ForDraft(fileName, body, draftcheck.Source{
		DocumentID: texts.SeedInstructionID,
		Title:      texts.SeedInstructionTitle,
		Edition:    texts.EditionDefault,
		Scope:      texts.ScopeGlobal,
	}, stubStore.layoutFormat)
	if err != nil {
		write_DraftError(responseWriter, err)
		return
	}
	recommendations.Requisites = requisites_ForDraft(fileName, body, stubStore.organization.officials_Snapshot())
	write_JSONResponse(responseWriter, http.StatusOK, recommendations)
}

func recommendations_ForDraft(fileName string, body []byte, source draftcheck.Source, format layout.Format) (draftCheckResponse, error) {
	plain, err := chunk.Extract_DocumentText(fileName, body)
	if err != nil {
		return draftCheckResponse{}, err
	}
	items, err := draftcheck.Check_Draft(plain, source)
	if err != nil {
		return draftCheckResponse{}, err
	}
	// Сначала оформление файла по правилам Инструкции и ГОСТ, затем рекомендации по тексту
	recommendations, formatChecked := format_Recommendations(fileName, body, source, format)
	for _, item := range items {
		// У .docx подпись ищет проверка оформления по ФИО в конце документа, поиск слова «подпись» в тексте не нужен
		if formatChecked && item.Text == texts.DraftSignatureMissing {
			continue
		}
		link := ""
		if item.DocumentID != "" && item.Paragraph != "" {
			link = build_OriginalFileLink(item.DocumentID, item.Paragraph)
		}
		recommendations = append(recommendations, draftRecommendation{
			Text: item.Text,
			Citation: citation{
				Document:     item.Title,
				Edition:      item.Edition,
				Section:      item.Section,
				Paragraph:    item.Paragraph,
				Appendix:     item.Appendix,
				Quote:        item.Quote,
				OriginalLink: link,
				DocumentID:   item.DocumentID,
				Scope:        item.Scope,
			},
		})
	}
	return draftCheckResponse{Recommendations: recommendations, FormatChecked: formatChecked}, nil
}

func write_DraftError(responseWriter http.ResponseWriter, err error) {
	if errors.Is(err, chunk.ErrUnsupportedFile) {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	write_ErrorResponse(responseWriter, http.StatusInternalServerError, texts.ErrorStore)
}

type draftRecommendation struct {
	Text     string   `json:"text"`
	Citation citation `json:"citation"`
	// Замечание по оформлению: error - нарушено правило, remark - стоит проверить. Пусто - рекомендация по тексту
	Level string `json:"level,omitempty"`
}

func (stubStore *stub) find_OwnedSession(responseWriter http.ResponseWriter, request *http.Request) (*sessionRecord, bool) {
	record, found := stubStore.find_UserByBearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return nil, false
	}
	session := stubStore.sessions[request.PathValue("sessionId")]
	if session == nil {
		write_ErrorResponse(responseWriter, http.StatusNotFound, texts.ErrorNotFound)
		return nil, false
	}
	if session.ownerID != record.id {
		write_ErrorResponse(responseWriter, http.StatusForbidden, texts.ErrorAccessDenied)
		return nil, false
	}
	return session, true
}

func (stubStore *stub) require_AdminUser(responseWriter http.ResponseWriter, request *http.Request) (*userRecord, bool) {
	record, found := stubStore.find_UserByBearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return nil, false
	}
	if record.pending {
		write_ErrorResponse(responseWriter, http.StatusForbidden, texts.ErrorAccountPending)
		return nil, false
	}
	if !auth.Has_Permission(stubStore.permissions_Of(record), texts.PermissionUsersManage) {
		write_ErrorResponse(responseWriter, http.StatusForbidden, texts.ErrorAccessDenied)
		return nil, false
	}
	return record, true
}

func (stubStore *stub) find_UserByBearerToken(request *http.Request) (*userRecord, bool) {
	header := request.Header.Get("Authorization")
	token, found := strings.CutPrefix(header, "Bearer ")
	if !found || token == "" || token != strings.TrimSpace(token) {
		return nil, false
	}
	userID := stubStore.tokens[token]
	record := stubStore.usersByID[userID]
	if record == nil {
		return nil, false
	}
	return record, true
}

func (stubStore *stub) append_AuditEntry(record *userRecord, question string) {
	stubStore.audit = append(stubStore.audit, auditEntry{
		EntryID:     "audit-" + strconv.Itoa(stubStore.nextAudit),
		UserID:      record.id,
		DisplayName: record.displayName,
		Question:    question,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
		Documents:   []shownDocument{},
	})
	stubStore.nextAudit++
}

func (record *userRecord) account() accountResponse {
	return accountResponse{
		UserID:      record.id,
		Login:       record.login,
		DisplayName: record.displayName,
		AccessLevel: record.accessLevel,
		Role:        record.role(),
		RoleIDs:     copy_Strings(record.roleIDs),
		CreatedAt:   record.createdAt.UTC().Format(time.RFC3339),
	}
}

func (record *userRecord) role() string {
	return auth.Role_ForAccess(record.accessLevel, record.pending)
}

func (session *sessionRecord) line() sessionLine {
	return sessionLine{
		SessionID: session.id,
		Title:     session.title,
		CreatedAt: session.createdAt.UTC().Format(time.RFC3339),
		UpdatedAt: session.updatedAt.UTC().Format(time.RFC3339),
		CanRename: true,
		CanDelete: true,
	}
}

// Заголовок чата берётся из первого вопроса и дальше не меняется.
func build_SessionTitle(text string) string {
	collapsed := strings.Join(strings.Fields(text), " ")
	runes := []rune(collapsed)
	const titleRuneLimit = 60
	if len(runes) <= titleRuneLimit {
		return collapsed
	}
	return strings.TrimSpace(string(runes[:titleRuneLimit-3])) + "..."
}

func (document *documentRecord) line(viewer *userRecord, ownerName string, permissions []string) documentResponse {
	return documentResponse{
		DocumentID:       document.id,
		Title:            document.title,
		Edition:          document.edition,
		AccessLevel:      document.accessLevel,
		Scope:            document.scope,
		ProcessID:        document.processID,
		OwnerID:          document.ownerID,
		OwnerName:        ownerName,
		UploadedAt:       document.uploadedAt.UTC().Format(time.RFC3339),
		VisibleToAll:     document.visibleToAll,
		RoleIDs:          copy_Strings(document.roleIDs),
		CanDelete:        check_CanDeleteDocument(viewer.id, permissions, document.scope, document.ownerID) && !document.standard,
		Standard:         document.standard,
		ArchivedEditions: len(document.editions),
	}
}

func (stubStore *stub) lookup_Contacts(record *userRecord, question string) []replyContact {
	if record == nil {
		return []replyContact{}
	}
	entries := []contacts.Entry{}
	for _, document := range stubStore.documents {
		if document == nil || !contacts.Title_IsDirectory(document.title) {
			continue
		}
		if document_ReadStatus(record.id, stubStore.permissions_Of(record), record.roleIDs, document.scope, document.ownerID, document.visibleToAll, document.roleIDs) != http.StatusOK {
			continue
		}
		plain, extractError := chunk.Extract_DocumentText(document.fileName, document.body)
		if extractError != nil {
			continue
		}
		entries = append(entries, contacts.Parse_Entries(plain)...)
	}
	return contacts_FromEntries(entries, question)
}

// Пока бланк заполняется, реплика - ответ на поле, а не новая фраза заглушки.
func build_AssistantReply(session *sessionRecord, text string, allowBlank bool) (assistantReply, blank.Step, error) {
	if allowBlank {
		step, handled := blank.Advance_Blank(session.blankFill, text)
		if handled {
			apply_BlankStep(session, step)
			return reply_FromBlank(step), step, nil
		}
	}
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, texts.PhraseAccessDenied):
		return build_PlainReply(texts.KindAccessDenied, texts.AccessDeniedText), blank.Step{}, nil
	case strings.Contains(lower, texts.PhraseNotFound):
		return build_PlainReply(texts.KindFallback, texts.FallbackText), blank.Step{}, nil
	case strings.Contains(lower, texts.PhraseClarify):
		reply := build_PlainReply(texts.KindClarify, texts.ClarifyText)
		reply.Options = []string{texts.SeedInstructionTitle, texts.SeedRestrictedTitle}
		return reply, blank.Step{}, nil
	default:
		return build_PlainReply(texts.KindFallback, texts.FallbackText), blank.Step{}, nil
	}
}

// Название модуля из свойств файла, иначе заголовок из текста
func blank_Title(step blank.Step) string {
	if step.Name != "" {
		return step.Name
	}
	return step.Title
}

func apply_BlankStep(session *sessionRecord, step blank.Step) {
	session.blankFill = step.State
	session.templateStep = templateStepIdle
	if step.State.BlankID != "" {
		session.templateStep = templateStepAsked
	}
}

func reply_FromBlank(step blank.Step) assistantReply {
	if step.Ready {
		reply := build_PlainReply(texts.KindTemplateReady, texts.TemplateReadyText)
		reply.Template = &templateState{
			ID:        step.BlankID,
			Title:     blank_Title(step),
			DraftText: step.DraftText,
			FileLink:  blank_FileLink(step.BlankID),
		}
		return reply
	}
	reply := build_PlainReply(texts.KindTemplateField, step.Question)
	reply.Template = &templateState{
		ID:         step.BlankID,
		Title:      blank_Title(step),
		Field:      step.Field,
		FieldLabel: step.FieldLabel,
		FileLink:   blank_FileLink(step.BlankID),
		Questions:  questions_FromBlank(step.Questions),
	}
	return reply
}

func (stubStore *stub) save_DraftFile(ownerID, fileName string, body []byte) string {
	draftID := "draft-" + strconv.Itoa(stubStore.nextDraft)
	stubStore.nextDraft++
	stubStore.draftsByID[draftID] = &draftFile{
		ownerID:     ownerID,
		fileName:    fileName,
		contentType: texts.DocxContentType,
		body:        body,
	}
	return draftID
}

func (stubStore *stub) serve_BlankFile(responseWriter http.ResponseWriter, request *http.Request) {
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	record, found := stubStore.find_UserByBearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	if !auth.Has_Permission(stubStore.permissions_Of(record), texts.PermissionTemplatesUse) {
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

func (stubStore *stub) serve_DraftFile(responseWriter http.ResponseWriter, request *http.Request) {
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	record, found := stubStore.find_UserByBearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	file := stubStore.draftsByID[request.PathValue("draftId")]
	if file == nil {
		write_ErrorResponse(responseWriter, http.StatusNotFound, texts.ErrorNotFound)
		return
	}
	if file.ownerID != record.id {
		write_ErrorResponse(responseWriter, http.StatusForbidden, texts.ErrorAccessDenied)
		return
	}
	write_FileResponse(responseWriter, file.fileName, file.contentType, file.body)
}

func encode_FillState(state blank.FillState) []byte {
	if state.BlankID == "" && len(state.Answers) == 0 && len(state.Skipped) == 0 {
		return []byte("{}")
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return []byte("{}")
	}
	return raw
}

func decode_FillState(raw []byte) blank.FillState {
	var state blank.FillState
	if len(raw) == 0 {
		return state
	}
	_ = json.Unmarshal(raw, &state)
	return state
}

func build_PlainReply(kind, text string) assistantReply {
	return assistantReply{
		Kind:             kind,
		Text:             text,
		RelevancePercent: 0,
		Citations:        []citation{},
	}
}

func build_OriginalFileLink(documentID, paragraph string) string {
	return "/documents/" + documentID + "/file#p" + paragraph
}

func match_Password(stored, given string) bool {
	if len(stored) != len(given) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(stored), []byte(given)) == 1
}

func create_RandomToken() (string, error) {
	randomBytes := make([]byte, 16)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(randomBytes), nil
}

func read_UploadedFile(responseWriter http.ResponseWriter, request *http.Request) (string, []byte, string, error) {
	request.Body = http.MaxBytesReader(responseWriter, request.Body, maxUploadBytes)
	if err := request.ParseMultipartForm(maxUploadBytes); err != nil {
		return "", nil, "", err
	}
	file, header, err := request.FormFile("file")
	if err != nil {
		return "", nil, "", err
	}
	defer file.Close()
	body, err := io.ReadAll(file)
	if err != nil {
		return "", nil, "", err
	}
	// Тип файла задает ядро по расширению, а не браузер: присланный text/html или image/svg+xml
	// при скачивании мог бы открыться как страница с нашего адреса
	return header.Filename, body, contentType_ForFile(header.Filename), nil
}

func contentType_ForFile(fileName string) string {
	switch strings.ToLower(filepath.Ext(fileName)) {
	case ".txt":
		return texts.PlainTextContentType
	case ".docx":
		return texts.DocxContentType
	case ".pdf":
		return "application/pdf"
	default:
		return texts.PlainTextContentType
	}
}

func build_AttachmentHeader(name string) string {
	name = filepath.Base(name)
	name = strings.Map(func(symbol rune) rune {
		if symbol == '"' || symbol == '\n' || symbol == '\r' {
			return -1
		}
		return symbol
	}, name)
	if name == "" || name == "." {
		name = "document"
	}
	// Кириллица в имени - через filename* (RFC 6266), в filename - запасное имя латиницей для старых клиентов
	fallback := strings.Map(func(symbol rune) rune {
		if symbol < 0x20 || symbol > 0x7e || symbol == '\\' {
			return '_'
		}
		return symbol
	}, name)
	if fallback == name {
		return `attachment; filename="` + name + `"`
	}
	return `attachment; filename="` + fallback + `"; filename*=UTF-8''` + url.PathEscape(name)
}
