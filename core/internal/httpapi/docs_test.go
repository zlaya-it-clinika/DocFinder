package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"docfinder/core/internal/config"
	"docfinder/core/internal/texts"
)

func TestDocsRequiresPasswordFromConfig(t *testing.T) {
	handler := must_Handler(t, config.Config{Mode: config.ModeStub, DocsPassword: "secret"})

	locked := doRaw(t, handler, http.MethodGet, "/docs", "", nil, "")
	if locked.Code != http.StatusOK {
		t.Fatalf("locked status %d", locked.Code)
	}
	if strings.Contains(locked.Body.String(), "api-token") {
		t.Fatal("docs console is visible before login")
	}
	if !strings.Contains(locked.Body.String(), "DOCS_PASSWORD") {
		t.Fatal("login page does not mention the env password")
	}

	wrong := postDocsPassword(t, handler, "nope")
	if wrong.Code != http.StatusUnauthorized || !strings.Contains(wrong.Body.String(), texts.DocsWrongPassword) {
		t.Fatalf("wrong password %d %s", wrong.Code, wrong.Body.String())
	}

	opened := postDocsPassword(t, handler, "secret")
	if opened.Code != http.StatusSeeOther {
		t.Fatalf("login status %d", opened.Code)
	}
	cookies := opened.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != docsCookieName || cookies[0].Value == "" {
		t.Fatalf("cookie %+v", cookies)
	}

	page := serveDocsWithCookie(t, handler, cookies[0].Name, cookies[0].Value)
	body := page.Body.String()
	if page.Code != http.StatusOK || !strings.Contains(body, "api-token") || !strings.Contains(body, "/audit") {
		t.Fatalf("console %d %s", page.Code, body)
	}
	for _, path := range []string{
		"/auth/logout",
		`GET</span>/sessions</h2>`,
		"DELETE",
		"/blanks/order-work-group/file",
		"/drafts/draft-1/file",
		"/drafts/check",
		"/drafts/fix",
		"/sessions/session-1/attachments",
		`name="scope"`,
		"/processes",
		"/documents/import",
		"/sources",
		"/sources/kinds",
		"/sources/{sourceId}/sync",
		`name="process"`,
		"/permissions",
		"/roles",
		"/audit/permissions",
		`name="roleIds"`,
		`PATCH</span>/sessions/{sessionId}`,
		`DELETE</span>/sessions/{sessionId}`,
		"/auth/kerberos",
		"ldapGroup",
	} {
		if !strings.Contains(body, path) {
			t.Fatalf("console missing %s", path)
		}
	}

	api := doJSON(t, handler, http.MethodPost, "/auth/login", "", map[string]string{
		"login":    texts.SeedEmployeeLogin,
		"password": texts.SeedEmployeePassword,
	})
	if api.Code != http.StatusOK {
		t.Fatalf("api login behind docs gate %d", api.Code)
	}
}

func TestDocsStaysClosedWithoutConfiguredPassword(t *testing.T) {
	handler := must_Handler(t, config.Config{Mode: config.ModeStub})
	page := doRaw(t, handler, http.MethodGet, "/docs", "", nil, "")
	if !strings.Contains(page.Body.String(), texts.DocsPasswordNotSet) || strings.Contains(page.Body.String(), "api-token") {
		t.Fatalf("unset password page %s", page.Body.String())
	}
	login := postDocsPassword(t, handler, "")
	if login.Code != http.StatusServiceUnavailable {
		t.Fatalf("empty password status %d", login.Code)
	}
}

func TestDocsPageNeedsDatabaseInLiveMode(t *testing.T) {
	_, err := NewHandler(config.Config{Mode: config.ModeLive, DocsPassword: "secret"})
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("live docs startup %v", err)
	}
}

func TestDocsLoginIsNotWrittenToDebugLog(t *testing.T) {
	var logs bytes.Buffer
	original := logWriter()
	setLogWriter(&logs)
	defer setLogWriter(original)

	handler := must_Handler(t, config.Config{Mode: config.ModeStub, Debug: true, DocsPassword: "top-secret-docs"})
	response := postDocsPassword(t, handler, "top-secret-docs")
	if response.Code != http.StatusSeeOther {
		t.Fatalf("login %d", response.Code)
	}
	if strings.Contains(logs.String(), "top-secret-docs") {
		t.Fatalf("password leaked: %s", logs.String())
	}
}

func postDocsPassword(t *testing.T, handler http.Handler, password string) *httptest.ResponseRecorder {
	t.Helper()
	body := url.Values{}
	body.Set("password", password)
	return doRaw(t, handler, http.MethodPost, "/docs/login", "", []byte(body.Encode()), "application/x-www-form-urlencoded")
}

func serveDocsWithCookie(t *testing.T, handler http.Handler, name, value string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/docs", nil)
	request.AddCookie(&http.Cookie{Name: name, Value: value})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
