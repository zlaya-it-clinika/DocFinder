// Экран ожидания подтверждения, как AccountPending в open-webui.
// Пока администратор не сменил роль, остальной интерфейс недоступен: ядро отвечает account_pending.
import { fetch_AdminDetails } from '../api.js';
import { create_Button, create_Element, create_Icon } from '../lib/dom.js';
import { AppTexts, PendingTexts, format_Text } from '../texts.js';

// onRecheck() - перечитать сессию, вернуть true, если доступ уже открыт. onLogout() - выйти
export function create_PendingView({ onRecheck, onLogout, compact = false }) {
	const adminContact = create_Element('p', { className: 'pending-contact' });
	adminContact.hidden = true;

	const recheckButton = create_Button(PendingTexts.checkAgain, {
		variant: 'primary',
		icon: 'refresh',
		onClick: async () => {
			recheckButton.disabled = true;
			try {
				await onRecheck();
			} finally {
				recheckButton.disabled = false;
			}
		},
	});

	const element = create_Element('main', { className: 'auth-page' }, [
		create_Element('div', { className: 'auth-card pending-card' }, [
			create_Element('div', { className: 'auth-header' }, [
				create_Element('span', { className: 'brand-mark' }, [create_Icon('clock')]),
				create_Element('h1', { className: 'auth-title', text: compact ? AppTexts.productName : PendingTexts.title }),
				compact ? create_Element('p', { className: 'auth-lead', text: PendingTexts.title }) : null,
				create_Element('p', { className: 'auth-lead', text: PendingTexts.lead }),
			]),
			create_Element('p', { className: 'pending-hint', text: PendingTexts.hint }),
			adminContact,
			create_Element('div', { className: 'pending-actions' }, [
				recheckButton,
				create_Button(PendingTexts.signOut, { variant: 'ghost', icon: 'logout', onClick: onLogout }),
			]),
		]),
	]);

	// Контакт показываем, только если администратор разрешил это в настройках
	fetch_AdminDetails()
		.then((details) => {
			if (details?.email) {
				adminContact.textContent = format_Text(PendingTexts.adminContact, { name: details.name || details.email, email: details.email });
				adminContact.hidden = false;
			}
		})
		.catch(() => {
			// Контакт скрыт или ядро старое - экран работает и без него
		});

	return {
		element,
		focus: () => recheckButton.focus(),
	};
}
