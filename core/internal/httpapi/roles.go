package httpapi

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"docfinder/core/internal/auth"
	"docfinder/core/internal/livestore"
	"docfinder/core/internal/texts"
)

type profileRole struct {
	RoleID string `json:"roleId"`
	Title  string `json:"title"`
}

type permissionLine struct {
	Permission  string `json:"permission"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

type roleResponse struct {
	RoleID      string   `json:"roleId"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	System      bool     `json:"system"`
	LdapGroup   string   `json:"ldapGroup"`
	Permissions []string `json:"permissions"`
	UserCount   int      `json:"userCount"`
}

type permissionEventResponse struct {
	EventID      string `json:"eventId"`
	ActorUserID  string `json:"actorUserId"`
	ActorName    string `json:"actorName"`
	Action       string `json:"action"`
	RoleID       string `json:"roleId"`
	TargetUserID string `json:"targetUserId"`
	CreatedAt    string `json:"createdAt"`
}

type roleRecord struct {
	id          string
	title       string
	description string
	system      bool
	ldapGroup   string
	permissions []string
}

type permissionEvent struct {
	id           string
	actorID      string
	action       string
	roleID       string
	targetUserID string
	createdAt    time.Time
}

type roleBody struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	LdapGroup   string   `json:"ldapGroup"`
	Permissions []string `json:"permissions"`
}

func copy_Strings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return append([]string{}, values...)
}

func profile_Roles(ids, titles []string) []profileRole {
	roles := make([]profileRole, 0, len(ids))
	for index, roleID := range ids {
		title := ""
		if index < len(titles) {
			title = titles[index]
		}
		roles = append(roles, profileRole{RoleID: roleID, Title: title})
	}
	return roles
}

func allow_Upload(permissions []string, scope string) bool {
	if scope == texts.ScopeGlobal {
		return auth.Has_Permission(permissions, texts.PermissionGlobalManage)
	}
	return auth.Has_Permission(permissions, texts.PermissionPersonalUpload)
}

func permission_Lines() []permissionLine {
	catalog := texts.Permission_Catalog()
	lines := make([]permissionLine, 0, len(catalog))
	for _, item := range catalog {
		lines = append(lines, permissionLine{Permission: item.Permission, Title: item.Title, Description: item.Description})
	}
	return lines
}

func (api *liveAPI) serve_ListPermissions(responseWriter http.ResponseWriter, request *http.Request) {
	token, found := read_BearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	if _, err := api.store.Find_AccountByToken(request.Context(), token); err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	write_JSONResponse(responseWriter, http.StatusOK, struct {
		Permissions []permissionLine `json:"permissions"`
	}{Permissions: permission_Lines()})
}

func (api *liveAPI) serve_ListRoles(responseWriter http.ResponseWriter, request *http.Request) {
	token, found := read_BearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	roles, err := api.store.List_Roles(request.Context(), token)
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	lines := make([]roleResponse, 0, len(roles))
	for _, role := range roles {
		lines = append(lines, role_ResponseFrom(role))
	}
	write_JSONResponse(responseWriter, http.StatusOK, struct {
		Roles []roleResponse `json:"roles"`
	}{Roles: lines})
}

func (api *liveAPI) serve_CreateRole(responseWriter http.ResponseWriter, request *http.Request) {
	body, ok := read_RoleBody(responseWriter, request)
	if !ok {
		return
	}
	token, found := read_BearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	created, err := api.store.Create_Role(request.Context(), token, role_Input(body))
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	write_JSONResponse(responseWriter, http.StatusOK, role_ResponseFrom(created))
}

func (api *liveAPI) serve_UpdateRole(responseWriter http.ResponseWriter, request *http.Request) {
	body, ok := read_RoleBody(responseWriter, request)
	if !ok {
		return
	}
	token, found := read_BearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	updated, err := api.store.Update_Role(request.Context(), token, request.PathValue("roleId"), role_Input(body))
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	write_JSONResponse(responseWriter, http.StatusOK, role_ResponseFrom(updated))
}

