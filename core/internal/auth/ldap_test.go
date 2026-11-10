package auth

import (
	"errors"
	"os"
	"strconv"
	"testing"

	"docfinder/core/internal/texts"
)

func TestBuild_LdapSearchFilterEscapesLogin(t *testing.T) {
	settings := LdapSettings{AttributeForUsername: "sAMAccountName", SearchFilters: "(objectClass=user)"}
	filter := Build_LdapSearchFilter(settings, "ivanov*)(objectClass=*")
	want := `(&(sAMAccountName=ivanov\2a\29\28objectClass=\2a)(objectClass=user))`
	if filter != want {
		t.Fatalf("filter %s", filter)
	}
	settings.SearchFilters = "memberOf=CN=Docs,DC=corp,DC=local"
	if got := Build_LdapSearchFilter(settings, "ivanov"); got != "(&(sAMAccountName=ivanov)(memberOf=CN=Docs,DC=corp,DC=local))" {
		t.Fatalf("filter without brackets %s", got)
	}
	settings.SearchFilters = ""
	if got := Build_LdapSearchFilter(settings, "ivanov"); got != "(sAMAccountName=ivanov)" {
		t.Fatalf("plain filter %s", got)
	}
}

// Пустой пароль отсекается до сети: AD принял бы его как анонимный вход
func TestAuthenticate_LdapUserRejectsEmptyPassword(t *testing.T) {
	settings := LdapSettings{Host: "127.0.0.1", Port: 1, AttributeForUsername: "uid", AttributeForMail: "mail", SearchBase: "dc=example,dc=org"}
	for _, password := range []string{"", "   "} {
		_, err := Authenticate_LdapUser(settings, "ivanov", password)
		if !errors.Is(err, ErrLdapInvalidCredentials) {
			t.Fatalf("password %q: %v", password, err)
		}
	}
	_, err := Authenticate_LdapUser(settings, "ivanov", "secret")
	if !errors.Is(err, ErrLdapUnavailable) {
		t.Fatalf("closed port: %v", err)
	}
}

func TestRolesMapToAccess(t *testing.T) {
	cases := []struct {
		role    string
		level   int
		pending bool
	}{
		{texts.RoleAdmin, texts.SeedAdminAccessLevel, false},
		{texts.RoleUser, texts.SeedEmployeeAccessLevel, false},
		{texts.RolePending, texts.SeedEmployeeAccessLevel, true},
	}
	for _, item := range cases {
		level, pending := Access_ForRole(item.role)
		if level != item.level || pending != item.pending {
			t.Fatalf("%s -> %d %t", item.role, level, pending)
		}
		if Role_ForAccess(level, pending) != item.role {
			t.Fatalf("back %s", item.role)
		}
	}
	if Check_IsKnownRole("owner") || !Check_IsKnownRole(Normalize_Role(" Admin ")) {
		t.Fatal("role check")
	}
}

func TestCiphersAndEmails(t *testing.T) {
	if !Check_CiphersKnown("ALL") || !Check_CiphersKnown("") || !Check_CiphersKnown("TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256:TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384") {
		t.Fatal("known ciphers rejected")
	}
	if Check_CiphersKnown("RC4-SHA-NOPE") {
		t.Fatal("unknown cipher accepted")
	}
	if !Check_IsEmail("ivanov@corp.local") || Check_IsEmail("ivanov") || Check_IsEmail("Иван <ivanov@corp.local>") ||
		Check_IsEmail("ivanov@corp") || Check_IsEmail("ivanov@") || Check_IsEmail("@corp.local") || Check_IsEmail("ivanov@corp.") {
		t.Fatal("email check")
	}
	if Check_PasswordFits("") || Check_PasswordFits(string(make([]byte, MaxPasswordBytes+1))) || !Check_PasswordFits("x") {
		t.Fatal("password length check")
	}
}

// Проверка на живом каталоге. Пример для контейнера OpenLDAP:
// LDAP_TEST_HOST=127.0.0.1 LDAP_TEST_PORT=1389 LDAP_TEST_APP_DN=cn=admin,dc=example,dc=org
// LDAP_TEST_APP_PASSWORD=adminpassword LDAP_TEST_BASE=ou=users,dc=example,dc=org
// LDAP_TEST_USER=user01 LDAP_TEST_PASSWORD=password1 LDAP_TEST_EMAIL=user01@example.org
func TestAuthenticate_LdapUserAgainstServer(t *testing.T) {
	host := os.Getenv("LDAP_TEST_HOST")
	if host == "" {
		t.Skip("LDAP_TEST_HOST is empty")
	}
	port, _ := strconv.Atoi(os.Getenv("LDAP_TEST_PORT"))
	settings := LdapSettings{
		EnableLdap:           true,
		Host:                 host,
		Port:                 port,
		AttributeForMail:     "mail",
		AttributeForUsername: "uid",
		AppDN:                os.Getenv("LDAP_TEST_APP_DN"),
		AppPassword:          os.Getenv("LDAP_TEST_APP_PASSWORD"),
		SearchBase:           os.Getenv("LDAP_TEST_BASE"),
		// LDAP_TEST_TLS=true: LDAPS с самоподписанным сертификатом тестового сервера
		UseTLS:       os.Getenv("LDAP_TEST_TLS") == "true",
		ValidateCert: os.Getenv("LDAP_TEST_TLS") != "true",
	}
	user := os.Getenv("LDAP_TEST_USER")
	identity, err := Authenticate_LdapUser(settings, user, os.Getenv("LDAP_TEST_PASSWORD"))
	if err != nil {
		t.Fatal(err)
	}
	if identity.Email != os.Getenv("LDAP_TEST_EMAIL") || identity.DN == "" {
		t.Fatalf("identity %+v", identity)
	}
	if _, err := Authenticate_LdapUser(settings, user, "wrong-password"); !errors.Is(err, ErrLdapInvalidCredentials) {
		t.Fatalf("wrong password: %v", err)
	}
	if _, err := Authenticate_LdapUser(settings, "*", os.Getenv("LDAP_TEST_PASSWORD")); !errors.Is(err, ErrLdapInvalidCredentials) {
		t.Fatalf("wildcard login: %v", err)
	}
	// LDAP_TEST_CA_FILE: тот же вход с проверкой сертификата по своему CA
	if caFile := os.Getenv("LDAP_TEST_CA_FILE"); caFile != "" && settings.UseTLS {
		validated := settings
		validated.ValidateCert = true
		if _, err := Authenticate_LdapUser(validated, user, os.Getenv("LDAP_TEST_PASSWORD")); !errors.Is(err, ErrLdapUnavailable) {
			t.Fatalf("self-signed certificate accepted without ca: %v", err)
		}
		validated.CertificatePath = caFile
		if _, err := Authenticate_LdapUser(validated, user, os.Getenv("LDAP_TEST_PASSWORD")); err != nil {
			t.Fatalf("ca file: %v", err)
		}
	}
	settings.AppPassword = "wrong"
	if _, err := Authenticate_LdapUser(settings, user, os.Getenv("LDAP_TEST_PASSWORD")); !errors.Is(err, ErrLdapUnavailable) {
		t.Fatalf("wrong service password: %v", err)
	}
}
