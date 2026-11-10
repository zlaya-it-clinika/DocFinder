// Мелкие помощники для ответов и чтения тела запроса.
import { ErrorCodes } from './texts.js';

export class RequestError extends Error {
	constructor(status, code) {
		super(code);
		this.status = status;
		this.code = code;
	}
}

export function send_Json(response, status, payload) {
	const body = JSON.stringify(payload);
	response.statusCode = status;
	response.setHeader('Content-Type', 'application/json; charset=utf-8');
	response.setHeader('Content-Length', Buffer.byteLength(body));
	response.end(body);
}

export function send_Error(response, status, code) {
	send_Json(response, status, { error: code });
}

export function send_Empty(response, status) {
	response.statusCode = status;
	response.end();
}

// Читает JSON целиком с ограничением размера
export async function read_JsonBody(request, maxBytes) {
	const chunks = [];
	let totalBytes = 0;
	for await (const chunk of request) {
		totalBytes += chunk.length;
		if (totalBytes > maxBytes) {
			throw new RequestError(413, ErrorCodes.payloadTooLarge);
		}
		chunks.push(chunk);
	}
	try {
		return JSON.parse(Buffer.concat(chunks).toString('utf8'));
	} catch {
		throw new RequestError(400, ErrorCodes.invalidBody);
	}
}

// Пропускает тело потоком и обрывает его, если файл больше разрешённого
export async function* limit_BodyStream(request, maxBytes) {
	let totalBytes = 0;
	for await (const chunk of request) {
		totalBytes += chunk.length;
		if (totalBytes > maxBytes) {
			throw new RequestError(413, ErrorCodes.payloadTooLarge);
		}
		yield chunk;
	}
}

export function read_ClientAddress(request) {
	return request.socket.remoteAddress ?? 'unknown';
}
