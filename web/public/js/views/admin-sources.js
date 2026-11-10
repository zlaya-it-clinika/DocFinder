// Вкладка «Источники»: автозагрузка из папки или лицензионного API. Включает флаг sources.manage.
import {
	check_IsNotSupported,
	create_Source,
	delete_Source,
	list_Processes,
	list_SourceKinds,
	list_Sources,
	sync_Source,
	update_Source,
	verify_Source,
} from '../api.js';
import { open_Confirm } from '../components/confirm.js';
import { open_Modal } from '../components/modal.js';
import { show_Toast } from '../components/toast.js';
import { create_Button, create_Element, create_EmptyBlock, create_IconButton, create_LoadingBlock } from '../lib/dom.js';
import { format_DateTime } from '../lib/format.js';
import { AppTexts, SourceTexts, format_Text, get_ErrorText } from '../texts.js';

function create_Field(id, label, control, hint = '') {
	return create_Element('div', { className: 'field' }, [
		create_Element('label', { className: 'field-label', text: label, attrs: { for: id } }),
		control,
		hint ? create_Element('span', { className: 'field-hint', text: hint }) : null,
	]);
}

function status_Label(source) {
	if (source.lastStatus === 'running') {
		return SourceTexts.statusRunning;
	}
	if (source.lastStatus === 'ok') {
		const when = source.lastSyncAt ? format_DateTime(source.lastSyncAt) : '';
		return when ? `${SourceTexts.statusOk} (${when})` : SourceTexts.statusOk;
	}
	if (source.lastStatus === 'error') {
		return source.lastError ? `${SourceTexts.statusError}: ${source.lastError}` : SourceTexts.statusError;
	}
	return SourceTexts.statusIdle;
}

function kind_Title(kinds, kind) {
	return kinds.find((item) => item.kind === kind)?.title || kind;
}

