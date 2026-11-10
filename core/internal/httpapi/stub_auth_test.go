package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jcmturner/gokrb5/v8/keytab"

	"docfinder/core/internal/auth"
	"docfinder/core/internal/config"
	"docfinder/core/internal/texts"
)

func empty_StubConfig(defaultRole string) config.Config {
	return config.Config{
		Mode:             config.ModeStub,
		StubWithoutUsers: true,
		AuthDefaults: auth.Settings{
			Signup: auth.SignupSettings{EnableSignup: true, DefaultUserRole: defaultRole, ShowAdminDetails: true},
			Ldap:   auth.LdapSettings{Label: texts.DefaultLdapLabel, AttributeForMail: "mail", AttributeForUsername: "uid", ValidateCert: true, Ciphers: "ALL"},
		},
	}
}

func signup(t *testing.T, handler http.Handler, email, displayName string) loginResponse {
	t.Helper()
	response := doJSON(t, handler, http.MethodPost, "/auth/signup", "", map[string]string{
		"email":       email,
		"password":    "secret-password",
		"displayName": displayName,
	})
	if response.Code != http.StatusOK {
		t.Fatalf("signup %s status %d body %s", email, response.Code, response.Body.String())
	}
	return decodeJSON[loginResponse](t, response)
}

// Первый запуск как в open-webui: пустая система, первая регистрация - администратор,
// следующие получают роль по умолчанию и ждут подтверждения
func TestStubFirstSignupBecomesAdminAndNextWaitsForApproval(t *testing.T) {
	handler := must_Handler(t, empty_StubConfig(texts.RolePending))

	publicConfig := decodeJSON[publicAuthConfigResponse](t, doJSON(t, handler, http.MethodGet, "/auth/config", "", nil))
	if !publicConfig.Onboarding || !publicConfig.EnableSignup || publicConfig.EnableLdap {
		t.Fatalf("config before first user %+v", publicConfig)
	}

	admin := signup(t, handler, " Boss@Corp.Local ", "Главный")
	if admin.Role != texts.RoleAdmin || admin.Login != "boss@corp.local" || admin.Token == "" {
		t.Fatalf("first account %+v", admin)
	}
	publicConfig = decodeJSON[publicAuthConfigResponse](t, doJSON(t, handler, http.MethodGet, "/auth/config", "", nil))
	if publicConfig.Onboarding {
		t.Fatal("onboarding after first user")
	}

	taken := doJSON(t, handler, http.MethodPost, "/auth/signup", "", map[string]string{"email": "BOSS@corp.local", "password": "x", "displayName": "Двойник"})
	if taken.Code != http.StatusConflict {
		t.Fatalf("same email status %d", taken.Code)
	}
	badEmail := doJSON(t, handler, http.MethodPost, "/auth/signup", "", map[string]string{"email": "no-at-sign", "password": "x", "displayName": "Имя"})
	if badEmail.Code != http.StatusBadRequest || !strings.Contains(badEmail.Body.String(), texts.ErrorInvalidEmail) {
		t.Fatalf("bad email %d %s", badEmail.Code, badEmail.Body.String())
	}

	employee := signup(t, handler, "clerk@corp.local", "Делопроизводитель")
	if employee.Role != texts.RolePending {
		t.Fatalf("second account role %s", employee.Role)
	}
	me := decodeJSON[profileResponse](t, doJSON(t, handler, http.MethodGet, "/me", employee.Token, nil))
	if me.Role != texts.RolePending || me.Login != "clerk@corp.local" {
		t.Fatalf("pending profile %+v", me)
	}
	blocked := doJSON(t, handler, http.MethodPost, "/sessions", employee.Token, nil)
	if blocked.Code != http.StatusForbidden || !strings.Contains(blocked.Body.String(), texts.ErrorAccountPending) {
		t.Fatalf("pending session %d %s", blocked.Code, blocked.Body.String())
	}
	details := doJSON(t, handler, http.MethodGet, "/auth/admin/details", employee.Token, nil)
	contact := decodeJSON[adminDetailsResponse](t, details)
	if details.Code != http.StatusOK || contact.Email != "boss@corp.local" || contact.Name != "Главный" {
		t.Fatalf("admin details %d %+v", details.Code, contact)
	}
	if code := doJSON(t, handler, http.MethodGet, "/auth/admin/config", employee.Token, nil).Code; code != http.StatusForbidden {
		t.Fatalf("pending reads admin config %d", code)
	}

	approved := doJSON(t, handler, http.MethodPatch, "/users/"+employee.UserID, admin.Token, map[string]string{"role": texts.RoleUser})
	if approved.Code != http.StatusOK || decodeJSON[accountResponse](t, approved).Role != texts.RoleUser {
		t.Fatalf("approve %d %s", approved.Code, approved.Body.String())
	}
	if code := doJSON(t, handler, http.MethodPost, "/sessions", employee.Token, nil).Code; code != http.StatusOK {
		t.Fatalf("approved session %d", code)
	}
	relogin := login(t, handler, "CLERK@corp.local", "secret-password")
	if relogin == "" {
		t.Fatal("email login is case sensitive")
	}
}

