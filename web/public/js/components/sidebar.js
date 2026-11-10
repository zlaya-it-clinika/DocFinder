// Боковая панель: новый чат, поиск, история по датам, меню пользователя.
import { HistorySources } from '../chat-history.js';
import { navigate_To } from '../router.js';
import { create_Element, create_Icon, create_IconButton, create_Logo, create_SearchBox } from '../lib/dom.js';
import { get_DateGroupLabel, get_Initials } from '../lib/format.js';
import { Themes, get_Theme, set_Theme } from '../preferences.js';
import { can_OpenAdmin, get_RoleLabel, get_UserRole } from '../roles.js';
import { AppTexts, SidebarTexts, format_Text, get_ErrorText } from '../texts.js';
import { get_FirstAdminTab } from '../views/admin.js';
import { open_Confirm } from './confirm.js';
import { create_MenuItem, open_Popover } from './popover.js';
import { show_Toast } from './toast.js';

const themeOptions = [
	{ value: Themes.system, icon: 'monitor', label: SidebarTexts.themeSystem },
	{ value: Themes.light, icon: 'sun', label: SidebarTexts.themeLight },
	{ value: Themes.dark, icon: 'moon', label: SidebarTexts.themeDark },
];

function group_ByDate(items) {
	const groups = [];
	const now = new Date();
	for (const item of items) {
		const label = get_DateGroupLabel(item.updatedAt ?? item.createdAt, now);
		let group = groups.find((existing) => existing.label === label);
		if (!group) {
			group = { label, items: [] };
			groups.push(group);
		}
		group.items.push(item);
	}
	return groups;
}

function create_DangerItem(menuItem) {
	menuItem.classList.add('menu-item-danger');
	return menuItem;
}

function create_ThemeSwitch(closeMenu) {
	const current = get_Theme();
	return create_Element('div', { className: 'segmented', attrs: { role: 'group', 'aria-label': SidebarTexts.themeLabel } },
		themeOptions.map((option) => create_Element('button', {
			className: 'segmented-option',
			attrs: { type: 'button', 'aria-pressed': option.value === current ? 'true' : 'false', title: option.label },
			on: {
				click: () => {
					set_Theme(option.value);
					closeMenu();
				},
			},
		}, [create_Icon(option.icon, 'icon-sm'), create_Element('span', { text: option.label })])));
}

