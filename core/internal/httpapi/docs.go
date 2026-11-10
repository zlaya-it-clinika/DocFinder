package httpapi

import (
	"embed"
	"html/template"
	"net/http"
	"sync"

	"docfinder/core/internal/texts"
)

//go:embed docs.html
var docsFiles embed.FS

var docsTemplates = template.Must(template.ParseFS(docsFiles, "docs.html"))

const docsCookieName = "docs_session"

type docsGate struct {
	password string
	mu       sync.Mutex
	sessions map[string]struct{}
}

type docsLoginView struct {
	Title         string
	Heading       string
	Lead          string
	PasswordLabel string
	Submit        string
	MissingHint   string
	Error         string
	PasswordSet   bool
}

type docsConsoleView struct {
	Title                    string
	Lead                     string
	Logout                   string
	TokenLabel               string
	TokenPlaceholder         string
	Send                     string
	FilePrefix               string
	FileSuffix               string
	HintLogin                string
	HintLogout               string
	HintMe                   string
	HintPermissions          string
	HintRoles                string
	HintCreateRole           string
	HintPatchRole            string
	HintDeleteRole           string
	HintPermissionAudit      string
	HintCreateSession        string
	HintListSessions         string
	HintRenameSession        string
	HintDeleteSession        string
	HintHistory              string
	HintMessage              string
	HintAttach               string
	HintListDocuments        string
	HintUpload               string
	HintFile                 string
	HintDeleteDocument       string
	HintEditions             string
	HintDraftFix             string
	HintEditionFile          string
	HintBlankFile            string
	HintDraftFile            string
	HintProcesses            string
	HintCreateProcess        string
	HintImport               string
	HintUsers                string
	HintCreateUser           string
	HintPatchUser            string
	HintDeleteUser           string
	HintAudit                string
	HintDraft                string
	HintAuthConfig           string
	HintSignup               string
	HintLdapLogin            string
	HintKerberosLogin        string
	HintAdminDetails         string
	HintSignupConfig         string
	HintSaveSignup           string
	HintLdapToggle           string
	HintSaveLdapToggle       string
	HintLdapServer           string
	HintSaveLdapServer       string
	HintSources              string
	HintSourceKinds          string
	HintCreateSource         string
	HintPatchSource          string
	HintDeleteSource         string
	HintVerifySource         string
	HintSyncSource           string
	HintSourceRuns           string
	ExampleLoginBody         string
	ExampleSignupBody        string
	ExampleLdapLoginBody     string
	ExampleSignupConfig      string
	ExampleLdapToggle        string
	ExampleLdapServer        string
	ExampleMessageBody       string
	ExampleRenameSessionBody string
	ExampleCreateProcessBody string
	ExampleCreateUserBody    string
	ExamplePatchUserBody     string
	ExampleCreateRoleBody    string
	ExamplePatchRoleBody     string
	ExampleCreateSourceBody  string
}

func create_DocsGate(password string) *docsGate {
	return &docsGate{
		password: password,
		sessions: map[string]struct{}{},
	}
}

func register_DocsRoutes(routeMux *http.ServeMux, docsAccess *docsGate) {
	routeMux.HandleFunc("GET /docs", docsAccess.serve_DocsPage)
	routeMux.HandleFunc("POST /docs/login", docsAccess.serve_DocsLogin)
	routeMux.HandleFunc("POST /docs/logout", docsAccess.serve_DocsLogout)
}

func (docsAccess *docsGate) serve_DocsPage(responseWriter http.ResponseWriter, request *http.Request) {
	if !docsAccess.has_DocsSession(request) {
		render_DocsTemplate(responseWriter, "login", loginView(docsAccess.text_WhenDocsPasswordMissing(), docsAccess.password != ""), http.StatusOK)
		return
	}
	render_DocsTemplate(responseWriter, "console", consoleView(), http.StatusOK)
}

