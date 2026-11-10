// Вкладка «Роли»: таблица ролей и конструктор прав. У каждого пользователя ровно одна роль.
import { create_Role, delete_Role, list_Permissions, list_Roles, update_Role } from '../api.js';
import { open_Confirm } from '../components/confirm.js';
import { open_Modal } from '../components/modal.js';
import { show_Toast } from '../components/toast.js';
import { create_Button, create_Element, create_EmptyBlock, create_Icon, create_IconButton, create_LoadingBlock } from '../lib/dom.js';
import { AppTexts, RoleAdminTexts, format_Text, get_ErrorText } from '../texts.js';

const permissionGroups = [
	{ title: RoleAdminTexts.groupChat, icon: 'new-chat', match: (permission) => permission.startsWith('chat.') },
	{ title: RoleAdminTexts.groupDocuments, icon: 'book', match: (permission) => permission.startsWith('documents.') },
	{ title: RoleAdminTexts.groupDrafts, icon: 'file-check', match: (permission) => permission.startsWith('drafts.') },
	{ title: RoleAdminTexts.groupTemplates, icon: 'file', match: (permission) => permission.startsWith('templates.') },
	{ title: RoleAdminTexts.groupAccounts, icon: 'users', match: (permission) => permission.startsWith('users.') || permission.startsWith('roles.') },
	{ title: RoleAdminTexts.groupAudit, icon: 'list', match: (permission) => permission.startsWith('audit.') },
];

function group_Permissions(catalog) {
	const used = new Set();
	const groups = permissionGroups.map((group) => {
		const items = catalog.filter((item) => group.match(item.permission));
		for (const item of items) {
			used.add(item.permission);
		}
		return { ...group, items };
	}).filter((group) => group.items.length > 0);
	const rest = catalog.filter((item) => !used.has(item.permission));
	if (rest.length > 0) {
		groups.push({ title: RoleAdminTexts.groupOther, icon: 'more', items: rest });
	}
	return groups;
}

function create_Field(id, label, control, hint = '', wide = false) {
	return create_Element('div', { className: wide ? 'field field-wide' : 'field' }, [
		create_Element('label', { className: 'field-label', text: label, attrs: { for: id } }),
		control,
		hint ? create_Element('span', { className: 'field-hint', text: hint }) : null,
	]);
}

// Группа прав в конструкторе: заголовок со счётчиком, кнопка «включить все» и строки с переключателями
function create_PermissionGroup(group, selected, locked, onChange) {
	const switches = [];
	const counter = create_Element('span', { className: 'perm-group-count' });
	const toggleAll = create_Element('button', { className: 'link-button perm-group-toggle', attrs: { type: 'button', disabled: locked } });

	const refresh = () => {
		const enabled = switches.filter((input) => input.checked).length;
		counter.textContent = format_Text(RoleAdminTexts.groupCount, { count: enabled, total: switches.length });
		toggleAll.textContent = enabled === switches.length ? RoleAdminTexts.groupNone : RoleAdminTexts.groupAll;
	};

	const rows = group.items.map((item) => {
		const id = `perm-${item.permission.replace(/[^a-z0-9]+/gi, '-')}`;
		const input = create_Element('input', { className: 'switch', attrs: { type: 'checkbox', id, value: item.permission, disabled: locked } });
		input.checked = selected.has(item.permission);
		input.addEventListener('change', () => {
			refresh();
			onChange();
		});
		switches.push(input);
		return create_Element('label', { className: 'perm-row', attrs: { for: id } }, [
			create_Element('span', { className: 'perm-row-text' }, [
				create_Element('span', { className: 'perm-row-title', text: item.title || item.permission }),
				item.description ? create_Element('span', { className: 'perm-row-hint', text: item.description }) : null,
			]),
			input,
		]);
	});

	toggleAll.addEventListener('click', () => {
		const enable = switches.some((input) => !input.checked);
		for (const input of switches) {
			input.checked = enable;
		}
		refresh();
		onChange();
	});
	refresh();

	const element = create_Element('div', { className: 'perm-group' }, [
		create_Element('div', { className: 'perm-group-head' }, [
			create_Icon(group.icon, 'icon-sm'),
			create_Element('span', { className: 'perm-group-title', text: group.title }),
			counter,
			toggleAll,
		]),
		create_Element('div', { className: 'perm-group-rows' }, rows),
	]);
	return { element, switches };
}

