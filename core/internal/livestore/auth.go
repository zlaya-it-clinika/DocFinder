package livestore

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"

	"docfinder/core/internal/auth"
	"docfinder/core/internal/texts"
)

// Разделы таблицы app_settings
const (
	settingsKeySignup = "signup"
	settingsKeyLdap   = "ldap"
)

// Что видит экран входа до входа: нужна ли первая регистрация и какие способы входа включены
type PublicAuthConfig struct {
	Onboarding   bool
	EnableSignup bool
	EnableLdap   bool
	LdapLabel    string
}

// Контакт администратора для экрана ожидания
type AdminDetails struct {
	Name  string
	Email string
}

func (store *LiveStore) Read_PublicAuthConfig(ctx context.Context) (PublicAuthConfig, error) {
	var usersCount int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&usersCount); err != nil {
		return PublicAuthConfig{}, err
	}
	signup, err := store.read_SignupSettings(ctx)
	if err != nil {
		return PublicAuthConfig{}, err
	}
	ldapSettings, err := store.read_LdapSettings(ctx)
	if err != nil {
		return PublicAuthConfig{}, err
	}
	return PublicAuthConfig{
		Onboarding:   usersCount == 0,
		EnableSignup: signup.EnableSignup,
		EnableLdap:   ldapSettings.EnableLdap,
		LdapLabel:    ldapSettings.Label,
	}, nil
}

// Регистрация по почте. Первый пользователь становится администратором даже при выключенной
// регистрации, остальные получают роль по умолчанию
func (store *LiveStore) Signup_User(ctx context.Context, email, password, displayName, token string) (LoginResult, error) {
	signup, err := store.read_SignupSettings(ctx)
	if err != nil {
		return LoginResult{}, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return LoginResult{}, err
	}
	var result LoginResult
	err = store.run_AccountsTransaction(ctx, func(tx pgx.Tx, usersBefore int) error {
		if usersBefore > 0 && !signup.EnableSignup {
			return ErrSignupDisabled
		}
		accessLevel, pending := auth.Access_ForRole(role_ForNewAccount(usersBefore, signup))
		account, err := insert_Account(ctx, tx, email, string(hash), displayName, accessLevel, pending)
		if err != nil {
			return err
		}
		if err := store.assign_NewAccountRole(ctx, tx, account.UserID, usersBefore, accessLevel, pending); err != nil {
			return err
		}
		if err := insert_AuthToken(ctx, tx, token, account.UserID); err != nil {
			return err
		}
		result = login_ResultFor(account, token)
		return nil
	})
	return result, err
}

// Вход доменной учётной записью. Пользователь каталога привязывается к локальному по почте.
// Если локального ещё нет, он создаётся с ролью по умолчанию. Флаг регистрации здесь не действует,
// как в open-webui: доступ ограничивают фильтры LDAP
func (store *LiveStore) Login_LdapUser(ctx context.Context, username, password, token string) (LoginResult, error) {
	ldapSettings, err := store.read_LdapSettings(ctx)
	if err != nil {
		return LoginResult{}, err
	}
	if !ldapSettings.EnableLdap {
		return LoginResult{}, ErrLdapDisabled
	}
	identity, err := auth.Authenticate_LdapUser(ldapSettings, username, password)
	if errors.Is(err, auth.ErrLdapInvalidCredentials) {
		return LoginResult{}, ErrInvalidCredentials
	}
	if err != nil {
		return LoginResult{}, err
	}
	return store.login_DirectoryIdentity(ctx, ldapSettings, identity, token)
}

func (store *LiveStore) Kerberos_IsEnabled(ctx context.Context) (bool, error) {
	ldapSettings, err := store.read_LdapSettings(ctx)
	if err != nil {
		return false, err
	}
	return ldapSettings.EnableKerberos, nil
}

