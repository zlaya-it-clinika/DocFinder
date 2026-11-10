package httpapi

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"docfinder/core/internal/auth"
	"docfinder/core/internal/texts"
)

// Регистрация, вход через LDAP и настройки входа в режиме заглушки.
// Логика та же, что в боевом режиме, только данные в памяти. LDAP настоящий:
// заглушку можно направить на рабочий каталог и проверить вход без PostgreSQL.

// Пути, которые открыты ожидающему подтверждения: профиль, выход и разделы /auth
func check_OpenForPending(path string) bool {
	return path == "/me" || strings.HasPrefix(path, "/auth/") || path == "/docs" || strings.HasPrefix(path, "/docs/")
}

// Ожидающий подтверждения видит только экран ожидания, остальные методы отвечают account_pending
func (stubStore *stub) reject_PendingUsers(nextHandler http.Handler) http.Handler {
	return http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		if !check_OpenForPending(request.URL.Path) {
			stubStore.mu.Lock()
			record, found := stubStore.find_UserByBearerToken(request)
			pending := found && record.pending
			stubStore.mu.Unlock()
			if pending {
				write_ErrorResponse(responseWriter, http.StatusForbidden, texts.ErrorAccountPending)
				return
			}
		}
		nextHandler.ServeHTTP(responseWriter, request)
	})
}

func (stubStore *stub) serve_AuthConfig(responseWriter http.ResponseWriter, request *http.Request) {
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	write_JSONResponse(responseWriter, http.StatusOK, publicAuthConfigResponse{
		Onboarding:   len(stubStore.users) == 0,
		EnableSignup: stubStore.signupSettings.EnableSignup,
		EnableLdap:   stubStore.ldapSettings.EnableLdap,
		LdapLabel:    stubStore.ldapSettings.Label,
	})
}

func (stubStore *stub) serve_Signup(responseWriter http.ResponseWriter, request *http.Request) {
	body, errorCode := read_SignupRequest(responseWriter, request)
	if errorCode != "" {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, errorCode)
		return
	}
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	usersBefore := len(stubStore.users)
	if usersBefore > 0 && !stubStore.signupSettings.EnableSignup {
		write_ErrorResponse(responseWriter, http.StatusForbidden, texts.ErrorSignupDisabled)
		return
	}
	if stubStore.find_UserByLogin(body.Email) != nil {
		write_ErrorResponse(responseWriter, http.StatusConflict, texts.ErrorLoginTaken)
		return
	}
	accessLevel, pending := auth.Access_ForRole(stubStore.role_ForNewAccount(usersBefore))
	record := stubStore.create_NextUser(body.Email, body.Password, body.DisplayName, accessLevel, pending)
	stubStore.write_LoginForRecord(responseWriter, record)
}

