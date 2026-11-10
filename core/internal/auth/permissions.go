package auth

import "docfinder/core/internal/texts"

type SeedRole struct {
	ID          string
	Title       string
	Description string
	System      bool
	Permissions []string
}

type DocumentAudience struct {
	VisibleToAll bool
	RoleIDs      []string
	AccessLevel  int
}

func Permission_IDs() []string {
	catalog := texts.Permission_Catalog()
	ids := make([]string, 0, len(catalog))
	for _, item := range catalog {
		ids = append(ids, item.Permission)
	}
	return ids
}

func Admin_PermissionIDs() []string {
	ids := make([]string, 0, len(texts.Permission_Catalog()))
	for _, item := range texts.Permission_Catalog() {
		if item.Permission == texts.PermissionRolesManage {
			continue
		}
		ids = append(ids, item.Permission)
	}
	return ids
}

func User_PermissionIDs() []string {
	return []string{
		texts.PermissionChatUse,
		texts.PermissionPersonalUpload,
		texts.PermissionDraftsCheck,
		texts.PermissionDraftsAssist,
		texts.PermissionTemplatesUse,
	}
}

func Seed_Roles() []SeedRole {
	return []SeedRole{
		{texts.RoleIDChief, texts.RoleTitleChief, texts.RoleDescriptionChief, true, Permission_IDs()},
		{texts.RoleIDAdmin, texts.RoleTitleAdmin, texts.RoleDescriptionAdmin, false, Admin_PermissionIDs()},
		{texts.RoleIDUser, texts.RoleTitleUser, texts.RoleDescriptionUser, false, User_PermissionIDs()},
	}
}

func Known_Permission(permission string) bool {
	for _, item := range texts.Permission_Catalog() {
		if item.Permission == permission {
			return true
		}
	}
	return false
}

func Has_Permission(permissions []string, permission string) bool {
	for _, item := range permissions {
		if item == permission {
			return true
		}
	}
	return false
}

// Уровень 2 оставляем для текущего сайта, если среди флагов есть управление людьми или ролями
func AccessLevel_ForPermissions(permissions []string) int {
	if Has_Permission(permissions, texts.PermissionUsersManage) || Has_Permission(permissions, texts.PermissionRolesManage) {
		return texts.SeedAdminAccessLevel
	}
	return texts.SeedEmployeeAccessLevel
}

// Первая учетная запись получает системную роль. Остальные - по старому уровню
func RoleID_ForNewAccount(usersBefore, accessLevel int, pending bool) string {
	if usersBefore == 0 {
		return texts.RoleIDChief
	}
	if !pending && accessLevel >= texts.SeedAdminAccessLevel {
		return texts.RoleIDAdmin
	}
	return texts.RoleIDUser
}

func Audience_FromLevel(accessLevel int) DocumentAudience {
	if accessLevel >= texts.SeedAdminAccessLevel {
		return DocumentAudience{
			VisibleToAll: false,
			RoleIDs:      []string{texts.RoleIDAdmin, texts.RoleIDChief},
			AccessLevel:  texts.SeedDocumentAccessClosed,
		}
	}
	return DocumentAudience{
		VisibleToAll: true,
		RoleIDs:      []string{},
		AccessLevel:  texts.SeedDocumentAccessOpen,
	}
}

func Audience_FromRoleIDs(roleIDs []string) DocumentAudience {
	copied := append([]string{}, roleIDs...)
	if len(copied) == 0 {
		return Audience_FromLevel(texts.SeedDocumentAccessOpen)
	}
	return DocumentAudience{
		VisibleToAll: false,
		RoleIDs:      copied,
		AccessLevel:  texts.SeedDocumentAccessClosed,
	}
}

// Пустой список ролей при visibleToAll значит «всем сотрудникам». Иначе документ виден только перечисленным ролям
func Sees_GlobalDocument(viewerRoleIDs []string, visibleToAll bool, documentRoleIDs []string) bool {
	if visibleToAll {
		return true
	}
	allowed := map[string]bool{}
	for _, roleID := range documentRoleIDs {
		allowed[roleID] = true
	}
	for _, roleID := range viewerRoleIDs {
		if allowed[roleID] {
			return true
		}
	}
	return false
}

func Unique_IDs(raw []string) []string {
	seen := map[string]bool{}
	ids := make([]string, 0, len(raw))
	for _, item := range raw {
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		ids = append(ids, item)
	}
	return ids
}
