// Вкладка «База знаний»: конструктор категорий знаний (процессы ядра), общие документы по категориям
// и личные документы всех сотрудников.
import { delete_Process, list_Documents } from '../api.js';
import { open_Confirm } from '../components/confirm.js';
import { open_DocumentUploadForm } from '../components/document-upload.js';
import { create_DocumentTable } from '../components/document-table.js';
import { show_Toast } from '../components/toast.js';
import { Scopes, check_IsPersonal, filter_ByQuery, read_DocumentList, read_DocumentPermissions } from '../documents.js';
import { create_Button, create_Element, create_EmptyBlock, create_Icon, create_IconButton, create_LoadingBlock, create_SearchBox } from '../lib/dom.js';
import { DefaultProcessId, create_ProcessDot, open_ProcessForm, try_LoadProcesses } from '../processes.js';
import { AdminTexts, AppTexts, DocumentsTexts, ProcessTexts, format_Text, get_ErrorText } from '../texts.js';

function create_Notice(text, hint) {
	return create_Element('div', { className: 'callout' }, [
		create_Icon('info'),
		create_Element('div', { className: 'callout-content' }, [
			create_Element('div', { text }),
			hint ? create_Element('div', { className: 'callout-hint', text: hint }) : null,
		]),
	]);
}

export function create_KnowledgeTab({ user, config }) {
	let documents = [];
	let processesState = { supported: false, canManage: false, processes: [] };
	// Фильтр документов по категории. Пустая строка - все документы
	let processFilter = '';
	let permissions = read_DocumentPermissions(null, user, config);
	let personalAvailable = false;
	let query = '';

	const processesCount = create_Element('span', { className: 'section-count' });
	const globalCount = create_Element('span', { className: 'section-count' });
	const personalCount = create_Element('span', { className: 'section-count' });
	const processesContent = create_Element('div', {}, [create_LoadingBlock(AppTexts.loading)]);
	const filterRow = create_Element('div', { className: 'filter-chips', attrs: { role: 'group', 'aria-label': DocumentsTexts.processFilter } });
	const globalContent = create_Element('div', {}, [create_LoadingBlock(AppTexts.loading)]);
	const personalContent = create_Element('div', {}, [create_LoadingBlock(AppTexts.loading)]);

	const get_Processes = () => processesState.processes;
	const get_ProcessTitles = () => Object.fromEntries(get_Processes().map((process) => [process.processId, process.title]));

	const open_Upload = (document = null) => open_DocumentUploadForm({
		config,
		scope: Scopes.global,
		document,
		onUploaded: reload,
		presetProcessId: processFilter,
	});

	const createButton = create_Button(ProcessTexts.create, {
		variant: 'outline',
		icon: 'plus',
		onClick: () => open_ProcessForm({ onSaved: reload }),
	});
	const uploadButton = create_Button(DocumentsTexts.uploadGlobal, { variant: 'primary', icon: 'upload', onClick: () => open_Upload() });

	const confirm_DeleteProcess = async (process) => {
		const confirmed = await open_Confirm({
			title: ProcessTexts.deleteTitle,
			text: format_Text(ProcessTexts.deleteText, {
				title: process.title,
				fallback: get_Processes().find((item) => item.processId === DefaultProcessId)?.title ?? ProcessTexts.defaultBadge,
			}),
			confirmLabel: ProcessTexts.delete,
			danger: true,
		});
		if (!confirmed) {
			return;
		}
		try {
			await delete_Process(process.processId);
			if (processFilter === process.processId) {
				processFilter = '';
			}
			show_Toast(ProcessTexts.deleted, 'success');
			reload();
		} catch (error) {
			show_Toast(get_ErrorText(error), 'error');
		}
	};

	const create_ProcessCard = (process) => {
		const isActive = processFilter === process.processId;
		const isDefault = process.processId === DefaultProcessId;
		const card = create_Element('div', {
			className: isActive ? 'topic-card is-active' : 'topic-card',
			attrs: { role: 'button', tabindex: 0, 'aria-pressed': isActive ? 'true' : 'false', title: process.description || process.title },
		}, [
			create_Element('div', { className: 'topic-card-head' }, [
				create_ProcessDot(process.color),
				create_Element('span', { className: 'topic-card-title', text: process.title }),
			]),
			process.description ? create_Element('p', { className: 'topic-card-text', text: process.description }) : null,
			create_Element('div', { className: 'topic-card-foot' }, [
				create_Element('span', { className: 'topic-card-meta' }, [
					create_Element('span', { className: 'cell-subtitle', text: format_Text(ProcessTexts.documentsCount, { count: process.documentCount ?? 0 }) }),
					isDefault ? create_Element('span', { className: 'badge', text: ProcessTexts.defaultBadge }) : null,
				]),
				processesState.canManage
					? create_Element('span', { className: 'topic-card-actions' }, [
						create_IconButton('edit', `${ProcessTexts.edit}: ${process.title}`, (event) => {
							event.stopPropagation();
							open_ProcessForm({ process, onSaved: reload });
						}, 'icon-button icon-button-sm'),
						isDefault ? null : create_IconButton('trash', `${ProcessTexts.delete}: ${process.title}`, (event) => {
							event.stopPropagation();
							confirm_DeleteProcess(process);
						}, 'icon-button icon-button-sm'),
					])
					: null,
			]),
		]);
		const select = () => {
			processFilter = isActive ? '' : process.processId;
			render_Sections();
		};
		card.addEventListener('click', select);
		card.addEventListener('keydown', (event) => {
			if (event.key === 'Enter' || event.key === ' ') {
				event.preventDefault();
				select();
			}
		});
		return card;
	};

	const create_FilterChip = (value, label, color = null) => create_Element('button', {
		className: processFilter === value ? 'filter-chip is-active' : 'filter-chip',
		attrs: { type: 'button', 'aria-pressed': processFilter === value ? 'true' : 'false' },
		on: {
			click: () => {
				processFilter = value;
				render_Sections();
			},
		},
	}, [color ? create_ProcessDot(color) : null, create_Element('span', { text: label })]);

	function render_Processes() {
		processesCount.textContent = processesState.supported ? String(get_Processes().length) : '';
		createButton.hidden = !processesState.canManage;
		processesContent.replaceChildren(get_Processes().length === 0
			? create_Notice(ProcessTexts.sectionLead)
			: create_Element('div', { className: 'topic-grid' }, get_Processes().map(create_ProcessCard)));
		filterRow.hidden = get_Processes().length < 2;
		filterRow.replaceChildren(
			create_FilterChip('', ProcessTexts.allDocuments),
			...get_Processes().map((process) => create_FilterChip(process.processId, process.title, process.color)),
		);
	}

	function render_Sections() {
		render_Processes();
		const searchFields = ['title', 'ownerName', 'ownerId'];
		const scoped = processFilter ? documents.filter((document) => document.processId === processFilter) : documents;
		const allGlobal = scoped.filter((document) => !check_IsPersonal(document));
		const allPersonal = scoped.filter(check_IsPersonal);
		const global = filter_ByQuery(allGlobal, query, searchFields);
		const personal = filter_ByQuery(allPersonal, query, searchFields);
		globalCount.textContent = String(allGlobal.length);
		personalCount.textContent = personalAvailable ? String(allPersonal.length) : '';
		uploadButton.hidden = !permissions.canUploadGlobal;
		const withProcess = get_Processes().length > 0;
		const processTitles = get_ProcessTitles();

		globalContent.replaceChildren(global.length === 0
			? create_EmptyBlock('book', query || processFilter ? DocumentsTexts.nothingFound : AdminTexts.knowledgeEmpty)
			: create_DocumentTable(global, {
				config,
				processTitles,
				columns: withProcess ? ['title', 'process', 'edition', 'access', 'uploaded'] : ['title', 'edition', 'access', 'uploaded'],
				onNewEdition: permissions.canUploadGlobal ? (document) => open_Upload(document) : null,
				onChanged: reload,
			}));

		if (!personalAvailable) {
			personalContent.replaceChildren(create_Notice(DocumentsTexts.personalNotSupported, DocumentsTexts.personalNotSupportedHint));
			return;
		}
		personalContent.replaceChildren(personal.length === 0
			? create_EmptyBlock('users', query ? DocumentsTexts.nothingFound : AdminTexts.knowledgePersonalEmpty)
			: create_DocumentTable(personal, {
				config,
				processTitles,
				columns: withProcess ? ['title', 'process', 'owner', 'edition', 'uploaded'] : ['title', 'owner', 'edition', 'uploaded'],
				onChanged: reload,
			}));
	}

	async function reload() {
		try {
			// scope=all отдаёт и личные документы всех сотрудников. Старое ядро параметр не знает и вернёт общий список
			const [payload, loadedProcesses] = await Promise.all([list_Documents(Scopes.all), try_LoadProcesses()]);
			documents = read_DocumentList(payload);
			permissions = read_DocumentPermissions(payload, user, config);
			personalAvailable = permissions.supportsScopes;
			processesState = loadedProcesses;
			if (processFilter && !loadedProcesses.processes.some((process) => process.processId === processFilter)) {
				processFilter = '';
			}
			render_Sections();
		} catch (error) {
			globalContent.replaceChildren(create_EmptyBlock('alert', AdminTexts.knowledgeLoadFailed, [
				create_Element('div', { className: 'callout-hint', text: get_ErrorText(error) }),
				create_Button(AppTexts.retry, { variant: 'outline', icon: 'refresh', onClick: reload }),
			]));
			processesContent.replaceChildren();
			personalContent.replaceChildren();
		}
	}

	const element = create_Element('div', { className: 'page-sections' }, [
		create_SearchBox(AdminTexts.knowledgeSearch, (value) => {
			query = value;
			render_Sections();
		}),
		create_Element('section', { className: 'page-section' }, [
			create_Element('div', { className: 'section-header' }, [
				create_Element('h2', { className: 'section-title' }, [ProcessTexts.sectionTitle, processesCount]),
				create_Element('div', { className: 'section-tools' }, [createButton]),
			]),
			create_Element('p', { className: 'section-lead', text: ProcessTexts.sectionLead }),
			processesContent,
		]),
		create_Element('section', { className: 'page-section' }, [
			create_Element('div', { className: 'section-header' }, [
				create_Element('h2', { className: 'section-title' }, [DocumentsTexts.globalTitle, globalCount]),
				create_Element('div', { className: 'section-tools' }, [uploadButton]),
			]),
			create_Element('p', { className: 'section-lead', text: AdminTexts.knowledgeLead }),
			filterRow,
			globalContent,
		]),
		create_Element('section', { className: 'page-section' }, [
			create_Element('div', { className: 'section-header' }, [
				create_Element('h2', { className: 'section-title' }, [AdminTexts.knowledgePersonalTitle, personalCount]),
			]),
			create_Element('p', { className: 'section-lead', text: AdminTexts.knowledgePersonalLead }),
			personalContent,
		]),
	]);

	reload();
	return { element };
}