func (stubStore *stub) serve_LdapLogin(responseWriter http.ResponseWriter, request *http.Request) {
	body, ok := read_LdapLoginRequest(responseWriter, request)
	if !ok {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	stubStore.mu.Lock()
	ldapSettings := stubStore.ldapSettings
	stubStore.mu.Unlock()
	if !ldapSettings.EnableLdap {
		write_ErrorResponse(responseWriter, http.StatusForbidden, texts.ErrorLdapDisabled)
		return
	}
	// Каталог может отвечать долго, поэтому ходим в него без блокировки заглушки
	identity, err := stubStore.lookup_LdapIdentity(ldapSettings, body.Login, body.Password)
	if err != nil {
		write_LdapError(responseWriter, err)
		return
	}
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	record, usersBefore, ok := stubStore.find_OrCreateDirectoryUser(responseWriter, identity)
	if !ok {
		return
	}
	if ldapSettings.EnableGroupManagement {
		stubStore.apply_GroupRoles(record, identity.Groups, usersBefore)
	}
	stubStore.write_LoginForRecord(responseWriter, record)
}

// Учетная запись для входа через каталог: созданная каталогом ранее или новая.
// Локальную запись с той же почтой не отдаем, как и боевой режим. Вызывать под stubStore.mu
func (stubStore *stub) find_OrCreateDirectoryUser(responseWriter http.ResponseWriter, identity auth.LdapIdentity) (*userRecord, int, bool) {
	record := stubStore.find_UserByLogin(identity.Email)
	usersBefore := len(stubStore.users)
	if record != nil {
		if !record.directory {
			write_ErrorResponse(responseWriter, http.StatusConflict, texts.ErrorDirectoryAccountConflict)
			return nil, 0, false
		}
		return record, usersBefore, true
	}
	randomPassword, err := create_RandomToken()
	if err != nil {
		write_ErrorResponse(responseWriter, http.StatusInternalServerError, texts.ErrorStore)
		return nil, 0, false
	}
	accessLevel, pending := auth.Access_ForRole(stubStore.role_ForNewAccount(usersBefore))
	record = stubStore.create_NextUser(identity.Email, randomPassword, identity.Name, accessLevel, pending)
	record.directory = true
	return record, usersBefore, true
}

type ldapDirectoryUser struct {
	password string
	identity auth.LdapIdentity
}

// Каталог для тестов: без сервера LDAP вход берет заранее записанную запись.
var ldapDirectory map[string]ldapDirectoryUser

func (stubStore *stub) lookup_LdapIdentity(settings auth.LdapSettings, username, password string) (auth.LdapIdentity, error) {
	if ldapDirectory != nil {
		entry, found := ldapDirectory[strings.ToLower(strings.TrimSpace(username))]
		if !found || entry.password != password {
			return auth.LdapIdentity{}, auth.ErrLdapInvalidCredentials
		}
		return entry.identity, nil
	}
	return auth.Authenticate_LdapUser(settings, username, password)
}

func (stubStore *stub) lookup_LdapIdentityWithoutPassword(settings auth.LdapSettings, username string) (auth.LdapIdentity, error) {
	if ldapDirectory != nil {
		entry, found := ldapDirectory[strings.ToLower(strings.TrimSpace(username))]
		if !found {
			return auth.LdapIdentity{}, auth.ErrLdapInvalidCredentials
		}
		return entry.identity, nil
	}
	return auth.Lookup_LdapUser(settings, username)
}

func (stubStore *stub) apply_GroupRoles(record *userRecord, groups []string, usersBefore int) {
	mapped := []auth.GroupRole{}
	for _, role := range stubStore.roles {
		if strings.TrimSpace(role.ldapGroup) == "" {
			continue
		}
		mapped = append(mapped, auth.GroupRole{RoleID: role.id, Group: role.ldapGroup})
	}
	roleName := stubStore.role_ForNewAccount(usersBefore)
	accessLevel, pending := auth.Access_ForRole(roleName)
	next := auth.RoleIDs_ForGroups(groups, mapped, record.roleIDs, auth.RoleID_ForNewAccount(usersBefore, accessLevel, pending))
	old := copy_Strings(record.roleIDs)
	if stubStore.chiefs_After(record.id, next) == 0 && contains_Role(old, texts.RoleIDChief) {
		return
	}
	record.roleIDs = next
	if _, matched := auth.Matched_GroupRole(groups, mapped); matched {
		record.pending = false
	}
	stubStore.sync_Access(record)
	stubStore.record_RoleDiff(record.id, record.id, old, next)
}

// Ответ на ошибку проверки в каталоге. Общий для заглушки, боевой режим разбирает ошибки в write_LiveError
func write_LdapError(responseWriter http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrLdapInvalidCredentials):
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorInvalidCredentials)
	case errors.Is(err, auth.ErrLdapEmailMissing):
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorLdapEmailMissing)
	default:
		log.Printf("ldap error: %v", err)
		write_ErrorResponse(responseWriter, http.StatusBadGateway, texts.ErrorLdapUnavailable)
	}
}

