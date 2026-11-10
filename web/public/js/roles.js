// Роли как в open-webui: администратор, пользователь и ожидающий подтверждения.
// Ядро отдаёт поле role. Если его нет (старое ядро), роль считается по уровню доступа,
// порог администратора приходит из .env веб-сервера. Менять только здесь.
import { RoleTexts } from './texts.js';

export const Roles = Object.freeze({
	admin: 'admin',
	user: 'user',
	pending: 'pending',
});

// Порядок для выбора роли в формах
export const roleChoices = Object.freeze([Roles.pending, Roles.user, Roles.admin]);

export function get_UserRole(user, config) {
	if (Object.values(Roles).includes(user?.role)) {
		return user.role;
	}
	return (user?.accessLevel ?? 0) >= config.adminAccessLevel ? Roles.admin : Roles.user;
}

export function check_IsAdmin(user, config) {
	return get_UserRole(user, config) === Roles.admin;
}

// Ожидающий подтверждения видит только экран ожидания
export function check_IsPending(user, config) {
	return Boolean(user) && get_UserRole(user, config) === Roles.pending;
}

// Следующая роль по нажатию на бейдж, как в open-webui: ожидает -> пользователь -> администратор -> ожидает
export function get_NextRole(role) {
	if (role === Roles.pending) {
		return Roles.user;
	}
	if (role === Roles.user) {
		return Roles.admin;
	}
	return Roles.pending;
}

export function get_AccessLevelForRole(role, config) {
	return role === Roles.admin ? config.adminAccessLevel : config.userAccessLevel;
}

export function get_RoleLabel(role) {
	if (role === Roles.admin) {
		return RoleTexts.admin;
	}
	return role === Roles.pending ? RoleTexts.pending : RoleTexts.user;
}

// Документ закрыт, если его уровень не ниже уровня администратора
export function get_DocumentAudience(accessLevel, config) {
	return (accessLevel ?? 0) >= config.adminAccessLevel ? Roles.admin : Roles.user;
}

export function get_DocumentAudienceLabel(accessLevel, config) {
	return get_DocumentAudience(accessLevel, config) === Roles.admin ? RoleTexts.documentForAdmins : RoleTexts.documentForAll;
}

// Флаги ядра. Если массива permissions нет, это старое ядро: права считаются по роли.
export const Permissions = Object.freeze({
	chatUse: 'chat.use',
	personalUpload: 'documents.personal.upload',
	globalManage: 'documents.global.manage',
	personalViewAll: 'documents.personal.view_all',
	draftsCheck: 'drafts.check',
	draftsAssist: 'drafts.assist',
	templatesUse: 'templates.use',
	usersManage: 'users.manage',
	rolesManage: 'roles.manage',
	auditView: 'audit.view',
	aiManage: 'ai.manage',
	organizationManage: 'organization.manage',
	sourcesManage: 'sources.manage',
});

const employeePermissions = new Set([
	Permissions.chatUse,
	Permissions.personalUpload,
	Permissions.draftsCheck,
	Permissions.templatesUse,
]);

export function has_PermissionList(user) {
	return Array.isArray(user?.permissions);
}

export function has_Permission(user, config, permission) {
	if (has_PermissionList(user)) {
		return user.permissions.includes(permission);
	}
	// Новые флаги на старом ядре не показываем: методов под них там нет
	if (permission === Permissions.rolesManage || permission === Permissions.draftsAssist) {
		return false;
	}
	if (check_IsAdmin(user, config)) {
		return true;
	}
	return employeePermissions.has(permission);
}

export function can_OpenAdmin(user, config) {
	return [
		Permissions.usersManage,
		Permissions.rolesManage,
		Permissions.auditView,
		Permissions.globalManage,
		Permissions.personalViewAll,
		Permissions.organizationManage,
		Permissions.sourcesManage,
	].some((permission) => has_Permission(user, config, permission));
}
