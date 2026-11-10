// Окно загрузки документа: новый общий или личный документ, либо новая редакция существующего.
import { list_Processes, list_Roles, upload_Document } from '../api.js';
import { DefaultProcessId } from '../processes.js';
import { Scopes } from '../documents.js';
import { create_Button, create_Element, create_Icon } from '../lib/dom.js';
import { build_AcceptAttribute, format_AllowedExtensions, validate_UploadFile } from '../lib/files.js';
import { format_FileSize } from '../lib/format.js';
import { Roles, get_AccessLevelForRole, get_DocumentAudience } from '../roles.js';
import { AppTexts, DocumentsTexts, FileTexts, RoleTexts, format_Text, get_ErrorText } from '../texts.js';
import { open_Modal } from './modal.js';
import { show_Toast } from './toast.js';

function create_Field(id, label, control, hint = '', wide = false) {
	return create_Element('div', { className: wide ? 'field field-wide' : 'field' }, [
		create_Element('label', { className: 'field-label', text: label, attrs: { for: id } }),
		control,
		hint ? create_Element('span', { className: 'field-hint', text: hint }) : null,
	]);
}

function pick_Title(scope, isNewEdition) {
	if (isNewEdition) {
		return DocumentsTexts.uploadEditionTitle;
	}
	return scope === Scopes.personal ? DocumentsTexts.uploadPersonalTitle : DocumentsTexts.uploadGlobalTitle;
}