func (api *liveAPI) serve_DeleteRole(responseWriter http.ResponseWriter, request *http.Request) {
	token, found := read_BearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	if err := api.store.Delete_Role(request.Context(), token, request.PathValue("roleId")); err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	responseWriter.WriteHeader(http.StatusNoContent)
}

func (api *liveAPI) serve_PermissionAudit(responseWriter http.ResponseWriter, request *http.Request) {
	token, found := read_BearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	events, err := api.store.List_PermissionEvents(request.Context(), token)
	if err != nil {
		write_LiveError(responseWriter, err)
		return
	}
	lines := make([]permissionEventResponse, 0, len(events))
	for _, event := range events {
		lines = append(lines, permissionEventResponse{
			EventID: event.EventID, ActorUserID: event.ActorUserID, ActorName: event.ActorName,
			Action: event.Action, RoleID: event.RoleID, TargetUserID: event.TargetUserID,
			CreatedAt: event.CreatedAt.UTC().Format(timeLayout),
		})
	}
	write_JSONResponse(responseWriter, http.StatusOK, struct {
		Events []permissionEventResponse `json:"events"`
	}{Events: lines})
}

func role_ResponseFrom(role livestore.Role) roleResponse {
	return roleResponse{
		RoleID: role.RoleID, Title: role.Title, Description: role.Description, System: role.System,
		LdapGroup: role.LdapGroup, Permissions: copy_Strings(role.Permissions), UserCount: role.UserCount,
	}
}

func read_RoleBody(responseWriter http.ResponseWriter, request *http.Request) (roleBody, bool) {
	var body roleBody
	if err := read_JSONBody(responseWriter, request, &body); err != nil {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return roleBody{}, false
	}
	body.Title = strings.TrimSpace(body.Title)
	body.Description = strings.TrimSpace(body.Description)
	body.LdapGroup = strings.TrimSpace(body.LdapGroup)
	body.Permissions = auth.Unique_IDs(body.Permissions)
	if body.Title == "" {
		write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
		return roleBody{}, false
	}
	for _, permission := range body.Permissions {
		if !auth.Known_Permission(permission) {
			write_ErrorResponse(responseWriter, http.StatusBadRequest, texts.ErrorInvalidBody)
			return roleBody{}, false
		}
	}
	return body, true
}

func (stubStore *stub) seed_Roles() {
	for _, role := range auth.Seed_Roles() {
		stubStore.store_Role(&roleRecord{
			id: role.ID, title: role.Title, description: role.Description,
			system: role.System, permissions: copy_Strings(role.Permissions),
		})
	}
}

func (stubStore *stub) store_Role(role *roleRecord) {
	if stubStore.rolesByID[role.id] != nil {
		return
	}
	stubStore.roles = append(stubStore.roles, role)
	stubStore.rolesByID[role.id] = role
}

func (stubStore *stub) permissions_Of(record *userRecord) []string {
	seen := map[string]bool{}
	permissions := []string{}
	for _, roleID := range record.roleIDs {
		role := stubStore.rolesByID[roleID]
		if role == nil {
			continue
		}
		for _, permission := range role.permissions {
			if seen[permission] {
				continue
			}
			seen[permission] = true
			permissions = append(permissions, permission)
		}
	}
	sort.Strings(permissions)
	return permissions
}

func (stubStore *stub) sync_Access(record *userRecord) {
	record.accessLevel = auth.AccessLevel_ForPermissions(stubStore.permissions_Of(record))
}

func (stubStore *stub) known_RoleIDs(roleIDs []string) bool {
	for _, roleID := range roleIDs {
		if stubStore.rolesByID[roleID] == nil {
			return false
		}
	}
	return true
}

func (stubStore *stub) chiefs_After(userID string, next []string) int {
	count := 0
	for _, user := range stubStore.users {
		roles := user.roleIDs
		if user.id == userID {
			roles = next
		}
		if contains_Role(roles, texts.RoleIDChief) {
			count++
		}
	}
	return count
}

