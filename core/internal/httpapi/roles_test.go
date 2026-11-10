package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"docfinder/core/internal/config"
	"docfinder/core/internal/texts"
)

func TestStubRolesFlagsAndDocumentVisibility(t *testing.T) {
	handler := must_Handler(t, config.Config{Mode: config.ModeStub})
	employeeToken := login(t, handler, texts.SeedEmployeeLogin, texts.SeedEmployeePassword)
	chiefToken := login(t, handler, texts.SeedAdminLogin, texts.SeedAdminPassword)

	me := decodeJSON[profileResponse](t, doJSON(t, handler, http.MethodGet, "/me", employeeToken, nil))
	if me.Role != texts.RoleUser || !containsString(me.Permissions, texts.PermissionChatUse) || containsString(me.Permissions, texts.PermissionRolesManage) {
		t.Fatalf("employee profile %+v", me)
	}
	if len(me.Roles) != 1 || me.Roles[0].RoleID != texts.RoleIDUser {
		t.Fatalf("employee roles %+v", me.Roles)
	}

	permissions := doJSON(t, handler, http.MethodGet, "/permissions", employeeToken, nil)
	if permissions.Code != http.StatusOK || !strings.Contains(permissions.Body.String(), texts.PermissionChatUse) {
		t.Fatalf("permissions %d %s", permissions.Code, permissions.Body.String())
	}
	if doJSON(t, handler, http.MethodPost, "/roles", employeeToken, map[string]any{
		"title": "Архив", "permissions": []string{texts.PermissionChatUse},
	}).Code != http.StatusForbidden {
		t.Fatal("employee created a role")
	}

	documents := listDocuments(t, handler, employeeToken)
	if !hasDocument(documents, texts.SeedInstructionID) || hasDocument(documents, texts.SeedRestrictedID) {
		t.Fatalf("employee documents %+v", documents)
	}

	if doJSON(t, handler, http.MethodDelete, "/roles/"+texts.RoleIDChief, chiefToken, nil).Code != http.StatusForbidden {
		t.Fatal("system role deleted")
	}

	createdAdmin := decodeJSON[accountResponse](t, doJSON(t, handler, http.MethodPost, "/users", chiefToken, map[string]any{
		"login": "second-admin@corp.local", "password": "second-admin", "displayName": "Второй", "role": texts.RoleAdmin,
	}))
	secondToken := login(t, handler, "second-admin@corp.local", "second-admin")
	demote := doJSON(t, handler, http.MethodPatch, "/users/"+texts.SeedAdminID, secondToken, map[string]any{
		"roleIds": []string{texts.RoleIDUser},
	})
	if demote.Code != http.StatusForbidden {
		t.Fatalf("last chief removed %d %s", demote.Code, demote.Body.String())
	}

	created := decodeJSON[roleResponse](t, doJSON(t, handler, http.MethodPost, "/roles", chiefToken, map[string]any{
		"title": "Архив", "description": "Свои вопросы", "permissions": []string{texts.PermissionChatUse},
	}))
	if created.RoleID != "role-1" || created.System {
		t.Fatalf("created role %+v", created)
	}
	if doJSON(t, handler, http.MethodPatch, "/users/"+createdAdmin.UserID, chiefToken, map[string]any{
		"roleIds": []string{texts.RoleIDAdmin, created.RoleID},
	}).Code != http.StatusBadRequest {
		t.Fatal("two roles assigned")
	}
	assign := doJSON(t, handler, http.MethodPatch, "/users/"+createdAdmin.UserID, chiefToken, map[string]any{
		"roleId": created.RoleID,
	})
	if assign.Code != http.StatusOK {
		t.Fatalf("assign %d %s", assign.Code, assign.Body.String())
	}
	assigned := decodeJSON[accountResponse](t, assign)
	if len(assigned.RoleIDs) != 1 || assigned.RoleIDs[0] != created.RoleID {
		t.Fatalf("assigned roles %+v", assigned.RoleIDs)
	}
	// Роль из конструктора подтверждает ожидающую учётную запись, role pending возвращает ожидание
	waiting := decodeJSON[accountResponse](t, doJSON(t, handler, http.MethodPost, "/users", chiefToken, map[string]any{
		"login": "waiting@corp.local", "password": "waiting-pass", "displayName": "Ждущий", "role": texts.RolePending,
	}))
	approved := decodeJSON[accountResponse](t, doJSON(t, handler, http.MethodPatch, "/users/"+waiting.UserID, chiefToken, map[string]any{"roleId": texts.RoleIDUser}))
	if waiting.Role != texts.RolePending || approved.Role != texts.RoleUser {
		t.Fatalf("approval %s -> %s", waiting.Role, approved.Role)
	}
	onHold := decodeJSON[accountResponse](t, doJSON(t, handler, http.MethodPatch, "/users/"+waiting.UserID, chiefToken, map[string]any{"role": texts.RolePending}))
	if onHold.Role != texts.RolePending {
		t.Fatalf("back to pending %s", onHold.Role)
	}
	for _, login := range []string{"не почта", "ivanov@corp", "ivanov@", "@corp.local"} {
		badCreate := doJSON(t, handler, http.MethodPost, "/users", chiefToken, map[string]any{
			"login": login, "password": "secret-pass", "displayName": "Иванов",
		})
		if badCreate.Code != http.StatusBadRequest || !strings.Contains(badCreate.Body.String(), texts.ErrorInvalidEmail) {
			t.Fatalf("create with %q: %d %s", login, badCreate.Code, badCreate.Body.String())
		}
	}
	badPatch := doJSON(t, handler, http.MethodPatch, "/users/"+createdAdmin.UserID, chiefToken, map[string]any{"login": "просто текст"})
	if badPatch.Code != http.StatusBadRequest || !strings.Contains(badPatch.Body.String(), texts.ErrorInvalidEmail) {
		t.Fatalf("patch login %d %s", badPatch.Code, badPatch.Body.String())
	}
	if doJSON(t, handler, http.MethodDelete, "/roles/"+created.RoleID, chiefToken, nil).Code != http.StatusConflict {
		t.Fatal("role with users was deleted")
	}
	events := doJSON(t, handler, http.MethodGet, "/audit/permissions", chiefToken, nil)
	if events.Code != http.StatusOK || !strings.Contains(events.Body.String(), texts.PermissionActionAssign) {
		t.Fatalf("permission audit %d %s", events.Code, events.Body.String())
	}
	if doJSON(t, handler, http.MethodGet, "/audit/permissions", employeeToken, nil).Code != http.StatusForbidden {
		t.Fatal("employee read permission audit")
	}
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
