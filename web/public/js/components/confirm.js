// Подтверждение необратимого действия. Возвращает Promise<boolean>.
import { create_Button, create_Element } from '../lib/dom.js';
import { AppTexts } from '../texts.js';
import { open_Modal } from './modal.js';

export function open_Confirm({ title, text, confirmLabel, danger = false }) {
	return new Promise((resolve) => {
		let confirmed = false;
		const confirmButton = create_Button(confirmLabel, {
			variant: danger ? 'danger' : 'primary',
			onClick: () => {
				confirmed = true;
				modal.close();
			},
		});
		const modal = open_Modal({
			title,
			body: [create_Element('p', { className: 'section-lead', text })],
			footer: [create_Button(AppTexts.cancel, { variant: 'ghost', onClick: () => modal.close() }), confirmButton],
			onClose: () => resolve(confirmed),
		});
		confirmButton.focus();
	});
}
