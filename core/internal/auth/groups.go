package auth

import (
	"strings"

	"docfinder/core/internal/texts"
)

type GroupRole struct {
	RoleID string
	Group  string
}

func CommonName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if len(part) > 3 && strings.EqualFold(part[:3], "CN=") {
			return strings.TrimSpace(part[3:])
		}
	}
	return value
}

func Group_Names(values []string) []string {
	names := []string{}
	seen := map[string]struct{}{}
	for _, value := range values {
		name := CommonName(value)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		names = append(names, name)
	}
	return names
}

// У пользователя одна роль. Главный администратор ей и остаётся, даже если его группы нет в списке.
// Иначе берётся первая роль, чья группа совпала. Без совпадений - роль по умолчанию
func RoleIDs_ForGroups(groups []string, mapped []GroupRole, current []string, defaultRoleID string) []string {
	if contains_RoleID(current, texts.RoleIDChief) {
		return []string{texts.RoleIDChief}
	}
	if roleID, found := Matched_GroupRole(groups, mapped); found {
		return []string{roleID}
	}
	if defaultRoleID == "" {
		return []string{}
	}
	return []string{defaultRoleID}
}

// Первая роль, чья группа есть у пользователя в каталоге. Совпадение значит, что доступ одобрил каталог:
// подтверждение администратора такому сотруднику не нужно
func Matched_GroupRole(groups []string, mapped []GroupRole) (string, bool) {
	known := map[string]struct{}{}
	for _, group := range Group_Names(groups) {
		known[strings.ToLower(group)] = struct{}{}
	}
	for _, role := range mapped {
		name := strings.ToLower(strings.TrimSpace(role.Group))
		if name == "" {
			continue
		}
		if _, found := known[name]; found {
			return role.RoleID, true
		}
	}
	return "", false
}

func contains_RoleID(ids []string, wanted string) bool {
	for _, id := range ids {
		if id == wanted {
			return true
		}
	}
	return false
}
