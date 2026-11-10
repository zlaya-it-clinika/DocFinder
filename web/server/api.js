// Маршруты /api: вход, регистрацию и выход обрабатывает сам веб-сервер, остальное уходит в ядро.
import { build_SessionCookie, parse_Cookies } from './cookies.js';
import { call_CoreJson, forward_ToCore } from './core-client.js';
import { RequestError, read_ClientAddress, read_JsonBody, send_Empty, send_Json } from './http-utils.js';
import { apply_SecurityHeaders, check_RequestOrigin, match_CsrfToken } from './security.js';
import { ErrorCodes } from './texts.js';

// Разделы ядра, которые можно вызывать из браузера. Вход ядра (/auth/login) и страница /docs
// сюда не входят: токен ядра не должен попасть в браузер.
const proxiedCoreSections = new Set(['me', 'sessions', 'documents', 'users', 'audit', 'drafts', 'templates', 'blanks', 'processes', 'roles', 'permissions', 'ai', 'organization', 'sources']);

// Настройки входа администратора (/auth/admin/...) идут в ядро с токеном сессии
const adminAuthPrefix = '/auth/admin/';

const csrfHeader = 'x-csrf-token';
const readOnlyMethods = new Set(['GET', 'HEAD']);

// Пользователь без служебных полей ядра
function pick_PublicUser(payload) {
	if (!payload || typeof payload !== 'object') {
		return null;
	}
	const { token, ...user } = payload;
	return user;
}

function read_SessionId(request, config) {
	return parse_Cookies(request.headers.cookie)[config.cookieName] ?? '';
}

function clear_SessionCookie(response, config) {
	response.setHeader('Set-Cookie', build_SessionCookie(config, '', 0));
}

// Меняющие запросы: свой origin и CSRF-токен из сессии
function require_SafeWrite(request, session, config) {
	if (!check_RequestOrigin(request, config)) {
		throw new RequestError(403, ErrorCodes.originDenied);
	}
	if (!match_CsrfToken(session.csrfToken, request.headers[csrfHeader])) {
		throw new RequestError(403, ErrorCodes.csrfFailed);
	}
}

function build_PublicConfig(config) {
	return {
		adminAccessLevel: config.adminAccessLevel,
		userAccessLevel: config.userAccessLevel,
		maxUploadBytes: config.maxUploadBytes,
		allowedUploadExtensions: config.allowedUploadExtensions,
		debug: config.debug,
		legacyDouFeatures: config.legacyDouFeatures,
	};
}

// Поля тела запроса, которые должны быть строками. Остальное не передаём в ядро
function pick_StringFields(body, fieldNames) {
	if (!body || typeof body !== 'object') {
		return null;
	}
	const picked = {};
	for (const name of fieldNames) {
		if (typeof body[name] !== 'string') {
			return null;
		}
		picked[name] = body[name];
	}
	return picked;
}

// Вход, регистрация и вход через LDAP устроены одинаково: ядро выдаёт токен,
// веб-сервер прячет его в своей сессии и отдаёт браузеру только cookie.
// countFailure решает, какие ответы ядра считать неудачной попыткой для защиты от перебора
async function create_CoreSession(context, { corePath, body, countFailure }) {
	const { config, request, response, sessionStore, loginLimiter } = context;
	if (request.method !== 'POST') {
		throw new RequestError(405, ErrorCodes.methodNotAllowed);
	}
	if (!check_RequestOrigin(request, config)) {
		throw new RequestError(403, ErrorCodes.originDenied);
	}
	const clientAddress = read_ClientAddress(request);
	if (loginLimiter.check_IsBlocked(clientAddress)) {
		throw new RequestError(429, ErrorCodes.tooManyAttempts);
	}
	const coreBody = body(await read_JsonBody(request, config.maxJsonBytes));
	if (!coreBody) {
		throw new RequestError(400, ErrorCodes.invalidBody);
	}

	const coreReply = await call_CoreJson(config, 'POST', corePath, { body: coreBody });
	if (countFailure(coreReply.status)) {
		loginLimiter.register_Failure(clientAddress);
	}
	if (coreReply.status !== 200 || typeof coreReply.payload?.token !== 'string') {
		send_Json(response, coreReply.status === 200 ? 502 : coreReply.status, coreReply.payload ?? { error: ErrorCodes.coreUnavailable });
		return;
	}

	loginLimiter.reset_Address(clientAddress);
	const previousSessionId = read_SessionId(request, config);
	if (previousSessionId) {
		sessionStore.destroy_Session(previousSessionId);
	}
	// В ответе входа нет permissions. Профиль /me нужен, чтобы разделы открывались по флагам сразу.
	let user = pick_PublicUser(coreReply.payload);
	const profile = await call_CoreJson(config, 'GET', '/me', { token: coreReply.payload.token });
	if (profile.status === 200 && profile.payload) {
		user = { ...user, ...pick_PublicUser(profile.payload) };
	}
	const session = sessionStore.create_Session(coreReply.payload.token, user);
	response.setHeader('Set-Cookie', build_SessionCookie(config, session.id, config.sessionTtlMs / 1000));
	send_Json(response, 200, { user: session.user, csrfToken: session.csrfToken });
}

function handle_Login(context) {
	return create_CoreSession(context, {
		corePath: '/auth/login',
		body: (raw) => {
			const fields = pick_StringFields(raw, ['login', 'password']);
			if (!fields || fields.login.trim() === '') {
				return null;
			}
			return { login: fields.login.trim(), password: fields.password };
		},
		countFailure: (status) => status === 401,
	});
}

