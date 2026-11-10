// Экран чата: история сессии, отправка вопросов, проверка черновика.
// Один и тот же экран работает на полной странице и в виджете (compact).
import { check_Draft, create_ChatSession, fetch_ChatSession, fix_DraftFormat, send_ChatMessage, upload_SessionAttachment } from '../api.js';
import { open_FormatFixDialog } from '../components/format-fix-dialog.js';
import { show_Toast } from '../components/toast.js';
import { create_ModeSwitch } from '../components/mode-switch.js';
import { create_ProcessPicker } from '../components/process-picker.js';
import { create_Composer } from '../components/composer.js';
import { MessageKinds, render_Message, render_PendingMessage, render_QuestionAnswers } from '../components/message.js';
import { QuestionModes, create_QuestionCard } from '../components/question-card.js';
import { create_Button, create_Element, create_EmptyBlock, create_Icon, create_LoadingBlock } from '../lib/dom.js';
import { build_ChatTitle } from '../lib/format.js';
import { Permissions, has_Permission } from '../roles.js';
import { AppTexts, ChatTexts, get_ErrorText } from '../texts.js';

function render_Greeting(user) {
	const name = user?.displayName ? `, ${user.displayName}` : '';
	return create_Element('div', { className: 'chat-greeting' }, [
		create_Element('h1', { className: 'chat-greeting-title', text: `${ChatTexts.greetingPrefix}${name}` }),
		create_Element('p', { className: 'chat-greeting-lead', text: ChatTexts.greetingLead }),
		create_Element('div', { className: 'chat-hints' }, [
			create_Element('span', { className: 'chat-hint' }, [create_Icon('quote', 'icon-sm'), ChatTexts.hintSources]),
			create_Element('span', { className: 'chat-hint' }, [create_Icon('file', 'icon-sm'), ChatTexts.hintTemplates]),
			create_Element('span', { className: 'chat-hint' }, [create_Icon('file-check', 'icon-sm'), ChatTexts.hintDrafts]),
		]),
	]);
}

// Вопросы ассистента, на которые отвечают в карточке над полем ввода
const questionKinds = new Set([MessageKinds.clarify, MessageKinds.templateChoice, MessageKinds.templateField]);
// Насколько близко к низу ленты должен быть пользователь, чтобы лента шла за проявляющимся ответом
const followDistancePx = 120;

