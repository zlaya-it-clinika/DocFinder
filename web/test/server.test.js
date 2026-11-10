// Проверка веб-сервера против маленького тестового ядра: вход, cookie, CSRF, прокси, статика.
// Запуск: npm run check
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import http from 'node:http';
import path from 'node:path';
import { after, before, test } from 'node:test';
import { fileURLToPath } from 'node:url';

const webRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const testToken = 'core-token-for-tests';

let coreServer;
let webProcess;
let webUrl;
const coreRequests = [];

// Тестовое ядро повторяет формы ответов контракта, но ничего не хранит
function start_TestCore() {
	return new Promise((resolve) => {
		coreServer = http.createServer(async (request, response) => {
			const chunks = [];
			for await (const chunk of request) {
				chunks.push(chunk);
			}
			const body = Buffer.concat(chunks).toString('utf8');
			coreRequests.push({ method: request.method, url: request.url, authorization: request.headers.authorization, contentType: request.headers['content-type'], body });
			const send = (status, payload) => {
				response.writeHead(status, { 'Content-Type': 'application/json' });
				response.end(JSON.stringify(payload));
			};
			if (request.url === '/auth/config') {
				send(200, { onboarding: true, enableSignup: true, enableLdap: true, ldapLabel: 'CORP' });
				return;
			}
			if (request.url === '/auth/signup') {
				const parsed = JSON.parse(body);
				if (parsed.email === 'taken@corp.local') {
					send(409, { error: 'login_taken' });
					return;
				}
				send(200, { token: testToken, userId: 'user-1', login: parsed.email, displayName: parsed.displayName, accessLevel: 2, role: 'admin' });
				return;
			}
			if (request.url === '/auth/ldap') {
				const parsed = JSON.parse(body);
				if (parsed.password !== 'domain-pass') {
					send(401, { error: 'invalid_credentials' });
					return;
				}
				send(200, { token: testToken, userId: 'user-2', login: 'ivanov@corp.local', displayName: 'Иванов', accessLevel: 1, role: 'pending' });
				return;
			}
			if (request.url === '/auth/login') {
				const parsed = JSON.parse(body);
				if (parsed.password !== 'right') {
					send(401, { error: 'invalid_credentials' });
					return;
				}
				send(200, { token: testToken, userId: 'user-1', displayName: 'Тест', accessLevel: 1 });
				return;
			}
			if (request.headers.authorization !== `Bearer ${testToken}`) {
				send(401, { error: 'unauthorized' });
				return;
			}
			if (request.url === '/me') {
				send(200, { userId: 'user-1', displayName: 'Тест', accessLevel: 1 });
				return;
			}
			if (request.url === '/sessions' && request.method === 'POST') {
				send(200, { sessionId: 'session-1' });
				return;
			}
			if (request.url === '/documents/doc-1' && request.method === 'DELETE') {
				response.writeHead(204);
				response.end();
				return;
			}
			if (request.url === '/documents/contract-1/structure' && request.method === 'GET') {
				send(200, { doc_id: 'contract-1', display_name: 'Договор № 1', chunks: [{ path: '4.2', text: 'Оплата в течение 30 дней.' }] });
				return;
			}
			if (request.url === '/auth/admin/config') {
				send(200, request.method === 'POST' ? JSON.parse(body) : { enableSignup: true, defaultUserRole: 'pending', showAdminDetails: true });
				return;
			}
			if (request.url === '/drafts/check') {
				send(200, { recommendations: [], receivedBytes: body.length });
				return;
			}
			send(404, { error: 'not_found' });
		});
		coreServer.listen(0, '127.0.0.1', () => resolve(coreServer.address().port));
	});
}

