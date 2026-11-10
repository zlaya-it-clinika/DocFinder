import { fetch_DocumentStructure } from '../api.js';
import { create_Element, create_Icon, create_LoadingBlock } from '../lib/dom.js';
import { navigate_To } from '../router.js';
import { AppTexts, DocumentViewTexts, get_ErrorText } from '../texts.js';

function normalize_Notes(value) {
	if (Array.isArray(value)) {
		return value.map(String).filter(Boolean);
	}
	return value ? [String(value)] : [];
}

function display_Title(document) {
	return document.display_name || document.title || document.file_name || DocumentViewTexts.untitled;
}

function status_Label(document) {
	if (document.status_note) {
		return document.status_note;
	}
	if (document.effective_to) {
		return `${DocumentViewTexts.validUntil} ${document.effective_to}`;
	}
	return DocumentViewTexts.currentEdition;
}

export function create_DocumentView({ documentId, path = '' }) {
	let destroyed = false;
	const content = create_Element('div', { className: 'document-reader-content' }, [create_LoadingBlock(AppTexts.loading)]);
	const element = create_Element('div', { className: 'document-reader' }, [content]);

	const select_Chunk = (chunkPath, { updateUrl = true } = {}) => {
		const id = `provision-${CSS.escape(chunkPath)}`;
		const target = content.querySelector(`[data-reader-id="${id}"]`);
		if (!target) {
			return;
		}
		content.querySelectorAll('.document-provision.is-target').forEach((item) => item.classList.remove('is-target'));
		content.querySelectorAll('.document-reader-toc-link[aria-current]').forEach((item) => item.removeAttribute('aria-current'));
		target.classList.add('is-target');
		content.querySelectorAll('.document-reader-toc-link').forEach((item) => {
			if (item.dataset.path === chunkPath) {
				item.setAttribute('aria-current', 'location');
			}
		});
		target.scrollIntoView({ behavior: window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth', block: 'center' });
		if (updateUrl) {
			window.history.replaceState(null, '', `/documents/${encodeURIComponent(documentId)}?p=${encodeURIComponent(chunkPath)}`);
		}
	};

	const render = (document) => {
		const chunks = Array.isArray(document.chunks) ? document.chunks : [];
		const meta = [document.category_label, document.doc_type, status_Label(document)].filter(Boolean);
		const toc = create_Element('nav', { className: 'document-reader-toc', attrs: { 'aria-label': DocumentViewTexts.contents } }, [
			create_Element('div', { className: 'document-reader-toc-title', text: DocumentViewTexts.contents }),
			...chunks.map((chunk) => create_Element('button', {
				className: 'document-reader-toc-link',
				text: chunk.path || chunk.section || DocumentViewTexts.provision,
				attrs: { type: 'button', title: chunk.section || chunk.path || '' },
				dataset: { path: chunk.path || chunk.section || '' },
				on: { click: () => select_Chunk(chunk.path || chunk.section || '') },
			})),
		]);
		const provisions = create_Element('article', { className: 'document-provisions' }, chunks.map((chunk) => {
			const chunkPath = chunk.path || chunk.section || '';
			const notes = normalize_Notes(chunk.notes);
			return create_Element('section', {
				className: 'document-provision',
				dataset: { readerId: `provision-${chunkPath}` },
			}, [
				create_Element('h2', { className: 'document-provision-heading' }, [
					create_Element('span', { className: 'document-provision-path', text: chunkPath || DocumentViewTexts.provision }),
					chunk.section && chunk.section !== chunkPath ? create_Element('span', { className: 'document-provision-section', text: chunk.section }) : null,
				]),
				create_Element('div', { className: 'document-provision-text', text: chunk.text ?? '' }),
				notes.length > 0 ? create_Element('div', { className: 'document-provision-notes' }, notes.map((note) => create_Element('p', { text: note }))) : null,
			]);
		}));
		content.replaceChildren(
			create_Element('header', { className: 'document-reader-header' }, [
				create_Element('button', {
					className: 'button button-ghost document-reader-back',
					attrs: { type: 'button' },
					on: { click: () => window.history.length > 1 ? window.history.back() : navigate_To('/documents') },
				}, [create_Icon('chevron-left', 'icon-sm'), create_Element('span', { text: DocumentViewTexts.back })]),
				create_Element('div', { className: 'document-reader-heading' }, [
					create_Element('h1', { className: 'document-reader-title', text: display_Title(document) }),
					create_Element('div', { className: 'document-reader-meta' }, meta.map((item) => create_Element('span', { text: item }))),
				]),
			]),
			create_Element('div', { className: 'document-reader-layout' }, [toc, provisions]),
		);
		if (path) {
			requestAnimationFrame(() => select_Chunk(path, { updateUrl: false }));
		}
	};

	fetch_DocumentStructure(documentId).then((document) => {
		if (!destroyed) {
			render(document);
		}
	}).catch((error) => {
		if (!destroyed) {
			content.replaceChildren(create_Element('div', { className: 'empty-block' }, [
				create_Icon('alert'),
				create_Element('strong', { text: DocumentViewTexts.loadFailed }),
				create_Element('span', { className: 'callout-hint', text: get_ErrorText(error) }),
			]));
		}
	});

	return { element, destroy: () => { destroyed = true; } };
}
