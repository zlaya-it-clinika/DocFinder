// Клиент API. Браузер ходит только в /api своего веб-сервера, тот добавляет токен ядра.
// Пути и формы ответов - по контракту из README репозитория.

const apiPrefix = '/api';
const csrfHeader = 'X-CSRF-Token';
const readOnlyMethods = new Set(['GET', 'HEAD']);

export class ApiError extends Error {
	constructor(status, code) {
		super(code);
		this.status = status;
		this.code = code;
	}
}

let csrfToken = '';
const unauthorizedListeners = new Set();
const pendingListeners = new Set();

export function set_CsrfToken(token) {
	csrfToken = token ?? '';
}

// Подписка на 401: сессия истекла или ядро перезапустилось
export function on_Unauthorized(listener) {
	unauthorizedListeners.add(listener);
	return () => unauthorizedListeners.delete(listener);
}

// Подписка на 403 account_pending: администратор вернул учётную запись в ожидание
export function on_AccountPending(listener) {
	pendingListeners.add(listener);
	return () => pendingListeners.delete(listener);
}

async function read_Payload(response) {
	if (response.status === 204) {
		return null;
	}
	const contentType = response.headers.get('content-type') ?? '';
	if (!contentType.includes('application/json')) {
		return null;
	}
	try {
		return await response.json();
	} catch {
		return null;
	}
}

// silentUnauthorized: 401 здесь ожидаем (вход, проверка сессии) и не считаем выходом
async function request_Api(method, path, { json, formData, silentUnauthorized = false } = {}) {
	const headers = { Accept: 'application/json' };
	let body;
	if (json !== undefined) {
		headers['Content-Type'] = 'application/json';
		body = JSON.stringify(json);
	}
	if (formData) {
		body = formData;
	}
	if (!readOnlyMethods.has(method) && csrfToken) {
		headers[csrfHeader] = csrfToken;
	}

	let response;
	try {
		response = await fetch(apiPrefix + path, { method, headers, body, credentials: 'same-origin' });
	} catch {
		throw new ApiError(0, 'network_error');
	}
	const payload = await read_Payload(response);
	if (!response.ok) {
		const code = typeof payload?.error === 'string' ? payload.error : `http_${response.status}`;
		if (response.status === 401 && !silentUnauthorized) {
			for (const listener of unauthorizedListeners) {
				listener();
			}
		}
		if (response.status === 403 && code === 'account_pending') {
			for (const listener of pendingListeners) {
				listener();
			}
		}
		throw new ApiError(response.status, code);
	}
	return payload;
}

function encode_Segment(value) {
	return encodeURIComponent(String(value));
}

// Метод не реализован в ядре: роут не найден или метод не разрешён
export function check_IsNotSupported(error) {
	return error instanceof ApiError && (error.status === 404 || error.status === 405) && error.code !== 'not_found';
}

// Настройки и вход

export function fetch_PublicConfig() {
	return request_Api('GET', '/config');
}

// Вход по почте и паролю. Старые учётные записи входят по логину через то же поле
export function login_User(login, password) {
	return request_Api('POST', '/auth/login', { json: { login, password }, silentUnauthorized: true });
}

// Вход доменной учётной записью: логин каталога (например, ivanov) и пароль
export function login_LdapUser(login, password) {
	return request_Api('POST', '/auth/ldap', { json: { login, password }, silentUnauthorized: true });
}

// Регистрация. Первая учётная запись в системе становится администратором
export function signup_User({ email, password, displayName }) {
	return request_Api('POST', '/auth/signup', { json: { email, password, displayName }, silentUnauthorized: true });
}

// { onboarding, enableSignup, enableLdap, ldapLabel } - что показать на экране входа
export function fetch_AuthConfig() {
	return request_Api('GET', '/auth/config', { silentUnauthorized: true });
}

// Контакт администратора для экрана ожидания
export function fetch_AdminDetails() {
	return request_Api('GET', '/auth/admin/details', { silentUnauthorized: true });
}

export function fetch_AuthSession() {
	return request_Api('GET', '/auth/session', { silentUnauthorized: true });
}

export function logout_User() {
	return request_Api('POST', '/auth/logout', { silentUnauthorized: true });
}

// Сессии чата

export function list_ChatSessions() {
	return request_Api('GET', '/sessions');
}

export function create_ChatSession() {
	return request_Api('POST', '/sessions');
}

// Переименование и удаление чата. Доступны, когда GET /sessions отдаёт canRename и canDelete
export function rename_ChatSession(sessionId, title) {
	return request_Api('PATCH', `/sessions/${encode_Segment(sessionId)}`, { json: { title } });
}

export function delete_ChatSession(sessionId) {
	return request_Api('DELETE', `/sessions/${encode_Segment(sessionId)}`);
}

export function fetch_ChatSession(sessionId) {
	return request_Api('GET', `/sessions/${encode_Segment(sessionId)}`);
}

