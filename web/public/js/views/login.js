// Экран входа, как в open-webui: вход по почте, регистрация, вход через LDAP и первый запуск.
// Какие режимы доступны, говорит ядро через /auth/config. Пока ответа нет, работает обычный вход.
import { fetch_AuthConfig, login_LdapUser, login_User, signup_User } from '../api.js';
import { show_Toast } from '../components/toast.js';
import { create_Element, create_Icon, create_Logo } from '../lib/dom.js';
import { check_IsEmail } from '../lib/validate.js';
import { read_Preference, write_Preference } from '../preferences.js';
import { navigate_To } from '../router.js';
import { AppTexts, AuthTexts, format_Text, get_ErrorText } from '../texts.js';

export const AuthModes = Object.freeze({
	signin: 'signin',
	signup: 'signup',
	ldap: 'ldap',
});

// Последний способ входа запоминается в браузере: сотрудник с доменной учётной записью
// не должен каждый раз нажимать «Продолжить через LDAP»
const preferredModeName = 'auth.mode';

function create_Field(id, label, input) {
	const field = create_Element('div', { className: 'field' }, [
		create_Element('label', { className: 'field-label', text: label, attrs: { for: id } }),
		input,
	]);
	return { field, input };
}

// onSuccess({ user, csrfToken })
export function create_LoginView({ onSuccess, compact = false }) {
	let authConfig = { onboarding: false, enableSignup: false, enableLdap: false, ldapLabel: '' };
	let mode = AuthModes.signin;

	const titleElement = create_Element('h1', { className: 'auth-title' });
	const leadElement = create_Element('p', { className: 'auth-lead' });
	const onboardingNote = create_Element('p', { className: 'auth-note' }, [
		create_Icon('info', 'icon-sm'),
		create_Element('span', { text: AuthTexts.onboardingNote }),
	]);

	const nameField = create_Field('auth-name', AuthTexts.nameLabel, create_Element('input', {
		className: 'input',
		attrs: { id: 'auth-name', name: 'name', autocomplete: 'name', placeholder: AuthTexts.namePlaceholder },
	}));
	const emailField = create_Field('auth-email', AuthTexts.emailLabel, create_Element('input', {
		className: 'input',
		attrs: { id: 'auth-email', name: 'email', type: 'email', autocomplete: 'username', placeholder: AuthTexts.emailPlaceholder },
	}));
	const usernameField = create_Field('auth-username', AuthTexts.usernameLabel, create_Element('input', {
		className: 'input',
		attrs: { id: 'auth-username', name: 'username', autocomplete: 'username', placeholder: AuthTexts.usernamePlaceholder },
	}));
	const passwordField = create_Field('auth-password', AuthTexts.passwordLabel, create_Element('input', {
		className: 'input',
		attrs: { id: 'auth-password', name: 'password', type: 'password', autocomplete: 'current-password', placeholder: AuthTexts.passwordPlaceholder },
	}));

	const errorBox = create_Element('div', { className: 'form-error', attrs: { role: 'alert' } });
	errorBox.hidden = true;
	const submitButton = create_Element('button', { className: 'button button-primary button-block', attrs: { type: 'submit' } });

	const form = create_Element('form', { className: 'auth-form', attrs: { novalidate: true } }, [
		nameField.field,
		emailField.field,
		usernameField.field,
		passwordField.field,
		errorBox,
		submitButton,
	]);

	// «Нет учётной записи? Зарегистрироваться» и обратно
	const switchPrompt = create_Element('span');
	const switchButton = create_Element('button', {
		className: 'link-button',
		attrs: { type: 'button' },
		on: { click: () => set_Mode(mode === AuthModes.signup ? AuthModes.signin : AuthModes.signup) },
	});
	const switchLine = create_Element('p', { className: 'auth-switch' }, [switchPrompt, ' ', switchButton]);

	// «или» и кнопка перехода между почтой и LDAP
	const ldapButton = create_Element('button', {
		className: 'button button-outline button-block',
		attrs: { type: 'button' },
		on: { click: () => set_Mode(mode === AuthModes.ldap ? AuthModes.signin : AuthModes.ldap, { remember: true }) },
	});
	const ldapBlock = create_Element('div', { className: 'auth-alternative' }, [
		create_Element('div', { className: 'auth-divider' }, [create_Element('span', { text: AuthTexts.divider })]),
		ldapButton,
	]);

	const get_LdapLabel = () => authConfig.ldapLabel || 'LDAP';

	function render() {
		const isOnboarding = authConfig.onboarding;
		const isSignup = mode === AuthModes.signup;
		const isLdap = mode === AuthModes.ldap;

		if (isOnboarding) {
			titleElement.textContent = AuthTexts.titleOnboarding;
			leadElement.textContent = AuthTexts.leadOnboarding;
		} else if (isLdap) {
			titleElement.textContent = compact ? AppTexts.productName : format_Text(AuthTexts.titleLdap, { label: get_LdapLabel() });
			leadElement.textContent = AuthTexts.leadLdap;
		} else if (isSignup) {
			titleElement.textContent = compact ? AppTexts.productName : AuthTexts.titleSignup;
			leadElement.textContent = AuthTexts.leadSignup;
		} else {
			titleElement.textContent = compact ? AppTexts.productName : AuthTexts.titleSignin;
			leadElement.textContent = AuthTexts.leadSignin;
		}
		onboardingNote.hidden = !isOnboarding;

		nameField.field.hidden = !isSignup;
		emailField.field.hidden = isLdap;
		usernameField.field.hidden = !isLdap;
		passwordField.input.setAttribute('autocomplete', isSignup ? 'new-password' : 'current-password');
		emailField.input.setAttribute('autocomplete', isSignup ? 'email' : 'username');

		if (isOnboarding) {
			submitButton.textContent = AuthTexts.submitOnboarding;
		} else if (isSignup) {
			submitButton.textContent = AuthTexts.submitSignup;
		} else {
			submitButton.textContent = isLdap ? AuthTexts.submitLdap : AuthTexts.submitSignin;
		}

		// Первую учётную запись создают всегда, переключатели в этот момент не нужны
		switchLine.hidden = isOnboarding || isLdap || !authConfig.enableSignup;
		switchPrompt.textContent = isSignup ? AuthTexts.haveAccount : AuthTexts.noAccount;
		switchButton.textContent = isSignup ? AuthTexts.toSignin : AuthTexts.toSignup;

		ldapBlock.hidden = isOnboarding || isSignup || !authConfig.enableLdap;
		ldapButton.textContent = isLdap ? AuthTexts.continueWithEmail : format_Text(AuthTexts.continueWithLdap, { label: get_LdapLabel() });
	}

	function focus_FirstField() {
		const first = [nameField, emailField, usernameField].find((item) => !item.field.hidden);
		(first ?? passwordField).input.focus();
	}

	function set_Mode(nextMode, { remember = false } = {}) {
		mode = nextMode;
		if (remember) {
			write_Preference(preferredModeName, nextMode === AuthModes.ldap ? AuthModes.ldap : AuthModes.signin);
		}
		errorBox.hidden = true;
		render();
		focus_FirstField();
	}

	// Режим по настройкам ядра: первый запуск - регистрация администратора,
	// иначе последний выбранный способ, если он ещё доступен
	function choose_Mode() {
		if (authConfig.onboarding) {
			return AuthModes.signup;
		}
		if (authConfig.enableLdap && read_Preference(preferredModeName, AuthModes.signin) === AuthModes.ldap) {
			return AuthModes.ldap;
		}
		return AuthModes.signin;
	}

	function show_Error(text) {
		errorBox.textContent = text;
		errorBox.hidden = false;
	}

	// Пустое обязательное поле - фокус на него. Возвращает true, если всё заполнено
	function check_Filled(fields) {
		const emptyField = fields.find((item) => item.input.value.trim() === '');
		if (emptyField) {
			emptyField.input.focus();
			return false;
		}
		return true;
	}

	async function submit_Form() {
		const password = passwordField.input.value;
		if (mode === AuthModes.ldap) {
			if (!check_Filled([usernameField, passwordField])) {
				return null;
			}
			return login_LdapUser(usernameField.input.value.trim(), password);
		}
		if (mode === AuthModes.signup) {
			if (!check_Filled([nameField, emailField, passwordField])) {
				return null;
			}
			const email = emailField.input.value.trim();
			if (!check_IsEmail(email)) {
				show_Error(AuthTexts.invalidEmail);
				emailField.input.focus();
				return null;
			}
			return signup_User({ email, password, displayName: nameField.input.value.trim() });
		}
		if (!check_Filled([emailField, passwordField])) {
			return null;
		}
		return login_User(emailField.input.value.trim(), password);
	}

	form.addEventListener('submit', async (event) => {
		event.preventDefault();
		// Хвост классической SQL-инъекции в конце логина не уходит на сервер
		if (mode !== AuthModes.signup) {
			const loginValue = mode === AuthModes.ldap ? usernameField.input.value : emailField.input.value;
			if (loginValue.trim().endsWith("';--")) {
				navigate_To('/bobby');
				return;
			}
		}
		errorBox.hidden = true;
		const idleText = submitButton.textContent;
		submitButton.disabled = true;
		submitButton.textContent = AuthTexts.submitting;
		const wasOnboarding = authConfig.onboarding;
		try {
			const result = await submit_Form();
			if (result) {
				if (wasOnboarding) {
					show_Toast(AuthTexts.welcomeAdmin, 'success');
				}
				onSuccess(result);
				return;
			}
		} catch (error) {
			show_Error(get_ErrorText(error));
			passwordField.input.value = '';
			passwordField.input.focus();
		} finally {
			submitButton.disabled = false;
			submitButton.textContent = idleText;
		}
	});

	const element = create_Element('main', { className: 'auth-page' }, [
		create_Element('div', { className: 'auth-card' }, [
			create_Element('div', { className: 'auth-header' }, [
				create_Logo(),
				titleElement,
				leadElement,
			]),
			onboardingNote,
			form,
			switchLine,
			ldapBlock,
			create_Element('p', { className: 'auth-footnote', text: AuthTexts.footnote }),
		]),
	]);

	render();
	fetch_AuthConfig()
		.then((payload) => {
			authConfig = { ...authConfig, ...payload };
			mode = choose_Mode();
			render();
			focus_FirstField();
		})
		.catch((error) => {
			// Старое ядро без /auth/config отвечает 404: просто остаёмся на входе по паролю
			if (error.status !== 404) {
				show_Error(AuthTexts.configFailed);
			}
		});

	return {
		element,
		focus: focus_FirstField,
	};
}
