// Чат внутри iframe виджета на портале. Тот же API и те же компоненты, что у полной страницы.
import { fetch_AuthSession, fetch_PublicConfig, logout_User, on_Unauthorized, set_CsrfToken } from './api.js';
import { create_ChatHistory } from './chat-history.js';
import { show_Toast } from './components/toast.js';
import { create_Element, create_EmptyBlock, create_Icon, create_IconButton, create_Logo } from './lib/dom.js';
import { apply_Theme, get_Theme, read_Preference, remove_Preference, write_Preference } from './preferences.js';
import { check_IsPending } from './roles.js';
import { AppTexts, AuthTexts, PendingTexts, WidgetTexts, get_ErrorText } from './texts.js';
import { create_ChatView } from './views/chat.js';
import { create_LoginView } from './views/login.js';
import { create_PendingView } from './views/pending.js';

const root = document.getElementById('app');
const closeMessageType = 'docfinder:close';

let config = null;
let user = null;
let chatView = null;

apply_Theme(get_Theme());
document.title = AppTexts.productName;

const content = create_Element('div', { className: 'embed-content' });
const fullLink = create_Element('a', {
	className: 'icon-button',
	attrs: { href: '/', target: '_blank', rel: 'noopener', title: WidgetTexts.openFull, 'aria-label': WidgetTexts.openFull },
}, [create_Icon('external')]);
const newChatButton = create_IconButton('new-chat', WidgetTexts.newChat, () => show_Chat(null));

root.replaceChildren(create_Element('div', { className: 'embed-root' }, [
	create_Element('header', { className: 'embed-header' }, [
		create_Element('span', { className: 'brand' }, [
			create_Logo(),
			create_Element('span', { text: WidgetTexts.title }),
		]),
		newChatButton,
		fullLink,
		// Свернуть просит загрузчик на странице портала, сам iframe себя скрыть не может
		create_IconButton('close', WidgetTexts.close, () => window.parent.postMessage({ type: closeMessageType }, '*')),
	]),
	content,
]));

// Последний чат виджета, чтобы при переходе по страницам портала разговор не терялся
function get_StoredSessionName() {
	return `widget.session.${user.userId}`;
}

function update_FullLink(sessionId) {
	fullLink.setAttribute('href', sessionId ? `/c/${encodeURIComponent(sessionId)}` : '/');
}

function show_Chat(sessionId) {
	chatView?.destroy();
	if (sessionId) {
		write_Preference(get_StoredSessionName(), sessionId);
	} else {
		remove_Preference(get_StoredSessionName());
	}
	update_FullLink(sessionId);
	chatView = create_ChatView({
		sessionId,
		user,
		config,
		history: create_ChatHistory(user),
		compact: true,
		onSessionCreated: (createdId) => {
			write_Preference(get_StoredSessionName(), createdId);
			update_FullLink(createdId);
		},
	});
	content.replaceChildren(chatView.element);
	chatView.focus();
}

// После входа: чат или экран ожидания, если учётную запись ещё не подтвердили
function open_Session(session) {
	set_CsrfToken(session.csrfToken);
	user = session.user;
	if (check_IsPending(user, config)) {
		show_Pending();
		return;
	}
	newChatButton.hidden = false;
	show_Chat(read_Preference(get_StoredSessionName(), null));
}

function show_Login() {
	user = null;
	chatView?.destroy();
	chatView = null;
	newChatButton.hidden = true;
	const view = create_LoginView({ compact: true, onSuccess: open_Session });
	content.replaceChildren(view.element);
	view.focus();
}

function show_Pending() {
	chatView?.destroy();
	chatView = null;
	newChatButton.hidden = true;
	const view = create_PendingView({
		compact: true,
		onRecheck: async () => {
			try {
				const session = await fetch_AuthSession();
				if (check_IsPending(session.user, config)) {
					show_Toast(PendingTexts.stillPending, 'info');
					return;
				}
				open_Session(session);
			} catch (error) {
				show_Toast(get_ErrorText(error), 'error');
			}
		},
		onLogout: async () => {
			try {
				await logout_User();
			} catch {
				// Сессию на клиенте сбрасываем в любом случае
			}
			set_CsrfToken('');
			show_Login();
		},
	});
	content.replaceChildren(view.element);
	view.focus();
}

// Ссылки data-link внутри чата ведут в роутер полной страницы. В виджете роутера нет
document.addEventListener('click', (event) => {
	const link = event.target.closest('a[data-link]');
	if (!link) {
		return;
	}
	event.preventDefault();
	if (link.getAttribute('href') === '/') {
		show_Chat(null);
		return;
	}
	window.open(link.getAttribute('href'), '_blank', 'noopener');
});

async function boot() {
	try {
		config = await fetch_PublicConfig();
	} catch (error) {
		content.replaceChildren(create_EmptyBlock('alert', AppTexts.fatalTitle, [
			create_Element('div', { className: 'callout-hint', text: get_ErrorText(error) }),
		]));
		return;
	}
	on_Unauthorized(() => {
		if (user) {
			show_Toast(AuthTexts.sessionExpired, 'info');
			show_Login();
		}
	});
	try {
		open_Session(await fetch_AuthSession());
	} catch {
		show_Login();
	}
}

boot();
