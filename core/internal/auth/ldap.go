package auth

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"
)

var (
	// Нет такого пользователя, неверный пароль или логин подходит нескольким записям
	ErrLdapInvalidCredentials = errors.New("ldap invalid credentials")
	// Сервер не отвечает, служебная учётная запись не вошла или настройки неверны
	ErrLdapUnavailable = errors.New("ldap unavailable")
	// У записи в каталоге нет почты: локальная учётная запись привязывается к почте
	ErrLdapEmailMissing = errors.New("ldap email missing")
)

const (
	ldapTimeout       = 10 * time.Second
	ldapPortPlain     = 389
	ldapPortTLS       = 636
	ldapNameAttribute = "cn"
	// Два результата достаточно, чтобы понять, что логин неоднозначный
	ldapSearchSizeLimit = 2
)

// Пользователь каталога после проверки пароля
type LdapIdentity struct {
	DN     string
	Email  string
	Name   string
	Groups []string
}

// Вход по логину каталога. Порядок шагов как в open-webui:
// служебная учётная запись ищет пользователя по атрибуту логина, затем каталог проверяет его пароль.
func Authenticate_LdapUser(settings LdapSettings, username, password string) (LdapIdentity, error) {
	username = strings.TrimSpace(username)
	// Пустой пароль Active Directory принимает как анонимный вход и отвечает успехом.
	// Без этой проверки можно было бы войти под любым логином
	if username == "" || strings.TrimSpace(password) == "" {
		return LdapIdentity{}, ErrLdapInvalidCredentials
	}
	connection, err := dial_LdapServer(settings)
	if err != nil {
		return LdapIdentity{}, fmt.Errorf("%w: connect: %v", ErrLdapUnavailable, err)
	}
	defer connection.Close()

	// Без DN служебной учётной записи поиск идёт анонимно, как в open-webui
	if strings.TrimSpace(settings.AppDN) != "" {
		if err := connection.Bind(settings.AppDN, settings.AppPassword); err != nil {
			return LdapIdentity{}, fmt.Errorf("%w: service bind: %v", ErrLdapUnavailable, err)
		}
	}

	searchRequest := ldap.NewSearchRequest(
		settings.SearchBase,
		ldap.ScopeWholeSubtree,
		ldap.NeverDerefAliases,
		ldapSearchSizeLimit,
		int(ldapTimeout/time.Second),
		false,
		Build_LdapSearchFilter(settings, username),
		ldap_SearchAttributes(settings),
		nil,
	)
	result, err := connection.Search(searchRequest)
	if err != nil {
		if ldap.IsErrorWithCode(err, ldap.LDAPResultSizeLimitExceeded) {
			return LdapIdentity{}, ErrLdapInvalidCredentials
		}
		return LdapIdentity{}, fmt.Errorf("%w: search: %v", ErrLdapUnavailable, err)
	}
	if len(result.Entries) != 1 {
		return LdapIdentity{}, ErrLdapInvalidCredentials
	}
	entry := result.Entries[0]
	identity, err := identity_FromEntry(entry, settings, username)
	if err != nil {
		return LdapIdentity{}, err
	}

	if err := connection.Bind(entry.DN, password); err != nil {
		if ldap.IsErrorWithCode(err, ldap.LDAPResultInvalidCredentials) {
			return LdapIdentity{}, ErrLdapInvalidCredentials
		}
		return LdapIdentity{}, fmt.Errorf("%w: user bind: %v", ErrLdapUnavailable, err)
	}
	return identity, nil
}

// Поиск без пароля сотрудника: билет Kerberos уже доказал имя, почту и группы берем из каталога.
func Lookup_LdapUser(settings LdapSettings, username string) (LdapIdentity, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return LdapIdentity{}, ErrLdapInvalidCredentials
	}
	connection, err := dial_LdapServer(settings)
	if err != nil {
		return LdapIdentity{}, fmt.Errorf("%w: connect: %v", ErrLdapUnavailable, err)
	}
	defer connection.Close()
	if strings.TrimSpace(settings.AppDN) != "" {
		if err := connection.Bind(settings.AppDN, settings.AppPassword); err != nil {
			return LdapIdentity{}, fmt.Errorf("%w: service bind: %v", ErrLdapUnavailable, err)
		}
	}
	searchRequest := ldap.NewSearchRequest(
		settings.SearchBase,
		ldap.ScopeWholeSubtree,
		ldap.NeverDerefAliases,
		ldapSearchSizeLimit,
		int(ldapTimeout/time.Second),
		false,
		Build_LdapSearchFilter(settings, username),
		ldap_SearchAttributes(settings),
		nil,
	)
	result, err := connection.Search(searchRequest)
	if err != nil {
		if ldap.IsErrorWithCode(err, ldap.LDAPResultSizeLimitExceeded) {
			return LdapIdentity{}, ErrLdapInvalidCredentials
		}
		return LdapIdentity{}, fmt.Errorf("%w: search: %v", ErrLdapUnavailable, err)
	}
	if len(result.Entries) != 1 {
		return LdapIdentity{}, ErrLdapInvalidCredentials
	}
	return identity_FromEntry(result.Entries[0], settings, username)
}