function start_Web(corePort) {
	return new Promise((resolve, reject) => {
		const webPort = 30000 + Math.floor(Math.random() * 20000);
		webProcess = spawn(process.execPath, ['server/index.js'], {
			cwd: webRoot,
			env: {
				...process.env,
				CORE_API_URL: `http://127.0.0.1:${corePort}`,
				WEB_HOST: '127.0.0.1',
				WEB_PORT: String(webPort),
				DEBUG: 'false',
				LOGIN_MAX_ATTEMPTS: '3',
				MAX_UPLOAD_MB: '1',
				COOKIE_SECURE: 'false',
				COOKIE_SAMESITE: 'Lax',
			},
			stdio: ['ignore', 'pipe', 'pipe'],
		});
		webProcess.stdout.on('data', (chunk) => {
			if (String(chunk).includes('http://')) {
				resolve(`http://127.0.0.1:${webPort}`);
			}
		});
		webProcess.on('exit', (code) => reject(new Error(`web exited ${code}`)));
	});
}

async function login_AsTestUser() {
	const response = await fetch(`${webUrl}/api/auth/login`, {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({ login: 'user', password: 'right' }),
	});
	const cookie = response.headers.get('set-cookie').split(';')[0];
	const payload = await response.json();
	return { response, cookie, payload };
}

before(async () => {
	const corePort = await start_TestCore();
	webUrl = await start_Web(corePort);
});

after(() => {
	webProcess?.kill();
	coreServer?.close();
});

test('вход отдаёт пользователя без токена ядра и ставит httpOnly-cookie', async () => {
	const { response, payload } = await login_AsTestUser();
	assert.equal(response.status, 200);
	assert.equal(payload.user.displayName, 'Тест');
	assert.equal(payload.user.token, undefined);
	assert.ok(payload.csrfToken.length > 20);
	const setCookie = response.headers.get('set-cookie');
	assert.match(setCookie, /HttpOnly/);
	assert.match(setCookie, /SameSite=Lax/);
	assert.ok(!setCookie.includes(testToken));
});

test('публичные настройки отдают роли и допустимые форматы файлов', async () => {
	const response = await fetch(`${webUrl}/api/config`);
	const config = await response.json();
	assert.equal(config.adminAccessLevel, 2);
	assert.deepEqual(config.allowedUploadExtensions, ['.txt', '.pdf', '.docx']);
	assert.equal(config.maxUploadBytes, 1024 * 1024);
	assert.equal(config.legacyDouFeatures, false);
});

test('прокси добавляет Bearer-токен ядра и отдаёт ответ', async () => {
	const { cookie } = await login_AsTestUser();
	const response = await fetch(`${webUrl}/api/me`, { headers: { cookie } });
	assert.equal(response.status, 200);
	assert.equal((await response.json()).userId, 'user-1');
	assert.equal(coreRequests.at(-1).authorization, `Bearer ${testToken}`);
	assert.equal(response.headers.get('cache-control'), 'no-store');
});

test('запись без CSRF-токена отклоняется, с токеном проходит', async () => {
	const { cookie, payload } = await login_AsTestUser();
	const rejected = await fetch(`${webUrl}/api/sessions`, { method: 'POST', headers: { cookie } });
	assert.equal(rejected.status, 403);
	assert.equal((await rejected.json()).error, 'csrf_failed');
	const accepted = await fetch(`${webUrl}/api/sessions`, { method: 'POST', headers: { cookie, 'X-CSRF-Token': payload.csrfToken } });
	assert.equal(accepted.status, 200);
	assert.equal((await accepted.json()).sessionId, 'session-1');
});

test('удаление документа идёт через прокси только с CSRF-токеном', async () => {
	const { cookie, payload } = await login_AsTestUser();
	const rejected = await fetch(`${webUrl}/api/documents/doc-1`, { method: 'DELETE', headers: { cookie } });
	assert.equal(rejected.status, 403);
	const accepted = await fetch(`${webUrl}/api/documents/doc-1`, { method: 'DELETE', headers: { cookie, 'X-CSRF-Token': payload.csrfToken } });
	assert.equal(accepted.status, 204);
	assert.equal(coreRequests.at(-1).method, 'DELETE');
	assert.equal(coreRequests.at(-1).url, '/documents/doc-1');
});

