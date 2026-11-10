package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"docfinder/core/internal/auth"
	"docfinder/core/internal/livestore"
	"docfinder/core/internal/texts"
)

func (api *liveAPI) serve_KerberosLogin(responseWriter http.ResponseWriter, request *http.Request) {
	enabled, err := api.store.Kerberos_IsEnabled(request.Context())
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	if !enabled {
		write_ErrorResponse(responseWriter, http.StatusForbidden, texts.ErrorKerberosDisabled)
		return
	}
	tokenBytes, present := read_NegotiateHeader(responseWriter, request)
	if !present {
		return
	}
	sessionToken, err := create_RandomToken()
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	result, err := api.store.Login_KerberosUser(request.Context(), tokenBytes, sessionToken)
	if err != nil {
		write_KerberosError(responseWriter, err)
		return
	}
	write_JSONResponse(responseWriter, http.StatusOK, build_LiveLoginResponse(result))
}

func (stubStore *stub) serve_KerberosLogin(responseWriter http.ResponseWriter, request *http.Request) {
	stubStore.mu.Lock()
	settings := stubStore.ldapSettings
	stubStore.mu.Unlock()
	if !settings.EnableKerberos {
		write_ErrorResponse(responseWriter, http.StatusForbidden, texts.ErrorKerberosDisabled)
		return
	}
	tokenBytes, present := read_NegotiateHeader(responseWriter, request)
	if !present {
		return
	}
	username, err := auth.Accept_NegotiateToken(settings.KeytabPath, settings.ServicePrincipal, tokenBytes)
	if err != nil {
		write_NegotiateFailure(responseWriter)
		return
	}
	if settings.EnableLdap {
		identity, lookupError := stubStore.lookup_LdapIdentityWithoutPassword(settings, username)
		if lookupError != nil {
			write_LdapError(responseWriter, lookupError)
			return
		}
		stubStore.mu.Lock()
		defer stubStore.mu.Unlock()
		record, usersBefore, ok := stubStore.find_OrCreateDirectoryUser(responseWriter, identity)
		if !ok {
			return
		}
		if settings.EnableGroupManagement {
			stubStore.apply_GroupRoles(record, identity.Groups, usersBefore)
		}
		stubStore.write_LoginForRecord(responseWriter, record)
		return
	}
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	record := stubStore.find_UserByLogin(username)
	if record == nil {
		write_NegotiateFailure(responseWriter)
		return
	}
	stubStore.write_LoginForRecord(responseWriter, record)
}

func read_NegotiateHeader(responseWriter http.ResponseWriter, request *http.Request) ([]byte, bool) {
	header := request.Header.Get("Authorization")
	if strings.TrimSpace(header) == "" {
		write_NegotiateChallenge(responseWriter)
		return nil, false
	}
	token, ok := auth.Decode_NegotiateToken(header)
	if !ok {
		write_NegotiateFailure(responseWriter)
		return nil, false
	}
	return token, true
}

func write_NegotiateChallenge(responseWriter http.ResponseWriter) {
	responseWriter.Header().Set("WWW-Authenticate", "Negotiate")
	write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorInvalidCredentials)
}

func write_NegotiateFailure(responseWriter http.ResponseWriter) {
	responseWriter.Header().Set("WWW-Authenticate", "Negotiate")
	write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorInvalidCredentials)
}

func write_KerberosError(responseWriter http.ResponseWriter, err error) {
	if errors.Is(err, livestore.ErrKerberosDisabled) {
		write_ErrorResponse(responseWriter, http.StatusForbidden, texts.ErrorKerberosDisabled)
		return
	}
	if errors.Is(err, livestore.ErrInvalidCredentials) {
		write_NegotiateFailure(responseWriter)
		return
	}
	write_LiveError(responseWriter, err)
}