func (store *LiveStore) Login_KerberosUser(ctx context.Context, negotiate []byte, token string) (LoginResult, error) {
	ldapSettings, err := store.read_LdapSettings(ctx)
	if err != nil {
		return LoginResult{}, err
	}
	if !ldapSettings.EnableKerberos {
		return LoginResult{}, ErrKerberosDisabled
	}
	username, err := auth.Accept_NegotiateToken(ldapSettings.KeytabPath, ldapSettings.ServicePrincipal, negotiate)
	if err != nil {
		return LoginResult{}, ErrInvalidCredentials
	}
	if ldapSettings.EnableLdap {
		identity, lookupError := auth.Lookup_LdapUser(ldapSettings, username)
		if errors.Is(lookupError, auth.ErrLdapInvalidCredentials) {
			return LoginResult{}, ErrInvalidCredentials
		}
		if lookupError != nil {
			return LoginResult{}, lookupError
		}
		return store.login_DirectoryIdentity(ctx, ldapSettings, identity, token)
	}
	return store.login_ExistingLogin(ctx, username, token)
}

func (store *LiveStore) login_DirectoryIdentity(ctx context.Context, ldapSettings auth.LdapSettings, identity auth.LdapIdentity, token string) (LoginResult, error) {
	signup, err := store.read_SignupSettings(ctx)
	if err != nil {
		return LoginResult{}, err
	}
	var result LoginResult
	err = store.run_AccountsTransaction(ctx, func(tx pgx.Tx, usersBefore int) error {
		var account Account
		var directoryAccount bool
		err := tx.QueryRow(ctx, `
			SELECT `+accountColumns+`, directory_account FROM users WHERE lower(login) = lower($1)
			ORDER BY created_at LIMIT 1`, identity.Email).
			Scan(&account.UserID, &account.Login, &account.DisplayName, &account.AccessLevel, &account.Pending, &account.CreatedAt, &directoryAccount)
		// Локальную запись с той же почтой каталог не забирает: иначе пользователь каталога, сменивший себе mail,
		// вошел бы под чужой учетной записью, а заранее зарегистрированная запись досталась бы ее создателю
		if err == nil && !directoryAccount {
			return ErrDirectoryAccountConflict
		}
		if errors.Is(err, pgx.ErrNoRows) {
			passwordHash, hashError := create_UnusablePasswordHash()
			if hashError != nil {
				return hashError
			}
			accessLevel, pending := auth.Access_ForRole(role_ForNewAccount(usersBefore, signup))
			account, err = insert_Account(ctx, tx, identity.Email, passwordHash, identity.Name, accessLevel, pending)
			if err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `UPDATE users SET directory_account = true WHERE user_id = $1`, account.UserID); err != nil {
				return err
			}
			if err = store.assign_NewAccountRole(ctx, tx, account.UserID, usersBefore, accessLevel, pending); err != nil {
				return err
			}
		}
		if err != nil {
			return err
		}
		if ldapSettings.EnableGroupManagement {
			if err = store.sync_GroupRoles(ctx, tx, account.UserID, identity.Groups, usersBefore, signup); err != nil {
				return err
			}
			account, err = scan_Account(tx.QueryRow(ctx, `SELECT `+accountColumns+` FROM users WHERE user_id = $1`, account.UserID))
			if err != nil {
				return err
			}
		}
		if err := insert_AuthToken(ctx, tx, token, account.UserID); err != nil {
			return err
		}
		result = login_ResultFor(account, token)
		return nil
	})
	return result, err
}

func (store *LiveStore) login_ExistingLogin(ctx context.Context, username, token string) (LoginResult, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return LoginResult{}, ErrInvalidCredentials
	}
	var result LoginResult
	err := store.run_AccountsTransaction(ctx, func(tx pgx.Tx, usersBefore int) error {
		account, err := scan_Account(tx.QueryRow(ctx, `
			SELECT `+accountColumns+` FROM users WHERE lower(login) = lower($1)
			ORDER BY created_at LIMIT 1`, username))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalidCredentials
		}
		if err != nil {
			return err
		}
		if err = insert_AuthToken(ctx, tx, token, account.UserID); err != nil {
			return err
		}
		result = login_ResultFor(account, token)
		return nil
	})
	return result, err
}

