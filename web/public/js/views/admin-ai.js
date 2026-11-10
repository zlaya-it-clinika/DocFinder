// Вкладка «Модели ИИ»: подключение к Ollama, выбор моделей, параметры ответа, скачивание моделей и индекс.
// Как разделы «Подключения» и «Модели» админ-панели open-webui. Все значения приходят из ядра.
import {
	cancel_ModelPull,
	check_IsNotSupported,
	delete_AIModel,
	fetch_AISettings,
	fetch_ModelPull,
	fetch_Reindex,
	list_AIModels,
	save_AISettings,
	start_ModelPull,
	start_Reindex,
	test_AIModel,
	verify_AIProvider,
} from '../api.js';
import { open_Confirm } from '../components/confirm.js';
import { show_Toast } from '../components/toast.js';
import { create_Button, create_Element, create_EmptyBlock, create_Icon, create_IconButton, create_LoadingBlock } from '../lib/dom.js';
import { format_DateTime, format_FileSize } from '../lib/format.js';
import { AITexts, AppTexts, format_Text, get_ErrorText } from '../texts.js';

// Как часто спрашивать ядро о ходе скачивания и переиндексации
const pullPollMs = 1000;
const reindexPollMs = 1500;

function create_Field(id, label, control, hint = '', wide = false) {
	return create_Element('div', { className: wide ? 'field field-wide' : 'field' }, [
		create_Element('label', { className: 'field-label', text: label, attrs: { for: id } }),
		control,
		hint ? create_Element('span', { className: 'field-hint', text: hint }) : null,
	]);
}

function create_Section(title, lead, children, tools = []) {
	return create_Element('section', { className: 'page-section' }, [
		create_Element('div', { className: 'section-header' }, [
			create_Element('h2', { className: 'section-title', text: title }),
			tools.length > 0 ? create_Element('div', { className: 'section-tools' }, tools) : null,
		]),
		lead ? create_Element('p', { className: 'section-lead', text: lead }) : null,
		...children,
	]);
}

function create_Progress() {
	const bar = create_Element('div', { className: 'progress-bar' });
	const element = create_Element('div', { className: 'progress', attrs: { role: 'progressbar', 'aria-valuemin': '0', 'aria-valuemax': '100' } }, [bar]);
	return {
		element,
		set: (fraction) => {
			const percent = Math.max(0, Math.min(100, Math.round((fraction ?? 0) * 100)));
			bar.style.width = `${percent}%`;
			element.setAttribute('aria-valuenow', String(percent));
		},
	};
}

// Пустое поле - null (значение из Modelfile). undefined - значение вне границ, которые прислало ядро
function read_Param(input, limit) {
	const text = input.value.trim().replace(',', '.');
	if (text === '') {
		return null;
	}
	const value = Number(text);
	if (!Number.isFinite(value) || value < limit.min || value > limit.max || (limit.integer && !Number.isInteger(value))) {
		return undefined;
	}
	return value;
}

