// Таблица документов для страницы «Документы» и админ-панели.
// columns: title, owner, edition, access, uploaded. Действия показываются только при наличии обработчика и прав.
import { build_DocumentFileUrl, build_OriginalUrl, delete_Document, list_DocumentEditions } from '../api.js';
import { check_CanDelete, check_IsPersonal } from '../documents.js';
import { create_ProcessBadge, get_KnownProcess } from '../processes.js';
import { create_Element, create_Icon, create_IconButton } from '../lib/dom.js';
import { format_DateTime } from '../lib/format.js';
import { Roles, get_DocumentAudience, get_DocumentAudienceLabel } from '../roles.js';
import { AppTexts, DocumentsTexts, format_Text, get_ErrorText } from '../texts.js';
import { open_Confirm } from './confirm.js';
import { open_Modal } from './modal.js';
import { show_Toast } from './toast.js';

const columnTitles = {
	title: DocumentsTexts.columnTitle,
	owner: DocumentsTexts.columnOwner,
	edition: DocumentsTexts.columnEdition,
	access: DocumentsTexts.columnAccess,
	uploaded: DocumentsTexts.columnUploaded,
	process: DocumentsTexts.columnProcess,
};

export function create_ScopeBadge(document, config) {
	if (check_IsPersonal(document)) {
		return create_Element('span', { className: 'badge badge-outline', text: DocumentsTexts.scopePersonal });
	}
	return create_Element('span', {
		className: get_DocumentAudience(document.accessLevel, config) === Roles.admin ? 'badge badge-strong' : 'badge',
		text: get_DocumentAudienceLabel(document.accessLevel, config),
	});
}

function render_Cell(column, document, config, processTitles) {
	switch (column) {
		case 'title':
			return create_Element('td', { className: 'table-cell-wrap' }, [
				create_Element('a', {
					className: 'cell-title cell-title-link',
					text: document.title,
					attrs: {
						href: `/documents/${encodeURIComponent(document.documentId)}`,
						'data-link': true,
						title: DocumentsTexts.openStructured,
					},
				}),
				create_Element('div', { className: 'cell-subtitle' }, [
					create_Element('span', { text: document.documentId }),
					document.standard
						? create_Element('span', { className: 'badge badge-outline', text: DocumentsTexts.standardBadge, attrs: { title: DocumentsTexts.standardHint } })
						: null,
				]),
			]);
		case 'owner':
			return create_Element('td', {}, [
				create_Element('div', { className: 'cell-title', text: document.ownerName || AppTexts.notSpecified }),
				create_Element('div', { className: 'cell-subtitle', text: document.ownerId || '' }),
			]);
		case 'edition':
			return create_Element('td', { className: 'table-cell-muted', text: document.edition || AppTexts.notSpecified });
		case 'access':
			return create_Element('td', {}, [create_ScopeBadge(document, config)]);
		case 'uploaded':
			return create_Element('td', { className: 'time-cell', text: format_DateTime(document.uploadedAt) || AppTexts.notSpecified });
		case 'process':
			return create_Element('td', {}, [document.processId
				? create_ProcessBadge(get_KnownProcess(document.processId) ?? { title: processTitles[document.processId] || document.processId, color: 'gray' })
				: create_Element('span', { className: 'table-cell-muted', text: AppTexts.notSpecified })]);
		default:
			return create_Element('td');
	}
}

