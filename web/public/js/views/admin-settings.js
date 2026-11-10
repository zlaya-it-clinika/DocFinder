// Вкладка «Настройки»: аутентификация и LDAP, как раздел General в админ-панели open-webui.
// Значения из .env ядра - только начальные, после «Сохранить» действуют значения из базы ядра.
import {
	check_IsNotSupported,
	fetch_LdapServer,
	fetch_LdapToggle,
	fetch_SignupSettings,
	save_LdapServer,
	save_LdapToggle,
	save_SignupSettings,
} from '../api.js';
import { show_Toast } from '../components/toast.js';
import { create_Button, create_Element, create_EmptyBlock, create_LoadingBlock } from '../lib/dom.js';
import { get_RoleLabel, roleChoices } from '../roles.js';
import { AppTexts, SettingsTexts, get_ErrorText } from '../texts.js';

const maxPort = 65535;

// Строка настройки: подпись и пояснение слева, переключатель или список справа
function create_SettingRow(id, label, hint, control) {
	// Список на узком экране уходит под подпись, переключатель остаётся справа
	const className = control.tagName === 'SELECT' ? 'setting-row setting-row-select' : 'setting-row';
	return create_Element('div', { className }, [
		create_Element('div', { className: 'setting-text' }, [
			create_Element('label', { className: 'setting-label', text: label, attrs: { for: id } }),
			hint ? create_Element('span', { className: 'field-hint', text: hint }) : null,
		]),
		control,
	]);
}

function create_Switch(id, checked) {
	const input = create_Element('input', {
		className: 'switch',
		attrs: { id, type: 'checkbox', role: 'switch' },
	});
	input.checked = Boolean(checked);
	return input;
}

function create_TextField(id, label, { value = '', placeholder = '', hint = '', type = 'text', wide = false } = {}) {
	const input = create_Element('input', {
		className: 'input',
		attrs: { id, type, placeholder, autocomplete: type === 'password' ? 'new-password' : 'off', spellcheck: 'false' },
	});
	input.value = value ?? '';
	const hintElement = create_Element('span', { className: 'field-hint', text: hint });
	hintElement.hidden = hint === '';
	const field = create_Element('div', { className: wide ? 'field field-wide' : 'field' }, [
		create_Element('label', { className: 'field-label', text: label, attrs: { for: id } }),
		input,
		hintElement,
	]);
	return { field, input, hintElement };
}

