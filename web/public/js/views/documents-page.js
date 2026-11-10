// Страница «Документы» для каждого сотрудника: свои личные документы и общие документы компании.
// Всё это одна база знаний: чат ищет по общим документам и по личным документам автора вопроса.
import { list_Documents, list_Processes } from '../api.js';
import { open_DocumentUploadForm } from '../components/document-upload.js';
import { create_DocumentTable, create_ProcessFilter } from '../components/document-table.js';
import { Scopes, check_IsPersonal, filter_ByQuery, read_DocumentList, read_DocumentPermissions } from '../documents.js';
import { create_Button, create_Element, create_EmptyBlock, create_Icon, create_LoadingBlock, create_SearchBox } from '../lib/dom.js';
import { Permissions, has_Permission } from '../roles.js';
import { AppTexts, DocumentsTexts, get_ErrorText } from '../texts.js';

function create_SectionHeader(title, countLabel, tools = []) {
	return create_Element('div', { className: 'section-header' }, [
		create_Element('h2', { className: 'section-title' }, [title, countLabel]),
		create_Element('div', { className: 'section-tools' }, tools),
	]);
}

export function create_DocumentsPage({ user, config }) {
	let documents = [];
	let processes = [];
	let processTitles = {};
	let processFilter = '';
	let permissions = read_DocumentPermissions(null, user, config);
	let query = '';

	const personalCount = create_Element('span', { className: 'section-count' });
	const globalCount = create_Element('span', { className: 'section-count' });
	const personalContent = create_Element('div', {}, [create_LoadingBlock(AppTexts.loading)]);
	const globalContent = create_Element('div', {}, [create_LoadingBlock(AppTexts.loading)]);
	const uploadButton = create_Button(DocumentsTexts.uploadPersonal, {
		variant: 'primary',
		icon: 'upload',
		onClick: () => open_DocumentUploadForm({ config, scope: Scopes.personal, onUploaded: reload }),
	});
	uploadButton.hidden = true;

	const render_Sections = () => {
		const byProcess = processFilter ? documents.filter((document) => document.processId === processFilter) : documents;
		const filtered = filter_ByQuery(byProcess, query);
		const personal = filtered.filter(check_IsPersonal);
		const global = filtered.filter((document) => !check_IsPersonal(document));
		personalCount.textContent = String(documents.filter(check_IsPersonal).length);
		globalCount.textContent = String(documents.length - documents.filter(check_IsPersonal).length);
		uploadButton.hidden = !permissions.canUploadPersonal;

		if (!permissions.supportsScopes) {
			// Старое ядро сохранило бы файл общим и показало бы его всем, поэтому загрузку не предлагаем
			personalContent.replaceChildren(create_Element('div', { className: 'callout' }, [
				create_Icon('info'),
				create_Element('div', { className: 'callout-content' }, [
					create_Element('div', { text: DocumentsTexts.personalNotSupported }),
					create_Element('div', { className: 'callout-hint', text: DocumentsTexts.personalNotSupportedHint }),
				]),
			]));
		} else if (personal.length === 0) {
			personalContent.replaceChildren(create_EmptyBlock('file', query ? DocumentsTexts.nothingFound : DocumentsTexts.personalEmpty, query ? [] : [
				create_Element('div', { className: 'callout-hint', text: DocumentsTexts.personalEmptyHint }),
			]));
		} else {
			personalContent.replaceChildren(create_DocumentTable(personal, {
				config,
				processTitles,
				columns: processes.length > 0 ? ['title', 'process', 'edition', 'uploaded'] : ['title', 'edition', 'uploaded'],
				onNewEdition: permissions.canUploadPersonal
					? (document) => open_DocumentUploadForm({ config, scope: Scopes.personal, document, onUploaded: reload })
					: null,
				onChanged: reload,
			}));
		}

		if (global.length === 0) {
			globalContent.replaceChildren(create_EmptyBlock('book', query ? DocumentsTexts.nothingFound : DocumentsTexts.globalEmpty));
		} else {
			// Общими документами управляют в админ-панели, здесь только просмотр
			globalContent.replaceChildren(create_DocumentTable(global, {
				config,
				processTitles,
				columns: processes.length > 0 ? ['title', 'process', 'edition', 'access'] : ['title', 'edition', 'access'],
			}));
		}
	};

	const show_LoadError = (error) => {
		const block = create_EmptyBlock('alert', DocumentsTexts.loadFailed, [
			create_Element('div', { className: 'callout-hint', text: get_ErrorText(error) }),
			create_Button(AppTexts.retry, { variant: 'outline', icon: 'refresh', onClick: reload }),
		]);
		personalContent.replaceChildren(block);
		globalContent.replaceChildren();
	};

	const filterHost = create_Element('div', { className: 'section-tools' });

	async function reload() {
		try {
			if (processes.length === 0) {
				try {
					processes = (await list_Processes())?.processes ?? [];
					processTitles = Object.fromEntries(processes.map((process) => [process.processId, process.title]));
					const filter = create_ProcessFilter(processes, (value) => {
						processFilter = value;
						render_Sections();
					});
					if (filter) {
						filterHost.replaceChildren(filter);
					}
				} catch {
					processes = [];
				}
			}
			const payload = await list_Documents();
			documents = read_DocumentList(payload);
			permissions = read_DocumentPermissions(payload, user, config);
			render_Sections();
		} catch (error) {
			show_LoadError(error);
		}
	}

	const globalLead = has_Permission(user, config, Permissions.globalManage)
		? create_Element('p', { className: 'section-lead' }, [
			DocumentsTexts.globalLeadAdmin,
			' ',
			create_Element('a', { text: DocumentsTexts.globalManageLink, attrs: { href: '/admin/knowledge', 'data-link': true } }),
		])
		: create_Element('p', { className: 'section-lead', text: DocumentsTexts.globalLead });

	const element = create_Element('div', { className: 'page' }, [
		create_Element('div', { className: 'page-inner' }, [
			create_Element('div', { className: 'page-heading' }, [
				create_Element('h1', { className: 'page-title', text: DocumentsTexts.pageTitle }),
				create_Element('p', { className: 'section-lead', text: DocumentsTexts.pageLead }),
			]),
			create_Element('div', { className: 'section-header' }, [
				create_SearchBox(DocumentsTexts.search, (value) => {
					query = value;
					render_Sections();
				}),
				filterHost,
			]),
			create_Element('section', { className: 'page-section' }, [
				create_SectionHeader(DocumentsTexts.personalTitle, personalCount, [uploadButton]),
				create_Element('p', { className: 'section-lead', text: DocumentsTexts.personalLead }),
				personalContent,
			]),
			create_Element('section', { className: 'page-section' }, [
				create_SectionHeader(DocumentsTexts.globalTitle, globalCount),
				globalLead,
				globalContent,
			]),
		]),
	]);

	reload();
	return { element };
}