export function create_AITab() {
	const timers = new Set();
	let destroyed = false;
	const content = create_Element('div', { className: 'page-sections' }, [create_LoadingBlock(AppTexts.loading)]);
	const element = create_Element('div', { className: 'page-sections' }, [content]);

	const schedule = (callback, delay) => {
		const timer = setTimeout(() => {
			timers.delete(timer);
			if (!destroyed) {
				callback();
			}
		}, delay);
		timers.add(timer);
	};

	const show_LoadError = (error) => {
		const notSupported = check_IsNotSupported(error);
		content.replaceChildren(create_EmptyBlock('alert', notSupported ? AITexts.notSupported : AITexts.loadFailed, [
			create_Element('div', { className: 'callout-hint', text: notSupported ? AITexts.notSupportedHint : get_ErrorText(error) }),
			create_Button(AppTexts.retry, { variant: 'outline', icon: 'refresh', onClick: load }),
		]));
	};

	async function load() {
		content.replaceChildren(create_LoadingBlock(AppTexts.loading));
		let settings;
		try {
			settings = await fetch_AISettings();
		} catch (error) {
			show_LoadError(error);
			return;
		}
		if (!destroyed) {
			build_Page(settings);
		}
	}

	function build_Page(settings) {
		let models = [];
		let modelsAvailable = true;
		const providerSelect = create_Element('select', { className: 'select', attrs: { id: 'ai-provider' } }, (settings.providers ?? ['ollama', 'openai']).map((provider) => create_Element('option', {
			text: provider === 'openai' ? AITexts.providerOpenAI : AITexts.providerOllama,
			attrs: { value: provider, selected: provider === (settings.provider ?? 'ollama') },
		})));
		const apiKeyInput = create_Element('input', { className: 'input', attrs: { id: 'ai-api-key', type: 'password', autocomplete: 'new-password', placeholder: settings.apiKeySet ? AITexts.apiKeySaved : AITexts.apiKeyPlaceholder } });

		// Подключение
		const urlInput = create_Element('input', { className: 'input', attrs: { id: 'ai-url', type: 'url', autocomplete: 'off', spellcheck: 'false' } });
		urlInput.value = settings.ollamaUrl ?? '';
		const statusDot = create_Element('span', { className: 'ai-status-dot' });
		const statusText = create_Element('span');
		const status = create_Element('span', { className: 'ai-status', attrs: { role: 'status' } }, [statusDot, statusText]);
		const set_Status = (state, text) => {
			statusDot.className = `ai-status-dot is-${state}`;
			statusText.textContent = text;
		};
		const verifyButton = create_Button(AITexts.verify, { variant: 'outline', icon: 'refresh' });
		const check_Connection = async () => {
			const url = urlInput.value.trim();
			set_Status('checking', AITexts.statusChecking);
			verifyButton.disabled = true;
			try {
				const result = await verify_AIProvider({ provider: providerSelect.value, ollamaUrl: url, apiKey: apiKeyInput.value.trim() });
				set_Status('online', format_Text(AITexts.statusOnline, { version: result.version }));
			} catch (error) {
				set_Status('offline', `${AITexts.statusOffline}. ${get_ErrorText(error)}`);
			} finally {
				verifyButton.disabled = false;
			}
		};
		verifyButton.addEventListener('click', check_Connection);
		const connectionSection = create_Section(AITexts.connectionTitle, AITexts.connectionLead, [
			create_Element('div', { className: 'form-grid' }, [
				create_Field('ai-provider', AITexts.fieldProvider, providerSelect, AITexts.fieldProviderHint),
				create_Field('ai-api-key', AITexts.fieldApiKey, apiKeyInput, AITexts.fieldApiKeyHint),
			]),
			create_Element('div', { className: 'field' }, [
				create_Element('label', { className: 'field-label', text: AITexts.fieldUrl, attrs: { for: 'ai-url' } }),
				create_Element('div', { className: 'ai-url-row' }, [urlInput, verifyButton]),
				create_Element('span', { className: 'field-hint', text: AITexts.fieldUrlHint }),
			]),
			status,
		]);

		// Выбор моделей
		const chatSelect = create_Element('select', { className: 'select', attrs: { id: 'ai-chat-model' } });
		const embedSelect = create_Element('select', { className: 'select', attrs: { id: 'ai-embed-model' } });
		const rerankInput = create_Element('input', { className: 'input', attrs: { id: 'ai-rerank-model', autocomplete: 'off', spellcheck: 'false', placeholder: AITexts.optional } });
		rerankInput.value = settings.rerankModel ?? '';
		let chosenChat = settings.chatModel ?? '';
		let chosenEmbed = settings.embedModel ?? '';
		// usedFlag - поле ядра usedForChat или usedForEmbed: имена с тегом и без сравнивает ядро.
		// Пока администратор не трогал список, выбрана модель из настроек
		const fill_Select = (select, chosen, usedFlag) => {
			const active = models.find((model) => model[usedFlag]);
			const selectedName = select.dataset.touched === 'true' ? chosen : (active?.name ?? chosen);
			const options = models.map((model) => create_Element('option', { text: model.name, attrs: { value: model.name, selected: model.name === selectedName } }));
			// Модель из настроек, которой нет в Ollama, тоже видна: её можно скачать или заменить
			if (selectedName && !models.some((model) => model.name === selectedName)) {
				options.unshift(create_Element('option', { text: format_Text(AITexts.notInstalled, { name: selectedName }), attrs: { value: selectedName, selected: true } }));
			}
			select.replaceChildren(...options);
			return selectedName;
		};
		chatSelect.addEventListener('change', () => {
			chosenChat = chatSelect.value;
			chatSelect.dataset.touched = 'true';
		});
		embedSelect.addEventListener('change', () => {
			chosenEmbed = embedSelect.value;
			embedSelect.dataset.touched = 'true';
		});
		const rerankField = create_Field('ai-rerank-model', AITexts.fieldRerankModel, rerankInput, AITexts.fieldRerankModelHint);
		const modelsSection = create_Section(AITexts.modelsTitle, AITexts.modelsLead, [
			create_Element('div', { className: 'form-grid' }, [
				create_Field('ai-chat-model', AITexts.fieldChatModel, chatSelect, AITexts.fieldChatModelHint),
				create_Field('ai-embed-model', AITexts.fieldEmbedModel, embedSelect, format_Text(AITexts.fieldEmbedModelHint, { dimensions: settings.embedDimensions })),
				rerankField,
			]),
		]);

		// Параметры ответа
		// Границы полей приходят из ядра в settings.limits
		const limits = settings.limits;
		const create_ParamInput = (id, limit, value) => {
			const input = create_Element('input', {
				className: 'input',
				attrs: { id, type: 'number', min: limit.min, max: limit.max, step: limit.integer ? 1 : 'any', inputmode: limit.integer ? 'numeric' : 'decimal', placeholder: AITexts.fromModel },
			});
			input.value = value ?? '';
			return input;
		};
		const params = {
			temperature: create_ParamInput('ai-temperature', limits.temperature, settings.temperature),
			topK: create_ParamInput('ai-top-k', limits.topK, settings.topK),
			topP: create_ParamInput('ai-top-p', limits.topP, settings.topP),
			numCtx: create_ParamInput('ai-num-ctx', limits.numCtx, settings.numCtx),
				maxTokens: create_ParamInput('ai-max-tokens', limits.maxTokens ?? { min: 64, max: 32768, integer: true }, settings.maxTokens),
		};
		const numCtxField = create_Field('ai-num-ctx', AITexts.fieldNumCtx, params.numCtx, format_Text(AITexts.fieldNumCtxHint, limits.numCtx));
		const topKField = create_Field('ai-top-k', AITexts.fieldTopK, params.topK, format_Text(AITexts.fieldTopKHint, limits.topK));
		const paramsSection = create_Section(AITexts.paramsTitle, AITexts.paramsLead, [
			create_Element('div', { className: 'form-grid' }, [
				create_Field('ai-temperature', AITexts.fieldTemperature, params.temperature, format_Text(AITexts.fieldTemperatureHint, limits.temperature)),
				numCtxField,
				topKField,
				create_Field('ai-top-p', AITexts.fieldTopP, params.topP, format_Text(AITexts.fieldTopPHint, limits.topP)),
				create_Field('ai-max-tokens', AITexts.fieldMaxTokens, params.maxTokens, AITexts.fieldMaxTokensHint),
			]),
		]);

		// Индекс базы знаний
		const reindexButton = create_Button(AITexts.reindex, { variant: 'outline', icon: 'refresh' });
		const reindexProgress = create_Progress();
		reindexProgress.element.hidden = true;
		const reindexText = create_Element('span', { className: 'field-hint' });
		const reindexNotice = create_Element('div', { className: 'callout callout-warning' }, [
			create_Icon('alert'),
			create_Element('div', { className: 'callout-content', text: AITexts.reindexRequired }),
		]);
		reindexNotice.hidden = true;
		const show_Reindex = (state) => {
			reindexProgress.element.hidden = !state.running;
			reindexButton.disabled = state.running;
			if (state.running) {
				reindexProgress.set(state.total > 0 ? state.done / state.total : 0);
				reindexText.textContent = format_Text(AITexts.reindexRunning, state);
			} else if (state.failed) {
				reindexText.textContent = format_Text(AITexts.reindexFailed, state);
			} else if (state.finishedAt) {
				reindexText.textContent = `${format_Text(AITexts.reindexDone, state)}, ${format_DateTime(state.finishedAt)}`;
				reindexNotice.hidden = true;
			} else {
				reindexText.textContent = AITexts.reindexIdle;
			}
		};
		const poll_Reindex = async () => {
			try {
				const state = await fetch_Reindex();
				show_Reindex(state);
				if (state.running) {
					schedule(poll_Reindex, reindexPollMs);
				}
			} catch (error) {
				reindexText.textContent = get_ErrorText(error);
			}
		};
		reindexButton.addEventListener('click', async () => {
			const confirmed = await open_Confirm({ title: AITexts.reindexConfirmTitle, text: AITexts.reindexConfirmText, confirmLabel: AITexts.reindex });
			if (!confirmed) {
				return;
			}
			try {
				show_Reindex(await start_Reindex());
				schedule(poll_Reindex, reindexPollMs);
			} catch (error) {
				show_Toast(get_ErrorText(error), 'error');
			}
		});
		const indexSection = create_Section(AITexts.indexTitle, AITexts.indexLead, [
			reindexNotice,
			create_Element('div', { className: 'ai-index-row' }, [reindexButton, reindexText]),
			reindexProgress.element,
		]);

		// Сохранение подключения, моделей и параметров
		const saveButton = create_Button(AITexts.save, { variant: 'primary', icon: 'check' });
		saveButton.addEventListener('click', async () => {
			const url = urlInput.value.trim();
			const provider = providerSelect.value;
			if (!chosenChat || !chosenEmbed) {
				show_Toast(AITexts.modelRequired, 'error');
				return;
			}
			const values = Object.fromEntries(Object.entries(params).map(([name, input]) => [name, read_Param(input, limits[name])]));
			const invalid = Object.entries(values).find(([, value]) => value === undefined);
			if (invalid) {
				show_Toast(AITexts.paramsInvalid, 'error');
				params[invalid[0]].focus();
				return;
			}
			saveButton.disabled = true;
			try {
				const saved = await save_AISettings({
					provider,
					ollamaUrl: url,
					apiKey: apiKeyInput.value.trim(),
					chatModel: chosenChat,
					embedModel: chosenEmbed,
					rerankModel: provider === 'openai' ? rerankInput.value.trim() : '',
					...values,
				});
				urlInput.value = saved.ollamaUrl;
				show_Toast(AITexts.saved, 'success');
				if (saved.reindexRequired) {
					reindexNotice.hidden = false;
				}
				settings = saved;
				chatSelect.dataset.touched = 'false';
				embedSelect.dataset.touched = 'false';
				refresh_Models();
				check_Connection();
			} catch (error) {
				show_Toast(get_ErrorText(error), 'error');
			} finally {
				saveButton.disabled = false;
			}
		});

		// Скачанные модели
		const modelsContent = create_Element('div', {}, [create_LoadingBlock(AppTexts.loading)]);
		const testResult = create_Element('div', { className: 'callout ai-test-result', attrs: { role: 'status' } });
		testResult.hidden = true;
		const show_TestResult = (text) => {
			testResult.replaceChildren(create_Icon('info'), create_Element('div', { className: 'callout-content', text }));
			testResult.hidden = false;
		};

		const run_Test = async (model, button) => {
			button.disabled = true;
			show_TestResult(format_Text(AITexts.testing, { name: model.name }));
			try {
				const result = await test_AIModel(model.name);
				const seconds = (result.durationMs / 1000).toFixed(1).replace('.', ',');
				show_TestResult(format_Text(AITexts.testResult, { name: model.name, seconds, reply: result.reply.trim() }));
			} catch (error) {
				show_TestResult(`${model.name}: ${get_ErrorText(error)}`);
			} finally {
				button.disabled = false;
			}
		};

		const remove_Model = async (model) => {
			const confirmed = await open_Confirm({
				title: AITexts.deleteTitle,
				text: format_Text(AITexts.deleteText, { name: model.name }),
				confirmLabel: AITexts.delete,
				danger: true,
			});
			if (!confirmed) {
				return;
			}
			try {
				await delete_AIModel(model.name);
				show_Toast(AITexts.deleted, 'success');
				refresh_Models();
			} catch (error) {
				show_Toast(get_ErrorText(error), 'error');
			}
		};

		const render_Models = () => {
			chosenChat = fill_Select(chatSelect, chosenChat, 'usedForChat');
			chosenEmbed = fill_Select(embedSelect, chosenEmbed, 'usedForEmbed');
			if (!modelsAvailable) {
				modelsContent.replaceChildren(create_EmptyBlock('alert', AITexts.modelsUnavailable));
				return;
			}
			if (models.length === 0) {
				modelsContent.replaceChildren(create_EmptyBlock('cpu', AITexts.modelsEmpty));
				return;
			}
			modelsContent.replaceChildren(create_Element('div', { className: 'table-wrap' }, [
				create_Element('table', { className: 'table' }, [
					create_Element('thead', {}, [create_Element('tr', {}, [
						create_Element('th', { text: AITexts.columnModel }),
						create_Element('th', { text: AITexts.columnDetails }),
						create_Element('th', { className: 'table-cell-number', text: AITexts.columnSize }),
						create_Element('th', { text: AITexts.columnModified }),
						create_Element('th', { className: 'table-cell-actions' }),
					])]),
					create_Element('tbody', {}, models.map((model) => {
						const isChat = model.usedForChat === true;
						const isEmbed = model.usedForEmbed === true;
						const details = [model.family, model.parameterSize, model.quantization].filter(Boolean).join(' · ');
						const testButton = create_IconButton('play', `${AITexts.test}: ${model.name}`, () => run_Test(model, testButton));
						return create_Element('tr', {}, [
							create_Element('td', {}, [
								create_Element('div', { className: 'ai-model-name' }, [
									create_Element('span', { className: 'cell-title', text: model.name }),
									isChat ? create_Element('span', { className: 'badge badge-strong', text: AITexts.badgeChat }) : null,
									isEmbed ? create_Element('span', { className: 'badge badge-strong', text: AITexts.badgeEmbed }) : null,
									model.loaded ? create_Element('span', { className: 'badge badge-outline', text: AITexts.badgeLoaded }) : null,
								]),
							]),
							create_Element('td', { className: 'table-cell-muted', text: details || AppTexts.notSpecified }),
							create_Element('td', { className: 'table-cell-number', text: format_FileSize(model.size) }),
							create_Element('td', { className: 'time-cell', text: format_DateTime(model.modifiedAt) || AppTexts.notSpecified }),
							create_Element('td', { className: 'table-cell-actions' }, [
								isEmbed ? null : testButton,
								isChat || isEmbed ? null : create_IconButton('trash', `${AITexts.delete}: ${model.name}`, () => remove_Model(model)),
							]),
						]);
					})),
				]),
			]));
		};

		async function refresh_Models() {
			try {
				const payload = await list_AIModels();
				models = Array.isArray(payload?.models) ? payload.models : [];
				modelsAvailable = true;
			} catch {
				models = [];
				modelsAvailable = false;
			}
			if (!destroyed) {
				render_Models();
			}
		}

		// Скачивание модели
		const pullInput = create_Element('input', { className: 'input', attrs: { id: 'ai-pull', autocomplete: 'off', spellcheck: 'false', placeholder: AITexts.pullPlaceholder } });
		const pullButton = create_Button(AITexts.pull, { variant: 'primary', icon: 'download' });
		const cancelButton = create_Button(AITexts.pullCancel, { variant: 'ghost', icon: 'close' });
		cancelButton.hidden = true;
		const pullProgress = create_Progress();
		const pullText = create_Element('span', { className: 'field-hint' });
		const pullState = create_Element('div', { className: 'ai-pull-state' }, [pullText, pullProgress.element]);
		pullState.hidden = true;
		let watchedPull = '';

		const show_Pull = (state) => {
			pullButton.disabled = state.running;
			pullInput.disabled = state.running;
			cancelButton.hidden = !state.running;
			pullState.hidden = !state.running;
			if (state.running) {
				const fraction = state.total > 0 ? state.completed / state.total : 0;
				pullProgress.set(fraction);
				const sizes = state.total > 0 ? ` · ${format_FileSize(state.completed)} / ${format_FileSize(state.total)}` : '';
				pullText.textContent = `${format_Text(AITexts.pullProgress, { name: state.model })}: ${state.status || ''}${sizes}`;
			}
		};
		const poll_Pull = async () => {
			try {
				const state = await fetch_ModelPull();
				show_Pull(state);
				if (state.running) {
					schedule(poll_Pull, pullPollMs);
					return;
				}
				if (watchedPull) {
					show_Toast(format_Text(state.failed ? AITexts.pullFailed : AITexts.pullDone, { name: state.model }), state.failed ? 'error' : 'success');
					watchedPull = '';
					if (!state.failed) {
						pullInput.value = '';
					}
					refresh_Models();
				}
			} catch (error) {
				show_Toast(get_ErrorText(error), 'error');
			}
		};
		pullButton.addEventListener('click', async () => {
			const name = pullInput.value.trim();
			if (name === '') {
				pullInput.focus();
				return;
			}
			try {
				const state = await start_ModelPull(name);
				watchedPull = name;
				show_Pull(state);
				schedule(poll_Pull, pullPollMs);
			} catch (error) {
				show_Toast(get_ErrorText(error), 'error');
			}
		});
		pullInput.addEventListener('keydown', (event) => {
			if (event.key === 'Enter') {
				event.preventDefault();
				pullButton.click();
			}
		});
		cancelButton.addEventListener('click', async () => {
			try {
				await cancel_ModelPull();
			} catch (error) {
				show_Toast(get_ErrorText(error), 'error');
			}
		});

		const installedSection = create_Section(AITexts.installedTitle, AITexts.installedLead, [
			create_Element('div', { className: 'field' }, [
				create_Element('label', { className: 'field-label', text: AITexts.pullLabel, attrs: { for: 'ai-pull' } }),
				create_Element('div', { className: 'ai-url-row' }, [pullInput, pullButton, cancelButton]),
			]),
			pullState,
			modelsContent,
			testResult,
		], [create_IconButton('refresh', AppTexts.retry, () => refresh_Models())]);

		const apply_Provider = () => {
			const openAI = providerSelect.value === 'openai';
			apiKeyInput.closest('.field').hidden = !openAI;
			rerankField.hidden = !openAI;
			numCtxField.hidden = openAI;
			topKField.hidden = openAI;
			installedSection.hidden = openAI;
		};
		providerSelect.addEventListener('change', () => {
			apply_Provider();
			set_Status('checking', AITexts.statusUnchecked);
			refresh_Models();
		});

		content.replaceChildren(
			connectionSection,
			modelsSection,
			paramsSection,
			create_Element('div', { className: 'settings-actions' }, [saveButton]),
			installedSection,
			indexSection,
		);

		fill_Select(chatSelect, chosenChat, 'usedForChat');
		fill_Select(embedSelect, chosenEmbed, 'usedForEmbed');
		apply_Provider();
		check_Connection();
		refresh_Models();
		poll_Reindex();
		// Скачивание могло начаться до открытия вкладки: продолжаем показывать его ход
		fetch_ModelPull().then((state) => {
			if (state.running && !destroyed) {
				watchedPull = state.model;
				show_Pull(state);
				schedule(poll_Pull, pullPollMs);
			}
		}).catch(() => {});
	}

	load();
	return {
		element,
		destroy: () => {
			destroyed = true;
			for (const timer of timers) {
				clearTimeout(timer);
			}
			timers.clear();
		},
	};
}
