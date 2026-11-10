// Заголовки безопасности, проверка Origin и ограничение попыток входа.
import crypto from 'node:crypto';

// Политика для обычных страниц: всё только со своего origin, встраивать нельзя
function build_ContentSecurityPolicy(frameAncestors) {
	return [
		"default-src 'self'",
		"script-src 'self'",
		"style-src 'self'",
		"img-src 'self' data: blob:",
		"font-src 'self'",
		"connect-src 'self'",
		"object-src 'none'",
		"base-uri 'none'",
		"form-action 'self'",
		`frame-ancestors ${frameAncestors}`,
	].join('; ');
}

// kind: page - страница приложения, embed - страница для iframe виджета,
// loader - widget.js, который подключают с чужого портала, api - ответы JSON
export function apply_SecurityHeaders(response, config, kind) {
	response.setHeader('X-Content-Type-Options', 'nosniff');
	response.setHeader('Referrer-Policy', 'no-referrer');
	response.setHeader('Permissions-Policy', 'camera=(), microphone=(), geolocation=(), payment=()');

	if (kind === 'loader') {
		// Скрипт виджета должен грузиться с портала
		response.setHeader('Cross-Origin-Resource-Policy', 'cross-origin');
		return;
	}
	response.setHeader('Cross-Origin-Resource-Policy', 'same-origin');

	if (kind === 'api') {
		response.setHeader('Cache-Control', 'no-store');
		// Ответ API (JSON или файл документа) не страница: если браузер всё же откроет его,
		// скрипты, стили и встраивание запрещены
		response.setHeader('Content-Security-Policy', "default-src 'none'; frame-ancestors 'none'; sandbox");
		return;
	}
	const frameAncestors = kind === 'embed'
		? ["'self'", ...config.embedAllowedOrigins].join(' ')
		: "'self'";
	response.setHeader('Content-Security-Policy', build_ContentSecurityPolicy(frameAncestors));
	if (kind !== 'embed') {
		response.setHeader('X-Frame-Options', 'SAMEORIGIN');
	}
}

// Запросы, меняющие данные, принимаем только со своего origin или с разрешённых порталов.
// Если браузер не прислал Origin (старые клиенты, curl), решает CSRF-токен.
export function check_RequestOrigin(request, config) {
	const origin = request.headers.origin;
	if (!origin) {
		return true;
	}
	const host = request.headers.host;
	try {
		if (new URL(origin).host === host) {
			return true;
		}
	} catch {
		return false;
	}
	return config.embedAllowedOrigins.includes(origin);
}

// Сравнение токенов без утечки по времени
export function match_CsrfToken(expected, given) {
	if (typeof given !== 'string' || given.length !== expected.length) {
		return false;
	}
	return crypto.timingSafeEqual(Buffer.from(expected), Buffer.from(given));
}

// Ограничение попыток входа с одного адреса за окно времени
export function create_LoginLimiter(config) {
	const attemptsByAddress = new Map();

	const cleanupTimer = setInterval(() => {
		const now = Date.now();
		for (const [address, record] of attemptsByAddress) {
			if (now - record.windowStartedAt > config.loginWindowMs) {
				attemptsByAddress.delete(address);
			}
		}
	}, config.loginWindowMs);
	cleanupTimer.unref();

	return {
		// true, если адрес исчерпал попытки
		check_IsBlocked(address) {
			const record = attemptsByAddress.get(address);
			if (!record) {
				return false;
			}
			if (Date.now() - record.windowStartedAt > config.loginWindowMs) {
				attemptsByAddress.delete(address);
				return false;
			}
			return record.count >= config.loginMaxAttempts;
		},

		register_Failure(address) {
			const record = attemptsByAddress.get(address);
			if (!record || Date.now() - record.windowStartedAt > config.loginWindowMs) {
				attemptsByAddress.set(address, { count: 1, windowStartedAt: Date.now() });
				return;
			}
			record.count += 1;
		},

		reset_Address(address) {
			attemptsByAddress.delete(address);
		},
	};
}