func identity_FromEntry(entry *ldap.Entry, settings LdapSettings, username string) (LdapIdentity, error) {
	if !check_ValueListed(entry.GetEqualFoldAttributeValues(settings.AttributeForUsername), username) {
		return LdapIdentity{}, ErrLdapInvalidCredentials
	}
	email := Normalize_Email(entry.GetEqualFoldAttributeValue(settings.AttributeForMail))
	if email == "" {
		return LdapIdentity{}, ErrLdapEmailMissing
	}
	name := strings.TrimSpace(entry.GetEqualFoldAttributeValue(ldapNameAttribute))
	if name == "" {
		name = username
	}
	return LdapIdentity{
		DN:     entry.DN,
		Email:  email,
		Name:   name,
		Groups: Group_Names(entry.GetAttributeValues(group_Attribute(settings))),
	}, nil
}

func group_Attribute(settings LdapSettings) string {
	name := strings.TrimSpace(settings.GroupAttribute)
	if name == "" {
		return "memberOf"
	}
	return name
}

func ldap_SearchAttributes(settings LdapSettings) []string {
	attributes := []string{settings.AttributeForUsername, settings.AttributeForMail, ldapNameAttribute}
	groupAttribute := group_Attribute(settings)
	for _, attribute := range attributes {
		if strings.EqualFold(attribute, groupAttribute) {
			return attributes
		}
	}
	return append(attributes, groupAttribute)
}

// (&(sAMAccountName=<логин>)<дополнительные фильтры>). Логин экранируется, чтобы его нельзя было
// превратить в свой фильтр вроде *)(objectClass=*
func Build_LdapSearchFilter(settings LdapSettings, username string) string {
	userFilter := "(" + strings.TrimSpace(settings.AttributeForUsername) + "=" + ldap.EscapeFilter(username) + ")"
	extraFilter := strings.TrimSpace(settings.SearchFilters)
	if extraFilter == "" {
		return userFilter
	}
	if !strings.HasPrefix(extraFilter, "(") {
		extraFilter = "(" + extraFilter + ")"
	}
	return "(&" + userFilter + extraFilter + ")"
}

func check_ValueListed(values []string, wanted string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), wanted) {
			return true
		}
	}
	return false
}

// Хост можно указать и с префиксом ldap:// или ldaps://, протокол всё равно выбирает useTls
func read_LdapHost(raw string) string {
	host := strings.TrimSpace(raw)
	for _, prefix := range []string{"ldaps://", "ldap://"} {
		if len(host) >= len(prefix) && strings.EqualFold(host[:len(prefix)], prefix) {
			host = host[len(prefix):]
		}
	}
	return strings.TrimRight(host, "/")
}

func dial_LdapServer(settings LdapSettings) (*ldap.Conn, error) {
	host := read_LdapHost(settings.Host)
	if host == "" {
		return nil, errors.New("empty host")
	}
	port := settings.Port
	if port <= 0 {
		port = ldapPortPlain
		if settings.UseTLS {
			port = ldapPortTLS
		}
	}
	address := net.JoinHostPort(host, strconv.Itoa(port))
	dialer := &net.Dialer{Timeout: ldapTimeout}
	var connection *ldap.Conn
	var err error
	if settings.UseTLS {
		tlsConfig, tlsError := build_LdapTLSConfig(settings, host)
		if tlsError != nil {
			return nil, tlsError
		}
		connection, err = ldap.DialURL("ldaps://"+address, ldap.DialWithDialer(dialer), ldap.DialWithTLSConfig(tlsConfig))
	} else {
		connection, err = ldap.DialURL("ldap://"+address, ldap.DialWithDialer(dialer))
	}
	if err != nil {
		return nil, err
	}
	connection.SetTimeout(ldapTimeout)
	return connection, nil
}

// LDAPS: своя цепочка CA из файла, проверка сертификата и список шифров, как LDAP_CA_CERT_FILE,
// LDAP_VALIDATE_CERT и LDAP_CIPHERS в open-webui
func build_LdapTLSConfig(settings LdapSettings, host string) (*tls.Config, error) {
	tlsConfig := &tls.Config{
		ServerName:         host,
		InsecureSkipVerify: !settings.ValidateCert,
	}
	if certificatePath := strings.TrimSpace(settings.CertificatePath); certificatePath != "" {
		pemBytes, err := os.ReadFile(certificatePath)
		if err != nil {
			return nil, fmt.Errorf("read ca file: %v", err)
		}
		certificatePool := x509.NewCertPool()
		if !certificatePool.AppendCertsFromPEM(pemBytes) {
			return nil, errors.New("ca file has no pem certificates")
		}
		tlsConfig.RootCAs = certificatePool
	}
	cipherSuites, err := parse_CipherSuites(settings.Ciphers)
	if err != nil {
		return nil, err
	}
	tlsConfig.CipherSuites = cipherSuites
	return tlsConfig, nil
}

// Проверка списка шифров при сохранении настроек, чтобы ошибка была видна сразу, а не при входе
func Check_CiphersKnown(raw string) bool {
	_, err := parse_CipherSuites(raw)
	return err == nil
}

// "ALL" или пусто - набор Go по умолчанию. Иначе имена через двоеточие или запятую,
// например TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256. Шифры TLS 1.3 Go не настраивает
func parse_CipherSuites(raw string) ([]uint16, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "ALL") {
		return nil, nil
	}
	known := map[string]uint16{}
	for _, suite := range append(tls.CipherSuites(), tls.InsecureCipherSuites()...) {
		known[suite.Name] = suite.ID
	}
	var selected []uint16
	for _, name := range strings.FieldsFunc(raw, func(symbol rune) bool { return symbol == ':' || symbol == ',' || symbol == ' ' }) {
		suiteID, found := known[strings.ToUpper(name)]
		if !found {
			return nil, fmt.Errorf("unknown cipher %q", name)
		}
		selected = append(selected, suiteID)
	}
	return selected, nil
}