func (stubStore *stub) profile_Of(record *userRecord) profileResponse {
	roles := make([]profileRole, 0, len(record.roleIDs))
	for _, roleID := range record.roleIDs {
		role := stubStore.rolesByID[roleID]
		title := ""
		if role != nil {
			title = role.title
		}
		roles = append(roles, profileRole{RoleID: roleID, Title: title})
	}
	return profileResponse{
		UserID: record.id, Login: record.login, DisplayName: record.displayName,
		AccessLevel: record.accessLevel, Role: record.role(),
		Roles: roles, Permissions: stubStore.permissions_Of(record),
	}
}

func (stubStore *stub) require_Permission(responseWriter http.ResponseWriter, request *http.Request, permission string) (*userRecord, bool) {
	record, found := stubStore.find_UserByBearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return nil, false
	}
	if record.pending {
		write_ErrorResponse(responseWriter, http.StatusForbidden, texts.ErrorAccountPending)
		return nil, false
	}
	if !auth.Has_Permission(stubStore.permissions_Of(record), permission) {
		write_ErrorResponse(responseWriter, http.StatusForbidden, texts.ErrorAccessDenied)
		return nil, false
	}
	return record, true
}

func (stubStore *stub) serve_ListPermissions(responseWriter http.ResponseWriter, request *http.Request) {
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	record, found := stubStore.find_UserByBearerToken(request)
	if !found {
		write_ErrorResponse(responseWriter, http.StatusUnauthorized, texts.ErrorUnauthorized)
		return
	}
	if record.pending {
		write_ErrorResponse(responseWriter, http.StatusForbidden, texts.ErrorAccountPending)
		return
	}
	write_JSONResponse(responseWriter, http.StatusOK, struct {
		Permissions []permissionLine `json:"permissions"`
	}{Permissions: permission_Lines()})
}

func (stubStore *stub) serve_ListRoles(responseWriter http.ResponseWriter, request *http.Request) {
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	if _, found := stubStore.require_Permission(responseWriter, request, texts.PermissionRolesManage); !found {
		return
	}
	write_JSONResponse(responseWriter, http.StatusOK, struct {
		Roles []roleResponse `json:"roles"`
	}{Roles: stubStore.role_Lines("")})
}

func (stubStore *stub) serve_CreateRole(responseWriter http.ResponseWriter, request *http.Request) {
	body, ok := read_RoleBody(responseWriter, request)
	if !ok {
		return
	}
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	actor, found := stubStore.require_Permission(responseWriter, request, texts.PermissionRolesManage)
	if !found {
		return
	}
	created := &roleRecord{
		id: "role-" + strconv.Itoa(stubStore.nextRole), title: body.Title,
		description: body.Description, ldapGroup: body.LdapGroup, permissions: copy_Strings(body.Permissions),
	}
	stubStore.nextRole++
	stubStore.store_Role(created)
	stubStore.append_PermissionEvent(actor.id, texts.PermissionActionUpdate, created.id, "")
	write_JSONResponse(responseWriter, http.StatusOK, stubStore.role_Line(created))
}

func (stubStore *stub) serve_UpdateRole(responseWriter http.ResponseWriter, request *http.Request) {
	body, ok := read_RoleBody(responseWriter, request)
	if !ok {
		return
	}
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	actor, found := stubStore.require_Permission(responseWriter, request, texts.PermissionRolesManage)
	if !found {
		return
	}
	role := stubStore.rolesByID[request.PathValue("roleId")]
	if role == nil {
		write_ErrorResponse(responseWriter, http.StatusNotFound, texts.ErrorNotFound)
		return
	}
	if role.system {
		write_ErrorResponse(responseWriter, http.StatusForbidden, texts.ErrorAccessDenied)
		return
	}
	role.title = body.Title
	role.description = body.Description
	role.ldapGroup = body.LdapGroup
	role.permissions = copy_Strings(body.Permissions)
	for _, user := range stubStore.users {
		stubStore.sync_Access(user)
	}
	stubStore.append_PermissionEvent(actor.id, texts.PermissionActionUpdate, role.id, "")
	write_JSONResponse(responseWriter, http.StatusOK, stubStore.role_Line(role))
}

