package auth

import (
	"encoding/base64"
	"errors"
	"strings"

	"github.com/jcmturner/gokrb5/v8/credentials"
	"github.com/jcmturner/gokrb5/v8/gssapi"
	"github.com/jcmturner/gokrb5/v8/keytab"
	"github.com/jcmturner/gokrb5/v8/service"
	"github.com/jcmturner/gokrb5/v8/spnego"
)

var ErrKerberosInvalid = errors.New("kerberos invalid")

const negotiateContextCredentials = "github.com/jcmturner/gokrb5/v8/ctxCredentials"

// Билет SPNEGO сверяется с keytab. Наружу возвращается имя до @.
func Accept_NegotiateToken(keytabPath, servicePrincipal string, token []byte) (string, error) {
	if len(token) == 0 || strings.TrimSpace(keytabPath) == "" {
		return "", ErrKerberosInvalid
	}
	loaded, err := keytab.Load(keytabPath)
	if err != nil {
		return "", ErrKerberosInvalid
	}
	options := []func(*service.Settings){service.RequireHostAddr(false)}
	if principal := strings.TrimSpace(servicePrincipal); principal != "" {
		options = append(options, service.KeytabPrincipal(principal))
		name, _, _ := strings.Cut(principal, "@")
		if name != "" {
			options = append(options, service.SName(name))
		}
	}
	verifier := spnego.SPNEGOService(loaded, options...)
	var parsed spnego.SPNEGOToken
	if err = parsed.Unmarshal(token); err != nil {
		return "", ErrKerberosInvalid
	}
	accepted, ctx, status := verifier.AcceptSecContext(&parsed)
	if !accepted || status.Code != gssapi.StatusComplete || ctx == nil {
		return "", ErrKerberosInvalid
	}
	creds, ok := ctx.Value(negotiateContextCredentials).(*credentials.Credentials)
	if !ok || creds == nil {
		return "", ErrKerberosInvalid
	}
	username := strings.TrimSpace(creds.UserName())
	if username == "" {
		return "", ErrKerberosInvalid
	}
	return username, nil
}

func Decode_NegotiateToken(header string) ([]byte, bool) {
	scheme, value, found := strings.Cut(strings.TrimSpace(header), " ")
	if !found || !strings.EqualFold(scheme, "Negotiate") || strings.TrimSpace(value) == "" {
		return nil, false
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil || len(raw) == 0 {
		return nil, false
	}
	return raw, true
}