// onNavigate вызывается при переходе по пунктам панели (на телефоне закрывает её), onLogout, onToggle - свернуть
export function create_Sidebar({ user, config, history, onNavigate, onLogout, onToggle }) {
	let query = '';
	let activeSessionId = null;
	let currentUser = user;

	const list = create_Element('nav', { attrs: { 'aria-label': SidebarTexts.searchPlaceholder } });
	const note = create_Element('p', { className: 'sidebar-note', text: SidebarTexts.localHistoryNote });
	note.hidden = true;

	// Чат, который сейчас переименовывается: вместо ссылки показываем поле ввода
	let renamingSessionId = null;

	const start_Rename = (sessionId) => {
		renamingSessionId = sessionId;
		render_List();
	};

	const finish_Rename = async (sessionId, title) => {
		renamingSessionId = null;
		render_List();
		try {
			const before = history.get_Snapshot().items.find((item) => item.sessionId === sessionId)?.title;
			await history.rename_Chat(sessionId, title);
			if (before !== history.get_Snapshot().items.find((item) => item.sessionId === sessionId)?.title) {
				show_Toast(SidebarTexts.chatRenamed, 'success');
			}
		} catch (error) {
			show_Toast(get_ErrorText(error), 'error');
		}
	};

	const confirm_Delete = async (item) => {
		const confirmed = await open_Confirm({
			title: SidebarTexts.chatDeleteTitle,
			text: format_Text(SidebarTexts.chatDeleteText, { title: item.title }),
			confirmLabel: SidebarTexts.chatDeleteConfirm,
			danger: true,
		});
		if (!confirmed) {
			return;
		}
		try {
			await history.delete_Chat(item.sessionId);
			show_Toast(SidebarTexts.chatDeleted, 'success');
			if (item.sessionId === activeSessionId) {
				navigate_To('/', { replace: true });
			}
		} catch (error) {
			show_Toast(get_ErrorText(error), 'error');
		}
	};

	const open_ChatMenu = (button, item) => {
		const { permissions, pinned } = history.get_Snapshot();
		const isPinned = pinned.has(item.sessionId);
		open_Popover(button, (close) => [
			permissions.canRename
				? create_MenuItem(create_Icon('edit'), SidebarTexts.chatRename, () => {
					close();
					start_Rename(item.sessionId);
				})
				: null,
			create_MenuItem(create_Icon('pin'), isPinned ? SidebarTexts.chatUnpin : SidebarTexts.chatPin, () => {
				close();
				history.toggle_Pin(item.sessionId);
			}),
			permissions.canDelete ? create_Element('div', { className: 'menu-separator' }) : null,
			permissions.canDelete
				? create_DangerItem(create_MenuItem(create_Icon('trash'), SidebarTexts.chatDelete, () => {
					close();
					confirm_Delete(item);
				}))
				: null,
		], { placement: 'bottom-end' });
	};

	const create_RenameInput = (item) => {
		const input = create_Element('input', {
			className: 'input chat-rename-input',
			attrs: { 'aria-label': SidebarTexts.chatRenameLabel, maxlength: 120 },
		});
		input.value = item.title;
		let done = false;
		const commit = (save) => {
			if (done) {
				return;
			}
			done = true;
			if (save) {
				finish_Rename(item.sessionId, input.value);
			} else {
				renamingSessionId = null;
				render_List();
			}
		};
		input.addEventListener('keydown', (event) => {
			if (event.key === 'Enter') {
				event.preventDefault();
				commit(true);
			} else if (event.key === 'Escape') {
				event.preventDefault();
				commit(false);
			}
		});
		input.addEventListener('blur', () => commit(true));
		requestAnimationFrame(() => {
			input.focus();
			input.select();
		});
		return input;
	};

	const create_ChatItem = (item, isPinned) => {
		const isActive = item.sessionId === activeSessionId;
		if (item.sessionId === renamingSessionId) {
			return create_Element('div', { className: 'chat-item is-active' }, [create_RenameInput(item)]);
		}
		const menuButton = create_IconButton('more', `${SidebarTexts.chatMenu}: ${item.title}`, null, 'icon-button icon-button-sm chat-item-menu');
		menuButton.setAttribute('aria-haspopup', 'menu');
		menuButton.setAttribute('aria-expanded', 'false');
		menuButton.addEventListener('click', (event) => {
			event.preventDefault();
			open_ChatMenu(menuButton, item);
		});
		return create_Element('div', { className: isActive ? 'chat-item is-active' : 'chat-item' }, [
			create_Element('a', {
				className: 'chat-link',
				text: item.title,
				attrs: {
					href: `/c/${encodeURIComponent(item.sessionId)}`,
					'data-link': true,
					title: item.title,
					'aria-current': isActive ? 'page' : false,
				},
				on: { click: () => onNavigate?.() },
			}),
			isPinned ? create_Icon('pin', 'chat-item-pin') : null,
			menuButton,
		]);
	};

	const create_Group = (label, groupItems, isPinned) => create_Element('div', { className: 'chat-group' }, [
		create_Element('div', { className: 'chat-group-title', text: label }),
		...groupItems.map((item) => create_ChatItem(item, isPinned)),
	]);

	const render_List = () => {
		const { source, items, pinned } = history.get_Snapshot();
		note.hidden = source !== HistorySources.local || items.length === 0;
		const filtered = query ? items.filter((item) => item.title.toLowerCase().includes(query)) : items;
		if (filtered.length === 0) {
			list.replaceChildren(create_Element('div', {
				className: 'sidebar-note',
				text: query ? SidebarTexts.nothingFound : SidebarTexts.empty,
			}));
			return;
		}
		const pinnedItems = filtered.filter((item) => pinned.has(item.sessionId));
		const otherItems = filtered.filter((item) => !pinned.has(item.sessionId));
		list.replaceChildren(
			...(pinnedItems.length > 0 ? [create_Group(SidebarTexts.groupPinned, pinnedItems, true)] : []),
			...group_ByDate(otherItems).map((group) => create_Group(group.label, group.items, false)),
		);
	};

	const userName = create_Element('span', { className: 'user-button-name' });
	const userRole = create_Element('span', { className: 'user-button-role' });
	const avatar = create_Element('span', { className: 'avatar', attrs: { 'aria-hidden': 'true' } });
	const userButton = create_Element('button', {
		className: 'user-button',
		attrs: { type: 'button', 'aria-haspopup': 'menu', 'aria-expanded': 'false', 'aria-label': SidebarTexts.userMenu },
	}, [avatar, create_Element('span', { className: 'user-button-text' }, [userName, userRole])]);

	const render_User = () => {
		avatar.textContent = get_Initials(currentUser.displayName);
		userName.textContent = currentUser.displayName;
		userRole.textContent = get_RoleLabel(get_UserRole(currentUser, config));
	};

	userButton.addEventListener('click', () => {
		open_Popover(userButton, (close) => [
			create_Element('div', { className: 'menu-label', text: SidebarTexts.themeLabel }),
			create_ThemeSwitch(close),
			create_Element('div', { className: 'menu-separator' }),
			can_OpenAdmin(currentUser, config)
				? create_MenuItem(create_Icon('shield'), SidebarTexts.adminPanel, close, { href: `/admin/${get_FirstAdminTab(currentUser, config)}`, dataLink: true })
				: null,
			create_Element('div', { className: 'menu-separator' }),
			create_MenuItem(create_Icon('logout'), SidebarTexts.logout, () => {
				close();
				onLogout();
			}),
		], { placement: 'top-start' });
	});

	const newChatLink = create_Element('a', {
		className: 'sidebar-nav-button',
		attrs: { href: '/', 'data-link': true },
		on: { click: () => onNavigate?.() },
	}, [create_Icon('new-chat'), create_Element('span', { text: SidebarTexts.newChat })]);
	const documentsLink = create_Element('a', {
		className: 'sidebar-nav-button',
		attrs: { href: '/documents', 'data-link': true },
		on: { click: () => onNavigate?.() },
	}, [create_Icon('book'), create_Element('span', { text: SidebarTexts.documents })]);

	const element = create_Element('aside', { className: 'sidebar', attrs: { 'aria-label': AppTexts.productName } }, [
		create_Element('div', { className: 'sidebar-header' }, [
			create_Element('a', { className: 'brand', attrs: { href: '/', 'data-link': true } }, [
				create_Logo(),
				create_Element('span', { text: AppTexts.productName }),
			]),
			create_IconButton('sidebar', SidebarTexts.collapse, () => onToggle()),
		]),
		create_Element('div', { className: 'sidebar-actions' }, [
			newChatLink,
			documentsLink,
			create_SearchBox(SidebarTexts.searchPlaceholder, (value) => {
				query = value;
				render_List();
			}),
		]),
		create_Element('div', { className: 'sidebar-scroll' }, [list, note]),
		create_Element('div', { className: 'sidebar-footer' }, [userButton]),
	]);

	history.subscribe(render_List);
	render_User();
	render_List();

	return {
		element,
		set_ActiveChat(sessionId) {
			activeSessionId = sessionId;
			render_List();
		},
		// section: documents - подсветить пункт «Документы»
		set_ActiveSection(section) {
			documentsLink.classList.toggle('is-active', section === 'documents');
			if (section === 'documents') {
				documentsLink.setAttribute('aria-current', 'page');
			} else {
				documentsLink.removeAttribute('aria-current');
			}
		},
		update_User(nextUser) {
			currentUser = nextUser;
			render_User();
		},
		show_LoadError() {
			list.replaceChildren(create_Element('div', { className: 'sidebar-note', text: SidebarTexts.loadFailed }));
		},
	};
}