// Регистрация по почте. Неудачи (занятая почта, закрытая регистрация) тоже считаем,
// чтобы с одного адреса нельзя было перебирать почты сотрудников
function handle_Signup(context) {
	return create_CoreSession(context, {
		corePath: '/auth/signup',
		body: (raw) => {
			const fields = pick_StringFields(raw, ['email', 'password', 'displayName']);
			if (!fields || fields.email.trim() === '' || fields.displayName.trim() === '' || fields.password === '') {
				return null;
			}
			return { email: fields.email.trim(), password: fields.password, displayName: fields.displayName.trim() };
		},
		countFailure: (status) => status >= 400 && status < 500,
	});
}

function handle_LdapLogin(context) {
	return create_CoreSession(context, {
		corePath: '/auth/ldap',
		body: (raw) => {
			const fields = pick_StringFields(raw, ['login', 'password']);
			if (!fields || fields.login.trim() === '') {
				return null;
			}
			return { login: fields.login.trim(), password: fields.password };
		},
		countFailure: (status) => status === 401,
	});
}

// Что показать на экране входа: первый запуск, регистрация, LDAP. Без сессии
async function handle_AuthConfig(context) {
	const { config, request, response } = context;
	if (request.method !== 'GET') {
		throw new RequestError(405, ErrorCodes.methodNotAllowed);
	}
	const coreReply = await call_CoreJson(config, 'GET', '/auth/config');
	send_Json(response, coreReply.status, coreReply.payload ?? { error: ErrorCodes.coreUnavailable });
}

async function handle_SessionInfo(context) {
	const { config, request, response, sessionStore, session } = context;
	if (request.method !== 'GET') {
		throw new RequestError(405, ErrorCodes.methodNotAllowed);
	}
	if (!session) {
		clear_SessionCookie(response, config);
		throw new RequestError(401, ErrorCodes.unauthorized);
	}
	// Профиль берём у ядра каждый раз: администратор мог сменить уровень
	const coreReply = await call_CoreJson(config, 'GET', '/me', { token: session.coreToken });
	if (coreReply.status === 401) {
		sessionStore.destroy_Session(session.id);
		clear_SessionCookie(response, config);
		throw new RequestError(401, ErrorCodes.unauthorized);
	}
	if (coreReply.status !== 200) {
		send_Json(response, coreReply.status, coreReply.payload ?? { error: ErrorCodes.coreUnavailable });
		return;
	}
	const user = pick_PublicUser(coreReply.payload);
	sessionStore.update_SessionUser(session.id, user);
	send_Json(response, 200, { user, csrfToken: session.csrfToken });
}

async function handle_Logout(context) {
	const { config, request, response, sessionStore, session } = context;
	if (request.method !== 'POST') {
		throw new RequestError(405, ErrorCodes.methodNotAllowed);
	}
	if (session) {
		require_SafeWrite(request, session, config);
		// В контракте ядра выхода пока нет. Когда появится - токен отзовётся и там
		try {
			await call_CoreJson(config, 'POST', '/auth/logout', { token: session.coreToken });
		} catch {
			// Ядро недоступно - сессию веба всё равно закрываем
		}
		sessionStore.destroy_Session(session.id);
	}
	clear_SessionCookie(response, config);
	send_Empty(response, 204);
}

async function handle_Proxy(context, apiPath, search) {
	const { config, request, response, sessionStore, session } = context;
	const section = apiPath.split('/')[1] ?? '';
	if (!proxiedCoreSections.has(section) && !apiPath.startsWith(adminAuthPrefix)) {
		throw new RequestError(404, ErrorCodes.notFound);
	}
	if (!session) {
		throw new RequestError(401, ErrorCodes.unauthorized);
	}
	if (!readOnlyMethods.has(request.method)) {
		require_SafeWrite(request, session, config);
	}
	const declaredLength = Number.parseInt(request.headers['content-length'] ?? '0', 10);
	if (declaredLength > config.maxUploadBytes) {
		throw new RequestError(413, ErrorCodes.payloadTooLarge);
	}
	await forward_ToCore(config, request, response, {
		corePath: apiPath + search,
		token: session.coreToken,
		onUnauthorized: () => {
			sessionStore.destroy_Session(session.id);
			clear_SessionCookie(response, config);
		},
	});
}

export async function handle_Api(context, url) {
	const { config, request, response, sessionStore } = context;
	apply_SecurityHeaders(response, config, 'api');
	const apiPath = url.pathname.slice('/api'.length) || '/';
	const session = sessionStore.find_Session(read_SessionId(request, config));
	const routeContext = { ...context, session };

	switch (apiPath) {
		case '/config':
			send_Json(response, 200, build_PublicConfig(config));
			return;
		case '/auth/login':
			await handle_Login(routeContext);
			return;
		case '/auth/signup':
			await handle_Signup(routeContext);
			return;
		case '/auth/ldap':
			await handle_LdapLogin(routeContext);
			return;
		case '/auth/config':
			await handle_AuthConfig(routeContext);
			return;
		case '/auth/session':
			await handle_SessionInfo(routeContext);
			return;
		case '/auth/logout':
			await handle_Logout(routeContext);
			return;
		default:
			if (apiPath.startsWith('/auth/') && !apiPath.startsWith(adminAuthPrefix)) {
				throw new RequestError(404, ErrorCodes.notFound);
			}
			await handle_Proxy(routeContext, apiPath, url.search);
	}
}
