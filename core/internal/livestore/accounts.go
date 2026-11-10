package livestore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"

	"docfinder/core/internal/auth"
	"docfinder/core/internal/texts"
)

// Ключ рекомендательной блокировки PostgreSQL для создания учётных записей.
// Под ней считаем пользователей и вставляем нового, чтобы две первые регистрации
// одновременно не стали администраторами
const accountsAdvisoryLock = 73100001

// Поля, которые администратор меняет у учётной записи. nil - поле не трогаем.
// RoleIDs заменяет набор ролей. Без него смена уровня или ожидания ставит одну старую роль
type AccountPatch struct {
	DisplayName *string
	Login       *string
	Password    *string
	AccessLevel *int
	Pending     *bool
	RoleIDs     *[]string
}

const accountColumns = `user_id, login, display_name, access_level, pending, created_at`

type rowScanner interface {
	Scan(dest ...any) error
}

func scan_Account(row rowScanner) (Account, error) {
	var account Account
	err := row.Scan(&account.UserID, &account.Login, &account.DisplayName, &account.AccessLevel, &account.Pending, &account.CreatedAt)
	return account, err
}

// Вход по почте (или старому логину) и паролю. Логин сравнивается без учёта регистра
func (store *LiveStore) Login_User(ctx context.Context, loginName, password, token string) (LoginResult, error) {
	var account Account
	var passwordHash string
	err := store.pool.QueryRow(ctx, `
		SELECT `+accountColumns+`, password_hash
		FROM users WHERE lower(login) = lower($1)
		ORDER BY login = $1 DESC
		LIMIT 1`, strings.TrimSpace(loginName)).
		Scan(&account.UserID, &account.Login, &account.DisplayName, &account.AccessLevel, &account.Pending, &account.CreatedAt, &passwordHash)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)) != nil) {
		return LoginResult{}, ErrInvalidCredentials
	}
	if err != nil {
		return LoginResult{}, err
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO auth_tokens (token, user_id) VALUES ($1, $2)`, token, account.UserID); err != nil {
		return LoginResult{}, err
	}
	return login_ResultFor(account, token), nil
}

func login_ResultFor(account Account, token string) LoginResult {
	return LoginResult{
		Token:       token,
		UserID:      account.UserID,
		Login:       account.Login,
		DisplayName: account.DisplayName,
		AccessLevel: account.AccessLevel,
		Pending:     account.Pending,
	}
}

func (store *LiveStore) Delete_AuthToken(ctx context.Context, token string) error {
	tag, err := store.pool.Exec(ctx, `DELETE FROM auth_tokens WHERE token = $1`, token)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrUnauthorized
	}
	return nil
}

// Учётная запись по токену для всех методов, кроме профиля. Ожидающий подтверждения
// пользователь получает отказ, как в open-webui: ему доступен только экран ожидания
func (store *LiveStore) Find_AccountByToken(ctx context.Context, token string) (Account, error) {
	account, err := store.Find_ProfileByToken(ctx, token)
	if err != nil {
		return Account{}, err
	}
	if account.Pending {
		return Account{}, ErrAccountPending
	}
	return account, nil
}

// Учётная запись по токену без проверки ожидания: для /me и контакта администратора
func (store *LiveStore) Find_ProfileByToken(ctx context.Context, token string) (Account, error) {
	account, err := scan_Account(store.pool.QueryRow(ctx, `
		SELECT u.user_id, u.login, u.display_name, u.access_level, u.pending, u.created_at
		FROM auth_tokens t
		JOIN users u ON u.user_id = t.user_id
		WHERE t.token = $1`, token))
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrUnauthorized
	}
	if err != nil {
		return Account{}, err
	}
	if err := store.attach_Grants(ctx, &account); err != nil {
		return Account{}, err
	}
	return account, nil
}

func (store *LiveStore) List_Accounts(ctx context.Context, token string) ([]Account, error) {
	if _, err := store.require_Permission(ctx, token, texts.PermissionUsersManage); err != nil {
		return nil, err
	}
	rows, err := store.pool.Query(ctx, `SELECT `+accountColumns+` FROM users ORDER BY created_at, user_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	accounts := []Account{}
	for rows.Next() {
		account, err := scan_Account(rows)
		if err != nil {
			return nil, err
		}
		if err := store.attach_Grants(ctx, &account); err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}
	return accounts, rows.Err()
}

