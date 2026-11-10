package livestore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"docfinder/core/internal/auth"
	"docfinder/core/internal/texts"
)

const (
	counterUser     = "user"
	counterDocument = "document"
	counterSession  = "session"
	counterAudit    = "audit"
	counterSource   = "source"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUnauthorized       = errors.New("unauthorized")
	ErrAccessDenied       = errors.New("access denied")
	ErrNotFound           = errors.New("not found")
	ErrLoginTaken         = errors.New("login taken")
	ErrEmbedSize          = errors.New("embed size")
	ErrEmbedUnavailable   = errors.New("embed unavailable")
	ErrUnsupportedFile    = errors.New("unsupported file")
	ErrInvalidBody        = errors.New("invalid body")
	ErrSignupDisabled     = errors.New("signup disabled")
	ErrAccountPending     = errors.New("account pending")
	ErrLdapDisabled       = errors.New("ldap disabled")
	ErrKerberosDisabled   = errors.New("kerberos disabled")
	ErrSourceSyncRunning  = errors.New("source sync running")
	ErrStandardDocument   = errors.New("standard document")
	// Почта из каталога совпала с локальной учетной записью: входить в нее через каталог нельзя
	ErrDirectoryAccountConflict = errors.New("directory account conflict")
)

type Embedder interface {
	Embed_Texts(inputs []string) ([][]float32, error)
}

type Account struct {
	UserID      string
	Login       string
	DisplayName string
	AccessLevel int
	Pending     bool
	CreatedAt   time.Time
	RoleIDs     []string
	RoleTitles  []string
	Permissions []string
}

type LoginResult struct {
	Token       string
	UserID      string
	Login       string
	DisplayName string
	AccessLevel int
	Pending     bool
}

type DocumentLine struct {
	DocumentID   string
	Title        string
	Edition      string
	AccessLevel  int
	Scope        string
	OwnerID      string
	OwnerName    string
	UploadedAt   time.Time
	ProcessID    string
	VisibleToAll bool
	RoleIDs      []string
	Standard     bool
	// Сколько прежних редакций в архиве
	ArchivedEditions int
}

type DocumentFile struct {
	FileName    string
	ContentType string
	Body        []byte
}

type SimilarChunk struct {
	DocumentID string
	Title      string
	Edition    string
	Section    string
	Paragraph  string
	Appendix   string
	Content    string
	Scope      string
	Distance   float64
}

type LiveStore struct {
	pool         *pgxpool.Pool
	dimensions   int
	embedder     Embedder
	authDefaults auth.Settings
	reindex      reindexState
	sourceSync   sourceSyncState
}

// Учётных записей по умолчанию нет: первая регистрация создаёт администратора, как в open-webui
func Open_LiveStore(databaseURL string, dimensions int, embedder Embedder, authDefaults auth.Settings) (*LiveStore, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, errors.New(texts.LiveDatabaseMissing)
	}
	if dimensions <= 0 {
		return nil, errors.New(texts.LiveEmbedDimensions)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, errors.New(texts.LiveDatabaseConnect)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, errors.New(texts.LiveDatabaseConnect)
	}
	store := &LiveStore{pool: pool, dimensions: dimensions, embedder: embedder, authDefaults: authDefaults}
	if err := store.apply_Schema(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return store, nil
}

func (store *LiveStore) Close() {
	store.pool.Close()
}

func (store *LiveStore) apply_Schema(ctx context.Context) error {
	schema, err := read_MigrationSQL()
	if err != nil {
		return err
	}
	schema = strings.ReplaceAll(schema, "vector(768)", fmt.Sprintf("vector(%d)", store.dimensions))
	if _, err = store.pool.Exec(ctx, schema); err != nil {
		return err
	}
	if err = store.sync_EmbeddingDimensions(ctx); err != nil {
		return err
	}
	if err = store.seed_DefaultProcess(ctx); err != nil {
		return err
	}
	if err = store.seed_Roles(ctx); err != nil {
		return err
	}
	if err = store.grant_AIPermission(ctx); err != nil {
		return err
	}
	if err = store.grant_PermissionOnce(ctx, settingsKeyOrganizationPermission, texts.PermissionOrganizationManage); err != nil {
		return err
	}
	return store.grant_PermissionOnce(ctx, settingsKeySourcesPermission, texts.PermissionSourcesManage)
}

func read_MigrationSQL() (string, error) {
	var builder strings.Builder
	for _, name := range []string{"001_init.sql", "002_processes.sql", "003_roles.sql", "004_attachments.sql", "005_groups.sql", "006_process_meta.sql", "007_single_role.sql", "008_organization.sql", "009_chat_mode.sql", "010_document_editions.sql", "011_document_sources.sql", "012_directory_accounts.sql"} {
		raw, err := read_MigrationFile(name)
		if err != nil {
			return "", err
		}
		builder.WriteString(raw)
		builder.WriteByte('\n')
	}
	return builder.String(), nil
}

func read_MigrationFile(name string) (string, error) {
	candidates := []string{
		"migrations/" + name,
		"source/core/migrations/" + name,
		"../../migrations/" + name,
	}
	for _, candidate := range candidates {
		raw, err := os.ReadFile(candidate)
		if err == nil {
			return string(raw), nil
		}
	}
	return "", errors.New("migrations/" + name)
}

func (store *LiveStore) next_ID(ctx context.Context, name string) (int, error) {
	return next_IDWith(ctx, store.pool, name)
}

// Счётчик можно взять и внутри транзакции, тогда номер откатится вместе с ней
func next_IDWith(ctx context.Context, querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}, name string) (int, error) {
	var value int
	err := querier.QueryRow(ctx, `
		UPDATE id_counters SET value = value + 1 WHERE name = $1 RETURNING value - 1`, name).Scan(&value)
	return value, err
}