func TestStubSignupSettingsAndSelfRoleGuard(t *testing.T) {
	handler := must_Handler(t, empty_StubConfig(texts.RoleUser))
	admin := signup(t, handler, "boss@corp.local", "Главный")

	invalid := doJSON(t, handler, http.MethodPost, "/auth/admin/config", admin.Token, map[string]string{"defaultUserRole": "owner"})
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("unknown role %d", invalid.Code)
	}
	saved := doJSON(t, handler, http.MethodPost, "/auth/admin/config", admin.Token, map[string]any{"enableSignup": false})
	settings := decodeJSON[auth.SignupSettings](t, saved)
	if saved.Code != http.StatusOK || settings.EnableSignup || settings.DefaultUserRole != texts.RoleUser || !settings.ShowAdminDetails {
		t.Fatalf("partial save %d %+v", saved.Code, settings)
	}
	closed := doJSON(t, handler, http.MethodPost, "/auth/signup", "", map[string]string{"email": "late@corp.local", "password": "x", "displayName": "Поздний"})
	if closed.Code != http.StatusForbidden || !strings.Contains(closed.Body.String(), texts.ErrorSignupDisabled) {
		t.Fatalf("signup while disabled %d %s", closed.Code, closed.Body.String())
	}
	publicConfig := decodeJSON[publicAuthConfigResponse](t, doJSON(t, handler, http.MethodGet, "/auth/config", "", nil))
	if publicConfig.EnableSignup {
		t.Fatal("public config still allows signup")
	}

	selfDemote := doJSON(t, handler, http.MethodPatch, "/users/"+admin.UserID, admin.Token, map[string]string{"role": texts.RolePending})
	if selfDemote.Code != http.StatusForbidden {
		t.Fatalf("self demote %d", selfDemote.Code)
	}
	selfRename := doJSON(t, handler, http.MethodPatch, "/users/"+admin.UserID, admin.Token, map[string]string{"displayName": "Новое имя", "role": texts.RoleAdmin})
	if selfRename.Code != http.StatusOK || decodeJSON[accountResponse](t, selfRename).DisplayName != "Новое имя" {
		t.Fatalf("self rename %d %s", selfRename.Code, selfRename.Body.String())
	}

	created := doJSON(t, handler, http.MethodPost, "/users", admin.Token, map[string]string{"email": "Clerk@Corp.Local", "password": "clerk", "displayName": "Клерк", "role": texts.RolePending})
	account := decodeJSON[accountResponse](t, created)
	if created.Code != http.StatusOK || account.Login != "clerk@corp.local" || account.Role != texts.RolePending || account.CreatedAt == "" {
		t.Fatalf("admin create %d %+v", created.Code, account)
	}
	moved := doJSON(t, handler, http.MethodPatch, "/users/"+account.UserID, admin.Token, map[string]string{"email": "archive@corp.local", "password": "new-pass", "role": texts.RoleAdmin})
	if moved.Code != http.StatusOK || decodeJSON[accountResponse](t, moved).Role != texts.RoleAdmin {
		t.Fatalf("patch email and password %d %s", moved.Code, moved.Body.String())
	}
	login(t, handler, "archive@corp.local", "new-pass")
	if code := doJSON(t, handler, http.MethodPost, "/auth/login", "", map[string]string{"login": "clerk@corp.local", "password": "clerk"}).Code; code != http.StatusUnauthorized {
		t.Fatalf("old email still works %d", code)
	}
}

