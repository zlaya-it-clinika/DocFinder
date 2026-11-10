// Сессии браузера на стороне веб-сервера (схема BFF).
// Токен ядра живёт только здесь, браузер получает случайный идентификатор в httpOnly-cookie.
// Хранилище в памяти: после перезапуска веба сотрудник входит заново.
import crypto from 'node:crypto';

const cleanupIntervalMs = 60 * 1000;

function create_RandomId() {
	return crypto.randomBytes(32).toString('base64url');
}

export function create_SessionStore(config) {
	const sessionsById = new Map();

	const cleanupTimer = setInterval(() => {
		const now = Date.now();
		for (const [sessionId, session] of sessionsById) {
			if (now - session.lastSeenAt > config.sessionTtlMs) {
				sessionsById.delete(sessionId);
			}
		}
	}, cleanupIntervalMs);
	cleanupTimer.unref();

	return {
		// Новая сессия после успешного входа в ядро
		create_Session(coreToken, user) {
			const session = {
				id: create_RandomId(),
				csrfToken: create_RandomId(),
				coreToken,
				user,
				createdAt: Date.now(),
				lastSeenAt: Date.now(),
			};
			sessionsById.set(session.id, session);
			return session;
		},

		// Ищет живую сессию и продлевает её
		find_Session(sessionId) {
			if (!sessionId) {
				return null;
			}
			const session = sessionsById.get(sessionId);
			if (!session) {
				return null;
			}
			if (Date.now() - session.lastSeenAt > config.sessionTtlMs) {
				sessionsById.delete(sessionId);
				return null;
			}
			session.lastSeenAt = Date.now();
			return session;
		},

		update_SessionUser(sessionId, user) {
			const session = sessionsById.get(sessionId);
			if (session) {
				session.user = user;
			}
		},

		destroy_Session(sessionId) {
			sessionsById.delete(sessionId);
		},

		count_Sessions() {
			return sessionsById.size;
		},
	};
}
