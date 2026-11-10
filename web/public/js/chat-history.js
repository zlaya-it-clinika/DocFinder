// Список чатов для боковой панели.
// Основной источник - GET /sessions ядра. Пока ядро его не отдаёт, помним чаты,
// созданные в этом браузере: только id, заголовок и время, ответы всегда берутся из ядра.
// Переименовать и удалить можно, только если ядро разрешило это флагами canRename и canDelete
// (рядом со списком или у строк сессий).
// Закрепление - личная настройка этого браузера, ядру о нём знать не нужно.
import { check_IsNotSupported, delete_ChatSession, list_ChatSessions, rename_ChatSession } from './api.js';
import { read_Preference, write_Preference } from './preferences.js';
import { SidebarTexts } from './texts.js';

export const HistorySources = Object.freeze({
	core: 'core',
	local: 'local',
});

const maxLocalChats = 200;
const maxTitleLength = 120;

function get_StorageName(user) {
	return `chats.${user.userId}`;
}

function get_PinsName(user) {
	return `pinned.${user.userId}`;
}

function normalize_Item(raw) {
	if (!raw || typeof raw.sessionId !== 'string') {
		return null;
	}
	return {
		sessionId: raw.sessionId,
		title: typeof raw.title === 'string' && raw.title.trim() !== '' ? raw.title : SidebarTexts.untitledChat,
		createdAt: raw.createdAt ?? null,
		updatedAt: raw.updatedAt ?? raw.createdAt ?? null,
	};
}

// Флаги рядом со списком важнее. Ядро без них, но с флагами у строк, тоже разрешает действие
function read_Permissions(payload, rawItems) {
	const read_Flag = (name) => payload?.[name] === true || rawItems.some((raw) => raw?.[name] === true);
	return { canRename: read_Flag('canRename'), canDelete: read_Flag('canDelete') };
}

function sort_ByRecent(items) {
	return items.sort((first, second) => String(second.updatedAt ?? '').localeCompare(String(first.updatedAt ?? '')));
}

function read_LocalItems(user) {
	const stored = read_Preference(get_StorageName(user), []);
	return Array.isArray(stored) ? stored.map(normalize_Item).filter(Boolean) : [];
}

function write_LocalItems(user, items) {
	write_Preference(get_StorageName(user), items.slice(0, maxLocalChats));
}

function read_Pins(user) {
	const stored = read_Preference(get_PinsName(user), []);
	return new Set(Array.isArray(stored) ? stored.filter((value) => typeof value === 'string') : []);
}

export function normalize_ChatTitle(title) {
	return String(title ?? '').replace(/\s+/g, ' ').trim().slice(0, maxTitleLength);
}

export function create_ChatHistory(user) {
	let source = HistorySources.core;
	let items = [];
	let permissions = { canRename: false, canDelete: false };
	const pinned = read_Pins(user);
	const listeners = new Set();

	const snapshot = () => ({ source, items, permissions, pinned });

	const notify = () => {
		for (const listener of listeners) {
			listener(snapshot());
		}
	};

	const save_Local = () => {
		if (source === HistorySources.local) {
			write_LocalItems(user, items);
		}
	};

	return {
		subscribe(listener) {
			listeners.add(listener);
			return () => listeners.delete(listener);
		},

		get_Snapshot: snapshot,

		async load_Items() {
			try {
				const payload = await list_ChatSessions();
				const rawItems = Array.isArray(payload?.sessions) ? payload.sessions : [];
				source = HistorySources.core;
				items = sort_ByRecent(rawItems.map(normalize_Item).filter(Boolean));
				permissions = read_Permissions(payload, rawItems);
			} catch (error) {
				if (!check_IsNotSupported(error)) {
					throw error;
				}
				source = HistorySources.local;
				items = sort_ByRecent(read_LocalItems(user));
				permissions = { canRename: false, canDelete: false };
			}
			notify();
			return snapshot();
		},

		// Чат создан или получил новое сообщение
		remember_Chat(sessionId, title) {
			const now = new Date().toISOString();
			const existing = items.find((item) => item.sessionId === sessionId);
			if (existing) {
				existing.updatedAt = now;
				if (title && existing.title === SidebarTexts.untitledChat) {
					existing.title = title;
				}
			} else {
				items.unshift({ sessionId, title: title || SidebarTexts.untitledChat, createdAt: now, updatedAt: now });
			}
			sort_ByRecent(items);
			save_Local();
			notify();
		},

		// Ядро ответило 404 на чат из локального списка: убираем его
		forget_Chat(sessionId) {
			items = items.filter((item) => item.sessionId !== sessionId);
			if (pinned.delete(sessionId)) {
				write_Preference(get_PinsName(user), [...pinned]);
			}
			save_Local();
			notify();
		},

		async rename_Chat(sessionId, title) {
			const cleanTitle = normalize_ChatTitle(title);
			const item = items.find((entry) => entry.sessionId === sessionId);
			if (!item || cleanTitle === '' || cleanTitle === item.title) {
				return;
			}
			const saved = await rename_ChatSession(sessionId, cleanTitle);
			item.title = normalize_Item(saved)?.title ?? cleanTitle;
			save_Local();
			notify();
		},

		async delete_Chat(sessionId) {
			await delete_ChatSession(sessionId);
			this.forget_Chat(sessionId);
		},

		toggle_Pin(sessionId) {
			if (!pinned.delete(sessionId)) {
				pinned.add(sessionId);
			}
			write_Preference(get_PinsName(user), [...pinned]);
			notify();
		},
	};
}
