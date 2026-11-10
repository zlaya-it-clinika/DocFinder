// Вкладка «Организация»: генеральный директор, должностные лица, герб и штамп для бланков.
// Видит её флаг organization.manage. Без флага ядро отдаёт реквизиты только для чтения (canManage: false)
import {
	build_OrganizationImageUrl,
	check_IsNotSupported,
	create_Official,
	delete_Official,
	delete_OrganizationImage,
	fetch_Organization,
	update_Official,
	upload_OrganizationImage,
} from '../api.js';
import { open_Confirm } from '../components/confirm.js';
import { open_Modal } from '../components/modal.js';
import { show_Toast } from '../components/toast.js';
import { append_Children, create_Button, create_Element, create_EmptyBlock, create_IconButton, create_LoadingBlock } from '../lib/dom.js';
import { format_DateTime, format_FileSize } from '../lib/format.js';
import { AppTexts, OrganizationTexts, format_Text, get_ErrorText } from '../texts.js';

// Те же границы, что у ядра: тип и размер проверяем до отправки, чтобы не гонять большой файл
const maxImageBytes = 2 * 1024 * 1024;
const imageTypes = new Set(['image/png', 'image/jpeg']);
const imageKinds = [
	{ kind: 'emblem', title: OrganizationTexts.emblemTitle },
	{ kind: 'stamp', title: OrganizationTexts.stampTitle },
];

function create_Field(id, label, control, hint = '') {
	return create_Element('div', { className: 'field' }, [
		create_Element('label', { className: 'field-label', text: label, attrs: { for: id } }),
		control,
		hint ? create_Element('span', { className: 'field-hint', text: hint }) : null,
	]);
}

// 2026-11-01 -> 01.11.2026, как даты в документах по п. 8.7 Инструкции
function format_ValidFrom(value) {
	const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value ?? '');
	return match ? `${match[3]}.${match[2]}.${match[1]}` : '';
}

function create_SectionHeading(title, lead, tools = []) {
	return create_Element('div', { className: 'section-header' }, [
		create_Element('div', { className: 'page-heading' }, [
			create_Element('h2', { className: 'section-title', text: title }),
			create_Element('p', { className: 'section-lead', text: lead }),
		]),
		tools.length > 0 ? create_Element('div', { className: 'section-tools' }, tools) : null,
	]);
}

// Форма должностного лица. chief: форма генерального директора, отметка ставится сама
function open_OfficialForm({ official, chief = false, onSaved }) {
	const isEdit = Boolean(official);
	const errorBox = create_Element('div', { className: 'form-error', attrs: { role: 'alert' } });
	errorBox.hidden = true;
	const positionInput = create_Element('input', { className: 'input', attrs: { id: 'official-position', autocomplete: 'off', maxlength: 200, required: true } });
	const nameInput = create_Element('input', { className: 'input', attrs: { id: 'official-name', autocomplete: 'off', maxlength: 200, required: true } });
	const departmentInput = create_Element('input', { className: 'input', attrs: { id: 'official-department', autocomplete: 'off', maxlength: 200 } });
	const validFromInput = create_Element('input', { className: 'input', attrs: { id: 'official-valid-from', type: 'date' } });
	positionInput.value = official?.position ?? (chief ? OrganizationTexts.chiefDefaultPosition : '');
	nameInput.value = official?.fullName ?? '';
	departmentInput.value = official?.department ?? '';
	validFromInput.value = official?.validFrom ?? '';

	const form = create_Element('form', { className: 'form-grid', attrs: { novalidate: true, id: 'official-form' } }, [
		create_Field('official-position', OrganizationTexts.fieldPosition, positionInput, OrganizationTexts.fieldPositionHint),
		create_Field('official-name', OrganizationTexts.fieldName, nameInput, OrganizationTexts.fieldNameHint),
		create_Field('official-department', OrganizationTexts.fieldDepartment, departmentInput),
		create_Field('official-valid-from', OrganizationTexts.fieldValidFrom, validFromInput, OrganizationTexts.fieldValidFromHint),
		chief && !isEdit ? create_Element('p', { className: 'field-hint field-wide', text: OrganizationTexts.chiefReplaceHint }) : null,
		create_Element('div', { className: 'field-wide' }, [errorBox]),
	]);
	const submitButton = create_Button(AppTexts.save, { variant: 'primary', type: 'submit', attrs: { form: 'official-form' } });
	let title = isEdit ? OrganizationTexts.officialEditTitle : OrganizationTexts.officialCreateTitle;
	if (chief) {
		title = OrganizationTexts.chiefFormTitle;
	}
	const modal = open_Modal({
		title,
		body: [form],
		footer: [create_Button(AppTexts.cancel, { variant: 'ghost', onClick: () => modal.close() }), submitButton],
	});

	form.addEventListener('submit', async (event) => {
		event.preventDefault();
		const emptyField = [positionInput, nameInput].find((input) => input.value.trim() === '');
		if (emptyField) {
			errorBox.textContent = OrganizationTexts.requiredMissing;
			errorBox.hidden = false;
			emptyField.focus();
			return;
		}
		errorBox.hidden = true;
		const fields = {
			position: positionInput.value.trim(),
			fullName: nameInput.value.trim(),
			department: departmentInput.value.trim(),
			validFrom: validFromInput.value,
			// Правка директора не должна снимать с него отметку
			chief: chief || Boolean(official?.chief),
		};
		submitButton.disabled = true;
		try {
			const saved = isEdit ? await update_Official(official.officialId, fields) : await create_Official(fields);
			modal.close();
			let message = isEdit ? OrganizationTexts.officialUpdated : OrganizationTexts.officialCreated;
			if (chief) {
				message = OrganizationTexts.chiefSaved;
			}
			show_Toast(message, 'success');
			onSaved(saved);
		} catch (error) {
			errorBox.textContent = get_ErrorText(error);
			errorBox.hidden = false;
		} finally {
			submitButton.disabled = false;
		}
	});
	(isEdit || !chief ? positionInput : nameInput).focus();
}