// scope: global или personal. document задан - загружаем новую редакцию с тем же названием.
// presetProcessId - категория знаний, выбранная заранее (например, фильтр в админке)
export async function open_DocumentUploadForm({ config, scope, document = null, onUploaded, presetProcessId = '' }) {
	let selectedFile = null;
	const isNewEdition = Boolean(document);
	const isGlobal = scope === Scopes.global;
	let processes = [];
	let roles = [];
	try {
		processes = (await list_Processes())?.processes ?? [];
	} catch {
		processes = [];
	}
	if (isGlobal) {
		try {
			roles = (await list_Roles())?.roles ?? [];
		} catch {
			roles = [];
		}
	}

	const errorBox = create_Element('div', { className: 'form-error', attrs: { role: 'alert' } });
	errorBox.hidden = true;

	const fileInput = create_Element('input', { attrs: { id: 'document-file', type: 'file', accept: build_AcceptAttribute(config), hidden: true } });
	const dropName = create_Element('span', { className: 'file-drop-name' });
	const dropZone = create_Element('div', {
		className: 'file-drop',
		attrs: { role: 'button', tabindex: 0, 'aria-label': DocumentsTexts.dropLead },
	}, [
		create_Icon('upload', 'icon-lg'),
		create_Element('span', { text: DocumentsTexts.dropLead }),
		create_Element('span', { className: 'field-hint', text: format_Text(FileTexts.allowedHint, { list: format_AllowedExtensions(config) }) }),
		dropName,
	]);

	const titleInput = create_Element('input', { className: 'input', attrs: { id: 'document-title', autocomplete: 'off' } });
	const editionInput = create_Element('input', { className: 'input', attrs: { id: 'document-edition', autocomplete: 'off' } });
	const audienceSelect = create_Element('select', { className: 'select', attrs: { id: 'document-audience' } }, [
		create_Element('option', { text: RoleTexts.documentForAll, attrs: { value: Roles.user } }),
		create_Element('option', { text: RoleTexts.documentForAdmins, attrs: { value: Roles.admin } }),
	]);
	// Без выбора ядро кладёт документ в документооборот, поэтому пустого пункта нет
	const chosenProcess = document?.processId || presetProcessId || DefaultProcessId;
	const processSelect = create_Element('select', { className: 'select', attrs: { id: 'document-process' } },
		processes.map((process) => create_Element('option', {
			text: process.title,
			attrs: { value: process.processId, selected: chosenProcess === process.processId },
		})));
	const visibleToAllInput = create_Element('input', { attrs: { id: 'document-visible-all', type: 'checkbox' } });
	const roleBoxes = roles.map((role) => {
		const input = create_Element('input', {
			attrs: { type: 'checkbox', value: role.roleId },
		});
		input.checked = isNewEdition && Array.isArray(document.roleIds) && document.roleIds.includes(role.roleId);
		return { role, input };
	});
	const roleList = create_Element('div', { className: 'check-list' }, roleBoxes.map(({ role, input }) => create_Element('label', { className: 'check-row' }, [
		input,
		create_Element('span', {}, [
			create_Element('span', { text: role.title }),
			role.description ? create_Element('span', { className: 'field-hint', text: role.description }) : null,
		]),
	])));
	if (isNewEdition) {
		titleInput.value = document.title;
		titleInput.readOnly = true;
		audienceSelect.value = get_DocumentAudience(document.accessLevel, config);
		visibleToAllInput.checked = document.visibleToAll !== false && roleBoxes.every(({ input }) => !input.checked);
	} else {
		visibleToAllInput.checked = true;
	}
	const sync_RoleBoxes = () => {
		for (const { input } of roleBoxes) {
			input.disabled = visibleToAllInput.checked;
		}
	};
	visibleToAllInput.addEventListener('change', sync_RoleBoxes);
	sync_RoleBoxes();

	const select_File = (file) => {
		if (!file) {
			return;
		}
		const problem = validate_UploadFile(file, config);
		if (problem) {
			errorBox.textContent = problem;
			errorBox.hidden = false;
			return;
		}
		errorBox.hidden = true;
		selectedFile = file;
		dropName.textContent = `${file.name} · ${format_FileSize(file.size)}`;
		if (!isNewEdition && titleInput.value.trim() === '') {
			titleInput.value = file.name.replace(/\.[^.]+$/, '');
		}
	};

	dropZone.addEventListener('click', () => fileInput.click());
	dropZone.addEventListener('keydown', (event) => {
		if (event.key === 'Enter' || event.key === ' ') {
			event.preventDefault();
			fileInput.click();
		}
	});
	dropZone.addEventListener('dragover', (event) => {
		event.preventDefault();
		dropZone.classList.add('is-dragging');
	});
	dropZone.addEventListener('dragleave', () => dropZone.classList.remove('is-dragging'));
	dropZone.addEventListener('drop', (event) => {
		event.preventDefault();
		dropZone.classList.remove('is-dragging');
		select_File(event.dataTransfer.files[0]);
	});
	fileInput.addEventListener('change', () => select_File(fileInput.files[0]));

	const form = create_Element('form', { className: 'form-grid', attrs: { id: 'document-form', novalidate: true } }, [
		create_Element('div', { className: 'field field-wide' }, [
			create_Element('span', { className: 'field-label', text: DocumentsTexts.fieldFile }),
			dropZone,
			fileInput,
		]),
		create_Field('document-title', DocumentsTexts.fieldTitle, titleInput, isNewEdition ? '' : DocumentsTexts.fieldTitleHint, true),
		create_Field('document-edition', DocumentsTexts.fieldEdition, editionInput, DocumentsTexts.fieldEditionHint, !isGlobal),
		processes.length > 0 ? create_Field('document-process', DocumentsTexts.fieldProcess, processSelect, '', true) : null,
		// Список ролей заменяет «все / только администраторы», когда ядро отдаёт роли
		isGlobal && roles.length > 0
			? create_Element('div', { className: 'field field-wide' }, [
				create_Element('span', { className: 'field-label', text: DocumentsTexts.fieldRoles }),
				create_Element('label', { className: 'check-row' }, [
					visibleToAllInput,
					create_Element('span', { text: DocumentsTexts.visibleToAll }),
				]),
				roleList,
				create_Element('span', { className: 'field-hint', text: DocumentsTexts.fieldRolesHint }),
			])
			: null,
		isGlobal && roles.length === 0 ? create_Field('document-audience', DocumentsTexts.fieldAccess, audienceSelect) : null,
		create_Element('p', {
			className: 'field-hint field-wide',
			text: isGlobal ? DocumentsTexts.globalUploadHint : DocumentsTexts.personalUploadHint,
		}),
		create_Element('div', { className: 'field-wide' }, [errorBox]),
	]);
	const submitButton = create_Button(DocumentsTexts.upload, { variant: 'primary', icon: 'upload', type: 'submit', attrs: { form: 'document-form' } });

	const modal = open_Modal({
		title: pick_Title(scope, isNewEdition),
		body: [form],
		footer: [create_Button(AppTexts.cancel, { variant: 'ghost', onClick: () => modal.close() }), submitButton],
	});

	form.addEventListener('submit', async (event) => {
		event.preventDefault();
		if (!selectedFile) {
			errorBox.textContent = DocumentsTexts.fileRequired;
			errorBox.hidden = false;
			dropZone.focus();
			return;
		}
		errorBox.hidden = true;
		const fields = {
			title: titleInput.value,
			edition: editionInput.value,
			scope,
			process: processSelect.value,
		};
		if (isGlobal && roles.length > 0) {
			if (visibleToAllInput.checked) {
				fields.accessLevel = config.userAccessLevel;
			} else {
				fields.roleIds = roleBoxes.filter(({ input }) => input.checked).map(({ role }) => role.roleId);
				if (fields.roleIds.length === 0) {
					errorBox.textContent = DocumentsTexts.rolesRequired;
					errorBox.hidden = false;
					return;
				}
			}
		} else if (isGlobal) {
			fields.accessLevel = get_AccessLevelForRole(audienceSelect.value, config);
		}
		submitButton.disabled = true;
		try {
			await upload_Document(selectedFile, fields);
			modal.close();
			show_Toast(DocumentsTexts.uploaded, 'success');
			onUploaded();
		} catch (error) {
			errorBox.textContent = get_ErrorText(error);
			errorBox.hidden = false;
		} finally {
			submitButton.disabled = false;
		}
	});
}
