// Настройки веб-сервера: файл .env рядом с package.json и переменные окружения.
// Переменные окружения важнее файла, так удобнее в Docker.
import path from 'node:path';
import { fileURLToPath } from 'node:url';

import { ServerTexts } from './texts.js';

export const webRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
export const publicRoot = path.join(webRoot, 'public');

const allowedSameSite = ['Lax', 'Strict', 'None'];

function load_EnvFile() {
	try {
		process.loadEnvFile(path.join(webRoot, '.env'));
	} catch {
		// Файла нет - работаем только на переменных окружения
	}
}

function read_String(name, fallback = '') {
	const raw = process.env[name];
	if (raw === undefined) {
		return fallback;
	}
	return raw.trim();
}

function read_Integer(name, fallback) {
	const parsed = Number.parseInt(read_String(name), 10);
	return Number.isFinite(parsed) ? parsed : fallback;
}

function read_Boolean(name, fallback) {
	const raw = read_String(name).toLowerCase();
	if (raw === '') {
		return fallback;
	}
	return raw === 'true';
}

function read_List(name) {
	return read_String(name).split(/\s+/).filter(Boolean);
}

// Расширения в нижнем регистре с точкой: "txt pdf" -> [".txt", ".pdf"]
function read_Extensions(name, fallback) {
	const list = read_List(name);
	const source = list.length > 0 ? list : fallback;
	return source.map((item) => item.toLowerCase().replace(/^\.?/, '.'));
}

function normalize_SameSite(raw) {
	const found = allowedSameSite.find((value) => value.toLowerCase() === raw.toLowerCase());
	return found ?? '';
}

// Возвращает готовый объект настроек или бросает ошибку с понятным текстом
export function load_Config() {
	load_EnvFile();

	const coreApiUrl = read_String('CORE_API_URL').replace(/\/+$/, '');
	if (coreApiUrl === '') {
		throw new Error(ServerTexts.missingCoreUrl);
	}
	if (!/^https?:\/\//i.test(coreApiUrl)) {
		throw new Error(ServerTexts.invalidCoreUrl);
	}

	const cookieSecure = read_Boolean('COOKIE_SECURE', false);
	const cookieSameSite = normalize_SameSite(read_String('COOKIE_SAMESITE', 'Lax'));
	if (cookieSameSite === '') {
		throw new Error(ServerTexts.invalidSameSite);
	}
	if (cookieSameSite === 'None' && !cookieSecure) {
		throw new Error(ServerTexts.sameSiteNeedsSecure);
	}

	return Object.freeze({
		debug: read_Boolean('DEBUG', false),
		host: read_String('WEB_HOST', '0.0.0.0'),
		port: read_Integer('WEB_PORT', 3000),
		coreApiUrl,
		coreTimeoutMs: read_Integer('CORE_TIMEOUT_MS', 120000),
		adminAccessLevel: read_Integer('ADMIN_ACCESS_LEVEL', 2),
		userAccessLevel: read_Integer('USER_ACCESS_LEVEL', 1),
		sessionTtlMs: read_Integer('SESSION_TTL_MINUTES', 480) * 60 * 1000,
		cookieName: cookieSecure ? '__Host-ds_sid' : 'ds_sid',
		cookieSecure,
		cookieSameSite,
		embedAllowedOrigins: read_List('EMBED_ALLOWED_ORIGINS'),
		maxUploadBytes: read_Integer('MAX_UPLOAD_MB', 20) * 1024 * 1024,
		// Те же форматы, что разбирает ядро. Клиент проверяет файл до отправки
		allowedUploadExtensions: read_Extensions('ALLOWED_UPLOAD_EXTENSIONS', ['.txt', '.pdf', '.docx']),
		maxJsonBytes: 1024 * 1024,
		loginMaxAttempts: read_Integer('LOGIN_MAX_ATTEMPTS', 10),
		loginWindowMs: read_Integer('LOGIN_WINDOW_MINUTES', 15) * 60 * 1000,
		legacyDouFeatures: read_Boolean('LEGACY_DOU_FEATURES', false),
	});
}
