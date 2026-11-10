package config

import (
	"os"
	"strconv"
	"strings"

	"docfinder/core/internal/auth"
	"docfinder/core/internal/texts"
)

const (
	ModeStub = "stub"
	ModeLive = "live"
)

type Config struct {
	Debug                 bool
	Mode                  string
	Port                  string
	DocsPassword          string
	DatabaseURL           string
	OllamaURL             string
	OllamaChatModel       string
	OllamaEmbedModel      string
	OllamaEmbedDimensions int
	AIProvider            string
	LLMBaseURL            string
	LLMAPIKey             string
	LLMModel              string
	EmbedModel            string
	RerankerModel         string
	// Отдельный движок юридического ассистента. Пустой адрес оставляет прежний путь ответа ядра.
	EngineURL   string
	EngineToken string
	// Старые сценарии подготовки документов и ДОУ доступны только при явном включении.
	LegacyDOUFeatures bool
	// STUB_SEED_USERS=false: заглушка стартует без admin/admin и user/user, первый вход через регистрацию.
	// Нулевое значение оставляет учётные записи заглушки, на них опираются тесты и клиенты
	StubWithoutUsers bool
	// Начальные настройки входа. После сохранения в админ-панели действуют значения из базы
	AuthDefaults auth.Settings
	// LAYOUT_FORMAT_PRIORITY: профили оформления макетов через запятую, первый важнее (instruction,gost).
	// Пусто - порядок задают сами профили: Инструкция по ДОУ, затем ГОСТ
	LayoutFormatPriority []string
	// STANDARD_DOCS_DIR: каталог стандартного набора документов компании (source/docs).
	// Набор загружается в базу знаний при каждом запуске боевого режима. Пусто - набора нет
	StandardDocsDir string
}

func Load() Config {
	load_DotEnv(".env")
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("API_MODE")))
	if mode == "" {
		mode = ModeStub
	}
	port := strings.TrimPrefix(strings.TrimSpace(os.Getenv("API_PORT")), ":")
	if port == "" {
		port = "8080"
	}
	return Config{
		Debug:                 os.Getenv("DEBUG") == "true",
		Mode:                  mode,
		Port:                  port,
		DocsPassword:          os.Getenv("DOCS_PASSWORD"),
		DatabaseURL:           os.Getenv("DATABASE_URL"),
		OllamaURL:             os.Getenv("OLLAMA_URL"),
		OllamaChatModel:       os.Getenv("OLLAMA_CHAT_MODEL"),
		OllamaEmbedModel:      os.Getenv("OLLAMA_EMBED_MODEL"),
		OllamaEmbedDimensions: read_EmbedDimensions(),
		AIProvider:            strings.ToLower(strings.TrimSpace(os.Getenv("AI_PROVIDER"))),
		LLMBaseURL:            strings.TrimSpace(os.Getenv("LLM_BASE_URL")),
		LLMAPIKey:             strings.TrimSpace(os.Getenv("LLM_API_KEY")),
		LLMModel:              strings.TrimSpace(os.Getenv("LLM_MODEL")),
		EmbedModel:            strings.TrimSpace(os.Getenv("EMB_MODEL")),
		RerankerModel:         strings.TrimSpace(os.Getenv("RERANKER_MODEL")),
		EngineURL:             strings.TrimSpace(os.Getenv("ENGINE_URL")),
		EngineToken:           strings.TrimSpace(os.Getenv("ENGINE_TOKEN")),
		LegacyDOUFeatures:     read_Boolean("LEGACY_DOU_FEATURES", false),
		StubWithoutUsers:      !read_Boolean("STUB_SEED_USERS", true),
		AuthDefaults:          read_AuthDefaults(),
		LayoutFormatPriority:  read_List("LAYOUT_FORMAT_PRIORITY"),
		StandardDocsDir:       strings.TrimSpace(os.Getenv("STANDARD_DOCS_DIR")),
	}
}

// Имена переменных как в open-webui, чтобы их было легко узнать
func read_AuthDefaults() auth.Settings {
	defaultRole := auth.Normalize_Role(os.Getenv("DEFAULT_USER_ROLE"))
	if !auth.Check_IsKnownRole(defaultRole) {
		defaultRole = texts.RolePending
	}
	ldapLabel := strings.TrimSpace(os.Getenv("LDAP_SERVER_LABEL"))
	if ldapLabel == "" {
		ldapLabel = texts.DefaultLdapLabel
	}
	return auth.Settings{
		Signup: auth.SignupSettings{
			EnableSignup:     read_Boolean("ENABLE_SIGNUP", true),
			DefaultUserRole:  defaultRole,
			ShowAdminDetails: read_Boolean("SHOW_ADMIN_DETAILS", true),
		},
		Ldap: auth.LdapSettings{
			EnableLdap:            read_Boolean("ENABLE_LDAP", false),
			Label:                 ldapLabel,
			Host:                  strings.TrimSpace(os.Getenv("LDAP_SERVER_HOST")),
			Port:                  read_Integer("LDAP_SERVER_PORT", 0),
			AttributeForMail:      read_String("LDAP_ATTRIBUTE_FOR_MAIL", "mail"),
			AttributeForUsername:  read_String("LDAP_ATTRIBUTE_FOR_USERNAME", "sAMAccountName"),
			AppDN:                 strings.TrimSpace(os.Getenv("LDAP_APP_DN")),
			AppPassword:           os.Getenv("LDAP_APP_PASSWORD"),
			SearchBase:            strings.TrimSpace(os.Getenv("LDAP_SEARCH_BASE")),
			SearchFilters:         strings.TrimSpace(os.Getenv("LDAP_SEARCH_FILTERS")),
			UseTLS:                read_Boolean("LDAP_USE_TLS", false),
			CertificatePath:       strings.TrimSpace(os.Getenv("LDAP_CA_CERT_FILE")),
			ValidateCert:          read_Boolean("LDAP_VALIDATE_CERT", true),
			Ciphers:               read_String("LDAP_CIPHERS", "ALL"),
			EnableGroupManagement: read_Boolean("ENABLE_LDAP_GROUP_MANAGEMENT", false),
			GroupAttribute:        read_String("LDAP_ATTRIBUTE_FOR_GROUPS", "memberOf"),
			EnableKerberos:        read_Boolean("ENABLE_KERBEROS", false),
			KeytabPath:            strings.TrimSpace(os.Getenv("KERBEROS_KEYTAB")),
			ServicePrincipal:      strings.TrimSpace(os.Getenv("KERBEROS_SERVICE_PRINCIPAL")),
		},
	}
}

func read_String(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}

// Значения через запятую без пустых
func read_List(name string) []string {
	values := []string{}
	for _, value := range strings.Split(os.Getenv(name), ",") {
		if value = strings.TrimSpace(value); value != "" {
			values = append(values, value)
		}
	}
	return values
}

func read_Boolean(name string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	switch value {
	case "true", "1", "yes":
		return true
	case "false", "0", "no":
		return false
	default:
		return fallback
	}
}

func read_Integer(name string, fallback int) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
	if err != nil {
		return fallback
	}
	return parsed
}

func read_EmbedDimensions() int {
	raw := strings.TrimSpace(os.Getenv("OLLAMA_EMBED_DIMENSIONS"))
	if raw == "" {
		return 768
	}
	parsed, parseError := strconv.Atoi(raw)
	if parseError != nil || parsed <= 0 {
		return 0
	}
	return parsed
}

func load_DotEnv(path string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		_ = os.Setenv(key, value)
	}
}
