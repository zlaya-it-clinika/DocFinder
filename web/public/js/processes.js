// Категории знаний на клиенте. В ядре это процессы (GET /processes): у каждого свой пул документов
// и свои подсказки модели. Здесь справочник названий и цветов, метки и форма администратора.
import { create_Process, list_Processes, update_Process } from './api.js';
import { open_Modal } from './components/modal.js';
import { show_Toast } from './components/toast.js';
import { create_Button, create_Element } from './lib/dom.js';
import { AppTexts, ProcessTexts, get_ErrorText } from './texts.js';

// Цвета из контракта ядра
export const ProcessColors = Object.freeze(['gray', 'blue', 'green', 'amber', 'red', 'violet', 'teal']);

// Документооборот - стартовая категория, удалить её нельзя
export const DefaultProcessId = 'documents';

// Справочник «id -> категория» для подписей у ответов, источников и в таблицах
const knownProcesses = new Map();

export async function load_Processes() {
	const payload = await list_Processes();
	const processes = Array.isArray(payload?.processes) ? payload.processes : [];
	for (const item of processes) {
		knownProcesses.set(item.processId, item);
	}
	return { canManage: payload?.canManage === true, processes };
}

// Ядро без процессов отвечает 404 или 405: тогда выбор категорий скрыт
export async function try_LoadProcesses() {
	try {
		return { supported: true, ...(await load_Processes()) };
	} catch (error) {
		if (error.status === 404 || error.status === 405) {
			return { supported: false, canManage: false, processes: [] };
		}
		throw error;
	}
}

export function get_KnownProcess(processId) {
	return knownProcesses.get(processId) ?? null;
}

export function remember_Process(process) {
	knownProcesses.set(process.processId, process);
}

function pick_Color(color) {
	return ProcessColors.includes(color) ? color : 'gray';
}

export function create_ProcessDot(color) {
	return create_Element('span', { className: `topic-dot topic-dot-${pick_Color(color)}`, attrs: { 'aria-hidden': 'true' } });
}

export function create_ProcessBadge(process) {
	return create_Element('span', { className: 'topic-badge', attrs: { title: process.description || process.title } }, [
		create_ProcessDot(process.color),
		create_Element('span', { className: 'topic-badge-text', text: process.title }),
	]);
}

// Метка по id. Неизвестный процесс показывается как есть, без цвета
export function create_ProcessBadgeById(processId) {
	const process = get_KnownProcess(processId);
	return create_ProcessBadge(process ?? { title: processId, color: 'gray' });
}

function create_Field(id, label, control, hint = '', wide = false) {
	return create_Element('div', { className: wide ? 'field field-wide' : 'field' }, [
		create_Element('label', { className: 'field-label', text: label, attrs: { for: id } }),
		control,
		hint ? create_Element('span', { className: 'field-hint', text: hint }) : null,
	]);
}

function create_PromptInput(id, value) {
	const input = create_Element('textarea', { className: 'textarea', attrs: { id, rows: 3, maxlength: 4000 } });
	input.value = value ?? '';
	return input;
}

// Создание и правка категории. process задан - правка, в нём есть подсказки модели (их видит только администратор)
export function open_ProcessForm({ process = null, onSaved }) {
	const isEdit = Boolean(process);
	let color = pick_Color(process?.color ?? 'blue');

	const errorBox = create_Element('div', { className: 'form-error', attrs: { role: 'alert' } });
	errorBox.hidden = true;
	const titleInput = create_Element('input', { className: 'input', attrs: { id: 'process-title', autocomplete: 'off', maxlength: 80 } });
	const descriptionInput = create_Element('input', { className: 'input', attrs: { id: 'process-description', autocomplete: 'off', maxlength: 200 } });
	const answerInput = create_PromptInput('process-answer', process?.answerPrompt);
	const selectInput = create_PromptInput('process-select', process?.selectPrompt);
	const clarifyInput = create_PromptInput('process-clarify', process?.clarifyPrompt);
	if (isEdit) {
		titleInput.value = process.title;
		descriptionInput.value = process.description ?? '';
	}

	const swatches = create_Element('div', { className: 'color-swatches', attrs: { role: 'radiogroup', 'aria-label': ProcessTexts.fieldColor } }, ProcessColors.map((name) => {
		const swatch = create_Element('button', {
			className: 'color-swatch',
			attrs: { type: 'button', role: 'radio', 'aria-checked': name === color ? 'true' : 'false', 'aria-label': ProcessTexts.colors[name] ?? name, title: ProcessTexts.colors[name] ?? name },
			on: {
				click: () => {
					color = name;
					for (const item of swatches.children) {
						item.setAttribute('aria-checked', item === swatch ? 'true' : 'false');
					}
				},
			},
		}, [create_ProcessDot(name)]);
		return swatch;
	}));

	// Подсказки модели при создании берутся у документооборота, их можно поправить потом
	const promptFields = isEdit
		? [
			create_Element('p', { className: 'field-hint field-wide', text: ProcessTexts.promptsLead }),
			create_Field('process-answer', ProcessTexts.fieldAnswer, answerInput, ProcessTexts.fieldAnswerHint, true),
			create_Field('process-select', ProcessTexts.fieldSelect, selectInput, ProcessTexts.fieldSelectHint, true),
			create_Field('process-clarify', ProcessTexts.fieldClarify, clarifyInput, ProcessTexts.fieldClarifyHint, true),
		]
		: [create_Element('p', { className: 'field-hint field-wide', text: ProcessTexts.promptsOnCreate })];

	const form = create_Element('form', { className: 'form-grid', attrs: { id: 'process-form', novalidate: true } }, [
		create_Field('process-title', ProcessTexts.fieldTitle, titleInput, '', true),
		create_Field('process-description', ProcessTexts.fieldDescription, descriptionInput, ProcessTexts.fieldDescriptionHint, true),
		create_Element('div', { className: 'field field-wide' }, [create_Element('span', { className: 'field-label', text: ProcessTexts.fieldColor }), swatches]),
		...promptFields,
		create_Element('div', { className: 'field-wide' }, [errorBox]),
	]);
	const submitButton = create_Button(AppTexts.save, { variant: 'primary', type: 'submit', attrs: { form: 'process-form' } });
	const modal = open_Modal({
		title: isEdit ? ProcessTexts.editTitle : ProcessTexts.createTitle,
		body: [form],
		footer: [create_Button(AppTexts.cancel, { variant: 'ghost', onClick: () => modal.close() }), submitButton],
		wide: true,
	});

	form.addEventListener('submit', async (event) => {
		event.preventDefault();
		const title = titleInput.value.trim();
		if (title === '') {
			titleInput.focus();
			return;
		}
		errorBox.hidden = true;
		submitButton.disabled = true;
		const fields = { title, description: descriptionInput.value.trim(), color };
		if (isEdit) {
			fields.answerPrompt = answerInput.value.trim();
			fields.selectPrompt = selectInput.value.trim();
			fields.clarifyPrompt = clarifyInput.value.trim();
		}
		try {
			const saved = isEdit ? await update_Process(process.processId, fields) : await create_Process(fields);
			remember_Process(saved);
			modal.close();
			show_Toast(isEdit ? ProcessTexts.updated : ProcessTexts.created, 'success');
			onSaved(saved);
		} catch (error) {
			errorBox.textContent = get_ErrorText(error);
			errorBox.hidden = false;
		} finally {
			submitButton.disabled = false;
		}
	});
	titleInput.focus();
}