func role_ForNewAccount(usersBefore int, signup auth.SignupSettings) string {
	if usersBefore == 0 {
		return texts.RoleAdmin
	}
	if !auth.Check_IsKnownRole(signup.DefaultUserRole) {
		return texts.RolePending
	}
	return signup.DefaultUserRole
}

// Первый по времени администратор. Выключено в настройках - ErrNotFound
func (store *LiveStore) Read_AdminDetails(ctx context.Context, token string) (AdminDetails, error) {
	if _, err := store.Find_ProfileByToken(ctx, token); err != nil {
		return AdminDetails{}, err
	}
	signup, err := store.read_SignupSettings(ctx)
	if err != nil {
		return AdminDetails{}, err
	}
	if !signup.ShowAdminDetails {
		return AdminDetails{}, ErrNotFound
	}
	var details AdminDetails
	err = store.pool.QueryRow(ctx, `
		SELECT display_name, login FROM users
		WHERE access_level >= $1 AND NOT pending
		ORDER BY created_at, user_id LIMIT 1`, texts.SeedAdminAccessLevel).Scan(&details.Name, &details.Email)
	if errors.Is(err, pgx.ErrNoRows) {
		return AdminDetails{}, ErrNotFound
	}
	return details, err
}

func (store *LiveStore) Read_SignupSettings(ctx context.Context, token string) (auth.SignupSettings, error) {
	if _, err := store.require_Admin(ctx, token); err != nil {
		return auth.SignupSettings{}, err
	}
	return store.read_SignupSettings(ctx)
}

func (store *LiveStore) Save_SignupSettings(ctx context.Context, token string, settings auth.SignupSettings) (auth.SignupSettings, error) {
	if _, err := store.require_Admin(ctx, token); err != nil {
		return auth.SignupSettings{}, err
	}
	if err := store.write_Setting(ctx, settingsKeySignup, settings); err != nil {
		return auth.SignupSettings{}, err
	}
	return settings, nil
}

func (store *LiveStore) Read_LdapSettings(ctx context.Context, token string) (auth.LdapSettings, error) {
	if _, err := store.require_Admin(ctx, token); err != nil {
		return auth.LdapSettings{}, err
	}
	return store.read_LdapSettings(ctx)
}

func (store *LiveStore) Save_LdapSettings(ctx context.Context, token string, settings auth.LdapSettings) (auth.LdapSettings, error) {
	if _, err := store.require_Admin(ctx, token); err != nil {
		return auth.LdapSettings{}, err
	}
	if err := store.write_Setting(ctx, settingsKeyLdap, settings); err != nil {
		return auth.LdapSettings{}, err
	}
	return settings, nil
}

// Сохранённое значение ложится поверх значений из .env, так новые поля получают начальное значение
func (store *LiveStore) read_SignupSettings(ctx context.Context) (auth.SignupSettings, error) {
	settings := store.authDefaults.Signup
	err := store.read_Setting(ctx, settingsKeySignup, &settings)
	return settings, err
}

func (store *LiveStore) read_LdapSettings(ctx context.Context) (auth.LdapSettings, error) {
	settings := store.authDefaults.Ldap
	err := store.read_Setting(ctx, settingsKeyLdap, &settings)
	return settings, err
}

func (store *LiveStore) read_Setting(ctx context.Context, key string, target any) error {
	var raw []byte
	err := store.pool.QueryRow(ctx, `SELECT value FROM app_settings WHERE key = $1`, key).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, target)
}

func (store *LiveStore) write_Setting(ctx context.Context, key string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = store.pool.Exec(ctx, `
		INSERT INTO app_settings (key, value, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`, key, raw)
	return err
}
