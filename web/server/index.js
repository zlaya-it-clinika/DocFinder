// Точка входа веб-сервера DocFinder: статика, прокси /api в ядро, сессии браузера.
import http from 'node:http';

import { handle_Api } from './api.js';
import { load_Config } from './config.js';
import { RequestError, send_Error, send_Json } from './http-utils.js';
import { create_LoginLimiter } from './security.js';
import { create_SessionStore } from './sessions.js';
import { serve_Static } from './static.js';
import { ErrorCodes, ServerTexts } from './texts.js';

let config;
try {
	config = load_Config();
} catch (error) {
	console.error(error.message);
	process.exit(1);
}

const sessionStore = create_SessionStore(config);
const loginLimiter = create_LoginLimiter(config);

function log_RequestWhenDebug(request, response, startedAt) {
	if (!config.debug) {
		return;
	}
	response.on('finish', () => {
		// Строку запроса не пишем: там могут быть данные пользователя
		const pathOnly = (request.url ?? '').split('?')[0];
		console.log(`${request.method} ${pathOnly} ${response.statusCode} ${Date.now() - startedAt}ms`);
	});
}

async function handle_Request(request, response) {
	log_RequestWhenDebug(request, response, Date.now());
	const url = new URL(request.url ?? '/', 'http://localhost');
	const context = { config, request, response, sessionStore, loginLimiter };
	try {
		if (url.pathname === '/healthz') {
			send_Json(response, 200, { status: 'ok' });
			return;
		}
		if (url.pathname === '/api' || url.pathname.startsWith('/api/')) {
			await handle_Api(context, url);
			return;
		}
		await serve_Static(config, request, response, url.pathname);
	} catch (error) {
		if (!(error instanceof RequestError) && config.debug) {
			console.error('Необработанная ошибка запроса:', error);
		}
		if (response.headersSent) {
			response.destroy();
			return;
		}
		if (error instanceof RequestError) {
			send_Error(response, error.status, error.code);
			return;
		}
		send_Error(response, 500, ErrorCodes.internalError);
	}
}

const server = http.createServer((request, response) => {
	handle_Request(request, response);
});

// Ответ локальной модели может идти долго, обычный таймаут Node его бы оборвал
server.requestTimeout = config.coreTimeoutMs + 10000;

server.listen(config.port, config.host, () => {
	console.log(`${ServerTexts.listening} http://${config.host}:${config.port}`);
	console.log(`${ServerTexts.coreTarget}: ${config.coreApiUrl}`);
	if (config.debug) {
		console.log('DEBUG включён: логируем каждый запрос без строки параметров');
	}
});

function stop_Server() {
	server.close(() => process.exit(0));
	setTimeout(() => process.exit(0), 5000).unref();
}

process.on('SIGTERM', stop_Server);
process.on('SIGINT', stop_Server);
