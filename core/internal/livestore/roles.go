package livestore

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"docfinder/core/internal/auth"
	"docfinder/core/internal/texts"
)

const (
	counterRole            = "role"
	counterPermissionEvent = "permission_event"
	settingsKeyRolesMoved  = "roles_migrated"
)

var ErrRoleInUse = errors.New("role in use")

type Role struct {
	RoleID      string
	Title       string
	Description string
	System      bool
	LdapGroup   string
	Permissions []string
	UserCount   int
}

type PermissionEvent struct {
	EventID      string
	ActorUserID  string
	ActorName    string
	Action       string
	RoleID       string
	TargetUserID string
	CreatedAt    time.Time
}

type RoleInput struct {
	Title       string
	Description string
	LdapGroup   string
	Permissions []string
}

func (account Account) Has(permission string) bool {
	return auth.Has_Permission(account.Permissions, permission)
}

func (store *LiveStore) seed_Roles(ctx context.Context) error {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, role := range auth.Seed_Roles() {
		tag, err := tx.Exec(ctx, `
			INSERT INTO roles (role_id, title, description, system)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (role_id) DO NOTHING`, role.ID, role.Title, role.Description, role.System)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			continue
		}
		for _, permission := range role.Permissions {
			if _, err := tx.Exec(ctx, `
				INSERT INTO role_permissions (role_id, permission) VALUES ($1, $2)
				ON CONFLICT DO NOTHING`, role.ID, permission); err != nil {
				return err
			}
		}
	}
	var migrated bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM app_settings WHERE key = $1)`, settingsKeyRolesMoved).Scan(&migrated); err != nil {
		return err
	}
	if !migrated {
		if err := migrate_LegacyAccess(ctx, tx); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO app_settings (key, value) VALUES ($1, 'true'::jsonb) ON CONFLICT (key) DO NOTHING`, settingsKeyRolesMoved); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// Старый уровень переносится один раз: первая учетная запись становится главным администратором
