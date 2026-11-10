// Общие и личные документы единой базы знаний.
// Права приходят из ответа ядра (canUploadGlobal, canUploadPersonal, canDelete), клиент их не вычисляет.
// Ядро без этих полей - старый контракт: общие загружает администратор, личных документов нет.
import { check_IsAdmin } from './roles.js';

export const Scopes = Object.freeze({
	global: 'global',
	personal: 'personal',
	all: 'all',
});

export function get_DocumentScope(document) {
	return document?.scope === Scopes.personal ? Scopes.personal : Scopes.global;
}

export function check_IsPersonal(document) {
	return get_DocumentScope(document) === Scopes.personal;
}

export function check_CanDelete(document) {
	return document?.canDelete === true;
}

// supportsScopes: ядро знает про личные документы и отдаёт флаги прав
export function read_DocumentPermissions(payload, user, config) {
	const supportsScopes = typeof payload?.canUploadPersonal === 'boolean';
	return {
		supportsScopes,
		canUploadGlobal: typeof payload?.canUploadGlobal === 'boolean' ? payload.canUploadGlobal : check_IsAdmin(user, config),
		canUploadPersonal: payload?.canUploadPersonal === true,
	};
}

export function read_DocumentList(payload) {
	return Array.isArray(payload?.documents) ? payload.documents : [];
}

export function filter_ByQuery(documents, query, fields = ['title']) {
	if (!query) {
		return documents;
	}
	return documents.filter((document) => fields.some((field) => String(document[field] ?? '').toLowerCase().includes(query)));
}