// Архив прежних редакций: список со ссылками на скачивание
async function open_EditionArchive(document) {
	const list = create_Element('div', { className: 'edition-list' }, [create_Element('div', { className: 'callout-hint', text: AppTexts.loading })]);
	const modal = open_Modal({
		title: DocumentsTexts.archiveTitle,
		body: [
			create_Element('p', { className: 'callout-hint', text: format_Text(DocumentsTexts.archiveLead, { title: document.title }) }),
			create_Element('div', { className: 'callout-hint', text: format_Text(DocumentsTexts.archiveCurrent, { edition: document.edition || AppTexts.notSpecified }) }),
			list,
		],
	});
	try {
		const payload = await list_DocumentEditions(document.documentId);
		const editions = Array.isArray(payload?.editions) ? payload.editions : [];
		if (editions.length === 0) {
			list.replaceChildren(create_Element('div', { className: 'callout-hint', text: DocumentsTexts.archiveEmpty }));
			return;
		}
		list.replaceChildren(...editions.map((edition) => create_Element('div', { className: 'edition-row' }, [
			create_Element('div', { className: 'edition-row-text' }, [
				create_Element('div', { className: 'cell-title', text: format_Text(DocumentsTexts.archiveEdition, { edition: edition.edition || AppTexts.notSpecified }) }),
				create_Element('div', { className: 'cell-subtitle', text: `${edition.fileName} · ${format_Text(DocumentsTexts.archiveDates, {
					uploaded: format_DateTime(edition.uploadedAt) || AppTexts.notSpecified,
					archived: format_DateTime(edition.archivedAt) || AppTexts.notSpecified,
				})}` }),
			]),
			create_Element('a', {
				className: 'icon-button',
				attrs: {
					href: build_OriginalUrl(edition.fileLink),
					target: '_blank',
					rel: 'noopener',
					title: DocumentsTexts.openFile,
					'aria-label': `${DocumentsTexts.openFile}: ${edition.fileName}`,
				},
			}, [create_Icon('download')]),
		])));
	} catch (error) {
		modal.close();
		show_Toast(get_ErrorText(error), 'error');
	}
}

async function confirm_AndDelete(document, onChanged) {
	const confirmed = await open_Confirm({
		title: DocumentsTexts.deleteTitle,
		text: format_Text(DocumentsTexts.deleteText, { title: document.title }),
		confirmLabel: DocumentsTexts.deleteConfirm,
		danger: true,
	});
	if (!confirmed) {
		return;
	}
	try {
		await delete_Document(document.documentId);
		show_Toast(DocumentsTexts.deleted, 'success');
		onChanged();
	} catch (error) {
		show_Toast(get_ErrorText(error), 'error');
	}
}

export function create_ProcessFilter(processes, onChange) {
	if (!Array.isArray(processes) || processes.length === 0) {
		return null;
	}
	const select = create_Element('select', { className: 'select', attrs: { 'aria-label': DocumentsTexts.processFilter } }, [
		create_Element('option', { text: DocumentsTexts.processAll, attrs: { value: '' } }),
		...processes.map((process) => create_Element('option', { text: process.title, attrs: { value: process.processId } })),
	]);
	select.addEventListener('change', () => onChange(select.value));
	return select;
}

// onNewEdition(document) - загрузить новую редакцию, onChanged() - перечитать список после удаления
export function create_DocumentTable(documents, { config, columns, processTitles = {}, onNewEdition, onChanged }) {
	return create_Element('div', { className: 'table-wrap' }, [
		create_Element('table', { className: 'table' }, [
			create_Element('thead', {}, [create_Element('tr', {}, [
				...columns.map((column) => create_Element('th', { text: columnTitles[column] })),
				create_Element('th', { className: 'table-cell-actions' }),
			])]),
			create_Element('tbody', {}, documents.map((document) => create_Element('tr', {}, [
				...columns.map((column) => render_Cell(column, document, config, processTitles)),
				create_Element('td', { className: 'table-cell-actions' }, [
					onNewEdition
						? create_IconButton('upload', `${DocumentsTexts.newEdition}: ${document.title}`, () => onNewEdition(document))
						: null,
					Number(document.archivedEditions) > 0
						? create_IconButton('clock', `${DocumentsTexts.archive}: ${document.title}`, () => open_EditionArchive(document))
						: null,
					create_Element('a', {
						className: 'icon-button',
						attrs: {
							href: build_DocumentFileUrl(document.documentId),
							target: '_blank',
							rel: 'noopener',
							title: DocumentsTexts.openFile,
							'aria-label': `${DocumentsTexts.openFile}: ${document.title}`,
						},
					}, [create_Icon('download')]),
					onChanged && check_CanDelete(document)
						? create_IconButton('trash', `${DocumentsTexts.delete}: ${document.title}`, () => confirm_AndDelete(document, onChanged))
						: null,
				]),
			]))),
		]),
	]);
}
