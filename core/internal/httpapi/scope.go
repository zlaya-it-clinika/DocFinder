package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"docfinder/core/internal/auth"
	"docfinder/core/internal/texts"
)

func read_ListScope(raw string) (string, bool) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	switch raw {
	case "", texts.ScopeGlobal, texts.ScopePersonal, texts.ScopeAll:
		return raw, true
	default:
		return "", false
	}
}

func read_UploadScope(raw string, canUploadGlobal bool) (string, bool) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		if canUploadGlobal {
			return texts.ScopeGlobal, true
		}
		return texts.ScopePersonal, true
	}
	if raw == texts.ScopeGlobal || raw == texts.ScopePersonal {
		return raw, true
	}
	return "", false
}

func check_CanDeleteDocument(viewerID string, permissions []string, scope, ownerID string) bool {
	if scope == texts.ScopePersonal {
		return viewerID == ownerID || auth.Has_Permission(permissions, texts.PermissionPersonalViewAll)
	}
	return auth.Has_Permission(permissions, texts.PermissionGlobalManage)
}

func document_MatchesList(scopeFilter, documentScope, ownerID, viewerID string, visibleToAll bool, documentRoleIDs, viewerRoleIDs []string) bool {
	switch scopeFilter {
	case texts.ScopeAll:
		if documentScope == texts.ScopePersonal {
			return true
		}
		return auth.Sees_GlobalDocument(viewerRoleIDs, visibleToAll, documentRoleIDs)
	case texts.ScopeGlobal:
		return documentScope == texts.ScopeGlobal && auth.Sees_GlobalDocument(viewerRoleIDs, visibleToAll, documentRoleIDs)
	case texts.ScopePersonal:
		return documentScope == texts.ScopePersonal && ownerID == viewerID
	default:
		if documentScope == texts.ScopePersonal {
			return ownerID == viewerID
		}
		return documentScope == texts.ScopeGlobal && auth.Sees_GlobalDocument(viewerRoleIDs, visibleToAll, documentRoleIDs)
	}
}

// Чужой личный документ для сотрудника выглядит как отсутствующий.
func document_ReadStatus(viewerID string, permissions, viewerRoleIDs []string, scope, ownerID string, visibleToAll bool, documentRoleIDs []string) int {
	if scope == texts.ScopePersonal {
		if viewerID == ownerID || auth.Has_Permission(permissions, texts.PermissionPersonalViewAll) {
			return http.StatusOK
		}
		return http.StatusNotFound
	}
	if auth.Sees_GlobalDocument(viewerRoleIDs, visibleToAll, documentRoleIDs) {
		return http.StatusOK
	}
	return http.StatusForbidden
}

func read_AccessLevel(raw string, fallback int) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, true
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	return parsed, true
}

func read_FormRoleIDs(values []string) []string {
	var raw []string
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				raw = append(raw, part)
			}
		}
	}
	return auth.Unique_IDs(raw)
}

func read_UploadAudience(roleValues []string, accessLevelRaw string, userLevel int, personal bool) (int, bool, []string, bool) {
	if personal {
		return userLevel, false, []string{}, true
	}
	roleIDs := read_FormRoleIDs(roleValues)
	if len(roleIDs) > 0 {
		audience := auth.Audience_FromRoleIDs(roleIDs)
		return audience.AccessLevel, audience.VisibleToAll, audience.RoleIDs, true
	}
	parsed, levelOK := read_AccessLevel(accessLevelRaw, texts.SeedDocumentAccessOpen)
	if !levelOK {
		return 0, false, nil, false
	}
	audience := auth.Audience_FromLevel(parsed)
	return audience.AccessLevel, audience.VisibleToAll, audience.RoleIDs, true
}
