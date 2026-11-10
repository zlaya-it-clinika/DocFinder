// Поле ввода чата: растёт по мере набора, Enter отправляет, Shift+Enter переносит строку.
// Кнопка «+» прикрепляет черновик на проверку.
import { create_Element, create_Icon, create_IconButton } from '../lib/dom.js';
import { create_MenuItem, open_Popover } from './popover.js';
import { build_AcceptAttribute, validate_UploadFile } from '../lib/files.js';
import { format_FileSize } from '../lib/format.js';
import { ChatTexts, format_Text } from '../texts.js';
import { show_Toast } from './toast.js';

const maxInputHeight = 220;

// onSubmit({ text, file }) должен вернуть Promise, поле блокируется до его завершения
// config: настройки веб-сервера, из них берутся допустимые форматы и размер файла
// tools: дополнительные кнопки рядом с «+», например выбор категории знаний
export function create_Composer({ onSubmit, allowCheck = true, allowAssist = false, config, tools = [] }) {
	let attachedFile = null;
	let attachMode = allowCheck ? 'check' : 'assist';
	let busy = false;
	// Подсказка пустого поля: своя у каждого режима чата
	let idlePlaceholder = ChatTexts.placeholder;
	let basePlaceholder = idlePlaceholder;

	const textarea = create_Element('textarea', {
		className: 'composer-input',
		attrs: { rows: 1, placeholder: basePlaceholder, 'aria-label': basePlaceholder },
	});
	const attachmentsRow = create_Element('div', { className: 'composer-attachments' });
	attachmentsRow.hidden = true;

	const fileInput = create_Element('input', {
		attrs: { type: 'file', accept: build_AcceptAttribute(config), hidden: true, tabindex: -1 },
	});
	const open_Picker = (mode) => {
		attachMode = mode;
		fileInput.click();
	};
	const attachButton = create_IconButton('plus', ChatTexts.attach, () => {
		if (allowCheck && allowAssist) {
			open_Popover(attachButton, (close) => [
				create_MenuItem(create_Icon('file-check'), ChatTexts.attachCheck, () => {
					close();
					open_Picker('check');
				}),
				create_MenuItem(create_Icon('file'), ChatTexts.attachAssist, () => {
					close();
					open_Picker('assist');
				}),
			]);
			return;
		}
		open_Picker(allowCheck ? 'check' : 'assist');
	});
	attachButton.hidden = !allowCheck && !allowAssist;
	attachButton.setAttribute('aria-haspopup', allowCheck && allowAssist ? 'menu' : 'false');

	const sendButton = create_Element('button', {
		className: 'composer-send',
		attrs: { type: 'submit', 'aria-label': ChatTexts.send, title: ChatTexts.send, disabled: true },
	}, [create_Icon('arrow-up')]);

	const form = create_Element('form', { className: 'composer' }, [
		attachmentsRow,
		textarea,
		create_Element('div', { className: 'composer-toolbar' }, [
			create_Element('div', { className: 'composer-tools' }, [attachButton, fileInput, ...tools]),
			sendButton,
		]),
	]);

	const resize_Input = () => {
		textarea.style.height = 'auto';
		textarea.style.height = `${Math.min(textarea.scrollHeight, maxInputHeight)}px`;
		textarea.style.overflowY = textarea.scrollHeight > maxInputHeight ? 'auto' : 'hidden';
	};

	const update_SendState = () => {
		sendButton.disabled = busy || (textarea.value.trim() === '' && !attachedFile);
	};

	const update_Placeholder = () => {
		const placeholder = attachedFile ? ChatTexts.placeholderWithFile : basePlaceholder;
		textarea.placeholder = placeholder;
		textarea.setAttribute('aria-label', placeholder);
	};

	const render_Attachment = () => {
		attachmentsRow.replaceChildren();
		attachmentsRow.hidden = !attachedFile;
		if (attachedFile) {
			attachmentsRow.append(create_Element('div', { className: 'attachment-chip' }, [
				create_Icon('file-check'),
				create_Element('div', { className: 'attachment-chip-text' }, [
					create_Element('span', { className: 'attachment-chip-name', text: attachedFile.name }),
					create_Element('span', { className: 'attachment-chip-meta', text: `${attachMode === 'assist' ? ChatTexts.draftAssistRequest : ChatTexts.draftCheckRequest} · ${format_FileSize(attachedFile.size)}` }),
				]),
				create_IconButton('close', ChatTexts.removeAttachment, () => {
					attachedFile = null;
					render_Attachment();
					textarea.focus();
				}, 'icon-button icon-button-sm'),
			]));
		}
		update_Placeholder();
		update_SendState();
	};

	const submit_Form = async () => {
		if (busy) {
			return;
		}
		const text = textarea.value.trim();
		if (text === '' && !attachedFile) {
			return;
		}
		const file = attachedFile;
		const mode = attachMode;
		textarea.value = '';
		attachedFile = null;
		render_Attachment();
		resize_Input();
		await onSubmit({ text, file, attachMode: mode });
		textarea.focus();
	};

	textarea.addEventListener('input', () => {
		resize_Input();
		update_SendState();
	});
	textarea.addEventListener('keydown', (event) => {
		if (event.key === 'Enter' && !event.shiftKey && !event.isComposing) {
			event.preventDefault();
			submit_Form();
		}
	});
	form.addEventListener('submit', (event) => {
		event.preventDefault();
		submit_Form();
	});
	fileInput.addEventListener('change', () => {
		const [file] = fileInput.files ?? [];
		fileInput.value = '';
		if (file) {
			api.attach_File(file);
		}
	});

	const api = {
		element: form,

		focus() {
			textarea.focus();
		},

		set_Busy(value) {
			busy = value;
			attachButton.disabled = value;
			update_SendState();
		},

		// Подсказка меняется, когда ассистент спрашивает поле бланка
		set_FieldPrompt(fieldLabel) {
			basePlaceholder = fieldLabel ? format_Text(ChatTexts.placeholderField, { field: fieldLabel }) : idlePlaceholder;
			update_Placeholder();
		},

		// Подсказка пустого поля для режима чата. Подсказку поля бланка не перебивает
		set_IdlePlaceholder(text) {
			const wasIdle = basePlaceholder === idlePlaceholder;
			idlePlaceholder = text || ChatTexts.placeholder;
			if (wasIdle) {
				basePlaceholder = idlePlaceholder;
			}
			update_Placeholder();
		},

		set_Text(text) {
			textarea.value = text;
			resize_Input();
			update_SendState();
			textarea.focus();
		},

		attach_File(file, mode = attachMode) {
			if (!allowCheck && !allowAssist) {
				return;
			}
			attachMode = mode === 'assist' && allowAssist ? 'assist' : 'check';
			const problem = validate_UploadFile(file, config);
			if (problem) {
				show_Toast(problem, 'error');
				return;
			}
			attachedFile = file;
			render_Attachment();
			textarea.focus();
		},
	};
	return api;
}
