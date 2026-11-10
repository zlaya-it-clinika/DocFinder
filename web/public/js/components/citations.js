// Источники ответа: чипы под сообщением и окно с подробностями, как в open-webui.
import { build_OriginalUrl } from '../api.js';
import { check_IsPersonal } from '../documents.js';
import { get_KnownProcess } from '../processes.js';
import { create_Element, create_Icon } from '../lib/dom.js';
import { AppTexts, ChatTexts, CitationTexts, DocumentsTexts } from '../texts.js';
import { open_Modal } from './modal.js';

function check_HasValue(value) {
	return value !== undefined && value !== null && String(value).trim() !== '';
}

// Короткое место в документе: разд. 3, п. 3.2, прил. 1
export function format_CitationPlace(citation) {
	const parts = [];
	if (check_HasValue(citation.section)) {
		parts.push(`${CitationTexts.shortSection} ${citation.section}`);
	}
	if (check_HasValue(citation.paragraph)) {
		parts.push(`${CitationTexts.shortParagraph} ${citation.paragraph}`);
	}
	if (check_HasValue(citation.appendix)) {
		parts.push(`${CitationTexts.shortAppendix} ${citation.appendix}`);
	}
	return parts.join(', ');
}

export function render_Relevance(percent) {
	const safePercent = Math.max(0, Math.min(100, Math.round(Number(percent) || 0)));
	const fill = create_Element('span', { className: 'relevance-meter-fill' });
	fill.style.width = `${safePercent}%`;
	return create_Element('div', {
		className: 'relevance',
		attrs: { title: `${ChatTexts.relevance}: ${safePercent}%` },
	}, [
		create_Element('span', { text: ChatTexts.relevance }),
		create_Element('span', {
			className: 'relevance-meter',
			attrs: { role: 'meter', 'aria-valuemin': 0, 'aria-valuemax': 100, 'aria-valuenow': safePercent, 'aria-label': ChatTexts.relevance },
		}, [fill]),
		create_Element('span', { className: 'relevance-value', text: `${safePercent}%` }),
	]);
}

export function create_CitationChip(citation, index, relevancePercent) {
	const place = format_CitationPlace(citation);
	const label = place ? `${citation.document}, ${place}` : citation.document;
	return create_Element('button', {
		className: 'chip',
		attrs: { type: 'button', title: label },
		on: { click: () => open_CitationModal(citation, relevancePercent) },
	}, [
		create_Element('span', { className: 'chip-index', text: index + 1 }),
		create_Element('span', { className: 'chip-text', text: label }),
		check_IsPersonal(citation) ? create_Element('span', { className: 'chip-tag', text: DocumentsTexts.scopePersonal }) : null,
		get_KnownProcess(citation.processId) ? create_Element('span', { className: 'chip-tag', text: get_KnownProcess(citation.processId).title }) : null,
		citation.status ? create_Element('span', { className: 'chip-tag citation-status', text: citation.status }) : null,
	]);
}

export function render_Sources(citations, relevancePercent) {
	return create_Element('div', { className: 'sources' }, [
		create_Element('div', { className: 'sources-title', text: ChatTexts.sources }),
		create_Element('div', { className: 'chip-list' }, citations.map((citation, index) => create_CitationChip(citation, index, relevancePercent))),
	]);
}

function create_DetailRow(label, value) {
	if (!check_HasValue(value)) {
		return [];
	}
	return [create_Element('dt', { text: label }), create_Element('dd', { text: value })];
}

export function open_CitationModal(citation, relevancePercent) {
	const details = create_Element('dl', { className: 'citation-details' }, [
		...create_DetailRow(CitationTexts.document, citation.document),
		...create_DetailRow(DocumentsTexts.columnProcess, get_KnownProcess(citation.processId)?.title ?? ''),
		...create_DetailRow(CitationTexts.scope, check_IsPersonal(citation) ? DocumentsTexts.scopePersonal : DocumentsTexts.scopeGlobal),
		...create_DetailRow(CitationTexts.edition, citation.edition),
		...create_DetailRow(CitationTexts.section, citation.section),
		...create_DetailRow(CitationTexts.paragraph, citation.paragraph),
		...create_DetailRow(CitationTexts.appendix, citation.appendix),
		...create_DetailRow(CitationTexts.status, citation.status),
	]);
	const body = [details];
	if (Number(relevancePercent) > 0) {
		body.push(render_Relevance(relevancePercent));
	}
	if (check_HasValue(citation.quote)) {
		body.push(create_Element('blockquote', { className: 'citation-quote', text: citation.quote }));
	}

	const footer = [];
	const originalUrl = build_OriginalUrl(citation.originalLink);
	if (originalUrl) {
		const isDocumentView = originalUrl.startsWith('/documents/') && !originalUrl.includes('/file');
		footer.push(create_Element('a', {
			className: 'button button-primary',
			attrs: isDocumentView
				? { href: originalUrl, 'data-link': true }
				: { href: originalUrl, target: '_blank', rel: 'noopener' },
		}, [create_Icon('external', 'icon-sm'), create_Element('span', { text: CitationTexts.openOriginal })]));
	}
	const modal = open_Modal({ title: CitationTexts.modalTitle, body, footer });
	footer.unshift(create_Element('button', {
		className: 'button button-ghost',
		text: AppTexts.close,
		attrs: { type: 'button' },
		on: { click: () => modal.close() },
	}));
	modal.set_Footer(footer);
}