export function create_OrganizationTab() {
	const element = create_Element('div', { className: 'page-sections' }, [create_LoadingBlock(AppTexts.loading)]);
	let organization = null;

	const show_LoadError = (error) => {
		const unsupported = check_IsNotSupported(error);
		element.replaceChildren(create_EmptyBlock('alert', unsupported ? OrganizationTexts.notSupported : OrganizationTexts.loadFailed, [
			create_Element('div', { className: 'callout-hint', text: unsupported ? OrganizationTexts.notSupportedHint : get_ErrorText(error) }),
			create_Button(AppTexts.retry, { variant: 'outline', icon: 'refresh', onClick: load }),
		]));
	};

	async function load() {
		element.replaceChildren(create_LoadingBlock(AppTexts.loading));
		try {
			organization = await fetch_Organization();
		} catch (error) {
			show_LoadError(error);
			return;
		}
		render();
	}

	// После любой правки берём реквизиты заново: смена директора меняет и список должностных лиц
	const reload_Quietly = async () => {
		try {
			organization = await fetch_Organization();
			render();
		} catch (error) {
			show_Toast(get_ErrorText(error), 'error');
		}
	};

	function render_Chief() {
		const chief = organization.officials.find((official) => official.chief);
		const tools = [];
		if (organization.canManage && chief) {
			tools.push(create_Button(OrganizationTexts.officialEdit, { variant: 'outline', icon: 'edit', onClick: () => open_OfficialForm({ official: chief, chief: true, onSaved: reload_Quietly }) }));
		}
		if (organization.canManage) {
			tools.push(create_Button(chief ? OrganizationTexts.chiefChange : OrganizationTexts.chiefAssign, {
				variant: chief ? 'outline' : 'primary',
				icon: chief ? 'refresh' : 'plus',
				onClick: () => open_OfficialForm({ official: null, chief: true, onSaved: reload_Quietly }),
			}));
		}
		const card = chief
			? create_Element('div', { className: 'org-chief' }, [
				create_Element('span', { className: 'org-chief-name', text: chief.fullName }),
				create_Element('span', { className: 'org-chief-meta', text: [chief.position, chief.department].filter(Boolean).join(', ') }),
				chief.validFrom ? create_Element('span', { className: 'org-chief-meta', text: `${OrganizationTexts.columnValidFrom} ${format_ValidFrom(chief.validFrom)}` }) : null,
			])
			: create_EmptyBlock('user', OrganizationTexts.chiefEmpty);
		return create_Element('section', { className: 'page-section' }, [
			create_SectionHeading(OrganizationTexts.chiefTitle, OrganizationTexts.chiefLead, tools),
			card,
		]);
	}

	function render_Officials() {
		const officials = organization.officials.filter((official) => !official.chief);
		const tools = organization.canManage
			? [create_Button(OrganizationTexts.officialsAdd, { variant: 'primary', icon: 'plus', onClick: () => open_OfficialForm({ official: null, onSaved: reload_Quietly }) })]
			: [];
		const remove = async (official) => {
			const confirmed = await open_Confirm({
				title: OrganizationTexts.officialDeleteTitle,
				text: format_Text(OrganizationTexts.officialDeleteText, { name: official.fullName }),
				confirmLabel: OrganizationTexts.officialDelete,
				danger: true,
			});
			if (!confirmed) {
				return;
			}
			try {
				await delete_Official(official.officialId);
				show_Toast(OrganizationTexts.officialDeleted, 'success');
				await reload_Quietly();
			} catch (error) {
				show_Toast(get_ErrorText(error), 'error');
			}
		};
		const table = officials.length === 0
			? create_EmptyBlock('users', OrganizationTexts.officialsEmpty)
			: create_Element('div', { className: 'table-wrap' }, [
				create_Element('table', { className: 'table' }, [
					create_Element('thead', {}, [create_Element('tr', {}, [
						create_Element('th', { text: OrganizationTexts.columnPosition }),
						create_Element('th', { text: OrganizationTexts.columnName }),
						create_Element('th', { text: OrganizationTexts.columnDepartment }),
						create_Element('th', { text: OrganizationTexts.columnValidFrom }),
						organization.canManage ? create_Element('th', { className: 'table-cell-actions' }) : null,
					])]),
					create_Element('tbody', {}, officials.map((official) => create_Element('tr', {}, [
						create_Element('td', { className: 'table-cell-wrap', text: official.position }),
						create_Element('td', { className: 'cell-title', text: official.fullName }),
						create_Element('td', { className: 'table-cell-muted table-cell-wrap', text: official.department || AppTexts.notSpecified }),
						create_Element('td', { className: 'time-cell', text: format_ValidFrom(official.validFrom) || AppTexts.notSpecified }),
						organization.canManage
							? create_Element('td', { className: 'table-cell-actions' }, [
								create_IconButton('edit', `${OrganizationTexts.officialEdit}: ${official.fullName}`, () => open_OfficialForm({ official, onSaved: reload_Quietly })),
								create_IconButton('trash', `${OrganizationTexts.officialDelete}: ${official.fullName}`, () => remove(official)),
							])
							: null,
					]))),
				]),
			]);
		return create_Element('section', { className: 'page-section' }, [
			create_SectionHeading(OrganizationTexts.officialsTitle, OrganizationTexts.officialsLead, tools),
			table,
		]);
	}

	function render_ImageCard({ kind, title }) {
		const image = organization.images?.[kind] ?? null;
		const fileInput = create_Element('input', { attrs: { type: 'file', accept: 'image/png,image/jpeg', hidden: true } });
		fileInput.addEventListener('change', async () => {
			const file = fileInput.files?.[0];
			fileInput.value = '';
			if (!file) {
				return;
			}
			if (!imageTypes.has(file.type)) {
				show_Toast(OrganizationTexts.imageWrongType, 'error');
				return;
			}
			if (file.size > maxImageBytes) {
				show_Toast(OrganizationTexts.imageTooLarge, 'error');
				return;
			}
			try {
				await upload_OrganizationImage(kind, file);
				show_Toast(OrganizationTexts.imageUploaded, 'success');
				await reload_Quietly();
			} catch (error) {
				show_Toast(get_ErrorText(error), 'error');
			}
		});
		const remove = async () => {
			const confirmed = await open_Confirm({
				title: OrganizationTexts.imageDeleteTitle,
				text: format_Text(OrganizationTexts.imageDeleteText, { name: title }),
				confirmLabel: OrganizationTexts.imageDelete,
				danger: true,
			});
			if (!confirmed) {
				return;
			}
			try {
				await delete_OrganizationImage(kind);
				show_Toast(OrganizationTexts.imageDeleted, 'success');
				await reload_Quietly();
			} catch (error) {
				show_Toast(get_ErrorText(error), 'error');
			}
		};
		const preview = image
			? create_Element('img', { className: 'org-image', attrs: { src: build_OrganizationImageUrl(kind, image.updatedAt), alt: title } })
			: create_Element('span', { className: 'org-image-empty', text: OrganizationTexts.imageEmpty });
		const actions = organization.canManage
			? create_Element('div', { className: 'org-image-actions' }, [
				fileInput,
				create_Button(image ? OrganizationTexts.imageReplace : OrganizationTexts.imageUpload, {
					variant: image ? 'outline' : 'primary',
					icon: 'upload',
					onClick: () => fileInput.click(),
				}),
				image ? create_Button(OrganizationTexts.imageDelete, { variant: 'ghost', icon: 'trash', onClick: remove }) : null,
			])
			: null;
		return create_Element('div', { className: 'org-image-card' }, [
			create_Element('span', { className: 'cell-title', text: title }),
			create_Element('div', { className: 'org-image-preview' }, [preview]),
			image
				? create_Element('span', { className: 'field-hint', text: `${image.fileName}, ${format_FileSize(image.size)}, ${format_DateTime(image.updatedAt)}` })
				: null,
			actions,
		]);
	}

	function render() {
		const imagesSection = create_Element('section', { className: 'page-section' }, [
			create_SectionHeading(OrganizationTexts.imagesTitle, OrganizationTexts.imagesLead),
			create_Element('div', { className: 'org-images' }, imageKinds.map(render_ImageCard)),
		]);
		element.replaceChildren();
		append_Children(element, [
			organization.canManage ? null : create_Element('div', { className: 'callout-hint', text: OrganizationTexts.readOnlyHint }),
			render_Chief(),
			render_Officials(),
			imagesSection,
		]);
	}

	load();
	return { element, destroy() {} };
}
