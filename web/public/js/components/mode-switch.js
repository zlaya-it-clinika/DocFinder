// Переключатель режима чата под полем ввода: «Чтение» - ответы по базе знаний, «План» - подготовка документов.
// Режим хранится в сессии ядра, новый чат берёт последний выбор пользователя. Без права на шаблоны переключателя нет.
import { create_Element, create_Icon } from '../lib/dom.js';
import { read_Preference, write_Preference } from '../preferences.js';
import { Permissions, has_Permission } from '../roles.js';
import { ChatTexts } from '../texts.js';

// Значения поля mode из контракта ядра
export const ChatModes = Object.freeze({
	read: 'read',
	plan: 'plan',
});

const modeOptions = [
	{ mode: ChatModes.read, icon: 'book', label: ChatTexts.modeRead, hint: ChatTexts.modeReadHint },
	{ mode: ChatModes.plan, icon: 'edit', label: ChatTexts.modePlan, hint: ChatTexts.modePlanHint },
];

function get_PreferenceName(user) {
	return `chatMode.${user.userId}`;
}

function normalize_Mode(mode) {
	return mode === ChatModes.plan ? ChatModes.plan : ChatModes.read;
}

// onChange(mode) - режим сменили кнопкой или из истории сессии
export function create_ModeSwitch({ user, config, onChange }) {
	const active = has_Permission(user, config, Permissions.templatesUse) && has_Permission(user, config, Permissions.chatUse);
	let selected = normalize_Mode(read_Preference(get_PreferenceName(user), ChatModes.read));

	const buttons = modeOptions.map((option) => create_Element('button', {
		className: 'mode-switch-option',
		attrs: { type: 'button', role: 'radio', title: option.hint, 'aria-label': `${option.label}: ${option.hint}` },
		dataset: { mode: option.mode },
	}, [create_Icon(option.icon, 'icon-sm'), create_Element('span', { text: option.label })]));
	const element = create_Element('div', {
		className: 'mode-switch',
		attrs: { role: 'radiogroup', 'aria-label': ChatTexts.modeLabel },
	}, buttons);
	element.hidden = !active;

	const render_Buttons = () => {
		for (const button of buttons) {
			const isChecked = button.dataset.mode === selected;
			button.setAttribute('aria-checked', isChecked ? 'true' : 'false');
			// В группе радиокнопок с клавиатуры доступна только выбранная, стрелки переключают
			button.tabIndex = isChecked ? 0 : -1;
		}
	};

	const apply_Mode = (mode, { remember }) => {
		const next = normalize_Mode(mode);
		const changed = next !== selected;
		selected = next;
		render_Buttons();
		if (remember) {
			write_Preference(get_PreferenceName(user), selected);
		}
		if (changed) {
			onChange?.(selected);
		}
	};

	for (const button of buttons) {
		button.addEventListener('click', () => apply_Mode(button.dataset.mode, { remember: true }));
	}
	element.addEventListener('keydown', (event) => {
		if (!['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown'].includes(event.key)) {
			return;
		}
		event.preventDefault();
		const index = modeOptions.findIndex((option) => option.mode === selected);
		const step = event.key === 'ArrowLeft' || event.key === 'ArrowUp' ? -1 : 1;
		const next = modeOptions[(index + step + modeOptions.length) % modeOptions.length];
		apply_Mode(next.mode, { remember: true });
		buttons.find((button) => button.dataset.mode === next.mode)?.focus();
	});

	render_Buttons();

	return {
		element,
		// undefined - переключателя нет, поле mode не отправляем: ядро отвечает в режиме сессии
		get_Mode: () => (active ? selected : undefined),
		is_Plan: () => active && selected === ChatModes.plan,
		// Режим из истории сессии: не меняет выбор по умолчанию для новых чатов
		set_Mode: (mode) => {
			if (active) {
				apply_Mode(mode, { remember: false });
			}
		},
	};
}