function build_Form(signup, ldapToggle, ldapServer) {
	const roleSelect = create_Element('select', { className: 'select setting-select', attrs: { id: 'settings-default-role' } },
		roleChoices.map((role) => create_Element('option', {
			text: get_RoleLabel(role),
			attrs: { value: role, selected: role === signup.defaultUserRole },
		})));
	const signupSwitch = create_Switch('settings-enable-signup', signup.enableSignup);
	const adminDetailsSwitch = create_Switch('settings-admin-details', signup.showAdminDetails);
	const ldapSwitch = create_Switch('settings-enable-ldap', ldapToggle.enableLdap);

	const fields = {
		label: create_TextField('ldap-label', SettingsTexts.label, { value: ldapServer.label, placeholder: SettingsTexts.labelPlaceholder, hint: SettingsTexts.labelHint }),
		host: create_TextField('ldap-host', SettingsTexts.host, { value: ldapServer.host, placeholder: SettingsTexts.hostPlaceholder }),
		port: create_TextField('ldap-port', SettingsTexts.port, {
			value: ldapServer.port ? String(ldapServer.port) : '',
			placeholder: SettingsTexts.portPlaceholder,
			hint: SettingsTexts.portHint,
			type: 'number',
		}),
		appDn: create_TextField('ldap-app-dn', SettingsTexts.appDn, { value: ldapServer.appDn, placeholder: SettingsTexts.appDnPlaceholder, hint: SettingsTexts.appDnHint }),
		appPassword: create_TextField('ldap-app-password', SettingsTexts.appPassword, {
			placeholder: SettingsTexts.appPasswordPlaceholder,
			hint: ldapServer.appDnPasswordSet ? SettingsTexts.appPasswordSetHint : '',
			type: 'password',
		}),
		attributeUsername: create_TextField('ldap-attribute-username', SettingsTexts.attributeUsername, { value: ldapServer.attributeForUsername, hint: SettingsTexts.attributeUsernameHint }),
		attributeMail: create_TextField('ldap-attribute-mail', SettingsTexts.attributeMail, { value: ldapServer.attributeForMail, hint: SettingsTexts.attributeMailHint }),
		searchBase: create_TextField('ldap-search-base', SettingsTexts.searchBase, { value: ldapServer.searchBase, placeholder: SettingsTexts.searchBasePlaceholder, wide: true }),
		searchFilters: create_TextField('ldap-search-filters', SettingsTexts.searchFilters, {
			value: ldapServer.searchFilters,
			placeholder: SettingsTexts.searchFiltersPlaceholder,
			hint: SettingsTexts.searchFiltersHint,
			wide: true,
		}),
		certificatePath: create_TextField('ldap-certificate-path', SettingsTexts.certificatePath, {
			value: ldapServer.certificatePath,
			placeholder: SettingsTexts.certificatePathPlaceholder,
			hint: SettingsTexts.certificatePathHint,
		}),
		ciphers: create_TextField('ldap-ciphers', SettingsTexts.ciphers, { value: ldapServer.ciphers, placeholder: SettingsTexts.ciphersPlaceholder, hint: SettingsTexts.ciphersHint }),
	};
	const tlsSwitch = create_Switch('ldap-use-tls', ldapServer.useTls);
	const validateSwitch = create_Switch('ldap-validate-cert', ldapServer.validateCert);
	// Роли по группам каталога: роль, у которой в конструкторе указана группа, назначается при входе
	const groupsSwitch = create_Switch('ldap-enable-groups', ldapServer.enableGroupManagement);
	const groupAttribute = create_TextField('ldap-group-attribute', SettingsTexts.groupAttribute, { value: ldapServer.groupAttribute, hint: SettingsTexts.groupAttributeHint });
	const groupFields = create_Element('div', { className: 'form-grid' }, [groupAttribute.field]);

	// Сертификат и шифры нужны только с TLS, как в open-webui
	const tlsFields = create_Element('div', { className: 'form-grid' }, [
		fields.certificatePath.field,
		fields.ciphers.field,
		create_Element('div', { className: 'field-wide' }, [
			create_SettingRow('ldap-validate-cert', SettingsTexts.validateCert, SettingsTexts.validateCertHint, validateSwitch),
		]),
	]);
	const serverBlock = create_Element('div', { className: 'settings-server' }, [
		create_Element('div', { className: 'form-grid' }, [
			fields.label.field,
			fields.host.field,
			fields.port.field,
			fields.appDn.field,
			fields.appPassword.field,
			fields.attributeUsername.field,
			fields.attributeMail.field,
			fields.searchBase.field,
			fields.searchFilters.field,
		]),
		create_SettingRow('ldap-use-tls', SettingsTexts.useTls, SettingsTexts.useTlsHint, tlsSwitch),
		tlsFields,
		create_SettingRow('ldap-enable-groups', SettingsTexts.enableGroups, SettingsTexts.enableGroupsHint, groupsSwitch),
		groupFields,
	]);

	const sync_Visibility = () => {
		serverBlock.hidden = !ldapSwitch.checked;
		tlsFields.hidden = !tlsSwitch.checked;
		groupFields.hidden = !groupsSwitch.checked;
	};
	ldapSwitch.addEventListener('change', sync_Visibility);
	tlsSwitch.addEventListener('change', sync_Visibility);
	groupsSwitch.addEventListener('change', sync_Visibility);
	sync_Visibility();

	const authSection = create_Element('section', { className: 'page-section' }, [
		create_Element('div', { className: 'page-heading' }, [
			create_Element('h2', { className: 'section-title', text: SettingsTexts.authTitle }),
			create_Element('p', { className: 'section-lead', text: SettingsTexts.authLead }),
		]),
		create_Element('div', { className: 'settings-list' }, [
			create_SettingRow('settings-default-role', SettingsTexts.defaultRole, SettingsTexts.defaultRoleHint, roleSelect),
			create_SettingRow('settings-enable-signup', SettingsTexts.enableSignup, SettingsTexts.enableSignupHint, signupSwitch),
			create_SettingRow('settings-admin-details', SettingsTexts.showAdminDetails, SettingsTexts.showAdminDetailsHint, adminDetailsSwitch),
		]),
	]);
	const ldapSection = create_Element('section', { className: 'page-section' }, [
		create_Element('div', { className: 'page-heading' }, [
			create_Element('h2', { className: 'section-title', text: SettingsTexts.ldapTitle }),
			create_Element('p', { className: 'section-lead', text: SettingsTexts.ldapLead }),
		]),
		create_Element('div', { className: 'settings-list' }, [
			create_SettingRow('settings-enable-ldap', SettingsTexts.enableLdap, SettingsTexts.enableLdapHint, ldapSwitch),
		]),
		serverBlock,
	]);

	return {
		sections: [authSection, ldapSection],
		read_Signup: () => ({
			defaultUserRole: roleSelect.value,
			enableSignup: signupSwitch.checked,
			showAdminDetails: adminDetailsSwitch.checked,
		}),
		read_LdapEnabled: () => ldapSwitch.checked,
		read_LdapServer: () => {
			const portText = fields.port.input.value.trim();
			return {
				label: fields.label.input.value.trim(),
				host: fields.host.input.value.trim(),
				port: portText === '' ? 0 : Number(portText),
				appDn: fields.appDn.input.value.trim(),
				appDnPassword: fields.appPassword.input.value,
				attributeForUsername: fields.attributeUsername.input.value.trim(),
				attributeForMail: fields.attributeMail.input.value.trim(),
				searchBase: fields.searchBase.input.value.trim(),
				searchFilters: fields.searchFilters.input.value.trim(),
				useTls: tlsSwitch.checked,
				certificatePath: fields.certificatePath.input.value.trim(),
				validateCert: validateSwitch.checked,
				ciphers: fields.ciphers.input.value.trim(),
				enableGroupManagement: groupsSwitch.checked,
				groupAttribute: groupAttribute.input.value.trim(),
			};
		},
		// После сохранения пароль из поля убираем: ядро его больше не отдаёт
		apply_SavedServer: (saved) => {
			fields.appPassword.input.value = '';
			fields.appPassword.hintElement.textContent = saved.appDnPasswordSet ? SettingsTexts.appPasswordSetHint : '';
			fields.appPassword.hintElement.hidden = !saved.appDnPasswordSet;
		},
		focus_Field: (name) => fields[name]?.input.focus(),
	};
}

