// Подтверждение исправления оформления. Если в документе нет реквизитов, помощник спрашивает,
// кто подписывает, кому адресован документ и о чем он. Должностные лица - из вкладки «Организация».
// Результат: null - отмена, иначе поля для POST /drafts/fix (signerId, addresseeId, title)
import { create_Element } from '../lib/dom.js';
import { AppTexts, ChatTexts } from '../texts.js';
import { open_Confirm } from './confirm.js';
import { open_Modal } from './modal.js';

const Requisites = Object.freeze({ addressee: 'addressee', title: 'title', signature: 'signature' });

function create_Field(id, label, control, hint = '') {
	return create_Element('div', { className: 'field' }, [
		create_Element('label', { className: 'field-label', text: label, attrs: { for: id } }),
		control,
		hint ? create_Element('span', { className: 'field-hint', text: hint }) : null,
	]);
}

// Список должностных лиц с первым пунктом «Не добавлять». selected - код выбранного
function create_OfficialSelect(id, officials, selected) {
	return create_Element('select', { className: 'select', attrs: { id } }, [
		create_Element('option', { text: ChatTexts.formatFixSkip, attrs: { value: '' } }),
		...officials.map((official) => create_Element('option', {
			text: official.label,
			attrs: { value: official.officialId, selected: official.officialId === selected },
		})),
	]);
}

export function open_FormatFixDialog(requisites) {
	const missing = Array.isArray(requisites?.missing) ? requisites.missing : [];
	if (missing.length === 0) {
		return open_Confirm({
			title: ChatTexts.formatFixConfirmTitle,
			text: ChatTexts.formatFixConfirmText,
			confirmLabel: ChatTexts.formatFixConfirm,
		}).then((confirmed) => (confirmed ? {} : null));
	}
	const officials = Array.isArray(requisites.officials) ? requisites.officials : [];
	const chief = officials.find((official) => official.chief);
	const fields = [];
	let signerSelect = null;
	let addresseeSelect = null;
	let titleInput = null;
	if (missing.includes(Requisites.signature) && officials.length > 0) {
		signerSelect = create_OfficialSelect('fix-signer', officials, chief?.officialId ?? '');
		fields.push(create_Field('fix-signer', ChatTexts.formatFixSigner, signerSelect, ChatTexts.formatFixSignerHint));
	}
	if (missing.includes(Requisites.addressee) && officials.length > 0) {
		addresseeSelect = create_OfficialSelect('fix-addressee', officials, '');
		fields.push(create_Field('fix-addressee', ChatTexts.formatFixAddressee, addresseeSelect, ChatTexts.formatFixAddresseeHint));
	}
	if (missing.includes(Requisites.title)) {
		titleInput = create_Element('input', { className: 'input', attrs: { id: 'fix-title', autocomplete: 'off', placeholder: ChatTexts.formatFixTitlePlaceholder } });
		fields.push(create_Field('fix-title', ChatTexts.formatFixSubject, titleInput, ChatTexts.formatFixTitleHint));
	}
	if (officials.length === 0 && (missing.includes(Requisites.signature) || missing.includes(Requisites.addressee))) {
		fields.push(create_Element('div', { className: 'callout-hint', text: ChatTexts.formatFixNoOfficials }));
	}
	return new Promise((resolve) => {
		let answered = false;
		const finish = (value) => {
			if (answered) {
				return;
			}
			answered = true;
			resolve(value);
		};
		const modal = open_Modal({
			title: ChatTexts.formatFixConfirmTitle,
			body: [
				create_Element('p', { className: 'callout-hint', text: ChatTexts.formatFixQuestionsLead }),
				create_Element('div', { className: 'form-grid' }, fields),
			],
			onClose: () => finish(null),
		});
		modal.set_Footer([
			create_Element('button', {
				className: 'button button-ghost',
				text: AppTexts.cancel,
				attrs: { type: 'button' },
				on: { click: () => modal.close() },
			}),
			create_Element('button', {
				className: 'button button-primary',
				text: ChatTexts.formatFixConfirm,
				attrs: { type: 'button' },
				on: {
					click: () => {
						finish({
							signerId: signerSelect?.value ?? '',
							addresseeId: addresseeSelect?.value ?? '',
							title: titleInput?.value.trim() ?? '',
						});
						modal.close();
					},
				},
			}),
		]);
	});
}
