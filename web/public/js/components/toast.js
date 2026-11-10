// Короткие уведомления в углу экрана.
import { create_Element, create_Icon } from '../lib/dom.js';

const toastLifetimeMs = 4500;
const iconByKind = { info: 'info', success: 'check', error: 'alert' };

function get_Container() {
	let container = document.getElementById('toasts');
	if (!container) {
		container = create_Element('div', { className: 'toasts', attrs: { id: 'toasts', role: 'status', 'aria-live': 'polite' } });
		document.body.append(container);
	}
	return container;
}

export function show_Toast(message, kind = 'info') {
	const toast = create_Element('div', { className: `toast toast-${kind}` }, [
		create_Icon(iconByKind[kind] ?? 'info'),
		create_Element('span', { text: message }),
	]);
	get_Container().append(toast);
	setTimeout(() => toast.remove(), toastLifetimeMs);
}