// Первый пользователь - администратор, остальные - роль по умолчанию
func (stubStore *stub) role_ForNewAccount(usersBefore int) string {
	if usersBefore == 0 {
		return texts.RoleAdmin
	}
	if !auth.Check_IsKnownRole(stubStore.signupSettings.DefaultUserRole) {
		return texts.RolePending
	}
	return stubStore.signupSettings.DefaultUserRole
}

func (stubStore *stub) serve_AdminDetails(responseWriter http.ResponseWriter, request *http.Request) {
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	if _, found := stubStore.find_UserByBearerToken(request); !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	if !stubStore.signupSettings.ShowAdminDetails {
		write_ErrorResponse(responseWriter, http.StatusNotFound, texts.ErrorNotFound)
		return
	}
	for _, record := range stubStore.users {
		if record.accessLevel >= adminAccessLevel && !record.pending {
			write_JSONResponse(responseWriter, http.StatusOK, adminDetailsResponse{Name: record.displayName, Email: record.login})
			return
		}
	}
	write_ErrorResponse(responseWriter, http.StatusNotFound, texts.ErrorNotFound)
}

func (stubStore *stub) serve_SignupSettings(responseWriter http.ResponseWriter, request *http.Request) {
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	if _, found := stubStore.require_AdminUser(responseWriter, request); !found {
		return
	}
	write_JSONResponse(responseWriter, http.StatusOK, stubStore.signupSettings)
}

func (stubStore *stub) serve_SaveSignupSettings(responseWriter http.ResponseWriter, request *http.Request) {
	var body signupSettingsRequest
	if err := read_JSONBody(responseWriter, request, &body); err != nil {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	if _, found := stubStore.require_AdminUser(responseWriter, request); !found {
		return
	}
	updated, ok := apply_SignupSettingsRequest(stubStore.signupSettings, body)
	if !ok {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	stubStore.signupSettings = updated
	write_JSONResponse(responseWriter, http.StatusOK, updated)
}

func (stubStore *stub) serve_LdapToggle(responseWriter http.ResponseWriter, request *http.Request) {
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	if _, found := stubStore.require_AdminUser(responseWriter, request); !found {
		return
	}
	write_JSONResponse(responseWriter, http.StatusOK, ldapToggleResponse{EnableLdap: stubStore.ldapSettings.EnableLdap})
}

func (stubStore *stub) serve_SaveLdapToggle(responseWriter http.ResponseWriter, request *http.Request) {
	var body ldapToggleBody
	if err := read_JSONBody(responseWriter, request, &body); err != nil || body.EnableLdap == nil {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	if _, found := stubStore.require_AdminUser(responseWriter, request); !found {
		return
	}
	stubStore.ldapSettings.EnableLdap = *body.EnableLdap
	write_JSONResponse(responseWriter, http.StatusOK, ldapToggleResponse{EnableLdap: stubStore.ldapSettings.EnableLdap})
}

func (stubStore *stub) serve_LdapServer(responseWriter http.ResponseWriter, request *http.Request) {
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	if _, found := stubStore.require_AdminUser(responseWriter, request); !found {
		return
	}
	write_JSONResponse(responseWriter, http.StatusOK, build_LdapServerResponse(stubStore.ldapSettings))
}

func (stubStore *stub) serve_SaveLdapServer(responseWriter http.ResponseWriter, request *http.Request) {
	var body ldapServerRequest
	if err := read_JSONBody(responseWriter, request, &body); err != nil {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	if _, found := stubStore.require_AdminUser(responseWriter, request); !found {
		return
	}
	updated, ok := apply_LdapServerRequest(stubStore.ldapSettings, body)
	if !ok {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return
	}
	stubStore.ldapSettings = updated
	write_JSONResponse(responseWriter, http.StatusOK, build_LdapServerResponse(updated))
}
