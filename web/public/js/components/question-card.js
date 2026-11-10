// Карточка вопросов ассистента над полем ввода, как вопросы в Claude Code. Пока она открыта, обычное поле скрыто.
// Один вопрос (уточнение, выбор документа): вариант нажимается кнопкой или цифрой, свой ответ - Enter.
// Вопросы документа идут «в строку»: шаги по полям, назад и далее, пропуск необязательных,
// в конце «Проверьте ответы» и одна отправка всех ответов.
import { create_Element, create_Icon } from '../lib/dom.js';
import { ChatTexts, format_Text } from '../texts.js';

const maxInputHeight = 160;
// Пауза после выбора варианта перед следующим вопросом: видно, что выбор принят
const advanceDelayMs = 180;

// Виды карточки. single - один вопрос, ответ уходит сразу. steps - вопросы документа по шагам
export const QuestionModes = Object.freeze({
	single: 'single',
	steps: 'steps',
});

// question: { field, label, question, hint, optional, options }
function normalize_Question(question) {
	return {
		field: typeof question?.field === 'string' ? question.field : '',
		label: String(question?.label ?? '').trim(),
		question: String(question?.question ?? '').trim(),
		hint: String(question?.hint ?? '').trim(),
		optional: question?.optional === true,
		options: Array.isArray(question?.options) ? question.options.filter((option) => typeof option === 'string' && option.trim() !== '') : [],
	};
}

