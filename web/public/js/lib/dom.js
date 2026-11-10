// Сборка DOM без innerHTML: данные из API попадают в страницу только через textContent.

const svgNamespace = 'http://www.w3.org/2000/svg';
const iconSpritePath = '/icons.svg';

export function append_Children(parent, children) {
	for (const child of [].concat(children)) {
		if (child === null || child === undefined || child === false) {
			continue;
		}
		parent.append(child instanceof Node ? child : String(child));
	}
	return parent;
}

// options: className, text, attrs, dataset, on (обработчики событий)
export function create_Element(tagName, options = {}, children = []) {
	const element = document.createElement(tagName);
	const { className, text, attrs, dataset, on } = options;
	if (className) {
		element.className = className;
	}
	if (text !== undefined && text !== null) {
		element.textContent = String(text);
	}
	if (attrs) {
		for (const [name, value] of Object.entries(attrs)) {
			if (value === false || value === null || value === undefined) {
				continue;
			}
			element.setAttribute(name, value === true ? '' : String(value));
		}
	}
	if (dataset) {
		Object.assign(element.dataset, dataset);
	}
	if (on) {
		for (const [eventName, handler] of Object.entries(on)) {
			element.addEventListener(eventName, handler);
		}
	}
	return append_Children(element, children);
}

export function create_Icon(name, className = 'icon') {
	const svg = document.createElementNS(svgNamespace, 'svg');
	svg.setAttribute('class', className);
	svg.setAttribute('aria-hidden', 'true');
	svg.setAttribute('focusable', 'false');
	const use = document.createElementNS(svgNamespace, 'use');
	use.setAttribute('href', `${iconSpritePath}#${name}`);
	svg.append(use);
	return svg;
}

// Пиксельный логотип DocFinder. Подпись рядом, поэтому для экранного диктора картинка декоративная
export function create_Logo(className = 'brand-logo') {
	return create_Element('img', {
		className,
		attrs: { src: '/logo-192.png', alt: '', width: 192, height: 192, draggable: 'false', 'aria-hidden': 'true' },
	});
}

export function create_IconButton(iconName, label, onClick, className = 'icon-button') {
	return create_Element('button', {
		className,
		attrs: { type: 'button', 'aria-label': label, title: label },
		on: onClick ? { click: onClick } : undefined,
	}, [create_Icon(iconName)]);
}

export function create_Button(label, { variant = '', icon = '', onClick, type = 'button', attrs = {} } = {}) {
	const className = ['button', variant ? `button-${variant}` : ''].filter(Boolean).join(' ');
	return create_Element('button', {
		className,
		attrs: { type, ...attrs },
		on: onClick ? { click: onClick } : undefined,
	}, [icon ? create_Icon(icon, 'icon-sm') : null, create_Element('span', { text: label })]);
}

export function create_Spinner() {
	return create_Element('span', { className: 'spinner', attrs: { 'aria-hidden': 'true' } });
}

export function create_LoadingBlock(label) {
	return create_Element('div', { className: 'loading-block', attrs: { role: 'status' } }, [
		create_Spinner(),
		create_Element('span', { text: label }),
	]);
}

export function create_EmptyBlock(iconName, title, children = []) {
	return create_Element('div', { className: 'empty-block' }, [
		create_Icon(iconName, 'icon-lg'),
		create_Element('div', { text: title }),
		...children,
	]);
}

export function create_SearchBox(placeholder, onInput) {
	const input = create_Element('input', {
		className: 'input input-search',
		attrs: { type: 'search', placeholder, 'aria-label': placeholder, autocomplete: 'off' },
		on: { input: () => onInput(input.value.trim().toLowerCase()) },
	});
	return create_Element('div', { className: 'search-box' }, [create_Icon('search'), input]);
}

// Копирование в буфер с запасным вариантом для http без secure context
export async function copy_ToClipboard(text) {
	if (navigator.clipboard && window.isSecureContext) {
		await navigator.clipboard.writeText(text);
		return;
	}
	const helper = create_Element('textarea', { attrs: { readonly: true, 'aria-hidden': 'true' } });
	helper.value = text;
	helper.style.position = 'fixed';
	helper.style.opacity = '0';
	document.body.append(helper);
	helper.select();
	document.execCommand('copy');
	helper.remove();
}

// Сохранение текста файлом без обращения к серверу
export function download_TextFile(fileName, text) {
	const blob = new Blob([text], { type: 'text/plain;charset=utf-8' });
	const url = URL.createObjectURL(blob);
	const link = create_Element('a', { attrs: { href: url, download: fileName } });
	document.body.append(link);
	link.click();
	link.remove();
	setTimeout(() => URL.revokeObjectURL(url), 1000);
}