// onSessionCreated(sessionId) - ядро выдало id новой сессии, можно обновить адрес
export function create_ChatView({ sessionId = null, user, config, history, compact = false, onSessionCreated }) {
	let currentSessionId = sessionId;
	let messages = [];
	let pending = false;
	let loading = Boolean(sessionId);
	let destroyed = false;
	let dragDepth = 0;
	// Уточнения, которые пользователь пропустил: они остаются в ленте обычной репликой
	const skippedQuestions = new WeakSet();
	let openQuestion = null;

	const legacyDOU = config.legacyDouFeatures === true;
	const allowCheck = legacyDOU && has_Permission(user, config, Permissions.draftsCheck);
	const allowAssist = legacyDOU && has_Permission(user, config, Permissions.draftsAssist);
	const canChat = has_Permission(user, config, Permissions.chatUse);

	const messagesList = create_Element('div', { className: 'chat-messages', attrs: { role: 'log', 'aria-live': 'polite' } });
	const greeting = render_Greeting(user);
	const column = create_Element('div', { className: 'chat-column' }, [greeting, messagesList]);
	const scroll = create_Element('div', { className: 'chat-scroll' }, [column]);

	// Категория знаний для поиска. Для открытого чата её потом подставит история сессии
	const processPicker = create_ProcessPicker({ user });
	// Режим чата: «Чтение» или «План». Для открытого чата его подставит история сессии
	const modeSwitch = create_ModeSwitch({ user, config, onChange: () => update_FieldPrompt() });
	modeSwitch.element.hidden = !legacyDOU;
	const composer = create_Composer({
		onSubmit: (payload) => handle_Submit(payload),
		allowCheck,
		allowAssist,
		config,
		tools: legacyDOU ? [modeSwitch.element, processPicker.element] : [processPicker.element],
	});
	// Вопросы ассистента: уточнение, выбор документа, поля документа. Ответ уходит обычной репликой,
	// ответы на поля документа - ещё и списком answers
	const questionCard = create_QuestionCard({
		onSubmit: ({ text, answers }) => send_Text(text, null, answers),
		onSkip: () => {
			if (openQuestion) {
				skippedQuestions.add(openQuestion);
			}
			render_View();
			composer.focus();
		},
		onCancel: () => send_Text(ChatTexts.questionCancelText),
	});
	const quickActions = create_Element('div', { className: 'quick-actions' });
	quickActions.hidden = true;
	const footer = create_Element('div', { className: 'chat-footer' }, [
		quickActions,
		canChat ? questionCard.element : null,
		canChat ? composer.element : create_Element('div', { className: 'callout-hint', text: ChatTexts.chatDenied }),
		create_Element('div', { className: 'chat-disclaimer', text: ChatTexts.disclaimer }),
	]);
	const dropOverlay = create_Element('div', { className: 'drop-overlay' }, [
		create_Icon('upload', 'icon-lg'),
		create_Element('div', { className: 'drop-overlay-title', text: ChatTexts.dropTitle }),
		create_Element('div', { text: ChatTexts.dropLead }),
	]);
	dropOverlay.hidden = true;

	const view = create_Element('div', { className: compact ? 'chat-view is-compact' : 'chat-view' }, [scroll, footer, dropOverlay]);

	const handlers = {
		onRepeat: (text) => send_Text(text),
		onRevealProgress: () => follow_Bottom(),
		onDraftContinue: () => composer.focus(),
		onFixFormat: (message) => run_FormatFix(message),
		onRetry: (message) => {
			if (message.retryFile && message.attachMode === 'assist') {
				run_DraftAssist(message.retryFile, message.text, message);
				return;
			}
			if (message.kind === MessageKinds.draftRequest && message.retryFile) {
				run_DraftCheck(message.retryFile, message);
				return;
			}
			send_Text(message.text, message);
		},
	};

	const quickPrompts = [
		[ChatTexts.quickExpand, ChatTexts.quickExpandText],
		[ChatTexts.quickShorten, ChatTexts.quickShortenText],
		[ChatTexts.quickFormat, ChatTexts.quickFormatText],
	];
	for (const [label, text] of quickPrompts) {
		quickActions.append(create_Button(label, {
			variant: 'outline',
			onClick: () => composer.set_Text(text),
		}));
	}

	function scroll_ToBottom() {
		requestAnimationFrame(() => {
			scroll.scrollTop = scroll.scrollHeight;
		});
	}

	// Пока ответ проявляется, лента держится внизу, если пользователь сам не прокрутил её выше
	function follow_Bottom() {
		const distance = scroll.scrollHeight - scroll.scrollTop - scroll.clientHeight;
		if (distance < followDistancePx) {
			scroll.scrollTop = scroll.scrollHeight;
		}
	}

	function find_FirstQuestion() {
		return messages.find((message) => message.role === 'user' && message.text)?.text ?? '';
	}

	// Подсказка пустого поля ввода зависит от режима. Вопросы документа задаёт карточка, а не поле ввода
	function update_FieldPrompt() {
		composer.set_IdlePlaceholder(modeSwitch.is_Plan() ? ChatTexts.placeholderPlan : ChatTexts.placeholder);
		composer.set_FieldPrompt(null);
	}

	function is_QuestionMessage(message) {
		return message?.role === 'assistant' && questionKinds.has(message.kind);
	}

	// Вопросы документа списком: новое ядро отдаёт template.questions, старое - одно поле в тексте реплики
	function read_TemplateQuestions(message) {
		const template = message.template ?? {};
		if (Array.isArray(template.questions) && template.questions.length > 0) {
			return template.questions;
		}
		return [{ field: template.field ?? '', label: template.fieldLabel ?? '', question: message.text ?? '' }];
	}

	// Вопрос выбора документа - первая строка текста, дальше в тексте тот же список, что в options
	function read_ChoiceQuestion(message) {
		return String(message.text ?? '').split('\n')[0];
	}

	// Последняя реплика - вопрос ассистента без ответа: он открывается карточкой над полем ввода.
	// Вопросы документа живут только в «Плане»: в «Чтении» начатый документ ядро забывает
	function find_OpenQuestion() {
		const lastMessage = messages[messages.length - 1];
		if (pending || !canChat || !is_QuestionMessage(lastMessage) || skippedQuestions.has(lastMessage)) {
			return null;
		}
		if (lastMessage.kind !== MessageKinds.clarify && !modeSwitch.is_Plan()) {
			return null;
		}
		return lastMessage;
	}

	function build_QuestionModel(message) {
		if (message.kind === MessageKinds.clarify) {
			return {
				source: message,
				mode: QuestionModes.single,
				title: ChatTexts.kindClarify,
				questions: [{ question: message.text, options: message.options }],
				canSkip: true,
			};
		}
		if (message.kind === MessageKinds.templateChoice) {
			return {
				source: message,
				mode: QuestionModes.single,
				title: ChatTexts.kindTemplateChoice,
				questions: [{ question: read_ChoiceQuestion(message), options: message.options }],
				canCancel: true,
			};
		}
		const questions = read_TemplateQuestions(message);
		return {
			source: message,
			mode: questions.length > 1 ? QuestionModes.steps : QuestionModes.single,
			title: message.template?.title ?? ChatTexts.kindTemplateField,
			questions,
			withFields: questions.every((question) => Boolean(question.field)),
			canCancel: true,
		};
	}

	// Ответ на вопрос: следующая реплика пользователя, дошедшая до ядра. На вопросы документа из нового ядра
	// ответ - список answers, остальное - обычный текст без файла
	function find_Answer(question, next) {
		if (next?.role !== 'user' || next.failed || skippedQuestions.has(question)) {
			return null;
		}
		if (question.kind === MessageKinds.templateField && Array.isArray(question.template?.questions) && question.template.questions.length > 0) {
			return Array.isArray(next.answers) && next.answers.length > 0 ? next : null;
		}
		if (!next.text || next.attachMode || next.kind === MessageKinds.draftRequest) {
			return null;
		}
		return next;
	}

	function build_AnswerRows(question, answer) {
		if (question.kind === MessageKinds.clarify) {
			return [{ label: question.text, value: answer?.text ?? '' }];
		}
		if (question.kind === MessageKinds.templateChoice) {
			return [{ label: read_ChoiceQuestion(question), value: answer?.text ?? '' }];
		}
		if (Array.isArray(answer?.answers)) {
			return answer.answers.map((line) => ({ label: line.label, value: line.value }));
		}
		return read_TemplateQuestions(question).map((item, index) => ({
			label: item.label || item.question,
			value: index === 0 ? (answer?.text ?? '') : '',
		}));
	}

	function build_QuestionTitle(question) {
		if (question.kind === MessageKinds.clarify) {
			return ChatTexts.kindClarify;
		}
		if (question.kind === MessageKinds.templateChoice) {
			return ChatTexts.kindTemplateChoice;
		}
		return question.template?.title ?? ChatTexts.kindTemplateField;
	}

	// Вопрос ассистента и ответ пользователя - один блок ленты «вопрос -> ответ».
	// Вопрос без ответа (отмена, смена режима) - тот же блок с пометкой. Пропущенное уточнение - обычная реплика
	function build_Timeline(openMessage) {
		const items = [];
		for (let index = 0; index < messages.length; index += 1) {
			const message = messages[index];
			if (message === openMessage) {
				break;
			}
			if (is_QuestionMessage(message) && !skippedQuestions.has(message)) {
				const answer = find_Answer(message, messages[index + 1]);
				// Брошенный документ - только заголовок: перечень пустых полей ничего не говорит
				const rows = answer || message.kind !== MessageKinds.templateField ? build_AnswerRows(message, answer) : [];
				items.push(render_QuestionAnswers({ title: build_QuestionTitle(message), rows, unanswered: !answer }));
				if (answer) {
					index += 1;
				}
				continue;
			}
			items.push(render_Message(message, handlers));
		}
		return items;
	}

	function update_QuestionCard(openMessage) {
		openQuestion = openMessage;
		composer.element.hidden = Boolean(openMessage);
		if (!openMessage) {
			questionCard.close();
			return;
		}
		questionCard.open(build_QuestionModel(openMessage));
	}

	function render_View() {
		if (destroyed) {
			return;
		}
		const isEmpty = messages.length === 0 && !pending && !loading;
		view.classList.toggle('is-empty', isEmpty);
		greeting.hidden = !isEmpty;
		if (loading) {
			messagesList.replaceChildren(create_LoadingBlock(ChatTexts.thinking));
			update_QuestionCard(null);
			return;
		}
		const openMessage = find_OpenQuestion();
		messagesList.replaceChildren(
			...build_Timeline(openMessage),
			...(pending ? [render_PendingMessage()] : []),
		);
		update_QuestionCard(openMessage);
		update_FieldPrompt();
		quickActions.hidden = !messages.some((message) => message.kind === MessageKinds.draftRevision);
	}

	function render_LoadError(error) {
		loading = false;
		view.classList.remove('is-empty');
		greeting.hidden = true;
		if (error.status === 404) {
			history?.forget_Chat(currentSessionId);
			messagesList.replaceChildren(create_EmptyBlock('help', ChatTexts.notFoundTitle, [
				create_Element('div', { className: 'callout-hint', text: ChatTexts.notFoundLead }),
				create_Element('a', { className: 'button button-primary', attrs: { href: '/', 'data-link': true }, text: ChatTexts.startNew }),
			]));
			footer.hidden = true;
			return;
		}
		messagesList.replaceChildren(create_EmptyBlock('alert', ChatTexts.loadFailed, [
			create_Element('div', { className: 'callout-hint', text: get_ErrorText(error) }),
			create_Button(AppTexts.retry, { variant: 'outline', icon: 'refresh', onClick: () => load_Session() }),
		]));
	}

	async function load_Session() {
		loading = true;
		footer.hidden = false;
		render_View();
		try {
			const payload = await fetch_ChatSession(currentSessionId);
			if (destroyed) {
				return;
			}
			messages = Array.isArray(payload?.messages) ? payload.messages : [];
			if (Array.isArray(payload?.processIds)) {
				processPicker.set_ProcessIds(payload.processIds);
			} else if (typeof payload?.processId === 'string') {
				processPicker.set_ProcessIds(payload.processId);
			}
			if (typeof payload?.mode === 'string') {
				modeSwitch.set_Mode(payload.mode);
			}
			loading = false;
			render_View();
			scroll_ToBottom();
		} catch (error) {
			if (!destroyed) {
				render_LoadError(error);
			}
		}
	}

	async function ensure_Session(firstText) {
		if (currentSessionId) {
			return currentSessionId;
		}
		const created = await create_ChatSession();
		currentSessionId = created.sessionId;
		history?.remember_Chat(currentSessionId, build_ChatTitle(firstText));
		onSessionCreated?.(currentSessionId);
		return currentSessionId;
	}

	// answers - ответы на поля документа из карточки: [{ field, label, value }]
	async function send_Text(text, existingMessage = null, answers = null) {
		const userMessage = existingMessage ?? { role: 'user', text, ...(answers ? { answers } : {}) };
		if (existingMessage) {
			existingMessage.failed = false;
		} else {
			messages.push(userMessage);
		}
		pending = true;
		composer.set_Busy(true);
		questionCard.set_Busy(true);
		render_View();
		scroll_ToBottom();
		try {
			const activeSessionId = await ensure_Session(text);
			const reply = await send_ChatMessage(activeSessionId, text, {
				processIds: processPicker.get_ProcessIds(),
				mode: modeSwitch.get_Mode(),
				answers: userMessage.answers?.map(({ field, value }) => ({ field, value })),
			});
			messages.push({ ...reply, role: 'assistant', reveal: true });
			history?.remember_Chat(activeSessionId, build_ChatTitle(find_FirstQuestion()));
		} catch (error) {
			userMessage.failed = true;
			userMessage.errorText = get_ErrorText(error);
		} finally {
			pending = false;
			composer.set_Busy(false);
			questionCard.set_Busy(false);
			render_View();
			scroll_ToBottom();
		}
	}

	async function run_DraftCheck(file, existingMessage = null) {
		const requestMessage = existingMessage ?? {
			role: 'user',
			kind: MessageKinds.draftRequest,
			fileName: file.name,
			fileSize: file.size,
			retryFile: file,
		};
		if (existingMessage) {
			existingMessage.failed = false;
		} else {
			messages.push(requestMessage);
		}
		pending = true;
		composer.set_Busy(true);
		render_View();
		scroll_ToBottom();
		try {
			const payload = await check_Draft(file);
			messages.push({
				role: 'assistant',
				kind: MessageKinds.draftCheck,
				fileName: file.name,
				recommendations: Array.isArray(payload?.recommendations) ? payload.recommendations : [],
				// false - файл не .docx, оформление не проверялось
				formatChecked: payload?.formatChecked !== false,
				// Файл остается только в этом окне: по нему исправляется оформление
				sourceFile: file,
				// Каких реквизитов нет и кого предложить: вопросы перед исправлением
				requisites: payload?.requisites ?? null,
				reveal: true,
			});
		} catch (error) {
			requestMessage.failed = true;
			requestMessage.errorText = get_ErrorText(error);
		} finally {
			pending = false;
			composer.set_Busy(false);
			render_View();
			scroll_ToBottom();
		}
	}

	// Исправление оформления только после подтверждения сотрудника. Исправленный файл - новая карточка со ссылкой
	async function run_FormatFix(checkMessage) {
		const file = checkMessage?.sourceFile;
		if (!file || pending) {
			return;
		}
		const answers = await open_FormatFixDialog(checkMessage.requisites);
		if (!answers) {
			return;
		}
		checkMessage.fixRequested = true;
		pending = true;
		composer.set_Busy(true);
		render_View();
		scroll_ToBottom();
		try {
			const payload = await fix_DraftFormat(file, answers);
			messages.push({
				role: 'assistant',
				kind: MessageKinds.draftFix,
				fileName: payload?.fileName ?? file.name,
				draftLink: payload?.draftLink,
				applied: Array.isArray(payload?.applied) ? payload.applied : [],
				recommendations: Array.isArray(payload?.recommendations) ? payload.recommendations : [],
				reveal: true,
			});
		} catch (error) {
			checkMessage.fixRequested = false;
			show_Toast(get_ErrorText(error), 'error');
		} finally {
			pending = false;
			composer.set_Busy(false);
			render_View();
			scroll_ToBottom();
		}
	}

	async function run_DraftAssist(file, text, existingMessage = null) {
		const question = text.trim() || ChatTexts.draftAssistDefault;
		const requestMessage = existingMessage ?? {
			role: 'user',
			text: question,
			attachMode: 'assist',
			retryFile: file,
			attachments: [{ fileName: file.name }],
		};
		if (existingMessage) {
			existingMessage.failed = false;
		} else {
			messages.push(requestMessage);
		}
		pending = true;
		composer.set_Busy(true);
		render_View();
		scroll_ToBottom();
		try {
			const activeSessionId = await ensure_Session(question);
			const uploaded = await upload_SessionAttachment(activeSessionId, file);
			requestMessage.attachments = [{ attachmentId: uploaded.attachmentId, fileName: uploaded.fileName }];
			const reply = await send_ChatMessage(activeSessionId, question, {
				processIds: processPicker.get_ProcessIds(),
				mode: modeSwitch.get_Mode(),
				attachmentIds: [uploaded.attachmentId],
			});
			messages.push({ ...reply, role: 'assistant', reveal: true });
			history?.remember_Chat(activeSessionId, build_ChatTitle(find_FirstQuestion()));
		} catch (error) {
			requestMessage.failed = true;
			requestMessage.errorText = get_ErrorText(error);
		} finally {
			pending = false;
			composer.set_Busy(false);
			render_View();
			scroll_ToBottom();
		}
	}

	// Проверка файла идёт отдельно. Помощь с черновиком отправляет файл и текст одним шагом.
	async function handle_Submit({ text, file, attachMode }) {
		if (file && attachMode === 'assist') {
			await run_DraftAssist(file, text);
			return;
		}
		if (file) {
			await run_DraftCheck(file);
		}
		if (text) {
			await send_Text(text);
		}
	}

	// Перетаскивание файла в окно чата
	const has_Files = (event) => Array.from(event.dataTransfer?.types ?? []).includes('Files');
	view.addEventListener('dragenter', (event) => {
		if (!has_Files(event)) {
			return;
		}
		event.preventDefault();
		dragDepth += 1;
		dropOverlay.hidden = false;
	});
	view.addEventListener('dragover', (event) => {
		if (has_Files(event)) {
			event.preventDefault();
		}
	});
	view.addEventListener('dragleave', () => {
		dragDepth = Math.max(0, dragDepth - 1);
		if (dragDepth === 0) {
			dropOverlay.hidden = true;
		}
	});
	view.addEventListener('drop', (event) => {
		if (!has_Files(event)) {
			return;
		}
		event.preventDefault();
		dragDepth = 0;
		dropOverlay.hidden = true;
		const [file] = event.dataTransfer.files;
		if (file) {
			composer.attach_File(file);
		}
	});

	render_View();
	if (currentSessionId) {
		load_Session();
	}

	return {
		element: view,
		get_SessionId: () => currentSessionId,
		focus: () => (questionCard.is_Open() ? questionCard.focus() : composer.focus()),
		destroy() {
			destroyed = true;
		},
	};
}
