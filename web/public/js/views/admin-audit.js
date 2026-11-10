// Вкладка «Журнал»: кто спрашивал и какие документы были показаны. Новые записи сверху.
import { fetch_AuditLog } from '../api.js';
import { create_Button, create_Element, create_EmptyBlock, create_LoadingBlock, create_SearchBox } from '../lib/dom.js';
import { format_DateTime } from '../lib/format.js';
import { check_IsPersonal } from '../documents.js';
import { AdminTexts, AppTexts, CitationTexts, DocumentsTexts, get_ErrorText } from '../texts.js';

function render_ShownDocuments(documents) {
	if (!Array.isArray(documents) || documents.length === 0) {
		return create_Element('span', { className: 'table-cell-muted', text: AdminTexts.auditNoDocuments });
	}
	return create_Element('div', { className: 'chip-list' }, documents.map((document) => {
		const label = document.paragraph ? `${document.title}, ${CitationTexts.shortParagraph} ${document.paragraph}` : document.title;
		return create_Element('span', { className: 'chip', attrs: { title: label } }, [
			create_Element('span', { className: 'chip-text', text: label }),
			check_IsPersonal(document) ? create_Element('span', { className: 'chip-tag', text: DocumentsTexts.scopePersonal }) : null,
		]);
	}));
}

export function create_AuditTab() {
	let entries = [];
	let query = '';

	const countLabel = create_Element('span', { className: 'section-count' });
	const content = create_Element('div', {}, [create_LoadingBlock(AppTexts.loading)]);

	const reload = async () => {
		try {
			const payload = await fetch_AuditLog();
			entries = Array.isArray(payload?.entries) ? payload.entries.slice().reverse() : [];
			render_Table();
		} catch (error) {
			content.replaceChildren(create_EmptyBlock('alert', AdminTexts.auditLoadFailed, [
				create_Element('div', { className: 'callout-hint', text: get_ErrorText(error) }),
				create_Button(AppTexts.retry, { variant: 'outline', icon: 'refresh', onClick: reload }),
			]));
		}
	};

	function render_Table() {
		countLabel.textContent = String(entries.length);
		const filtered = query
			? entries.filter((entry) => `${entry.displayName} ${entry.question}`.toLowerCase().includes(query))
			: entries;
		if (filtered.length === 0) {
			content.replaceChildren(create_EmptyBlock('list', AdminTexts.auditEmpty));
			return;
		}
		content.replaceChildren(create_Element('div', { className: 'table-wrap' }, [
			create_Element('table', { className: 'table' }, [
				create_Element('thead', {}, [create_Element('tr', {}, [
					create_Element('th', { text: AdminTexts.auditColumnTime }),
					create_Element('th', { text: AdminTexts.auditColumnUser }),
					create_Element('th', { text: AdminTexts.auditColumnQuestion }),
					create_Element('th', { text: AdminTexts.auditColumnDocuments }),
				])]),
				create_Element('tbody', {}, filtered.map((entry) => create_Element('tr', {}, [
					create_Element('td', { className: 'time-cell', text: format_DateTime(entry.createdAt) }),
					create_Element('td', {}, [
						create_Element('div', { className: 'cell-title', text: entry.displayName }),
						create_Element('div', { className: 'cell-subtitle', text: entry.userId }),
					]),
					create_Element('td', { className: 'cell-question', text: entry.question }),
					create_Element('td', { className: 'table-cell-wrap' }, [render_ShownDocuments(entry.documents)]),
				]))),
			]),
		]));
	}

	const element = create_Element('section', { className: 'page-section' }, [
		create_Element('div', { className: 'section-header' }, [
			create_Element('h2', { className: 'section-title' }, [AdminTexts.tabAudit, countLabel]),
			create_Element('div', { className: 'section-tools' }, [
				create_SearchBox(AdminTexts.auditSearch, (value) => {
					query = value;
					render_Table();
				}),
				create_Button(AppTexts.refresh, { variant: 'outline', icon: 'refresh', onClick: reload }),
			]),
		]),
		create_Element('p', { className: 'section-lead', text: AdminTexts.auditLead }),
		content,
	]);

	reload();
	return { element };
}