// onSubmit({ text, answers }) - ответы готовы: text - запись для ленты, answers - [{ field, label, value }].
// onSkip() - закрыть вопрос без ответа, onCancel() - остановить подготовку документа
export function create_QuestionCard({ onSubmit, onSkip, onCancel }) {
	let model = null;
	let questions = [];
	let answers = [];
	let step = 0;
	let busy = false;
	let advanceTimer = 0;

	const titleText = create_Element('span', { className: 'question-card-title' });
	const progressText = create_Element('span', { className: 'question-card-progress' });
	const closeButton = create_Element('button', { className: 'question-card-link', attrs: { type: 'button' } });
	const steps = create_Element('div', { className: 'question-steps', attrs: { role: 'tablist', 'aria-label': ChatTexts.questionStepsLabel } });
	const body = create_Element('div', { className: 'question-card-body' });
	const footer = create_Element('div', { className: 'question-card-footer' });
	const keysHint = create_Element('div', { className: 'question-card-keys' });
	const element = create_Element('section', { className: 'question-card', attrs: { 'aria-live': 'polite' } }, [
		create_Element('div', { className: 'question-card-header' }, [
			create_Icon('help', 'icon-sm'),
			titleText,
			progressText,
			closeButton,
		]),
		steps,
		body,
		footer,
		keysHint,
	]);
	element.hidden = true;

	const is_Steps = () => model?.mode === QuestionModes.steps;
	const is_Review = () => is_Steps() && step === questions.length;
	const has_Answer = (index) => answers[index].value.trim() !== '' || answers[index].skipped;
	const all_Answered = () => questions.every((question, index) => has_Answer(index) || question.optional);

	function create_Textarea(index) {
		const textarea = create_Element('textarea', {
			className: 'question-input',
			attrs: { rows: 1, placeholder: ChatTexts.questionPlaceholder, 'aria-label': ChatTexts.questionPlaceholder },
		});
		textarea.value = answers[index].option === null ? answers[index].value : '';
		const resize = () => {
			textarea.style.height = 'auto';
			textarea.style.height = `${Math.min(textarea.scrollHeight, maxInputHeight)}px`;
			textarea.style.overflowY = textarea.scrollHeight > maxInputHeight ? 'auto' : 'hidden';
		};
		textarea.addEventListener('input', () => {
			answers[index] = { value: textarea.value, option: null, skipped: false };
			resize();
			// Свой ответ снимает выбор варианта
			for (const button of body.querySelectorAll('.question-option')) {
				button.setAttribute('aria-pressed', 'false');
			}
			render_Footer();
		});
		textarea.addEventListener('keydown', (event) => {
			if (event.key === 'Enter' && !event.shiftKey && !event.isComposing) {
				event.preventDefault();
				go_Forward();
			}
		});
		requestAnimationFrame(resize);
		return textarea;
	}

	function create_Option(option, optionIndex, questionIndex) {
		const isChosen = answers[questionIndex].option === optionIndex;
		return create_Element('button', {
			className: 'question-option',
			attrs: { type: 'button', 'aria-pressed': isChosen ? 'true' : 'false', disabled: busy },
			on: { click: () => choose_Option(questionIndex, optionIndex) },
		}, [
			create_Element('span', { className: 'question-option-key', text: String(optionIndex + 1) }),
			create_Element('span', { className: 'question-option-text', text: option }),
			create_Icon('check', 'icon-sm question-option-check'),
		]);
	}

	function choose_Option(questionIndex, optionIndex) {
		if (busy) {
			return;
		}
		const option = questions[questionIndex].options[optionIndex];
		answers[questionIndex] = { value: option, option: optionIndex, skipped: false };
		if (!is_Steps()) {
			submit();
			return;
		}
		render();
		window.clearTimeout(advanceTimer);
		advanceTimer = window.setTimeout(() => go_To(questionIndex + 1), advanceDelayMs);
	}

	function render_Steps() {
		steps.hidden = !is_Steps();
		if (!is_Steps()) {
			steps.replaceChildren();
			return;
		}
		const items = questions.map((question, index) => {
			const done = has_Answer(index);
			return create_Element('button', {
				className: ['question-step', index === step ? 'is-current' : '', done ? 'is-done' : ''].filter(Boolean).join(' '),
				attrs: { type: 'button', role: 'tab', 'aria-selected': index === step ? 'true' : 'false', disabled: busy },
				on: { click: () => go_To(index) },
			}, [
				done ? create_Icon('check', 'icon-sm') : create_Element('span', { className: 'question-step-dot' }),
				create_Element('span', { text: question.label || format_Text(ChatTexts.questionNumber, { number: index + 1 }) }),
			]);
		});
		items.push(create_Element('button', {
			className: ['question-step', is_Review() ? 'is-current' : ''].filter(Boolean).join(' '),
			attrs: { type: 'button', role: 'tab', 'aria-selected': is_Review() ? 'true' : 'false', disabled: busy },
			on: { click: () => go_To(questions.length) },
		}, [create_Icon('list', 'icon-sm'), create_Element('span', { text: ChatTexts.questionReview })]));
		steps.replaceChildren(...items);
		// Длинная полоса шагов прокручивается к текущему
		steps.querySelector('.is-current')?.scrollIntoView({ block: 'nearest', inline: 'nearest' });
	}

	function render_Question() {
		const question = questions[step];
		const children = [
			create_Element('div', { className: 'question-text', text: question.question || question.label }),
			question.hint ? create_Element('div', { className: 'question-hint', text: question.hint }) : null,
		];
		if (question.options.length > 0) {
			children.push(create_Element('div', { className: 'question-options', attrs: { role: 'group', 'aria-label': ChatTexts.questionOptions } },
				question.options.map((option, index) => create_Option(option, index, step))));
		}
		const textarea = create_Textarea(step);
		textarea.disabled = busy;
		children.push(create_Element('div', { className: 'question-input-row' }, [textarea]));
		body.replaceChildren(...children.filter(Boolean));
	}

	function render_Review() {
		const rows = questions.map((question, index) => {
			const answer = answers[index];
			const missing = !has_Answer(index) && !question.optional;
			let valueText = answer.value.trim();
			if (answer.skipped || (valueText === '' && question.optional)) {
				valueText = ChatTexts.questionSkipped;
			}
			if (missing) {
				valueText = ChatTexts.questionMissing;
			}
			return create_Element('button', {
				className: ['question-review-row', missing ? 'is-missing' : ''].filter(Boolean).join(' '),
				attrs: { type: 'button', title: ChatTexts.questionEdit, disabled: busy },
				on: { click: () => go_To(index) },
			}, [
				create_Element('span', { className: 'question-review-label', text: question.label }),
				create_Element('span', { className: 'question-review-value', text: valueText }),
				create_Icon('edit', 'icon-sm'),
			]);
		});
		body.replaceChildren(
			create_Element('div', { className: 'question-text', text: ChatTexts.questionReviewTitle }),
			create_Element('div', { className: 'question-review' }, rows),
		);
	}

	function create_Button(label, variant, onClick, disabled = false) {
		return create_Element('button', {
			className: `button button-${variant}`,
			attrs: { type: 'button', disabled: busy || disabled },
			on: { click: onClick },
		}, [create_Element('span', { text: label })]);
	}

	function render_Footer() {
		const left = [];
		const right = [];
		if (is_Steps() && step > 0) {
			left.push(create_Button(ChatTexts.questionBack, 'ghost', () => go_To(step - 1)));
		}
		if (is_Review()) {
			right.push(create_Button(ChatTexts.questionSubmit, 'primary', () => submit(), !all_Answered()));
		} else {
			const question = questions[step];
			if (question?.optional) {
				right.push(create_Button(ChatTexts.questionSkip, 'ghost', () => skip_Question()));
			}
			const hasValue = answers[step]?.value.trim() !== '';
			if (is_Steps()) {
				right.push(create_Button(ChatTexts.questionNext, 'primary', () => go_Forward(), !hasValue && !question?.optional));
			} else {
				right.push(create_Button(ChatTexts.questionSend, 'primary', () => go_Forward(), !hasValue));
			}
		}
		footer.replaceChildren(
			create_Element('div', { className: 'question-card-actions' }, left),
			create_Element('div', { className: 'question-card-actions' }, right),
		);
		keysHint.textContent = build_KeysHint();
	}

	function build_KeysHint() {
		if (is_Review()) {
			return ChatTexts.questionKeysReview;
		}
		const withOptions = questions[step]?.options.length > 0;
		if (is_Steps()) {
			return withOptions ? ChatTexts.questionKeysStepsOptions : ChatTexts.questionKeysSteps;
		}
		const hint = withOptions ? ChatTexts.questionKeysSingleOptions : ChatTexts.questionKeysSingle;
		return model?.canSkip ? `${hint}, ${ChatTexts.questionKeysEsc}` : hint;
	}

	function render() {
		titleText.textContent = model?.title ?? '';
		if (is_Steps()) {
			progressText.textContent = is_Review()
				? ChatTexts.questionReview
				: format_Text(ChatTexts.questionProgress, { current: step + 1, total: questions.length });
		} else {
			progressText.textContent = '';
		}
		closeButton.hidden = !model?.canCancel && !model?.canSkip;
		closeButton.textContent = model?.canCancel ? ChatTexts.questionCancel : ChatTexts.questionClose;
		closeButton.disabled = busy;
		render_Steps();
		if (is_Review()) {
			render_Review();
		} else {
			render_Question();
		}
		render_Footer();
	}

	function focus_Current() {
		const target = body.querySelector('.question-option[aria-pressed="true"]')
			?? body.querySelector('.question-option')
			?? body.querySelector('.question-input')
			?? footer.querySelector('.button-primary');
		target?.focus();
	}

	// Фокус сразу после перерисовки: ответ, набранный сразу после Enter, попадает в новый вопрос
	function go_To(index) {
		window.clearTimeout(advanceTimer);
		step = Math.max(0, Math.min(index, is_Steps() ? questions.length : 0));
		render();
		focus_Current();
	}

	// Enter и «Далее»: один вопрос отправляется, шаги идут к следующему, с проверки - отправка
	function go_Forward() {
		if (busy) {
			return;
		}
		if (!is_Steps()) {
			if (answers[0].value.trim() !== '') {
				submit();
			}
			return;
		}
		if (is_Review()) {
			if (all_Answered()) {
				submit();
			}
			return;
		}
		if (answers[step].value.trim() === '' && !questions[step].optional) {
			return;
		}
		// Следующий вопрос без ответа, а если все отвечены - проверка
		const next = questions.findIndex((question, index) => index > step && !has_Answer(index));
		go_To(next === -1 ? questions.length : next);
	}

	function skip_Question() {
		answers[step] = { value: '', option: null, skipped: true };
		if (!is_Steps()) {
			submit();
			return;
		}
		go_Forward();
	}

	function submit() {
		if (busy) {
			return;
		}
		const lines = questions.map((question, index) => {
			const value = answers[index].skipped ? '' : answers[index].value.trim();
			return { field: question.field, label: question.label || question.question, value };
		});
		const text = is_Steps() || model?.withFields
			? lines.map((line) => `${line.label}: ${line.value || ChatTexts.questionSkipped}`).join('\n')
			: lines[0].value;
		onSubmit({ text, answers: model?.withFields ? lines : null });
	}

	closeButton.addEventListener('click', () => {
		if (busy) {
			return;
		}
		if (model?.canCancel) {
			onCancel?.();
		} else {
			onSkip?.();
		}
	});

	element.addEventListener('keydown', (event) => {
		if (event.key === 'Escape' && model?.canSkip && !busy) {
			event.preventDefault();
			onSkip?.();
			return;
		}
		const inInput = event.target instanceof HTMLTextAreaElement;
		// Цифра выбирает вариант, когда фокус не в поле: там цифры - часть ответа
		const number = Number(event.key);
		const options = questions[step]?.options ?? [];
		if (!inInput && !is_Review() && Number.isInteger(number) && number >= 1 && number <= options.length && !event.ctrlKey && !event.metaKey && !event.altKey) {
			event.preventDefault();
			choose_Option(step, number - 1);
		}
	});

	return {
		element,

		// next: { source, mode, title, questions, withFields, canSkip, canCancel }. source - реплика ядра:
		// пока карточка открыта для той же реплики, введённые ответы и шаг сохраняются
		open(next) {
			const isSame = model?.source === next.source;
			model = next;
			if (!isSame) {
				questions = next.questions.map(normalize_Question);
				answers = questions.map(() => ({ value: '', option: null, skipped: false }));
				step = 0;
			}
			element.hidden = false;
			render();
			if (!isSame) {
				element.classList.remove('is-entering');
				// Перезапуск анимации появления для нового вопроса
				void element.offsetWidth;
				element.classList.add('is-entering');
				requestAnimationFrame(focus_Current);
			}
		},

		close() {
			window.clearTimeout(advanceTimer);
			model = null;
			element.hidden = true;
		},

		is_Open: () => !element.hidden,

		set_Busy(value) {
			busy = value;
			if (model) {
				render();
			}
		},

		focus: focus_Current,
	};
}