test('параметры списка документов доходят до ядра', async () => {
	const { cookie } = await login_AsTestUser();
	await fetch(`${webUrl}/api/documents?scope=all`, { headers: { cookie } });
	assert.equal(coreRequests.at(-1).url, '/documents?scope=all');
});

test('структура документа проксируется с токеном ядра', async () => {
	const { cookie } = await login_AsTestUser();
	const response = await fetch(`${webUrl}/api/documents/contract-1/structure`, { headers: { cookie } });
	assert.equal(response.status, 200);
	const payload = await response.json();
	assert.equal(payload.chunks[0].path, '4.2');
	assert.equal(coreRequests.at(-1).authorization, `Bearer ${testToken}`);
});

test('запрос с чужого origin отклоняется', async () => {
	const { cookie, payload } = await login_AsTestUser();
	const response = await fetch(`${webUrl}/api/sessions`, {
		method: 'POST',
		headers: { cookie, 'X-CSRF-Token': payload.csrfToken, Origin: 'https://evil.example' },
	});
	assert.equal(response.status, 403);
	assert.equal((await response.json()).error, 'origin_denied');
});

test('процессы, роли, бланки, флаги и реквизиты организации уходят в ядро', async () => {
	const { cookie } = await login_AsTestUser();
	for (const path of ['/processes', '/roles', '/permissions', '/blanks/order/file', '/organization', '/organization/images/stamp', '/sources', '/sources/kinds']) {
		await fetch(`${webUrl}/api${path}`, { headers: { cookie } });
		assert.equal(coreRequests.at(-1).url, path);
	}
});

test('без сессии прокси отвечает 401, вход ядра через прокси недоступен', async () => {
	const anonymous = await fetch(`${webUrl}/api/me`);
	assert.equal(anonymous.status, 401);
	const { cookie } = await login_AsTestUser();
	const hiddenRoute = await fetch(`${webUrl}/api/docs`, { headers: { cookie } });
	assert.equal(hiddenRoute.status, 404);
});

test('настройки экрана входа отдаются без сессии', async () => {
	const response = await fetch(`${webUrl}/api/auth/config`);
	assert.equal(response.status, 200);
	const payload = await response.json();
	assert.equal(payload.onboarding, true);
	assert.equal(payload.ldapLabel, 'CORP');
	assert.equal(coreRequests.at(-1).authorization, undefined);
});

test('регистрация создаёт сессию веба и не отдаёт токен ядра', async () => {
	const response = await fetch(`${webUrl}/api/auth/signup`, {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({ email: ' boss@corp.local ', password: 'secret', displayName: ' Главный ', extra: 'drop-me' }),
	});
	assert.equal(response.status, 200);
	const payload = await response.json();
	assert.equal(payload.user.role, 'admin');
	assert.equal(payload.user.token, undefined);
	assert.match(response.headers.get('set-cookie'), /HttpOnly/);
	const signupRequest = coreRequests.findLast((request) => request.url === '/auth/signup' && request.method === 'POST');
	assert.deepEqual(JSON.parse(signupRequest.body), { email: 'boss@corp.local', password: 'secret', displayName: 'Главный' });

	const invalid = await fetch(`${webUrl}/api/auth/signup`, {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({ email: 'boss@corp.local', password: 'secret' }),
	});
	assert.equal(invalid.status, 400);
	const taken = await fetch(`${webUrl}/api/auth/signup`, {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({ email: 'taken@corp.local', password: 'secret', displayName: 'Двойник' }),
	});
	assert.equal(taken.status, 409);
	assert.equal((await taken.json()).error, 'login_taken');
});

test('вход через LDAP создаёт сессию, роль pending доходит до браузера', async () => {
	const response = await fetch(`${webUrl}/api/auth/ldap`, {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({ login: 'ivanov', password: 'domain-pass' }),
	});
	assert.equal(response.status, 200);
	const payload = await response.json();
	assert.equal(payload.user.role, 'pending');
	assert.equal(coreRequests.findLast((request) => request.method === 'POST').url, '/auth/ldap');
	const cookie = response.headers.get('set-cookie').split(';')[0];
	const session = await fetch(`${webUrl}/api/auth/session`, { headers: { cookie } });
	assert.equal(session.status, 200);
});