func (store *LiveStore) Create_Account(ctx context.Context, token, loginName, password, displayName string, accessLevel int, pending bool) (Account, error) {
	if _, err := store.require_Permission(ctx, token, texts.PermissionUsersManage); err != nil {
		return Account{}, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return Account{}, err
	}
	var account Account
	err = store.run_AccountsTransaction(ctx, func(tx pgx.Tx, usersBefore int) error {
		var insertError error
		account, insertError = insert_Account(ctx, tx, loginName, string(hash), displayName, accessLevel, pending)
		if insertError != nil {
			return insertError
		}
		return store.assign_NewAccountRole(ctx, tx, account.UserID, usersBefore, accessLevel, pending)
	})
	if err != nil {
		return Account{}, err
	}
	if err := store.attach_Grants(ctx, &account); err != nil {
		return Account{}, err
	}
	return account, nil
}

func (store *LiveStore) Patch_Account(ctx context.Context, token, userID string, patch AccountPatch) (Account, error) {
	actor, err := store.require_Permission(ctx, token, texts.PermissionUsersManage)
	if err != nil {
		return Account{}, err
	}
	if patch.DisplayName == nil && patch.Login == nil && patch.Password == nil && patch.AccessLevel == nil && patch.Pending == nil && patch.RoleIDs == nil {
		return Account{}, ErrNotFound
	}
	account, err := store.find_AccountByID(ctx, userID)
	if err != nil {
		return Account{}, err
	}
	// Свою роль администратор не меняет: так в системе не останется без администратора
	if actor.UserID == account.UserID && check_RoleChanges(account, patch) {
		return Account{}, ErrAccessDenied
	}
	levelChanges := patch.AccessLevel != nil && *patch.AccessLevel != account.AccessLevel
	pendingChanges := patch.Pending != nil && *patch.Pending != account.Pending
	if patch.DisplayName != nil {
		account.DisplayName = strings.TrimSpace(*patch.DisplayName)
	}
	if patch.Login != nil {
		account.Login = strings.TrimSpace(*patch.Login)
		taken, err := check_LoginTaken(ctx, store.pool, account.Login, account.UserID)
		if err != nil {
			return Account{}, err
		}
		if taken {
			return Account{}, ErrLoginTaken
		}
	}
	if patch.AccessLevel != nil {
		account.AccessLevel = *patch.AccessLevel
	}
	if patch.Pending != nil {
		account.Pending = *patch.Pending
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return Account{}, err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `
		UPDATE users SET display_name = $2, login = $3, access_level = $4, pending = $5 WHERE user_id = $1`,
		account.UserID, account.DisplayName, account.Login, account.AccessLevel, account.Pending)
	if is_UniqueViolation(err) {
		return Account{}, ErrLoginTaken
	}
	if err != nil {
		return Account{}, err
	}
	if patch.Password != nil {
		hash, err := bcrypt.GenerateFromPassword([]byte(*patch.Password), bcrypt.DefaultCost)
		if err != nil {
			return Account{}, err
		}
		if _, err := tx.Exec(ctx, `UPDATE users SET password_hash = $2 WHERE user_id = $1`, account.UserID, string(hash)); err != nil {
			return Account{}, err
		}
	}
	if patch.RoleIDs != nil || levelChanges || pendingChanges {
		next := []string{auth.RoleID_ForNewAccount(1, account.AccessLevel, account.Pending)}
		if patch.RoleIDs != nil {
			next = *patch.RoleIDs
		}
		if err := store.replace_UserRoles(ctx, tx, actor.UserID, account.UserID, next); err != nil {
			return Account{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Account{}, err
	}
	updated, err := store.find_AccountByID(ctx, account.UserID)
	if err != nil {
		return Account{}, err
	}
	if err := store.attach_Grants(ctx, &updated); err != nil {
		return Account{}, err
	}
	return updated, nil
}

// Удаление учетной записи вместе с тем, что принадлежит только ей: токены, сессии,
// вложения, черновики и личные документы. Общие документы и журнал вопросов остаются.
// Себя не удалить, главного администратора удаляет только обладатель roles.manage,
// последнего главного администратора не удалить никому
func (store *LiveStore) Delete_Account(ctx context.Context, token, userID string) error {
	actor, err := store.require_Permission(ctx, token, texts.PermissionUsersManage)
	if err != nil {
		return err
	}
	if actor.UserID == userID {
		return ErrAccessDenied
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Под той же блокировкой, что и создание: две параллельные операции не оставят систему без главного администратора
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, accountsAdvisoryLock); err != nil {
		return err
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE user_id = $1)`, userID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	roleIDs, err := list_UserRoleIDs(ctx, tx, userID)
	if err != nil {
		return err
	}
	if contains_ID(roleIDs, texts.RoleIDChief) {
		if !actor.Has(texts.PermissionRolesManage) {
			return ErrAccessDenied
		}
		var otherChiefs int
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FROM user_roles WHERE role_id = $1 AND user_id <> $2`, texts.RoleIDChief, userID).Scan(&otherChiefs); err != nil {
			return err
		}
		if otherChiefs == 0 {
			return ErrAccessDenied
		}
	}
	// Порядок важен: сообщения и файлы держат внешние ключи на сессии и пользователя.
	// Фрагменты и роли личных документов уходят каскадом
	cleanup := []string{
		`DELETE FROM auth_tokens WHERE user_id = $1`,
		`DELETE FROM session_messages WHERE session_id IN (SELECT session_id FROM sessions WHERE owner_user_id = $1)`,
		`DELETE FROM session_attachments WHERE owner_user_id = $1`,
		`DELETE FROM draft_files WHERE owner_user_id = $1`,
		`DELETE FROM sessions WHERE owner_user_id = $1`,
		`DELETE FROM documents WHERE scope = '` + texts.ScopePersonal + `' AND owner_id = $1`,
	}
	for _, statement := range cleanup {
		if _, err := tx.Exec(ctx, statement, userID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM users WHERE user_id = $1`, userID); err != nil {
		return err
	}
	if err := insert_PermissionEvent(ctx, tx, actor.UserID, texts.PermissionActionDeleteUser, "", userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func check_RoleChanges(account Account, patch AccountPatch) bool {
	if patch.RoleIDs != nil {
		return true
	}
	if patch.AccessLevel != nil && *patch.AccessLevel != account.AccessLevel {
		return true
	}
	return patch.Pending != nil && *patch.Pending != account.Pending
}

func (store *LiveStore) require_Admin(ctx context.Context, token string) (Account, error) {
	return store.require_Permission(ctx, token, texts.PermissionUsersManage)
}

func (store *LiveStore) find_AccountByID(ctx context.Context, userID string) (Account, error) {
	account, err := scan_Account(store.pool.QueryRow(ctx, `SELECT `+accountColumns+` FROM users WHERE user_id = $1`, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	return account, err
}

// Транзакция под блокировкой создания учётных записей. usersBefore - сколько пользователей было до неё
func (store *LiveStore) run_AccountsTransaction(ctx context.Context, work func(tx pgx.Tx, usersBefore int) error) error {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, accountsAdvisoryLock); err != nil {
		return err
	}
	var usersBefore int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&usersBefore); err != nil {
		return err
	}
	if err := work(tx, usersBefore); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func insert_Account(ctx context.Context, tx pgx.Tx, loginName, passwordHash, displayName string, accessLevel int, pending bool) (Account, error) {
	taken, err := check_LoginTaken(ctx, tx, loginName, "")
	if err != nil {
		return Account{}, err
	}
	if taken {
		return Account{}, ErrLoginTaken
	}
	number, err := next_IDWith(ctx, tx, counterUser)
	if err != nil {
		return Account{}, err
	}
	userID := "user-" + strconv.Itoa(number)
	account, err := scan_Account(tx.QueryRow(ctx, `
		INSERT INTO users (user_id, login, password_hash, display_name, access_level, pending)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING `+accountColumns,
		userID, loginName, passwordHash, displayName, accessLevel, pending))
	if is_UniqueViolation(err) {
		return Account{}, ErrLoginTaken
	}
	return account, err
}

// Логин занят другой учётной записью, без учёта регистра
func check_LoginTaken(ctx context.Context, querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}, loginName, exceptUserID string) (bool, error) {
	var taken bool
	err := querier.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM users WHERE lower(login) = lower($1) AND user_id <> $2)`, loginName, exceptUserID).Scan(&taken)
	return taken, err
}

func insert_AuthToken(ctx context.Context, tx pgx.Tx, token, userID string) error {
	_, err := tx.Exec(ctx, `INSERT INTO auth_tokens (token, user_id) VALUES ($1, $2)`, token, userID)
	return err
}

// Пароль доменной учётной записи живёт только в каталоге. Локально - случайный хеш, как в open-webui
func create_UnusablePasswordHash() (string, error) {
	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(hex.EncodeToString(randomBytes)), bcrypt.DefaultCost)
	return string(hash), err
}

func is_UniqueViolation(err error) bool {
	var pgError *pgconn.PgError
	return errors.As(err, &pgError) && pgError.Code == "23505"
}