function open_SourceForm({ source, kinds, processes, onSaved }) {
	const isEdit = Boolean(source);
	const errorBox = create_Element('div', { className: 'form-error', attrs: { role: 'alert' } });
	errorBox.hidden = true;
	const kindSelect = create_Element('select', { className: 'input', attrs: { id: 'source-kind' } }, [
		create_Element('option', { text: SourceTexts.kindRequired, attrs: { value: '' } }),
		...kinds.map((kind) => create_Element('option', { text: kind.title, attrs: { value: kind.kind } })),
	]);
	const titleInput = create_Element('input', { className: 'input', attrs: { id: 'source-title', autocomplete: 'off' } });
	const intervalInput = create_Element('input', { className: 'input', attrs: { id: 'source-interval', type: 'number', min: '1', max: '10080' } });
	const processSelect = create_Element('select', { className: 'input', attrs: { id: 'source-process' } },
		(processes ?? []).map((process) => create_Element('option', { text: process.title, attrs: { value: process.processId } })));
	const pathInput = create_Element('input', { className: 'input', attrs: { id: 'source-path', autocomplete: 'off' } });
	const baseUrlInput = create_Element('input', { className: 'input', attrs: { id: 'source-base-url', autocomplete: 'off' } });
	const clientIdInput = create_Element('input', { className: 'input', attrs: { id: 'source-client-id', autocomplete: 'off' } });
	const licenseInput = create_Element('input', { className: 'input', attrs: { id: 'source-license', type: 'password', autocomplete: 'new-password' } });
	const bankInput = create_Element('input', { className: 'input', attrs: { id: 'source-bank', autocomplete: 'off' } });
	const rubricInput = create_Element('input', { className: 'input', attrs: { id: 'source-rubric', autocomplete: 'off' } });
	const folderFields = create_Element('div', { className: 'form-grid' }, [
		create_Field('source-path', SourceTexts.fieldPath, pathInput),
	]);
	const consultantFields = create_Element('div', { className: 'form-grid' }, [
		create_Field('source-base-url', SourceTexts.fieldBaseUrl, baseUrlInput),
		create_Field('source-client-id', SourceTexts.fieldClientId, clientIdInput),
		create_Field('source-license', SourceTexts.fieldLicenseKey, licenseInput, source?.config?.licenseKeySet ? SourceTexts.fieldLicenseKeySet : SourceTexts.fieldLicenseKeyHint),
		create_Field('source-bank', SourceTexts.fieldBank, bankInput),
		create_Field('source-rubric', SourceTexts.fieldRubric, rubricInput),
	]);

	kindSelect.value = source?.kind ?? '';
	kindSelect.disabled = isEdit;
	titleInput.value = source?.title ?? '';
	intervalInput.value = String(source?.intervalMinutes ?? 60);
	processSelect.value = source?.processId ?? processSelect.value;
	pathInput.value = source?.config?.path ?? '';
	baseUrlInput.value = source?.config?.baseUrl ?? '';
	clientIdInput.value = source?.config?.clientId ?? '';
	bankInput.value = source?.config?.bank ?? '';
	rubricInput.value = source?.config?.rubric ?? '';

	function show_KindFields() {
		folderFields.hidden = kindSelect.value !== 'folder';
		consultantFields.hidden = kindSelect.value !== 'consultantplus';
	}
	kindSelect.addEventListener('change', show_KindFields);
	show_KindFields();

	const form = create_Element('form', { className: 'form-grid', attrs: { novalidate: true, id: 'source-form' } }, [
		create_Field('source-kind', SourceTexts.fieldKind, kindSelect),
		create_Field('source-title', SourceTexts.fieldTitle, titleInput),
		create_Field('source-interval', SourceTexts.fieldInterval, intervalInput, SourceTexts.fieldIntervalHint),
		create_Field('source-process', SourceTexts.fieldProcess, processSelect),
		folderFields,
		consultantFields,
		create_Element('div', { className: 'field-wide' }, [errorBox]),
	]);
	const submitButton = create_Button(AppTexts.save, { variant: 'primary', type: 'submit', attrs: { form: 'source-form' } });
	const modal = open_Modal({
		title: isEdit ? SourceTexts.editTitle : SourceTexts.createTitle,
		body: [form],
		footer: [create_Button(AppTexts.cancel, { variant: 'ghost', onClick: () => modal.close() }), submitButton],
	});

	form.addEventListener('submit', async (event) => {
		event.preventDefault();
		if (!kindSelect.value) {
			errorBox.textContent = SourceTexts.kindRequired;
			errorBox.hidden = false;
			kindSelect.focus();
			return;
		}
		errorBox.hidden = true;
		const fields = {
			kind: kindSelect.value,
			title: titleInput.value.trim(),
			intervalMinutes: Number(intervalInput.value) || 60,
			processId: processSelect.value,
			config: {},
		};
		if (kindSelect.value === 'folder') {
			fields.config.path = pathInput.value.trim();
		}
		if (kindSelect.value === 'consultantplus') {
			fields.config.baseUrl = baseUrlInput.value.trim();
			fields.config.clientId = clientIdInput.value.trim();
			fields.config.bank = bankInput.value.trim();
			fields.config.rubric = rubricInput.value.trim();
			if (licenseInput.value.trim()) {
				fields.config.licenseKey = licenseInput.value.trim();
			}
		}
		try {
			if (isEdit) {
				await update_Source(source.sourceId, fields);
				show_Toast(SourceTexts.saved, 'success');
			} else {
				await create_Source(fields);
				show_Toast(SourceTexts.created, 'success');
			}
			modal.close();
			onSaved();
		} catch (error) {
			errorBox.textContent = get_ErrorText(error);
			errorBox.hidden = false;
		}
	});
}