export function send_ChatMessage(sessionId, text, extra = {}) {
	const json = { text };
	// Несколько категорий сразу. Пустой список - все категории, его тоже отправляем: ядро запоминает выбор в сессии
	if (Array.isArray(extra.processIds)) {
		json.processIds = extra.processIds;
	} else if (typeof extra.processId === 'string') {
		json.processId = extra.processId;
	}
	if (Array.isArray(extra.attachmentIds) && extra.attachmentIds.length > 0) {
		json.attachmentIds = extra.attachmentIds;
	}
	// Режим чата: read или plan. Без поля ядро отвечает в режиме, сохранённом в сессии
	if (typeof extra.mode === 'string') {
		json.mode = extra.mode;
	}
	// Ответы на вопросы документа пачкой: [{ field, value }]. text - их запись для ленты
	if (Array.isArray(extra.answers) && extra.answers.length > 0) {
		json.answers = extra.answers;
	}
	return request_Api('POST', `/sessions/${encode_Segment(sessionId)}/messages`, { json });
}

export function upload_SessionAttachment(sessionId, file) {
	const formData = new FormData();
	formData.append('file', file);
	return request_Api('POST', `/sessions/${encode_Segment(sessionId)}/attachments`, { formData });
}

// Документы

// scope: global, personal или all (all - только администратор). Без параметра - всё, что видит пользователь
// Архив прежних редакций документа: новые первыми, у каждой fileLink для скачивания
export function list_DocumentEditions(documentId) {
	return request_Api('GET', `/documents/${encode_Segment(documentId)}/editions`);
}

export function list_Documents(scope = '') {
	const query = scope ? `?scope=${encodeURIComponent(scope)}` : '';
	return request_Api('GET', `/documents${query}`);
}

export function fetch_DocumentStructure(documentId) {
	return request_Api('GET', `/documents/${encode_Segment(documentId)}/structure`);
}

export function delete_Document(documentId) {
	return request_Api('DELETE', `/documents/${encode_Segment(documentId)}`);
}

// fields: title, edition, scope, accessLevel - необязательные
export function upload_Document(file, fields = {}) {
	const formData = new FormData();
	formData.append('file', file);
	for (const [name, value] of Object.entries(fields)) {
		if (Array.isArray(value)) {
			for (const item of value) {
				if (String(item).trim() !== '') {
					formData.append(name, String(item).trim());
				}
			}
			continue;
		}
		if (value !== undefined && value !== null && String(value).trim() !== '') {
			formData.append(name, String(value).trim());
		}
	}
	return request_Api('POST', '/documents', { formData });
}

export function build_DocumentFileUrl(documentId) {
	return `${apiPrefix}/documents/${encode_Segment(documentId)}/file`;
}

// originalLink из цитаты: /documents/<id>/file#p3.2. Пускаем только пути к документам
export function build_OriginalUrl(originalLink) {
	if (typeof originalLink === 'string' && originalLink.startsWith('/documents/') && !originalLink.includes('/file')) {
		return originalLink;
	}
	return build_LinkedFileUrl(originalLink, '/documents/');
}

// Ссылки ядра на файл: оригинал, бланк и docx черновика
export function build_LinkedFileUrl(link, prefix = '') {
	if (typeof link !== 'string') {
		return null;
	}
	const allowed = prefix
		? link.startsWith(prefix)
		: link.startsWith('/documents/') || link.startsWith('/blanks/') || link.startsWith('/drafts/');
	return allowed ? apiPrefix + link : null;
}

export function list_Processes() {
	return request_Api('GET', '/processes');
}

export function create_Process(fields) {
	return request_Api('POST', '/processes', { json: fields });
}

// fields: title, description, color, selectPrompt, answerPrompt, clarifyPrompt
export function update_Process(processId, fields) {
	return request_Api('PATCH', `/processes/${encode_Segment(processId)}`, { json: fields });
}

export function delete_Process(processId) {
	return request_Api('DELETE', `/processes/${encode_Segment(processId)}`);
}

export function list_Roles() {
	return request_Api('GET', '/roles');
}

export function list_Permissions() {
	return request_Api('GET', '/permissions');
}

export function create_Role(fields) {
	return request_Api('POST', '/roles', { json: fields });
}

export function update_Role(roleId, fields) {
	return request_Api('PATCH', `/roles/${encode_Segment(roleId)}`, { json: fields });
}

export function delete_Role(roleId) {
	return request_Api('DELETE', `/roles/${encode_Segment(roleId)}`);
}

export function check_Draft(file) {
	const formData = new FormData();
	formData.append('file', file);
	return request_Api('POST', '/drafts/check', { formData });
}

// Исправление оформления .docx после подтверждения сотрудника: draftLink на исправленный файл,
// applied - что исправлено, recommendations - что осталось поправить вручную
// fields: signerId, addresseeId - должностные лица из вкладки «Организация», title - о чем документ
export function fix_DraftFormat(file, fields = {}) {
	const formData = new FormData();
	formData.append('file', file);
	for (const [name, value] of Object.entries(fields)) {
		if (typeof value === 'string' && value !== '') {
			formData.append(name, value);
		}
	}
	return request_Api('POST', '/drafts/fix', { formData });
}

