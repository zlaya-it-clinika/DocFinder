// Личные настройки этого браузера: тема и свёрнутая панель.
// localStorage может быть недоступен (приватный режим, политика компании) - тогда работаем без него.

const storagePrefix = 'docfinder.';

export const Themes = Object.freeze({
	system: 'system',
	light: 'light',
	dark: 'dark',
});

export function read_Preference(name, fallback) {
	try {
		const value = window.localStorage.getItem(storagePrefix + name);
		return value === null ? fallback : JSON.parse(value);
	} catch {
		return fallback;
	}
}

export function write_Preference(name, value) {
	try {
		window.localStorage.setItem(storagePrefix + name, JSON.stringify(value));
	} catch {
		// Хранилище недоступно - настройка просто не запомнится
	}
}

export function remove_Preference(name) {
	try {
		window.localStorage.removeItem(storagePrefix + name);
	} catch {
		// Нечего удалять
	}
}

export function apply_Theme(theme) {
	const root = document.documentElement;
	if (theme === Themes.light || theme === Themes.dark) {
		root.dataset.theme = theme;
	} else {
		delete root.dataset.theme;
	}
}

export function get_Theme() {
	const theme = read_Preference('theme', Themes.system);
	return Object.values(Themes).includes(theme) ? theme : Themes.system;
}

export function set_Theme(theme) {
	write_Preference('theme', theme);
	apply_Theme(theme);
}