func migrate_LegacyAccess(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO user_roles (user_id, role_id)
		SELECT user_id, $1 FROM users
		WHERE NOT EXISTS (SELECT 1 FROM user_roles WHERE role_id = $1)
		ORDER BY created_at, user_id
		LIMIT 1
		ON CONFLICT DO NOTHING`, texts.RoleIDChief); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO user_roles (user_id, role_id)
		SELECT u.user_id, CASE WHEN u.access_level >= $1 THEN $2 ELSE $3 END
		FROM users u
		WHERE NOT EXISTS (SELECT 1 FROM user_roles ur WHERE ur.user_id = u.user_id)
		ON CONFLICT DO NOTHING`, texts.SeedAdminAccessLevel, texts.RoleIDAdmin, texts.RoleIDUser); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE users u SET access_level = CASE
			WHEN EXISTS (
				SELECT 1 FROM user_roles ur
				JOIN role_permissions rp ON rp.role_id = ur.role_id
				WHERE ur.user_id = u.user_id AND rp.permission IN ($1, $2)
			) THEN $3::int ELSE $4::int END`,
		texts.PermissionUsersManage, texts.PermissionRolesManage, texts.SeedAdminAccessLevel, texts.SeedEmployeeAccessLevel); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE documents
		SET visible_to_all = (scope = 'global' AND access_level < $1::int)`, texts.SeedAdminAccessLevel); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO document_roles (document_id, role_id)
		SELECT d.document_id, r.role_id
		FROM documents d
		CROSS JOIN (VALUES ($1::text), ($2::text)) AS r(role_id)
		WHERE d.scope = 'global' AND NOT d.visible_to_all
		ON CONFLICT DO NOTHING`, texts.RoleIDAdmin, texts.RoleIDChief)
	return err
}

func (store *LiveStore) attach_Grants(ctx context.Context, account *Account) error {
	rows, err := store.pool.Query(ctx, `
		SELECT r.role_id, r.title
		FROM user_roles ur
		JOIN roles r ON r.role_id = ur.role_id
		WHERE ur.user_id = $1
		ORDER BY r.role_id`, account.UserID)
	if err != nil {
		return err
	}
	defer rows.Close()
	roleIDs := []string{}
	titles := []string{}
	for rows.Next() {
		var roleID, title string
		if err := rows.Scan(&roleID, &title); err != nil {
			return err
		}
		roleIDs = append(roleIDs, roleID)
		titles = append(titles, title)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	permissionRows, err := store.pool.Query(ctx, `
		SELECT DISTINCT rp.permission
		FROM role_permissions rp
		JOIN user_roles ur ON ur.role_id = rp.role_id
		WHERE ur.user_id = $1
		ORDER BY rp.permission`, account.UserID)
	if err != nil {
		return err
	}
	defer permissionRows.Close()
	permissions := []string{}
	for permissionRows.Next() {
		var permission string
		if err := permissionRows.Scan(&permission); err != nil {
			return err
		}
		permissions = append(permissions, permission)
	}
	if err := permissionRows.Err(); err != nil {
		return err
	}
	account.RoleIDs = roleIDs
	account.RoleTitles = titles
	account.Permissions = permissions
	if len(roleIDs) > 0 {
		account.AccessLevel = auth.AccessLevel_ForPermissions(permissions)
	}
	return nil
}

func (store *LiveStore) require_Permission(ctx context.Context, token, permission string) (Account, error) {
	account, err := store.Find_AccountByToken(ctx, token)
	if err != nil {
		return Account{}, err
	}
	if !account.Has(permission) {
		return Account{}, ErrAccessDenied
	}
	return account, nil
}

func (store *LiveStore) List_Roles(ctx context.Context, token string) ([]Role, error) {
	if _, err := store.require_Permission(ctx, token, texts.PermissionRolesManage); err != nil {
		return nil, err
	}
	return store.query_Roles(ctx, "")
}

func (store *LiveStore) Create_Role(ctx context.Context, token string, input RoleInput) (Role, error) {
	actor, err := store.require_Permission(ctx, token, texts.PermissionRolesManage)
	if err != nil {
		return Role{}, err
	}
	title := strings.TrimSpace(input.Title)
	permissions, ok := normalize_Permissions(input.Permissions)
	if title == "" || !ok {
		return Role{}, ErrInvalidBody
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return Role{}, err
	}
	defer tx.Rollback(ctx)
	number, err := next_IDWith(ctx, tx, counterRole)
	if err != nil {
		return Role{}, err
	}
	roleID := "role-" + strconv.Itoa(number)
	if _, err := tx.Exec(ctx, `
		INSERT INTO roles (role_id, title, description, system, ldap_group) VALUES ($1, $2, $3, false, $4)`,
		roleID, title, strings.TrimSpace(input.Description), strings.TrimSpace(input.LdapGroup)); err != nil {
		return Role{}, err
	}
	if err := replace_RolePermissions(ctx, tx, roleID, permissions); err != nil {
		return Role{}, err
	}
	if err := insert_PermissionEvent(ctx, tx, actor.UserID, texts.PermissionActionUpdate, roleID, ""); err != nil {
		return Role{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Role{}, err
	}
	found, err := store.query_Roles(ctx, roleID)
	if err != nil || len(found) == 0 {
		return Role{}, err
	}
	return found[0], nil
}

func (store *LiveStore) Update_Role(ctx context.Context, token, roleID string, input RoleInput) (Role, error) {
	actor, err := store.require_Permission(ctx, token, texts.PermissionRolesManage)
	if err != nil {
		return Role{}, err
	}
	title := strings.TrimSpace(input.Title)
	permissions, ok := normalize_Permissions(input.Permissions)
	if title == "" || !ok {
		return Role{}, ErrInvalidBody
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return Role{}, err
	}
	defer tx.Rollback(ctx)
	var system bool
	err = tx.QueryRow(ctx, `SELECT system FROM roles WHERE role_id = $1`, roleID).Scan(&system)
	if errors.Is(err, pgx.ErrNoRows) {
		return Role{}, ErrNotFound
	}
	if err != nil {
		return Role{}, err
	}
	if system {
		return Role{}, ErrAccessDenied
	}
	if _, err := tx.Exec(ctx, `UPDATE roles SET title = $2, description = $3, ldap_group = $4 WHERE role_id = $1`, roleID, title, strings.TrimSpace(input.Description), strings.TrimSpace(input.LdapGroup)); err != nil {
		return Role{}, err
	}
	if err := replace_RolePermissions(ctx, tx, roleID, permissions); err != nil {
		return Role{}, err
	}
	if err := insert_PermissionEvent(ctx, tx, actor.UserID, texts.PermissionActionUpdate, roleID, ""); err != nil {
		return Role{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Role{}, err
	}
	found, err := store.query_Roles(ctx, roleID)
	if err != nil || len(found) == 0 {
		return Role{}, err
	}
	return found[0], nil
}

func (store *LiveStore) Delete_Role(ctx context.Context, token, roleID string) error {
	if _, err := store.require_Permission(ctx, token, texts.PermissionRolesManage); err != nil {
		return err
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var system bool
	err = tx.QueryRow(ctx, `SELECT system FROM roles WHERE role_id = $1`, roleID).Scan(&system)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if system {
		return ErrAccessDenied
	}
	var users int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM user_roles WHERE role_id = $1`, roleID).Scan(&users); err != nil {
		return err
	}
	if users > 0 {
		return ErrRoleInUse
	}
	if _, err := tx.Exec(ctx, `DELETE FROM roles WHERE role_id = $1`, roleID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (store *LiveStore) List_PermissionEvents(ctx context.Context, token string) ([]PermissionEvent, error) {
	if _, err := store.require_Permission(ctx, token, texts.PermissionAuditView); err != nil {
		return nil, err
	}
	rows, err := store.pool.Query(ctx, `
		SELECT e.event_id, e.actor_user_id, COALESCE(u.display_name, ''), e.action, e.role_id, e.target_user_id, e.created_at
		FROM permission_events e
		LEFT JOIN users u ON u.user_id = e.actor_user_id
		ORDER BY e.created_at DESC, e.event_id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []PermissionEvent{}
	for rows.Next() {
		var event PermissionEvent
		if err := rows.Scan(&event.EventID, &event.ActorUserID, &event.ActorName, &event.Action, &event.RoleID, &event.TargetUserID, &event.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (store *LiveStore) query_Roles(ctx context.Context, roleID string) ([]Role, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT r.role_id, r.title, r.description, r.system, r.ldap_group,
			COALESCE((SELECT array_agg(rp.permission ORDER BY rp.permission) FROM role_permissions rp WHERE rp.role_id = r.role_id), '{}'),
			(SELECT count(*) FROM user_roles ur WHERE ur.role_id = r.role_id)
		FROM roles r
		WHERE ($1 = '' OR r.role_id = $1)
		ORDER BY r.role_id`, roleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	found := []Role{}
	for rows.Next() {
		var role Role
		if err := rows.Scan(&role.RoleID, &role.Title, &role.Description, &role.System, &role.LdapGroup, &role.Permissions, &role.UserCount); err != nil {
			return nil, err
		}
		if role.Permissions == nil {
			role.Permissions = []string{}
		}
		found = append(found, role)
	}
	return found, rows.Err()
}

func (store *LiveStore) replace_UserRoles(ctx context.Context, tx pgx.Tx, actorID, userID string, next []string) error {
	next = auth.Unique_IDs(next)
	if err := check_RoleIDs(ctx, tx, next); err != nil {
		return err
	}
	old, err := list_UserRoleIDs(ctx, tx, userID)
	if err != nil {
		return err
	}
	keepsChief := contains_ID(next, texts.RoleIDChief)
	var otherChiefs int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM user_roles WHERE role_id = $1 AND user_id <> $2`, texts.RoleIDChief, userID).Scan(&otherChiefs); err != nil {
		return err
	}
	if !keepsChief && otherChiefs == 0 && contains_ID(old, texts.RoleIDChief) {
		return ErrAccessDenied
	}
	if _, err := tx.Exec(ctx, `DELETE FROM user_roles WHERE user_id = $1`, userID); err != nil {
		return err
	}
	for _, roleID := range next {
		if _, err := tx.Exec(ctx, `INSERT INTO user_roles (user_id, role_id) VALUES ($1, $2)`, userID, roleID); err != nil {
			return err
		}
	}
	for _, roleID := range next {
		if !contains_ID(old, roleID) {
			if err := insert_PermissionEvent(ctx, tx, actorID, texts.PermissionActionAssign, roleID, userID); err != nil {
				return err
			}
		}
	}
	for _, roleID := range old {
		if !contains_ID(next, roleID) {
			if err := insert_PermissionEvent(ctx, tx, actorID, texts.PermissionActionRevoke, roleID, userID); err != nil {
				return err
			}
		}
	}
	permissions, err := permissions_ForRoles(ctx, tx, next)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE users SET access_level = $2::int WHERE user_id = $1`, userID, auth.AccessLevel_ForPermissions(permissions))
	return err
}

func (store *LiveStore) sync_GroupRoles(ctx context.Context, tx pgx.Tx, userID string, groups []string, usersBefore int, signup auth.SignupSettings) error {
	rows, err := tx.Query(ctx, `SELECT role_id, ldap_group FROM roles WHERE ldap_group <> ''`)
	if err != nil {
		return err
	}
	defer rows.Close()
	mapped := []auth.GroupRole{}
	for rows.Next() {
		var role auth.GroupRole
		if err := rows.Scan(&role.RoleID, &role.Group); err != nil {
			return err
		}
		mapped = append(mapped, role)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	current, err := list_UserRoleIDs(ctx, tx, userID)
	if err != nil {
		return err
	}
	roleName := role_ForNewAccount(usersBefore, signup)
	accessLevel, pending := auth.Access_ForRole(roleName)
	next := auth.RoleIDs_ForGroups(groups, mapped, current, auth.RoleID_ForNewAccount(usersBefore, accessLevel, pending))
	if err := store.replace_UserRoles(ctx, tx, userID, userID, next); err != nil {
		return err
	}
	if _, matched := auth.Matched_GroupRole(groups, mapped); matched {
		_, err = tx.Exec(ctx, `UPDATE users SET pending = false WHERE user_id = $1`, userID)
	}
	return err
}

func (store *LiveStore) assign_NewAccountRole(ctx context.Context, tx pgx.Tx, userID string, usersBefore, accessLevel int, pending bool) error {
	roleID := auth.RoleID_ForNewAccount(usersBefore, accessLevel, pending)
	if _, err := tx.Exec(ctx, `INSERT INTO user_roles (user_id, role_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, userID, roleID); err != nil {
		return err
	}
	return insert_PermissionEvent(ctx, tx, userID, texts.PermissionActionAssign, roleID, userID)
}

func list_UserRoleIDs(ctx context.Context, tx pgx.Tx, userID string) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT role_id FROM user_roles WHERE user_id = $1 ORDER BY role_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var roleID string
		if err := rows.Scan(&roleID); err != nil {
			return nil, err
		}
		ids = append(ids, roleID)
	}
	return ids, rows.Err()
}

func permissions_ForRoles(ctx context.Context, tx pgx.Tx, roleIDs []string) ([]string, error) {
	if len(roleIDs) == 0 {
		return []string{}, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT DISTINCT permission FROM role_permissions
		WHERE role_id = ANY($1::text[])
		ORDER BY permission`, roleIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	permissions := []string{}
	for rows.Next() {
		var permission string
		if err := rows.Scan(&permission); err != nil {
			return nil, err
		}
		permissions = append(permissions, permission)
	}
	return permissions, rows.Err()
}

func check_RoleIDs(ctx context.Context, tx pgx.Tx, roleIDs []string) error {
	for _, roleID := range roleIDs {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM roles WHERE role_id = $1)`, roleID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrInvalidBody
		}
	}
	return nil
}

func replace_RolePermissions(ctx context.Context, tx pgx.Tx, roleID string, permissions []string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM role_permissions WHERE role_id = $1`, roleID); err != nil {
		return err
	}
	for _, permission := range permissions {
		if _, err := tx.Exec(ctx, `INSERT INTO role_permissions (role_id, permission) VALUES ($1, $2)`, roleID, permission); err != nil {
			return err
		}
	}
	return nil
}

func insert_PermissionEvent(ctx context.Context, tx pgx.Tx, actorID, action, roleID, targetUserID string) error {
	number, err := next_IDWith(ctx, tx, counterPermissionEvent)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO permission_events (event_id, actor_user_id, action, role_id, target_user_id)
		VALUES ($1, $2, $3, $4, $5)`,
		"event-"+strconv.Itoa(number), actorID, action, roleID, targetUserID)
	return err
}

func normalize_Permissions(raw []string) ([]string, bool) {
	permissions := auth.Unique_IDs(raw)
	for _, permission := range permissions {
		if !auth.Known_Permission(permission) {
			return nil, false
		}
	}
	return permissions, true
}

func contains_ID(ids []string, wanted string) bool {
	for _, id := range ids {
		if id == wanted {
			return true
		}
	}
	return false
}

func role_Filter(roleIDs []string) []string {
	if roleIDs == nil {
		return []string{}
	}
	return roleIDs
}
