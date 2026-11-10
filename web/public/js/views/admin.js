// Админ-панель: вкладки как в open-webui. Пользователи, база знаний, журнал, модели ИИ, реквизиты организации, настройки входа.
import { create_Element, create_Icon } from '../lib/dom.js';
import { Permissions, has_Permission } from '../roles.js';
import { AdminTexts } from '../texts.js';
import { create_AITab } from './admin-ai.js';
import { create_AuditTab } from './admin-audit.js';
import { create_KnowledgeTab } from './admin-knowledge.js';
import { create_OrganizationTab } from './admin-organization.js';
import { create_RolesTab } from './admin-roles.js';
import { create_SettingsTab } from './admin-settings.js';
import { create_UsersTab } from './admin-users.js';

const adminTabs = [
	{ id: 'users', icon: 'users', label: AdminTexts.tabUsers, permission: Permissions.usersManage, create: create_UsersTab },
	{ id: 'roles', icon: 'shield', label: AdminTexts.tabRoles, permission: Permissions.rolesManage, create: create_RolesTab },
	{
		id: 'knowledge',
		icon: 'book',
		label: AdminTexts.tabKnowledge,
		permission: Permissions.globalManage,
		create: create_KnowledgeTab,
		visible: (user, config) => has_Permission(user, config, Permissions.globalManage) || has_Permission(user, config, Permissions.personalViewAll),
	},
	{ id: 'audit', icon: 'list', label: AdminTexts.tabAudit, permission: Permissions.auditView, create: create_AuditTab },
	{ id: 'ai', icon: 'cpu', label: AdminTexts.tabAI, permission: Permissions.aiManage, create: create_AITab },
	{ id: 'organization', icon: 'building', label: AdminTexts.tabOrganization, permission: Permissions.organizationManage, create: create_OrganizationTab },
	{ id: 'settings', icon: 'lock', label: AdminTexts.tabSettings, permission: Permissions.usersManage, create: create_SettingsTab },
];

function visible_AdminTabs(user, config) {
	return adminTabs.filter((item) => (item.visible ? item.visible(user, config) : has_Permission(user, config, item.permission)));
}

export function get_FirstAdminTab(user, config) {
	return visible_AdminTabs(user, config)[0]?.id ?? 'users';
}

export function create_AdminView({ tab, user, config }) {
	const tabs = visible_AdminTabs(user, config);
	const activeTab = tabs.find((item) => item.id === tab) ?? tabs[0];
	if (!activeTab) {
		return { element: create_Element('div', { className: 'page' }), destroy() {} };
	}

	const tabsNav = create_Element('nav', { className: 'tabs', attrs: { 'aria-label': AdminTexts.title } },
		tabs.map((item) => create_Element('a', {
			className: item.id === activeTab.id ? 'tab is-active' : 'tab',
			attrs: {
				href: `/admin/${item.id}`,
				'data-link': true,
				'aria-current': item.id === activeTab.id ? 'page' : false,
			},
		}, [create_Icon(item.icon, 'icon-sm'), create_Element('span', { text: item.label })])));

	const content = activeTab.create({ user, config });

	const element = create_Element('div', { className: 'page' }, [
		create_Element('div', { className: 'page-inner' }, [
			create_Element('h1', { className: 'page-title', text: AdminTexts.title }),
			tabsNav,
			content.element,
		]),
	]);

	return {
		element,
		destroy: () => content.destroy?.(),
	};
}
