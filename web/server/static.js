// Раздача файлов из public/ и маршруты одностраничного приложения.
import fs from 'node:fs';
import path from 'node:path';

import { publicRoot } from './config.js';
import { apply_SecurityHeaders } from './security.js';
import { ErrorCodes } from './texts.js';
import { send_Error } from './http-utils.js';

const mimeByExtension = {
	'.html': 'text/html; charset=utf-8',
	'.js': 'text/javascript; charset=utf-8',
	'.css': 'text/css; charset=utf-8',
	'.svg': 'image/svg+xml',
	'.png': 'image/png',
	'.ico': 'image/x-icon',
	'.json': 'application/json; charset=utf-8',
	'.txt': 'text/plain; charset=utf-8',
	'.woff2': 'font/woff2',
};

// Пути приложения без расширения отдают оболочку, дальше разбирает клиентский роутер
const appShellFile = 'index.html';
const embedShellFile = 'embed.html';
const loaderFile = 'widget.js';
// Файлы, которые страница портала грузит с нашего сервера: скрипт виджета и логотип для его кнопки
const crossOriginFiles = new Set([loaderFile, 'logo-192.png', 'favicon-32.png']);

function resolve_PublicPath(urlPath) {
	let decoded;
	try {
		decoded = decodeURIComponent(urlPath);
	} catch {
		return null;
	}
	if (decoded.includes('\0')) {
		return null;
	}
	const resolved = path.resolve(publicRoot, '.' + path.posix.normalize('/' + decoded));
	if (resolved !== publicRoot && !resolved.startsWith(publicRoot + path.sep)) {
		return null;
	}
	return resolved;
}

function pick_HeaderKind(relativePath) {
	if (crossOriginFiles.has(relativePath)) {
		return 'loader';
	}
	if (relativePath === embedShellFile) {
		return 'embed';
	}
	return 'page';
}

async function send_File(config, request, response, filePath) {
	const relativePath = path.relative(publicRoot, filePath).split(path.sep).join('/');
	const stat = await fs.promises.stat(filePath);
	const contentType = mimeByExtension[path.extname(filePath).toLowerCase()] ?? 'application/octet-stream';
	apply_SecurityHeaders(response, config, pick_HeaderKind(relativePath));
	response.statusCode = 200;
	response.setHeader('Content-Type', contentType);
	response.setHeader('Content-Length', stat.size);
	// Прототип часто меняется: браузер каждый раз сверяется с сервером
	response.setHeader('Cache-Control', 'no-cache');
	response.setHeader('Last-Modified', stat.mtime.toUTCString());
	if (request.method === 'HEAD') {
		response.end();
		return;
	}
	await new Promise((resolve, reject) => {
		const fileStream = fs.createReadStream(filePath);
		fileStream.on('error', reject);
		response.on('close', resolve);
		fileStream.pipe(response);
	});
}

async function check_IsFile(filePath) {
	try {
		return (await fs.promises.stat(filePath)).isFile();
	} catch {
		return false;
	}
}

export async function serve_Static(config, request, response, urlPath) {
	if (!['GET', 'HEAD'].includes(request.method)) {
		send_Error(response, 405, ErrorCodes.methodNotAllowed);
		return;
	}
	const filePath = resolve_PublicPath(urlPath);
	if (filePath && await check_IsFile(filePath)) {
		await send_File(config, request, response, filePath);
		return;
	}
	// Страница для iframe виджета
	if (urlPath === '/embed' || urlPath.startsWith('/embed/')) {
		await send_File(config, request, response, path.join(publicRoot, embedShellFile));
		return;
	}
	// Путь без расширения - маршрут приложения: /c/<id>, /admin/users и так далее
	if (path.extname(urlPath) === '') {
		await send_File(config, request, response, path.join(publicRoot, appShellFile));
		return;
	}
	send_Error(response, 404, ErrorCodes.notFound);
}
