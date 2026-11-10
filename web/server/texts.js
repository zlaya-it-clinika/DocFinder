// Фиксированные тексты и коды ошибок веб-сервера.
// Подписи интерфейса лежат отдельно, в public/js/texts.js.

// Коды ошибок, которые веб-сервер добавляет к кодам ядра. Форма та же: {"error": "<код>"}
export const ErrorCodes = Object.freeze({
	unauthorized: 'unauthorized',
	invalidBody: 'invalid_body',
	notFound: 'not_found',
	csrfFailed: 'csrf_failed',
	originDenied: 'origin_denied',
	payloadTooLarge: 'payload_too_large',
	tooManyAttempts: 'too_many_attempts',
	coreUnavailable: 'core_unavailable',
	coreTimeout: 'core_timeout',
	methodNotAllowed: 'method_not_allowed',
	internalError: 'internal_error',
});

export const ServerTexts = Object.freeze({
	missingCoreUrl: 'Не задан CORE_API_URL. Скопируйте .env.example в .env и укажите адрес ядра.',
	invalidCoreUrl: 'CORE_API_URL не похож на адрес http(s)://.',
	invalidSameSite: 'COOKIE_SAMESITE должен быть Lax, Strict или None.',
	sameSiteNeedsSecure: 'COOKIE_SAMESITE=None работает только вместе с COOKIE_SECURE=true.',
	listening: 'Веб DocFinder слушает',
	coreTarget: 'Ядро',
});
