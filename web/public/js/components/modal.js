// Модальное окно на <dialog>: фокус внутри окна, Esc закрывает, клик по фону тоже.
import { create_Element, create_IconButton } from '../lib/dom.js';
import { AppTexts } from '../texts.js';

export function open_Modal({ title, body = [], footer = [], wide = false, onClose } = {}) {
	const titleId = `modal-title-${Math.random().toString(36).slice(2)}`;
	const bodyElement = create_Element('div', { className: 'modal-body' }, body);
	const footerElement = create_Element('div', { className: 'modal-footer' }, footer);
	footerElement.hidden = footer.length === 0;

	const dialog = create_Element('dialog', {
		className: wide ? 'modal modal-wide' : 'modal',
		attrs: { 'aria-labelledby': titleId },
	});
	// Закрытие не ждёт события close: в некоторых браузерах оно приходит с задержкой или не приходит вовсе
	let finished = false;
	const finish_Close = () => {
		if (finished) {
			return;
		}
		finished = true;
		if (dialog.open) {
			dialog.close();
		}
		dialog.remove();
		onClose?.();
	};
	const closeButton = create_IconButton('close', AppTexts.close, finish_Close);
	dialog.append(create_Element('div', { className: 'modal-inner' }, [
		create_Element('div', { className: 'modal-header' }, [
			create_Element('h2', { className: 'modal-title', text: title, attrs: { id: titleId } }),
			closeButton,
		]),
		bodyElement,
		footerElement,
	]));

	// Esc: закрываем сами, не дожидаясь события close
	dialog.addEventListener('cancel', (event) => {
		event.preventDefault();
		finish_Close();
	});
	dialog.addEventListener('close', finish_Close);
	// Клик точно по фону: у самого dialog нет отступов, содержимое занимает его целиком
	dialog.addEventListener('click', (event) => {
		if (event.target === dialog) {
			finish_Close();
		}
	});

	document.body.append(dialog);
	dialog.showModal();

	return {
		dialog,
		bodyElement,
		footerElement,
		close: finish_Close,
		set_Footer(children) {
			footerElement.replaceChildren(...children);
			footerElement.hidden = children.length === 0;
		},
	};
}
