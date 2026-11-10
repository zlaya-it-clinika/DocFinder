// Безопасный минимальный markdown для ответов модели.
// HTML из текста не исполняется: всё собирается из узлов, текст идёт через textContent.
// Поддержка: абзацы, переносы строк, заголовки #, списки - и 1., **жирный**, *курсив*, `код`.
import { create_Element } from './dom.js';

const inlinePattern = /(\*\*[^*]+\*\*|`[^`]+`|\*[^*\s][^*]*\*)/g;

function render_Inline(text) {
	const nodes = [];
	let lastIndex = 0;
	for (const match of text.matchAll(inlinePattern)) {
		if (match.index > lastIndex) {
			nodes.push(document.createTextNode(text.slice(lastIndex, match.index)));
		}
		const token = match[0];
		if (token.startsWith('**')) {
			nodes.push(create_Element('strong', { text: token.slice(2, -2) }));
		} else if (token.startsWith('`')) {
			nodes.push(create_Element('code', { text: token.slice(1, -1) }));
		} else {
			nodes.push(create_Element('em', { text: token.slice(1, -1) }));
		}
		lastIndex = match.index + token.length;
	}
	if (lastIndex < text.length) {
		nodes.push(document.createTextNode(text.slice(lastIndex)));
	}
	return nodes;
}

// Строки абзаца соединяются через <br>, как пишет модель
function render_Paragraph(lines) {
	const paragraph = create_Element('p');
	lines.forEach((line, index) => {
		if (index > 0) {
			paragraph.append(create_Element('br'));
		}
		paragraph.append(...render_Inline(line));
	});
	return paragraph;
}

export function render_Markdown(source) {
	const container = create_Element('div', { className: 'prose' });
	const lines = String(source ?? '').replace(/\r\n?/g, '\n').split('\n');
	let paragraphLines = [];
	let currentList = null;

	const flush_Paragraph = () => {
		if (paragraphLines.length > 0) {
			container.append(render_Paragraph(paragraphLines));
			paragraphLines = [];
		}
	};
	const flush_List = () => {
		currentList = null;
	};

	for (const rawLine of lines) {
		const line = rawLine.trimEnd();
		if (line.trim() === '') {
			flush_Paragraph();
			flush_List();
			continue;
		}
		const heading = line.match(/^(#{1,4})\s+(.*)$/);
		if (heading) {
			flush_Paragraph();
			flush_List();
			const level = heading[1].length <= 2 ? 'h3' : 'h4';
			container.append(create_Element(level, {}, render_Inline(heading[2])));
			continue;
		}
		const bullet = line.match(/^\s*[-*•]\s+(.*)$/);
		const numbered = line.match(/^\s*(\d+)[.)]\s+(.*)$/);
		if (bullet || numbered) {
			flush_Paragraph();
			const listTag = bullet ? 'ul' : 'ol';
			if (!currentList || currentList.tagName.toLowerCase() !== listTag) {
				currentList = create_Element(listTag);
				if (numbered && numbered[1] !== '1') {
					currentList.setAttribute('start', numbered[1]);
				}
				container.append(currentList);
			}
			currentList.append(create_Element('li', {}, render_Inline(bullet ? bullet[1] : numbered[2])));
			continue;
		}
		flush_List();
		paragraphLines.push(line);
	}
	flush_Paragraph();
	return container;
}