test('настройки входа администратора проксируются с токеном и CSRF', async () => {
	const { cookie, payload } = await login_AsTestUser();
	const read = await fetch(`${webUrl}/api/auth/admin/config`, { headers: { cookie } });
	assert.equal(read.status, 200);
	assert.equal((await read.json()).defaultUserRole, 'pending');
	assert.equal(coreRequests.at(-1).authorization, `Bearer ${testToken}`);

	const noCsrf = await fetch(`${webUrl}/api/auth/admin/config`, { method: 'POST', headers: { cookie, 'Content-Type': 'application/json' }, body: '{}' });
	assert.equal(noCsrf.status, 403);
	const saved = await fetch(`${webUrl}/api/auth/admin/config`, {
		method: 'POST',
		headers: { cookie, 'Content-Type': 'application/json', 'X-CSRF-Token': payload.csrfToken },
		body: JSON.stringify({ enableSignup: false }),
	});
	assert.equal(saved.status, 200);
	assert.equal((await saved.json()).enableSignup, false);

	const anonymous = await fetch(`${webUrl}/api/auth/admin/config`);
	assert.equal(anonymous.status, 401);
	const unknownAuthRoute = await fetch(`${webUrl}/api/auth/anything`, { headers: { cookie } });
	assert.equal(unknownAuthRoute.status, 404);
});

test('файл проходит потоком, слишком большой файл режется', async () => {
	const { cookie, payload } = await login_AsTestUser();
	const small = new FormData();
	small.append('file', new Blob(['черновик']), 'draft.txt');
	const accepted = await fetch(`${webUrl}/api/drafts/check`, {
		method: 'POST',
		headers: { cookie, 'X-CSRF-Token': payload.csrfToken },
		body: small,
	});
	assert.equal(accepted.status, 200);
	assert.match(coreRequests.at(-1).contentType, /multipart\/form-data/);

	const large = new FormData();
	large.append('file', new Blob([Buffer.alloc(2 * 1024 * 1024)]), 'large.bin');
	const rejected = await fetch(`${webUrl}/api/drafts/check`, {
		method: 'POST',
		headers: { cookie, 'X-CSRF-Token': payload.csrfToken },
		body: large,
	});
	assert.equal(rejected.status, 413);
});

test('после нескольких неверных паролей вход блокируется', async () => {
	let lastStatus = 0;
	for (let attempt = 0; attempt < 4; attempt += 1) {
		const response = await fetch(`${webUrl}/api/auth/login`, {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ login: 'user', password: 'wrong' }),
		});
		lastStatus = response.status;
	}
	assert.equal(lastStatus, 429);
});

test('статика: страницы с CSP, маршруты отдают оболочку, выход за public невозможен', async () => {
	const page = await fetch(`${webUrl}/c/session-1`);
	assert.equal(page.status, 200);
	assert.match(page.headers.get('content-security-policy'), /default-src 'self'/);
	assert.match(await page.text(), /js\/app\.js/);

	const embed = await fetch(`${webUrl}/embed`);
	assert.match(embed.headers.get('content-security-policy'), /frame-ancestors 'self'/);
	assert.equal(embed.headers.get('x-frame-options'), null);

	const loader = await fetch(`${webUrl}/widget.js`);
	assert.equal(loader.headers.get('cross-origin-resource-policy'), 'cross-origin');

	for (const attackPath of ['/%2e%2e/server/config.js', '/..%2fserver/config.js', '/css/..%2f..%2fserver/config.js', '/%2e%2e/.env']) {
		const response = await fetch(`${webUrl}${attackPath}`);
		const text = await response.text();
		assert.ok(!text.includes('load_Config') && !text.includes('CORE_API_URL='), `путь ${attackPath} вышел за public`);
	}
});
