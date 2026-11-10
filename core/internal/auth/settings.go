// Пакет auth: настройки входа и регистрации и вход через LDAP.
// Устроено как в open-webui: значения из .env задают только начальные настройки,
// после сохранения в админ-панели действуют значения из базы.
package auth

import (
	"net/mail"
	"strings"

	"docfinder/core/internal/texts"
)

// Регистрация по почте и роль новых пользователей
type SignupSettings struct {
	EnableSignup     bool   `json:"enableSignup"`
	DefaultUserRole  string `json:"defaultUserRole"`
	ShowAdminDetails bool   `json:"showAdminDetails"`
}

// Подключение к LDAP или Active Directory. Поля повторяют настройки LDAP в open-webui
type LdapSettings struct {
	EnableLdap            bool   `json:"enableLdap"`
	Label                 string `json:"label"`
	Host                  string `json:"host"`
	Port                  int    `json:"port"`
	AttributeForMail      string `json:"attributeForMail"`
	AttributeForUsername  string `json:"attributeForUsername"`
	AppDN                 string `json:"appDn"`
	AppPassword           string `json:"appDnPassword"`
	SearchBase            string `json:"searchBase"`
	SearchFilters         string `json:"searchFilters"`
	UseTLS                bool   `json:"useTls"`
	CertificatePath       string `json:"certificatePath"`
	ValidateCert          bool   `json:"validateCert"`
	Ciphers               string `json:"ciphers"`
	EnableGroupManagement bool   `json:"enableGroupManagement"`
	GroupAttribute        string `json:"groupAttribute"`
	EnableKerberos        bool   `json:"enableKerberos"`
	KeytabPath            string `json:"keytabPath"`
	ServicePrincipal      string `json:"servicePrincipal"`
}

// Начальные значения из .env
type Settings struct {
	Signup SignupSettings
	Ldap   LdapSettings
}

// Роль из запроса в нижнем регистре. Пустая строка - роль не передана
func Normalize_Role(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func Check_IsKnownRole(role string) bool {
	switch role {
	case texts.RolePending, texts.RoleUser, texts.RoleAdmin:
		return true
	default:
		return false
	}
}

// Роль хранится как уровень доступа и признак ожидания: pending видит только экран ожидания,
// user - уровень сотрудника, admin - уровень администратора
func Access_ForRole(role string) (int, bool) {
	switch role {
	case texts.RoleAdmin:
		return texts.SeedAdminAccessLevel, false
	case texts.RolePending:
		return texts.SeedEmployeeAccessLevel, true
	default:
		return texts.SeedEmployeeAccessLevel, false
	}
}

func Role_ForAccess(accessLevel int, pending bool) string {
	if pending {
		return texts.RolePending
	}
	if accessLevel >= texts.SeedAdminAccessLevel {
		return texts.RoleAdmin
	}
	return texts.RoleUser
}

// Почта как логин: без пробелов по краям и в нижнем регистре
func Normalize_Email(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

// Простой адрес вида name@domain.zone, без имени в угловых скобках.
// После @ нужен домен с точкой, части домена непустые
func Check_IsEmail(value string) bool {
	parsed, err := mail.ParseAddress(value)
	if err != nil || parsed.Address != value || parsed.Name != "" {
		return false
	}
	domain := value[strings.LastIndex(value, "@")+1:]
	labels := strings.Split(domain, ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if label == "" {
			return false
		}
	}
	return true
}

// bcrypt учитывает только первые 72 байта пароля, длиннее не принимаем, как open-webui
const MaxPasswordBytes = 72

func Check_PasswordFits(password string) bool {
	return password != "" && len(password) <= MaxPasswordBytes
}