function open_RoleForm({ role, catalog, onSaved }) {
	const isEdit = Boolean(role);
	const locked = role?.system === true;
	const errorBox = create_Element('div', { className: 'form-error', attrs: { role: 'alert' } });
	errorBox.hidden = true;
	const titleInput = create_Element('input', { className: 'input', attrs: { id: 'role-title', required: true, autocomplete: 'off', maxlength: 80 } });
	const groupInput = create_Element('input', { className: 'input', attrs: { id: 'role-group', autocomplete: 'off', placeholder: RoleAdminTexts.fieldLdapGroupPlaceholder } });
	const descriptionInput = create_Element('textarea', { className: 'textarea', attrs: { id: 'role-description', rows: 2, maxlength: 300 } });
	titleInput.value = role?.title ?? '';
	descriptionInput.value = role?.description ?? '';
	groupInput.value = role?.ldapGroup ?? '';
	if (locked) {
		titleInput.readOnly = true;
		descriptionInput.readOnly = true;
		groupInput.readOnly = true;
	}

	const selected = new Set(role?.permissions ?? []);
	const totalCounter = create_Element('span', { className: 'perm-total' });
	const groups = [];
	const update_Total = () => {
		const enabled = groups.flatMap((group) => group.switches).filter((input) => input.checked).length;
		totalCounter.textContent = format_Text(RoleAdminTexts.selectedCount, { count: enabled, total: catalog.length });
	};
	for (const group of group_Permissions(catalog)) {
		groups.push(create_PermissionGroup(group, selected, locked, update_Total));
	}
	update_Total();

	const form = create_Element('form', { className: 'form-grid', attrs: { id: 'role-form', novalidate: true } }, [
		create_Field('role-title', RoleAdminTexts.fieldTitle, titleInput),
		create_Field('role-group', RoleAdminTexts.fieldLdapGroup, groupInput, RoleAdminTexts.fieldLdapGroupHint),
		create_Field('role-description', RoleAdminTexts.fieldDescription, descriptionInput, '', true),
		create_Element('div', { className: 'field field-wide' }, [
			create_Element('div', { className: 'perm-header' }, [
				create_Element('span', { className: 'field-label', text: RoleAdminTexts.fieldPermissions }),
				totalCounter,
			]),
			create_Element('div', { className: 'perm-groups' }, groups.map((group) => group.element)),
		]),
		locked ? create_Element('div', { className: 'callout field-wide' }, [
			create_Icon('lock'),
			create_Element('div', { className: 'callout-content', text: RoleAdminTexts.systemHint }),
		]) : null,
		create_Element('div', { className: 'field-wide' }, [errorBox]),
	]);
	const submitButton = create_Button(AppTexts.save, { variant: 'primary', type: 'submit', attrs: { form: 'role-form', disabled: locked } });
	const modal = open_Modal({
		title: isEdit ? RoleAdminTexts.editTitle : RoleAdminTexts.createTitle,
		body: [form],
		footer: [
			create_Button(AppTexts.cancel, { variant: 'ghost', onClick: () => modal.close() }),
			submitButton,
		],
		wide: true,
	});
	form.addEventListener('submit', async (event) => {
		event.preventDefault();
		if (locked || titleInput.value.trim() === '') {
			titleInput.focus();
			return;
		}
		const fields = {
			title: titleInput.value.trim(),
			description: descriptionInput.value.trim(),
			ldapGroup: groupInput.value.trim(),
			permissions: groups.flatMap((group) => group.switches).filter((input) => input.checked).map((input) => input.value),
		};
		errorBox.hidden = true;
		submitButton.disabled = true;
		try {
			const saved = isEdit ? await update_Role(role.roleId, fields) : await create_Role(fields);
			modal.close();
			show_Toast(RoleAdminTexts.saved, 'success');
			onSaved(saved);
		} catch (error) {
			errorBox.textContent = get_ErrorText(error);
			errorBox.hidden = false;
			submitButton.disabled = false;
		}
	});
	if (!locked) {
		titleInput.focus();
	}
}

// Права роли в таблице: группы, где включено хоть что-то, со счётчиком
function create_PermissionSummary(role, catalog) {
	const granted = new Set(role.permissions ?? []);
	const chips = group_Permissions(catalog).map((group) => {
		const enabled = group.items.filter((item) => granted.has(item.permission)).length;
		if (enabled === 0) {
			return null;
		}
		const full = enabled === group.items.length;
		return create_Element('span', {
			className: full ? 'perm-chip is-full' : 'perm-chip',
			attrs: { title: group.items.filter((item) => granted.has(item.permission)).map((item) => item.title || item.permission).join(', ') },
		}, [
			create_Element('span', { text: group.title }),
			full ? null : create_Element('span', { className: 'perm-chip-count', text: `${enabled}/${group.items.length}` }),
		]);
	}).filter(Boolean);
	if (chips.length === 0) {
		return create_Element('span', { className: 'table-cell-muted', text: RoleAdminTexts.noPermissions });
	}
	return create_Element('div', { className: 'perm-chips' }, chips);
}