func TestStubLdapSettingsHideServicePassword(t *testing.T) {
	handler := must_Handler(t, empty_StubConfig(texts.RolePending))
	admin := signup(t, handler, "boss@corp.local", "Главный")

	disabled := doJSON(t, handler, http.MethodPost, "/auth/ldap", "", map[string]string{"login": "ivanov", "password": "secret"})
	if disabled.Code != http.StatusForbidden || !strings.Contains(disabled.Body.String(), texts.ErrorLdapDisabled) {
		t.Fatalf("ldap while disabled %d %s", disabled.Code, disabled.Body.String())
	}

	server := map[string]any{
		"label":                "Active Directory",
		"host":                 "127.0.0.1",
		"port":                 1,
		"attributeForMail":     "mail",
		"attributeForUsername": "sAMAccountName",
		"appDn":                "CN=svc,DC=corp,DC=local",
		"appDnPassword":        "service-secret",
		"searchBase":           "DC=corp,DC=local",
		"useTls":               false,
		"validateCert":         true,
		"ciphers":              "ALL",
	}
	saved := doJSON(t, handler, http.MethodPost, "/auth/admin/config/ldap/server", admin.Token, server)
	if saved.Code != http.StatusOK || strings.Contains(saved.Body.String(), "service-secret") {
		t.Fatalf("save server %d %s", saved.Code, saved.Body.String())
	}
	if !decodeJSON[ldapServerResponse](t, saved).AppPasswordSet {
		t.Fatal("password flag is not set")
	}
	// Пустой пароль при повторном сохранении оставляет прежний
	server["appDnPassword"] = ""
	server["label"] = "Домен"
	resaved := doJSON(t, handler, http.MethodPost, "/auth/admin/config/ldap/server", admin.Token, server)
	if !decodeJSON[ldapServerResponse](t, resaved).AppPasswordSet {
		t.Fatal("empty password wiped the stored one")
	}
	delete(server, "searchBase")
	if code := doJSON(t, handler, http.MethodPost, "/auth/admin/config/ldap/server", admin.Token, server).Code; code != http.StatusBadRequest {
		t.Fatalf("missing search base %d", code)
	}

	toggled := doJSON(t, handler, http.MethodPost, "/auth/admin/config/ldap", admin.Token, map[string]bool{"enableLdap": true})
	if toggled.Code != http.StatusOK || !decodeJSON[ldapToggleResponse](t, toggled).EnableLdap {
		t.Fatalf("toggle %d %s", toggled.Code, toggled.Body.String())
	}
	publicConfig := decodeJSON[publicAuthConfigResponse](t, doJSON(t, handler, http.MethodGet, "/auth/config", "", nil))
	if !publicConfig.EnableLdap || publicConfig.LdapLabel != "Домен" {
		t.Fatalf("public ldap %+v", publicConfig)
	}
	emptyPassword := doJSON(t, handler, http.MethodPost, "/auth/ldap", "", map[string]string{"login": "ivanov", "password": ""})
	if emptyPassword.Code != http.StatusUnauthorized {
		t.Fatalf("empty ldap password %d", emptyPassword.Code)
	}
	unreachable := doJSON(t, handler, http.MethodPost, "/auth/ldap", "", map[string]string{"login": "ivanov", "password": "secret"})
	if unreachable.Code != http.StatusBadGateway || !strings.Contains(unreachable.Body.String(), texts.ErrorLdapUnavailable) {
		t.Fatalf("unreachable ldap %d %s", unreachable.Code, unreachable.Body.String())
	}
}

func TestStubLdapGroupAssignsRole(t *testing.T) {
	t.Cleanup(func() { ldapDirectory = nil })
	handler := must_Handler(t, config.Config{Mode: config.ModeStub})
	chiefToken := login(t, handler, texts.SeedAdminLogin, texts.SeedAdminPassword)
	created := decodeJSON[roleResponse](t, doJSON(t, handler, http.MethodPost, "/roles", chiefToken, map[string]any{
		"title": "Архив", "ldapGroup": "Архив", "permissions": []string{texts.PermissionChatUse},
	}))
	if created.LdapGroup != "Архив" {
		t.Fatalf("role %+v", created)
	}
	server := map[string]any{
		"label": "Active Directory", "host": "127.0.0.1", "attributeForMail": "mail",
		"attributeForUsername": "sAMAccountName", "searchBase": "DC=corp,DC=local",
		"ciphers": "ALL", "enableGroupManagement": true, "groupAttribute": "memberOf",
	}
	if doJSON(t, handler, http.MethodPost, "/auth/admin/config/ldap/server", chiefToken, server).Code != http.StatusOK {
		t.Fatal("ldap server")
	}
	if doJSON(t, handler, http.MethodPost, "/auth/admin/config/ldap", chiefToken, map[string]bool{"enableLdap": true}).Code != http.StatusOK {
		t.Fatal("ldap toggle")
	}
	ldapDirectory = map[string]ldapDirectoryUser{
		"ivanov": {
			password: "secret",
			identity: auth.LdapIdentity{
				Email: "ivanov@corp.local", Name: "Иванов",
				Groups: []string{"CN=Архив,OU=Groups,DC=corp,DC=local"},
			},
		},
	}
	logged := decodeJSON[loginResponse](t, doJSON(t, handler, http.MethodPost, "/auth/ldap", "", map[string]string{
		"login": "ivanov", "password": "secret",
	}))
	if logged.Token == "" {
		t.Fatal("ldap login")
	}
	profile := decodeJSON[profileResponse](t, doJSON(t, handler, http.MethodGet, "/me", logged.Token, nil))
	if len(profile.Roles) != 1 || profile.Roles[0].RoleID != created.RoleID {
		t.Fatalf("groups %+v", profile.Roles)
	}
}