func (docsAccess *docsGate) serve_DocsLogin(responseWriter http.ResponseWriter, request *http.Request) {
	if parseError := request.ParseForm(); parseError != nil {
		render_DocsTemplate(responseWriter, "login", loginView(texts.DocsBadForm, docsAccess.password != ""), http.StatusBadRequest)
		return
	}
	if docsAccess.password == "" {
		render_DocsTemplate(responseWriter, "login", loginView(texts.DocsPasswordNotSet, false), http.StatusServiceUnavailable)
		return
	}
	if !match_Password(docsAccess.password, request.FormValue("password")) {
		render_DocsTemplate(responseWriter, "login", loginView(texts.DocsWrongPassword, true), http.StatusUnauthorized)
		return
	}
	sessionToken, tokenError := create_RandomToken()
	if tokenError != nil {
		render_DocsTemplate(responseWriter, "login", loginView(texts.DocsBadForm, true), http.StatusInternalServerError)
		return
	}
	docsAccess.mu.Lock()
	docsAccess.sessions[sessionToken] = struct{}{}
	docsAccess.mu.Unlock()
	http.SetCookie(responseWriter, &http.Cookie{
		Name:     docsCookieName,
		Value:    sessionToken,
		Path:     "/docs",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(responseWriter, request, "/docs", http.StatusSeeOther)
}

func (docsAccess *docsGate) serve_DocsLogout(responseWriter http.ResponseWriter, request *http.Request) {
	if cookie, cookieError := request.Cookie(docsCookieName); cookieError == nil {
		docsAccess.mu.Lock()
		delete(docsAccess.sessions, cookie.Value)
		docsAccess.mu.Unlock()
	}
	http.SetCookie(responseWriter, &http.Cookie{
		Name:     docsCookieName,
		Value:    "",
		Path:     "/docs",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(responseWriter, request, "/docs", http.StatusSeeOther)
}

func (docsAccess *docsGate) has_DocsSession(request *http.Request) bool {
	cookie, cookieError := request.Cookie(docsCookieName)
	if cookieError != nil || cookie.Value == "" {
		return false
	}
	docsAccess.mu.Lock()
	defer docsAccess.mu.Unlock()
	_, sessionFound := docsAccess.sessions[cookie.Value]
	return sessionFound
}

// Пустой DOCS_PASSWORD не должен открывать страницу: сравнение двух пустых строк дало бы совпадение.
func (docsAccess *docsGate) text_WhenDocsPasswordMissing() string {
	if docsAccess.password == "" {
		return texts.DocsPasswordNotSet
	}
	return ""
}

func loginView(message string, passwordSet bool) docsLoginView {
	return docsLoginView{
		Title:         texts.DocsLoginTitle,
		Heading:       texts.DocsHeading,
		Lead:          texts.DocsLoginLead,
		PasswordLabel: texts.DocsPasswordLabel,
		Submit:        texts.DocsLoginButton,
		MissingHint:   texts.DocsMissingHint,
		Error:         message,
		PasswordSet:   passwordSet,
	}
}

func consoleView() docsConsoleView {
	return docsConsoleView{
		Title:                    texts.DocsConsoleTitle,
		Lead:                     texts.DocsConsoleLead,
		Logout:                   texts.DocsLogoutButton,
		TokenLabel:               texts.DocsTokenLabel,
		TokenPlaceholder:         texts.DocsTokenPlaceholder,
		Send:                     texts.DocsSendButton,
		FilePrefix:               texts.DocsFilePrefix,
		FileSuffix:               texts.DocsFileSuffix,
		HintLogin:                texts.DocsHintLogin,
		HintLogout:               texts.DocsHintLogout,
		HintMe:                   texts.DocsHintMe,
		HintPermissions:          texts.DocsHintPermissions,
		HintRoles:                texts.DocsHintRoles,
		HintCreateRole:           texts.DocsHintCreateRole,
		HintPatchRole:            texts.DocsHintPatchRole,
		HintDeleteRole:           texts.DocsHintDeleteRole,
		HintPermissionAudit:      texts.DocsHintPermissionAudit,
		HintCreateSession:        texts.DocsHintCreateSession,
		HintListSessions:         texts.DocsHintListSessions,
		HintRenameSession:        texts.DocsHintRenameSession,
		HintDeleteSession:        texts.DocsHintDeleteSession,
		HintHistory:              texts.DocsHintHistory,
		HintMessage:              texts.DocsHintMessage,
		HintAttach:               texts.DocsHintAttach,
		HintListDocuments:        texts.DocsHintListDocuments,
		HintUpload:               texts.DocsHintUpload,
		HintFile:                 texts.DocsHintFile,
		HintDeleteDocument:       texts.DocsHintDeleteDocument,
		HintEditions:             texts.DocsHintEditions,
		HintDraftFix:             texts.DocsHintDraftFix,
		HintEditionFile:          texts.DocsHintEditionFile,
		HintBlankFile:            texts.DocsHintBlankFile,
		HintDraftFile:            texts.DocsHintDraftFile,
		HintProcesses:            texts.DocsHintProcesses,
		HintCreateProcess:        texts.DocsHintCreateProcess,
		HintImport:               texts.DocsHintImport,
		HintUsers:                texts.DocsHintUsers,
		HintCreateUser:           texts.DocsHintCreateUser,
		HintPatchUser:            texts.DocsHintPatchUser,
		HintDeleteUser:           texts.DocsHintDeleteUser,
		HintAudit:                texts.DocsHintAudit,
		HintDraft:                texts.DocsHintDraft,
		HintAuthConfig:           texts.DocsHintAuthConfig,
		HintSignup:               texts.DocsHintSignup,
		HintLdapLogin:            texts.DocsHintLdapLogin,
		HintKerberosLogin:        texts.DocsHintKerberosLogin,
		HintAdminDetails:         texts.DocsHintAdminDetails,
		HintSignupConfig:         texts.DocsHintSignupConfig,
		HintSaveSignup:           texts.DocsHintSaveSignup,
		HintLdapToggle:           texts.DocsHintLdapToggle,
		HintSaveLdapToggle:       texts.DocsHintSaveLdapToggle,
		HintLdapServer:           texts.DocsHintLdapServer,
		HintSaveLdapServer:       texts.DocsHintSaveLdapServer,
		HintSources:              texts.DocsHintSources,
		HintSourceKinds:          texts.DocsHintSourceKinds,
		HintCreateSource:         texts.DocsHintCreateSource,
		HintPatchSource:          texts.DocsHintPatchSource,
		HintDeleteSource:         texts.DocsHintDeleteSource,
		HintVerifySource:         texts.DocsHintVerifySource,
		HintSyncSource:           texts.DocsHintSyncSource,
		HintSourceRuns:           texts.DocsHintSourceRuns,
		ExampleLoginBody:         texts.DocsExampleLoginBody,
		ExampleSignupBody:        texts.DocsExampleSignupBody,
		ExampleLdapLoginBody:     texts.DocsExampleLdapLoginBody,
		ExampleSignupConfig:      texts.DocsExampleSignupConfigBody,
		ExampleLdapToggle:        texts.DocsExampleLdapToggleBody,
		ExampleLdapServer:        texts.DocsExampleLdapServerBody,
		ExampleMessageBody:       texts.DocsExampleMessageBody,
		ExampleRenameSessionBody: texts.DocsExampleRenameSessionBody,
		ExampleCreateProcessBody: texts.DocsExampleCreateProcessBody,
		ExampleCreateUserBody:    texts.DocsExampleCreateUserBody,
		ExamplePatchUserBody:     texts.DocsExamplePatchUserBody,
		ExampleCreateRoleBody:    texts.DocsExampleCreateRoleBody,
		ExamplePatchRoleBody:     texts.DocsExamplePatchRoleBody,
		ExampleCreateSourceBody:  texts.DocsExampleCreateSourceBody,
	}
}

func render_DocsTemplate(responseWriter http.ResponseWriter, templateName string, data any, status int) {
	responseWriter.Header().Set("Content-Type", "text/html; charset=utf-8")
	responseWriter.Header().Set("Cache-Control", "no-store")
	responseWriter.Header().Set("X-Content-Type-Options", "nosniff")
	responseWriter.WriteHeader(status)
	_ = docsTemplates.ExecuteTemplate(responseWriter, templateName, data)
}