export function create_RolesTab() {
	let roles = [];
	let catalog = [];
	const countLabel = create_Element('span', { className: 'section-count' });
	const content = create_Element('div', {}, [create_LoadingBlock(AppTexts.loading)]);

	const reload = async () => {
		try {
			const [rolePayload, permissionPayload] = await Promise.all([list_Roles(), list_Permissions()]);
			roles = Array.isArray(rolePayload?.roles) ? rolePayload.roles : [];
			catalog = Array.isArray(permissionPayload?.permissions) ? permissionPayload.permissions : [];
			render_Table();
		} catch (error) {
			content.replaceChildren(create_EmptyBlock('alert', RoleAdminTexts.loadFailed, [
				create_Element('div', { className: 'callout-hint', text: get_ErrorText(error) }),
				create_Button(AppTexts.retry, { variant: 'outline', icon: 'refresh', onClick: reload }),
			]));
		}
	};

	const remove_Role = async (role) => {
		const confirmed = await open_Confirm({
			title: RoleAdminTexts.deleteTitle,
			text: format_Text(RoleAdminTexts.deleteText, { title: role.title }),
			confirmLabel: RoleAdminTexts.delete,
			danger: true,
		});
		if (!confirmed) {
			return;
		}
		try {
			await delete_Role(role.roleId);
			roles = roles.filter((item) => item.roleId !== role.roleId);
			render_Table();
			show_Toast(RoleAdminTexts.deleted, 'success');
		} catch (error) {
			show_Toast(get_ErrorText(error), 'error');
		}
	};

	const open_Edit = (role) => open_RoleForm({
		role,
		catalog,
		onSaved: (saved) => {
			Object.assign(role, saved);
			render_Table();
		},
	});

	function render_Table() {
		countLabel.textContent = String(roles.length);
		if (roles.length === 0) {
			content.replaceChildren(create_EmptyBlock('shield', RoleAdminTexts.empty));
			return;
		}
		content.replaceChildren(create_Element('div', { className: 'table-wrap' }, [
			create_Element('table', { className: 'table roles-table' }, [
				create_Element('thead', {}, [create_Element('tr', {}, [
					create_Element('th', { text: RoleAdminTexts.columnTitle }),
					create_Element('th', { text: RoleAdminTexts.columnPermissions }),
					create_Element('th', { className: 'table-cell-number', text: RoleAdminTexts.columnUsers }),
					create_Element('th', { text: RoleAdminTexts.columnGroup }),
					create_Element('th', { className: 'table-cell-actions' }),
				])]),
				create_Element('tbody', {}, roles.map((role) => {
					const row = create_Element('tr', { className: 'table-row-clickable' }, [
						create_Element('td', {}, [
							create_Element('div', { className: 'role-cell' }, [
								create_Element('span', { className: role.system ? 'role-icon is-system' : 'role-icon', attrs: { 'aria-hidden': 'true' } }, [create_Icon(role.system ? 'lock' : 'shield', 'icon-sm')]),
								create_Element('div', { className: 'role-cell-text' }, [
									create_Element('div', { className: 'cell-title' }, [
										role.title,
										role.system ? create_Element('span', { className: 'badge badge-outline', text: RoleAdminTexts.systemBadge }) : null,
									]),
									role.description ? create_Element('div', { className: 'cell-subtitle', text: role.description }) : null,
								]),
							]),
						]),
						create_Element('td', {}, [create_PermissionSummary(role, catalog)]),
						create_Element('td', { className: 'table-cell-number', text: String(role.userCount ?? 0) }),
						create_Element('td', {}, [role.ldapGroup
							? create_Element('code', { className: 'inline-code', text: role.ldapGroup })
							: create_Element('span', { className: 'table-cell-muted', text: AppTexts.notSpecified })]),
						create_Element('td', { className: 'table-cell-actions' }, [
							create_IconButton('edit', `${RoleAdminTexts.edit}: ${role.title}`, (event) => {
								event.stopPropagation();
								open_Edit(role);
							}),
							role.system ? null : create_IconButton('trash', `${RoleAdminTexts.delete}: ${role.title}`, (event) => {
								event.stopPropagation();
								remove_Role(role);
							}),
						]),
					]);
					row.addEventListener('click', () => open_Edit(role));
					return row;
				})),
			]),
		]));
	}

	const element = create_Element('section', { className: 'page-section' }, [
		create_Element('div', { className: 'section-header' }, [
			create_Element('h2', { className: 'section-title' }, [RoleAdminTexts.sectionTitle, countLabel]),
			create_Element('div', { className: 'section-tools' }, [
				create_Button(RoleAdminTexts.add, {
					variant: 'primary',
					icon: 'plus',
					onClick: () => open_RoleForm({
						role: null,
						catalog,
						onSaved: (saved) => {
							roles.push(saved);
							render_Table();
						},
					}),
				}),
			]),
		]),
		create_Element('p', { className: 'section-lead', text: RoleAdminTexts.lead }),
		content,
	]);
	reload();
	return { element };
}