func TestStubLdapDoesNotTakeLocalAccount(t *testing.T) {
	t.Cleanup(func() { ldapDirectory = nil })
	handler := must_Handler(t, config.Config{Mode: config.ModeStub})
	chiefToken := login(t, handler, texts.SeedAdminLogin, texts.SeedAdminPassword)
	server := map[string]any{
		"label": "Active Directory", "host": "127.0.0.1", "attributeForMail": "mail",
		"attributeForUsername": "sAMAccountName", "searchBase": "DC=corp,DC=local", "ciphers": "ALL",
	}
	if doJSON(t, handler, http.MethodPost, "/auth/admin/config/ldap/server", chiefToken, server).Code != http.StatusOK {
		t.Fatal("ldap server")
	}
	if doJSON(t, handler, http.MethodPost, "/auth/admin/config/ldap", chiefToken, map[string]bool{"enableLdap": true}).Code != http.StatusOK {
		t.Fatal("ldap toggle")
	}
	// Локальная запись с почтой, которую потом вернет каталог
	local := doJSON(t, handler, http.MethodPost, "/users", chiefToken, map[string]any{
		"email": "petrov@corp.local", "password": "local-secret", "displayName": "Петров", "role": "user",
	})
	if local.Code != http.StatusOK {
		t.Fatalf("local user %d %s", local.Code, local.Body.String())
	}
	ldapDirectory = map[string]ldapDirectoryUser{
		"petrov":  {password: "secret", identity: auth.LdapIdentity{Email: "petrov@corp.local", Name: "Петров"}},
		"sidorov": {password: "secret", identity: auth.LdapIdentity{Email: "sidorov@corp.local", Name: "Сидоров"}},
	}
	conflict := doJSON(t, handler, http.MethodPost, "/auth/ldap", "", map[string]string{"login": "petrov", "password": "secret"})
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), texts.ErrorDirectoryAccountConflict) {
		t.Fatalf("ldap into local account %d %s", conflict.Code, conflict.Body.String())
	}
	// Запись, созданную каталогом, каталог открывает и при следующем входе
	for attempt := 0; attempt < 2; attempt++ {
		logged := doJSON(t, handler, http.MethodPost, "/auth/ldap", "", map[string]string{"login": "sidorov", "password": "secret"})
		if logged.Code != http.StatusOK {
			t.Fatalf("directory login %d: %d %s", attempt, logged.Code, logged.Body.String())
		}
	}
}

func TestStubKerberosRejectsDisabledHeaderAndToken(t *testing.T) {
	handler := must_Handler(t, config.Config{Mode: config.ModeStub})
	disabled := doJSON(t, handler, http.MethodPost, "/auth/kerberos", "", nil)
	if disabled.Code != http.StatusForbidden || !strings.Contains(disabled.Body.String(), texts.ErrorKerberosDisabled) {
		t.Fatalf("disabled %d %s", disabled.Code, disabled.Body.String())
	}
	chiefToken := login(t, handler, texts.SeedAdminLogin, texts.SeedAdminPassword)
	keytabPath := write_TestKeytab(t)
	server := map[string]any{
		"label": "Active Directory", "host": "127.0.0.1", "attributeForMail": "mail",
		"attributeForUsername": "sAMAccountName", "searchBase": "DC=corp,DC=local",
		"ciphers": "ALL", "enableKerberos": true, "keytabPath": keytabPath,
		"servicePrincipal": "HTTP/docs@BANK.LOCAL",
	}
	if doJSON(t, handler, http.MethodPost, "/auth/admin/config/ldap/server", chiefToken, server).Code != http.StatusOK {
		t.Fatal("kerberos settings")
	}
	missing := doRaw(t, handler, http.MethodPost, "/auth/kerberos", "", nil, "")
	if missing.Code != http.StatusUnauthorized || missing.Header().Get("WWW-Authenticate") != "Negotiate" {
		t.Fatalf("missing header %d %s", missing.Code, missing.Header().Get("WWW-Authenticate"))
	}
	request := httptest.NewRequest(http.MethodPost, "/auth/kerberos", nil)
	request.Header.Set("Authorization", "Negotiate bm90LWEtdGlja2V0")
	bad := httptest.NewRecorder()
	handler.ServeHTTP(bad, request)
	if bad.Code != http.StatusUnauthorized || bad.Header().Get("WWW-Authenticate") != "Negotiate" {
		t.Fatalf("bad token %d %s", bad.Code, bad.Body.String())
	}
}

func write_TestKeytab(t *testing.T) string {
	t.Helper()
	loaded := keytab.New()
	if err := loaded.AddEntry("HTTP/docs", "BANK.LOCAL", "password", time.Now(), 1, 18); err != nil {
		t.Fatal(err)
	}
	file, err := os.CreateTemp(t.TempDir(), "docs.keytab")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = loaded.Write(file); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	return file.Name()
}
