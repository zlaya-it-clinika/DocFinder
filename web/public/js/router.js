// Клиентский роутер на History API. Ссылки с атрибутом data-link не перезагружают страницу.

const routes = [];
let notFoundHandler = null;

function compile_Pattern(pattern) {
	const names = [];
	const source = pattern.replace(/\/:([a-zA-Z]+)/g, (match, name) => {
		names.push(name);
		return '/([^/]+)';
	});
	return { regex: new RegExp(`^${source}/?$`), names };
}

export function define_Route(pattern, handler) {
	routes.push({ ...compile_Pattern(pattern), handler });
}

export function define_NotFound(handler) {
	notFoundHandler = handler;
}

function resolve_Route() {
	const path = window.location.pathname;
	for (const route of routes) {
		const match = path.match(route.regex);
		if (!match) {
			continue;
		}
		const params = {};
		route.names.forEach((name, index) => {
			try {
				params[name] = decodeURIComponent(match[index + 1]);
			} catch {
				params[name] = match[index + 1];
			}
		});
		route.handler(params);
		return;
	}
	notFoundHandler?.();
}

// silent: только поменять адрес, без перерисовки (чат сам создал сессию)
export function navigate_To(path, { replace = false, silent = false } = {}) {
	if (path === window.location.pathname && !silent) {
		resolve_Route();
		return;
	}
	if (replace) {
		window.history.replaceState(null, '', path);
	} else {
		window.history.pushState(null, '', path);
	}
	if (!silent) {
		resolve_Route();
	}
}

export function start_Router() {
	window.addEventListener('popstate', resolve_Route);
	document.addEventListener('click', (event) => {
		if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) {
			return;
		}
		const link = event.target.closest('a[data-link]');
		if (!link || link.target === '_blank') {
			return;
		}
		event.preventDefault();
		navigate_To(link.getAttribute('href'));
	});
	resolve_Route();
}