// Пользователи и журнал (уровень администратора)

export function list_Users() {
	return request_Api('GET', '/users');
}

export function create_User(fields) {
	return request_Api('POST', '/users', { json: fields });
}

export function update_User(userId, fields) {
	return request_Api('PATCH', `/users/${encode_Segment(userId)}`, { json: fields });
}

// Удаление учётной записи вместе с её чатами, черновиками и личными документами
export function delete_User(userId) {
	return request_Api('DELETE', `/users/${encode_Segment(userId)}`);
}

export function fetch_AuditLog() {
	return request_Api('GET', '/audit');
}

// Настройки входа (администратор): регистрация, роль по умолчанию, LDAP

export function fetch_SignupSettings() {
	return request_Api('GET', '/auth/admin/config');
}

export function save_SignupSettings(fields) {
	return request_Api('POST', '/auth/admin/config', { json: fields });
}

export function fetch_LdapToggle() {
	return request_Api('GET', '/auth/admin/config/ldap');
}

export function save_LdapToggle(enableLdap) {
	return request_Api('POST', '/auth/admin/config/ldap', { json: { enableLdap } });
}

export function fetch_LdapServer() {
	return request_Api('GET', '/auth/admin/config/ldap/server');
}

// Пустой appDnPassword оставляет сохранённый пароль служебной учётной записи
export function save_LdapServer(fields) {
	return request_Api('POST', '/auth/admin/config/ldap/server', { json: fields });
}

// Локальная модель: подключение к Ollama, модели и индекс, как в open-webui

export function fetch_AISettings() {
	return request_Api('GET', '/ai/settings');
}

// fields: provider, ollamaUrl, apiKey, модели и параметры (null - значение провайдера)
export function save_AISettings(fields) {
	return request_Api('POST', '/ai/settings', { json: fields });
}

export function verify_AIProvider({ provider, ollamaUrl, apiKey }) {
	return request_Api('POST', '/ai/verify', { json: { provider, ollamaUrl, apiKey } });
}

export function list_AIModels() {
	return request_Api('GET', '/ai/models');
}

export function start_ModelPull(name) {
	return request_Api('POST', '/ai/models/pull', { json: { name } });
}

export function fetch_ModelPull() {
	return request_Api('GET', '/ai/models/pull');
}

export function cancel_ModelPull() {
	return request_Api('POST', '/ai/models/pull/cancel', { json: {} });
}

export function delete_AIModel(name) {
	return request_Api('POST', '/ai/models/delete', { json: { name } });
}

export function test_AIModel(name) {
	return request_Api('POST', '/ai/models/test', { json: { name } });
}

export function start_Reindex() {
	return request_Api('POST', '/ai/reindex', { json: {} });
}

// Реквизиты организации: генеральный директор, должностные лица, герб и штамп
export function fetch_Organization() {
	return request_Api('GET', '/organization');
}

// fields: position, fullName, department, chief, validFrom (ГГГГ-ММ-ДД или пусто)
export function create_Official(fields) {
	return request_Api('POST', '/organization/officials', { json: fields });
}

export function update_Official(officialId, fields) {
	return request_Api('PATCH', `/organization/officials/${encode_Segment(officialId)}`, { json: fields });
}

export function delete_Official(officialId) {
	return request_Api('DELETE', `/organization/officials/${encode_Segment(officialId)}`);
}

// kind: emblem (герб) или stamp (штамп)
export function upload_OrganizationImage(kind, file) {
	const formData = new FormData();
	formData.append('file', file);
	return request_Api('POST', `/organization/images/${encode_Segment(kind)}`, { formData });
}

export function delete_OrganizationImage(kind) {
	return request_Api('DELETE', `/organization/images/${encode_Segment(kind)}`);
}

// Версия в адресе - время обновления: после замены картинки браузер не покажет старую из кэша
export function build_OrganizationImageUrl(kind, version = '') {
	const query = version ? `?v=${encodeURIComponent(version)}` : '';
	return `${apiPrefix}/organization/images/${encode_Segment(kind)}${query}`;
}

export function fetch_Reindex() {
	return request_Api('GET', '/ai/reindex');
}

export function list_SourceKinds() {
	return request_Api('GET', '/sources/kinds');
}

export function list_Sources() {
	return request_Api('GET', '/sources');
}

export function create_Source(fields) {
	return request_Api('POST', '/sources', { json: fields });
}

export function update_Source(sourceId, fields) {
	return request_Api('PATCH', `/sources/${encode_Segment(sourceId)}`, { json: fields });
}

export function delete_Source(sourceId) {
	return request_Api('DELETE', `/sources/${encode_Segment(sourceId)}`);
}

export function verify_Source(sourceId) {
	return request_Api('POST', `/sources/${encode_Segment(sourceId)}/verify`);
}

export function sync_Source(sourceId) {
	return request_Api('POST', `/sources/${encode_Segment(sourceId)}/sync`);
}

export function fetch_SourceRun(sourceId) {
	return request_Api('GET', `/sources/${encode_Segment(sourceId)}/runs`);
}
