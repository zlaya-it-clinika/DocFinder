// Оболочка после входа: боковая панель, верхняя строка и сменяемая область контента.
import { create_ChatHistory } from '../chat-history.js';
import { create_Sidebar } from '../components/sidebar.js';
import { create_Element, create_Icon, create_IconButton } from '../lib/dom.js';
import { read_Preference, write_Preference } from '../preferences.js';
import { navigate_To } from '../router.js';
import { AppTexts, SidebarTexts } from '../texts.js';
import { create_AdminView } from './admin.js';
import { create_ChatView } from './chat.js';
import { create_DocumentsPage } from './documents-page.js';
import { create_DocumentView } from './document-view.js';

const narrowScreenQuery = window.matchMedia('(max-width: 800px)');

export function create_Shell({ root, user, config, onLogout }) {
	const history = create_ChatHistory(user);
	let currentView = null;
	let currentChatId = null;

	const app = create_Element('div', { className: 'app' });
	const viewHost = create_Element('section', { className: 'view', attrs: { id: 'main-content', tabindex: '-1' } });
	const topbarTitle = create_Element('div', { className: 'topbar-title' });

	const close_MobileSidebar = () => app.classList.remove('is-sidebar-open');

	const toggle_Sidebar = () => {
		if (narrowScreenQuery.matches) {
			app.classList.toggle('is-sidebar-open');
			return;
		}
		const collapsed = !app.classList.contains('is-sidebar-collapsed');
		app.classList.toggle('is-sidebar-collapsed', collapsed);
		write_Preference('sidebarCollapsed', collapsed);
	};

	const sidebar = create_Sidebar({
		user,
		config,
		history,
		onNavigate: close_MobileSidebar,
		onLogout,
		onToggle: toggle_Sidebar,
	});

	const topbar = create_Element('header', { className: 'topbar' }, [
		create_IconButton('sidebar', SidebarTexts.expand, toggle_Sidebar, 'icon-button topbar-sidebar-toggle'),
		topbarTitle,
		create_Element('div', { className: 'topbar-actions' }, [
			create_Element('a', {
				className: 'icon-button',
				attrs: { href: '/documents', 'data-link': true, title: SidebarTexts.documents, 'aria-label': SidebarTexts.documents },
			}, [create_Icon('book')]),
			create_Element('a', {
				className: 'icon-button',
				attrs: { href: '/', 'data-link': true, title: SidebarTexts.newChat, 'aria-label': SidebarTexts.newChat },
			}, [create_Icon('new-chat')]),
		]),
	]);

	app.append(
		create_Element('a', { className: 'skip-link', text: AppTexts.skipToContent, attrs: { href: '#main-content' } }),
		sidebar.element,
		create_Element('div', { className: 'sidebar-backdrop', on: { click: close_MobileSidebar } }),
		create_Element('main', { className: 'main' }, [topbar, viewHost]),
	);
	app.classList.toggle('is-sidebar-collapsed', read_Preference('sidebarCollapsed', false) === true);
	root.replaceChildren(app);

	// Заголовок верхней строки - название открытого чата
	const update_Title = () => {
		const item = history.get_Snapshot().items.find((entry) => entry.sessionId === currentChatId);
		topbarTitle.textContent = item ? item.title : '';
	};
	history.subscribe(update_Title);

	history.load_Items().catch(() => sidebar.show_LoadError());

	const mount_View = (view) => {
		currentView?.destroy?.();
		currentView = view;
		viewHost.replaceChildren(view.element);
		close_MobileSidebar();
	};

	return {
		show_Chat(sessionId) {
			// Чат сам создал сессию и поменял адрес: экран уже открыт, перерисовывать не нужно
			if (sessionId && currentView?.get_SessionId?.() === sessionId) {
				return;
			}
			currentChatId = sessionId;
			sidebar.set_ActiveChat(sessionId);
			sidebar.set_ActiveSection('chat');
			update_Title();
			const view = create_ChatView({
				sessionId,
				user,
				config,
				history,
				onSessionCreated: (createdId) => {
					currentChatId = createdId;
					sidebar.set_ActiveChat(createdId);
					navigate_To(`/c/${encodeURIComponent(createdId)}`, { silent: true });
					update_Title();
				},
			});
			mount_View(view);
			view.focus();
		},

		show_Documents() {
			currentChatId = null;
			sidebar.set_ActiveChat(null);
			sidebar.set_ActiveSection('documents');
			topbarTitle.textContent = '';
			mount_View(create_DocumentsPage({ user, config }));
		},

		show_Document(documentId, path) {
			currentChatId = null;
			sidebar.set_ActiveChat(null);
			sidebar.set_ActiveSection('documents');
			topbarTitle.textContent = '';
			mount_View(create_DocumentView({ documentId, path }));
		},

		show_Admin(tab) {
			currentChatId = null;
			sidebar.set_ActiveChat(null);
			sidebar.set_ActiveSection('admin');
			topbarTitle.textContent = '';
			mount_View(create_AdminView({ tab, user, config }));
		},

		update_User(nextUser) {
			sidebar.update_User(nextUser);
		},
	};
}
