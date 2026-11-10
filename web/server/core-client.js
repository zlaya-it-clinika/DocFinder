// Запросы к Go-ядру. Браузер в ядро напрямую не ходит.
import { Readable } from 'node:stream';

import { ErrorCodes } from './texts.js';
import { RequestError, limit_BodyStream, send_Error } from './http-utils.js';

// Заголовки ответа ядра, которые имеет смысл отдать браузеру
const passedResponseHeaders = ['content-type', 'content-disposition', 'last-modified', 'etag'];

function map_FetchError(error) {
	if (error instanceof RequestError) {
		return error;
	}
	// Ошибка из потока тела (например, файл больше лимита) приходит обёрнутой в cause
	if (error?.cause instanceof RequestError) {
		return error.cause;
	}
	if (error?.name === 'TimeoutError' || error?.name === 'AbortError') {
		return new RequestError(504, ErrorCodes.coreTimeout);
	}
	return new RequestError(502, ErrorCodes.coreUnavailable);
}

// Короткий JSON-запрос из самого веб-сервера (вход, профиль, выход)
export async function call_CoreJson(config, method, corePath, { token, body } = {}) {
	const headers = { Accept: 'application/json' };
	if (token) {
		headers.Authorization = `Bearer ${token}`;
	}
	if (body !== undefined) {
		headers['Content-Type'] = 'application/json';
	}
	let coreResponse;
	try {
		coreResponse = await fetch(config.coreApiUrl + corePath, {
			method,
			headers,
			body: body === undefined ? undefined : JSON.stringify(body),
			signal: AbortSignal.timeout(config.coreTimeoutMs),
		});
	} catch (error) {
		throw map_FetchError(error);
	}
	const text = await coreResponse.text();
	let payload = null;
	try {
		payload = text === '' ? null : JSON.parse(text);
	} catch {
		payload = null;
	}
	return { status: coreResponse.status, payload };
}

// Прокси /api/* в ядро потоком: тело запроса (в том числе multipart с файлом) и тело ответа.
// onUnauthorized вызывается, если ядро больше не знает токен.
export async function forward_ToCore(config, request, response, { corePath, token, onUnauthorized }) {
	const headers = {
		Accept: request.headers.accept ?? '*/*',
		Authorization: `Bearer ${token}`,
	};
	const contentType = request.headers['content-type'];
	const hasBody = !['GET', 'HEAD'].includes(request.method);
	if (hasBody && contentType) {
		headers['Content-Type'] = contentType;
	}

	let coreResponse;
	try {
		coreResponse = await fetch(config.coreApiUrl + corePath, {
			method: request.method,
			headers,
			body: hasBody ? limit_BodyStream(request, config.maxUploadBytes) : undefined,
			duplex: hasBody ? 'half' : undefined,
			signal: AbortSignal.timeout(config.coreTimeoutMs),
		});
	} catch (error) {
		const mapped = map_FetchError(error);
		send_Error(response, mapped.status, mapped.code);
		return mapped.status;
	}

	if (coreResponse.status === 401) {
		onUnauthorized?.();
	}

	response.statusCode = coreResponse.status;
	for (const name of passedResponseHeaders) {
		const value = coreResponse.headers.get(name);
		if (value) {
			response.setHeader(name, value);
		}
	}
	if (!coreResponse.body) {
		response.end();
		return coreResponse.status;
	}
	await new Promise((resolve) => {
		const bodyStream = Readable.fromWeb(coreResponse.body);
		bodyStream.on('error', () => {
			response.destroy();
			resolve();
		});
		response.on('close', resolve);
		bodyStream.pipe(response);
	});
	return coreResponse.status;
}