export function create_SourcesTab() {
	const content = create_Element('div', { className: 'page-sections' }, [create_LoadingBlock(AppTexts.loading)]);
	const element = create_Element('div', { className: 'page-sections' }, [content]);
	let kinds = [];
	let processes = [];

	async function reload() {
		try {
			const [kindsPayload, sourcesPayload, processesPayload] = await Promise.all([
				list_SourceKinds(),
				list_Sources(),
				list_Processes(),
			]);
			kinds = kindsPayload.kinds ?? [];
			processes = processesPayload.processes ?? [];
			render_List(sourcesPayload.sources ?? []);
		} catch (error) {
			if (check_IsNotSupported(error)) {
				content.replaceChildren(create_EmptyBlock('download', SourceTexts.notSupported, [
					create_Element('p', { className: 'empty-lead', text: SourceTexts.notSupportedHint }),
				]));
				return;
			}
			content.replaceChildren(create_EmptyBlock('alert', SourceTexts.loadFailed, [
				create_Button(AppTexts.retry, { variant: 'primary', icon: 'refresh', onClick: reload }),
			]));
		}
	}

	function render_List(sources) {
		const addButton = create_Button(SourceTexts.add, {
			variant: 'primary',
			icon: 'plus',
			onClick: () => open_SourceForm({ kinds, processes, onSaved: reload }),
		});
		if (sources.length === 0) {
			content.replaceChildren(create_Element('section', { className: 'page-section' }, [
				create_Element('div', { className: 'section-header' }, [
					create_Element('h2', { className: 'section-title', text: SourceTexts.columnTitle }),
					addButton,
				]),
				create_Element('p', { className: 'section-lead', text: SourceTexts.lead }),
				create_EmptyBlock('download', SourceTexts.empty),
			]));
			return;
		}
		const rows = sources.map((item) => create_Element('tr', {}, [
			create_Element('td', { text: item.title }),
			create_Element('td', { text: kind_Title(kinds, item.kind) }),
			create_Element('td', { text: item.enabled ? SourceTexts.enabledOn : SourceTexts.enabledOff }),
			create_Element('td', { text: status_Label(item) }),
			create_Element('td', { className: 'table-cell-actions' }, [
				create_IconButton('edit', SourceTexts.editTitle, () => open_SourceForm({ source: item, kinds, processes, onSaved: reload })),
				create_IconButton(item.enabled ? 'lock' : 'check', item.enabled ? SourceTexts.disable : SourceTexts.enable, async () => {
					try {
						await update_Source(item.sourceId, { enabled: !item.enabled });
						show_Toast(SourceTexts.saved, 'success');
						reload();
					} catch (error) {
						show_Toast(get_ErrorText(error), 'error');
					}
				}),
				create_IconButton('help', SourceTexts.verify, async () => {
					try {
						await verify_Source(item.sourceId);
						show_Toast(SourceTexts.verified, 'success');
					} catch (error) {
						show_Toast(get_ErrorText(error), 'error');
					}
				}),
				create_IconButton('refresh', SourceTexts.sync, async () => {
					try {
						const run = await sync_Source(item.sourceId);
						show_Toast(format_Text(SourceTexts.synced, { indexed: run.indexed ?? 0, skipped: run.skipped ?? 0 }), 'success');
						reload();
					} catch (error) {
						show_Toast(get_ErrorText(error), 'error');
					}
				}),
				create_IconButton('trash', SourceTexts.deleteTitle, async () => {
					const confirmed = await open_Confirm({
						title: SourceTexts.deleteTitle,
						text: format_Text(SourceTexts.deleteText, { title: item.title }),
						confirmLabel: SourceTexts.deleteConfirm,
					});
					if (!confirmed) {
						return;
					}
					try {
						await delete_Source(item.sourceId);
						show_Toast(SourceTexts.deleted, 'success');
						reload();
					} catch (error) {
						show_Toast(get_ErrorText(error), 'error');
					}
				}),
			]),
		]));
		content.replaceChildren(create_Element('section', { className: 'page-section' }, [
			create_Element('div', { className: 'section-header' }, [
				create_Element('h2', { className: 'section-title', text: SourceTexts.columnTitle }),
				addButton,
			]),
			create_Element('p', { className: 'section-lead', text: SourceTexts.lead }),
			create_Element('div', { className: 'table-wrap' }, [
				create_Element('table', { className: 'table' }, [
					create_Element('thead', {}, [
						create_Element('tr', {}, [
							create_Element('th', { text: SourceTexts.columnTitle }),
							create_Element('th', { text: SourceTexts.columnKind }),
							create_Element('th', { text: SourceTexts.columnEnabled }),
							create_Element('th', { text: SourceTexts.columnStatus }),
							create_Element('th', { className: 'table-cell-actions' }),
						]),
					]),
					create_Element('tbody', {}, rows),
				]),
			]),
		]));
	}

	reload();
	return { element, destroy() {} };
}
