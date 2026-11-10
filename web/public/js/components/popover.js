// Всплывающее меню рядом с кнопкой. Закрывается по Esc, клику снаружи и выбору пункта.
import { create_Element } from '../lib/dom.js';

const viewportPadding = 8;

// placement: top-start (над кнопкой, как меню пользователя) или bottom-end
export function open_Popover(anchor, buildContent, { placement = 'bottom-end' } = {}) {
	const menu = create_Element('div', { className: 'menu', attrs: { role: 'menu' } });
	let closed = false;

	const close = () => {
		if (closed) {
			return;
		}
		closed = true;
		menu.remove();
		anchor.setAttribute('aria-expanded', 'false');
		document.removeEventListener('pointerdown', handle_OutsidePointer, true);
		document.removeEventListener('keydown', handle_KeyDown, true);
		window.removeEventListener('resize', close);
	};

	function handle_OutsidePointer(event) {
		if (!menu.contains(event.target) && !anchor.contains(event.target)) {
			close();
		}
	}

	function handle_KeyDown(event) {
		if (event.key === 'Escape') {
			event.stopPropagation();
			close();
			anchor.focus();
		}
	}

	menu.append(...[].concat(buildContent(close)).filter(Boolean));
	menu.style.position = 'fixed';
	menu.style.visibility = 'hidden';
	document.body.append(menu);

	const anchorRect = anchor.getBoundingClientRect();
	const menuRect = menu.getBoundingClientRect();
	let top;
	let left;
	if (placement === 'top-start') {
		top = anchorRect.top - menuRect.height - 6;
		left = anchorRect.left;
	} else {
		top = anchorRect.bottom + 6;
		left = anchorRect.right - menuRect.width;
	}
	top = Math.min(Math.max(viewportPadding, top), window.innerHeight - menuRect.height - viewportPadding);
	left = Math.min(Math.max(viewportPadding, left), window.innerWidth - menuRect.width - viewportPadding);
	menu.style.top = `${top}px`;
	menu.style.left = `${left}px`;
	menu.style.visibility = '';

	anchor.setAttribute('aria-expanded', 'true');
	document.addEventListener('pointerdown', handle_OutsidePointer, true);
	document.addEventListener('keydown', handle_KeyDown, true);
	window.addEventListener('resize', close);
	menu.querySelector('button, a')?.focus();

	return { close };
}

export function create_MenuItem(iconElement, label, onSelect, { href, dataLink = false } = {}) {
	const tagName = href ? 'a' : 'button';
	const attrs = href ? { href, role: 'menuitem' } : { type: 'button', role: 'menuitem' };
	if (dataLink) {
		attrs['data-link'] = true;
	}
	return create_Element(tagName, { className: 'menu-item', attrs, on: { click: onSelect } }, [
		iconElement,
		create_Element('span', { text: label }),
	]);
}