func (stubStore *stub) serve_DeleteRole(responseWriter http.ResponseWriter, request *http.Request) {
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	if _, found := stubStore.require_Permission(responseWriter, request, texts.PermissionRolesManage); !found {
		return
	}
	roleID := request.PathValue("roleId")
	role := stubStore.rolesByID[roleID]
	if role == nil {
		write_ErrorResponse(responseWriter, http.StatusNotFound, texts.ErrorNotFound)
		return
	}
	if role.system {
		write_ErrorResponse(responseWriter, http.StatusForbidden, texts.ErrorAccessDenied)
		return
	}
	if stubStore.role_UserCount(roleID) > 0 {
		write_ErrorResponse(responseWriter, http.StatusConflict, texts.ErrorRoleInUse)
		return
	}
	delete(stubStore.rolesByID, roleID)
	remaining := make([]*roleRecord, 0, len(stubStore.roles))
	for _, item := range stubStore.roles {
		if item.id != roleID {
			remaining = append(remaining, item)
		}
	}
	stubStore.roles = remaining
	responseWriter.WriteHeader(http.StatusNoContent)
}

func (stubStore *stub) serve_PermissionAudit(responseWriter http.ResponseWriter, request *http.Request) {
	stubStore.mu.Lock()
	defer stubStore.mu.Unlock()
	if _, found := stubStore.require_Permission(responseWriter, request, texts.PermissionAuditView); !found {
		return
	}
	events := make([]permissionEventResponse, 0, len(stubStore.permissionEvents))
	for index := len(stubStore.permissionEvents) - 1; index >= 0; index-- {
		event := stubStore.permissionEvents[index]
		name := ""
		if actor := stubStore.usersByID[event.actorID]; actor != nil {
			name = actor.displayName
		}
		events = append(events, permissionEventResponse{
			EventID: event.id, ActorUserID: event.actorID, ActorName: name,
			Action: event.action, RoleID: event.roleID, TargetUserID: event.targetUserID,
			CreatedAt: event.createdAt.UTC().Format(time.RFC3339),
		})
	}
	write_JSONResponse(responseWriter, http.StatusOK, struct {
		Events []permissionEventResponse `json:"events"`
	}{Events: events})
}

func (stubStore *stub) role_Lines(onlyID string) []roleResponse {
	lines := make([]roleResponse, 0, len(stubStore.roles))
	for _, role := range stubStore.roles {
		if onlyID != "" && role.id != onlyID {
			continue
		}
		lines = append(lines, stubStore.role_Line(role))
	}
	return lines
}

func (stubStore *stub) role_Line(role *roleRecord) roleResponse {
	return roleResponse{
		RoleID: role.id, Title: role.title, Description: role.description, System: role.system,
		LdapGroup: role.ldapGroup, Permissions: copy_Strings(role.permissions), UserCount: stubStore.role_UserCount(role.id),
	}
}

func role_Input(body roleBody) livestore.RoleInput {
	return livestore.RoleInput{Title: body.Title, Description: body.Description, LdapGroup: body.LdapGroup, Permissions: body.Permissions}
}

func (stubStore *stub) role_UserCount(roleID string) int {
	count := 0
	for _, user := range stubStore.users {
		if contains_Role(user.roleIDs, roleID) {
			count++
		}
	}
	return count
}

func (stubStore *stub) append_PermissionEvent(actorID, action, roleID, targetUserID string) {
	stubStore.permissionEvents = append(stubStore.permissionEvents, permissionEvent{
		id: "event-" + strconv.Itoa(stubStore.nextPermissionEvent), actorID: actorID,
		action: action, roleID: roleID, targetUserID: targetUserID, createdAt: time.Now().UTC(),
	})
	stubStore.nextPermissionEvent++
}

func (stubStore *stub) record_RoleDiff(actorID, userID string, oldRoles, next []string) {
	for _, roleID := range next {
		if !contains_Role(oldRoles, roleID) {
			stubStore.append_PermissionEvent(actorID, texts.PermissionActionAssign, roleID, userID)
		}
	}
	for _, roleID := range oldRoles {
		if !contains_Role(next, roleID) {
			stubStore.append_PermissionEvent(actorID, texts.PermissionActionRevoke, roleID, userID)
		}
	}
}

func contains_Role(roleIDs []string, roleID string) bool {
	for _, item := range roleIDs {
		if item == roleID {
			return true
		}
	}
	return false
}
