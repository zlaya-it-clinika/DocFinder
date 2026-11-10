// Выбор категорий знаний для чата: кнопка рядом с «+» под полем ввода.
// Можно отметить несколько категорий сразу (например, «Документооборот» и «Финансы»).
// Ничего не отмечено - «Все знания», поиск по всем категориям.
// Выбор хранится в сессии ядра, новый чат берёт последний выбор пользователя.
import { create_ProcessDot, get_KnownProcess, try_LoadProcesses } from '../processes.js';
import { create_Element, create_Icon } from '../lib/dom.js';
import { read_Preference, write_Preference } from '../preferences.js';
import { ProcessTexts, format_Text } from '../texts.js';
import { open_Popover } from './popover.js';

const selectionSeparator = ',';

function get_PreferenceName(user) {
	return `knowledge.${user.userId}`;
}

// Выбор из настроек, сессии или ответа ядра: массив кодов или коды через запятую
function read_Selection(value) {
	const list = Array.isArray(value) ? value : String(value ?? '').split(selectionSeparator);
	return [...new Set(list.map((item) => String(item).trim()).filter(Boolean))];
}

function read_LastSelection(user) {
	const stored = read_Preference(get_PreferenceName(user), '');
	return read_Selection(stored);
}

export function create_ProcessPicker({ user }) {
	let processes = [];
	let selected = read_LastSelection(user);
	let active = false;

	const dot = create_Element('span', { className: 'knowledge-chip-dot' });
	const label = create_Element('span', { className: 'knowledge-chip-label' });
	const button = create_Element('button', {
		className: 'knowledge-chip',
		attrs: { type: 'button', 'aria-haspopup': 'menu', 'aria-expanded': 'false', title: ProcessTexts.pickerLabel },
	}, [create_Icon('book', 'icon-sm'), dot, label, create_Icon('chevron-down', 'icon-sm')]);
	button.hidden = true;

	const render_Button = () => {
		const chosen = selected.map((processId) => get_KnownProcess(processId)).filter(Boolean);
		if (chosen.length === 0) {
			label.textContent = ProcessTexts.allKnowledge;
		} else if (chosen.length <= 2) {
			label.textContent = chosen.map((process) => process.title).join(', ');
		} else {
			label.textContent = format_Text(ProcessTexts.severalSelected, { count: chosen.length });
		}
		dot.replaceChildren(...chosen.slice(0, 3).map((process) => create_ProcessDot(process.color)));
		button.classList.toggle('is-filtered', chosen.length > 0);
		button.setAttribute('aria-label', `${ProcessTexts.pickerLabel}: ${label.textContent}`);
	};

	// Удалённые или неизвестные категории выпадают из выбора
	const apply_Selection = (list, { remember = true } = {}) => {
		const known = new Set(processes.map((item) => item.processId));
		selected = read_Selection(list).filter((processId) => known.has(processId));
		render_Button();
		if (remember) {
			write_Preference(get_PreferenceName(user), selected.join(selectionSeparator));
		}
	};

	const create_CheckMark = (isChecked) => (isChecked ? create_Icon('check', 'icon-sm') : create_Element('span', { className: 'topic-option-spacer' }));

	// Категория переключается без закрытия меню: можно отметить несколько подряд
	const create_Option = (process) => {
		const mark = create_Element('span', { className: 'topic-option-mark' }, [create_CheckMark(selected.includes(process.processId))]);
		const option = create_Element('button', {
			className: 'menu-item topic-option',
			attrs: { type: 'button', role: 'menuitemcheckbox', 'aria-checked': selected.includes(process.processId) ? 'true' : 'false' },
		}, [
			mark,
			process.color ? create_ProcessDot(process.color) : create_Icon('book', 'icon-sm'),
			create_Element('span', { className: 'topic-option-text' }, [
				create_Element('span', { className: 'topic-option-title', text: process.title }),
				process.description ? create_Element('span', { className: 'topic-option-hint', text: process.description }) : null,
			]),
		]);
		option.addEventListener('click', () => {
			const isChecked = selected.includes(process.processId);
			apply_Selection(isChecked ? selected.filter((processId) => processId !== process.processId) : [...selected, process.processId]);
			const nowChecked = selected.includes(process.processId);
			option.setAttribute('aria-checked', nowChecked ? 'true' : 'false');
			mark.replaceChildren(create_CheckMark(nowChecked));
			sync_AllOption();
		});
		return option;
	};

	let allOption = null;
	const sync_AllOption = () => {
		if (!allOption) {
			return;
		}
		allOption.setAttribute('aria-checked', selected.length === 0 ? 'true' : 'false');
		allOption.firstChild.replaceWith(create_CheckMark(selected.length === 0));
	};

	button.addEventListener('click', () => {
		open_Popover(button, (close) => {
			allOption = create_Element('button', {
				className: 'menu-item topic-option',
				attrs: { type: 'button', role: 'menuitemradio', 'aria-checked': selected.length === 0 ? 'true' : 'false' },
				on: {
					click: () => {
						apply_Selection([]);
						close();
					},
				},
			}, [
				create_CheckMark(selected.length === 0),
				create_Icon('book', 'icon-sm'),
				create_Element('span', { className: 'topic-option-text' }, [
					create_Element('span', { className: 'topic-option-title', text: ProcessTexts.allKnowledge }),
					create_Element('span', { className: 'topic-option-hint', text: ProcessTexts.allKnowledgeHint }),
				]),
			]);
			return [
				create_Element('div', { className: 'menu-label', text: ProcessTexts.pickerTitle }),
				allOption,
				create_Element('div', { className: 'menu-separator' }),
				create_Element('div', { className: 'menu-hint', text: ProcessTexts.pickerSeveralHint }),
				...processes.map((process) => create_Option(process)),
			];
		}, { placement: 'top-start' });
	});

	render_Button();
	try_LoadProcesses()
		.then((result) => {
			processes = result.processes;
			// Одна категория - выбирать нечего, кнопка не нужна
			active = result.supported && processes.length > 1;
			button.hidden = !active;
			apply_Selection(selected, { remember: false });
		})
		.catch(() => {
			button.hidden = true;
		});

	return {
		element: button,
		// undefined - ядро без процессов, поле processIds не отправляем. Пустой список - все знания
		get_ProcessIds: () => (active ? [...selected] : undefined),
		// Выбор из истории сессии: не меняет выбор по умолчанию для новых чатов
		set_ProcessIds: (value) => {
			selected = read_Selection(value);
			if (processes.length > 0) {
				apply_Selection(selected, { remember: false });
			}
		},
	};
}