// Проверка до отправки. Возвращает текст ошибки и поле для фокуса или null
function check_LdapServer(server) {
	const required = [
		['label', server.label],
		['host', server.host],
		['attributeUsername', server.attributeForUsername],
		['attributeMail', server.attributeForMail],
		['searchBase', server.searchBase],
	];
	const missing = required.find(([, value]) => value === '');
	if (missing) {
		return { text: SettingsTexts.requiredMissing, field: missing[0] };
	}
	if (!Number.isInteger(server.port) || server.port < 0 || server.port > maxPort) {
		return { text: SettingsTexts.portInvalid, field: 'port' };
	}
	return null;
}

export function create_SettingsTab() {
	const content = create_Element('div', { className: 'page-sections' }, [create_LoadingBlock(AppTexts.loading)]);
	const element = create_Element('div', { className: 'page-sections' }, [content]);

	const show_LoadError = (error) => {
		const title = check_IsNotSupported(error) ? SettingsTexts.notSupported : SettingsTexts.loadFailed;
		const hint = check_IsNotSupported(error) ? SettingsTexts.notSupportedHint : get_ErrorText(error);
		content.replaceChildren(create_EmptyBlock('alert', title, [
			create_Element('div', { className: 'callout-hint', text: hint }),
			create_Button(AppTexts.retry, { variant: 'outline', icon: 'refresh', onClick: load }),
		]));
	};

	async function load() {
		content.replaceChildren(create_LoadingBlock(AppTexts.loading));
		let loaded;
		try {
			loaded = await Promise.all([fetch_SignupSettings(), fetch_LdapToggle(), fetch_LdapServer()]);
		} catch (error) {
			show_LoadError(error);
			return;
		}
		const form = build_Form(...loaded);
		const saveButton = create_Button(SettingsTexts.save, { variant: 'primary', icon: 'check' });
		saveButton.addEventListener('click', async () => {
			const ldapEnabled = form.read_LdapEnabled();
			const server = form.read_LdapServer();
			const problem = ldapEnabled ? check_LdapServer(server) : null;
			if (problem) {
				show_Toast(problem.text, 'error');
				form.focus_Field(problem.field);
				return;
			}
			saveButton.disabled = true;
			try {
				await save_SignupSettings(form.read_Signup());
				// Подключение сохраняем до включения: с неверными настройками LDAP не включится
				if (ldapEnabled) {
					form.apply_SavedServer(await save_LdapServer(server));
				}
				await save_LdapToggle(ldapEnabled);
				show_Toast(SettingsTexts.saved, 'success');
			} catch (error) {
				show_Toast(get_ErrorText(error), 'error');
			} finally {
				saveButton.disabled = false;
			}
		});
		content.replaceChildren(...form.sections, create_Element('div', { className: 'settings-actions' }, [saveButton]));
	}

	load();
	return { element };
}
