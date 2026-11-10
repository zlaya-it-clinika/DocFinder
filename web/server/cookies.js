// Разбор и сборка cookie без внешних библиотек.

export function parse_Cookies(headerValue) {
	const cookies = {};
	if (!headerValue) {
		return cookies;
	}
	for (const part of headerValue.split(';')) {
		const separatorIndex = part.indexOf('=');
		if (separatorIndex <= 0) {
			continue;
		}
		const name = part.slice(0, separatorIndex).trim();
		const value = part.slice(separatorIndex + 1).trim();
		try {
			cookies[name] = decodeURIComponent(value);
		} catch {
			// Битое значение пропускаем, сессия просто не найдётся
		}
	}
	return cookies;
}

// maxAgeSeconds = 0 удаляет cookie
export function build_SessionCookie(config, value, maxAgeSeconds) {
	const parts = [
		`${config.cookieName}=${encodeURIComponent(value)}`,
		'Path=/',
		'HttpOnly',
		`SameSite=${config.cookieSameSite}`,
		`Max-Age=${Math.max(0, Math.floor(maxAgeSeconds))}`,
	];
	if (config.cookieSecure) {
		parts.push('Secure');
	}
	return parts.join('; ');
}
