// Плавное появление ответа ассистента: текст проявляется по словам, как при потоковой генерации,
// а источники, карточки и подсказки мягко появляются после текста.
// Ядро отдаёт ответ целиком, поэтому проявление идёт в браузере. При prefers-reduced-motion
// и в фоновой вкладке (там браузер почти не рисует кадры) ответ виден сразу целиком.

// Скорость проявления: длинный ответ не тянется дольше предела
const msPerCharacter = 9;
const minDurationMs = 300;
const maxDurationMs = 1800;
// Блоки, которые не показываются пустыми, пока до них не дошёл текст
const blockSelector = 'p, li, h1, h2, h3, h4, blockquote, pre, td, th';

function prefers_ReducedMotion() {
	return window.matchMedia?.('(prefers-reduced-motion: reduce)').matches === true;
}

function collect_TextNodes(root) {
	const nodes = [];
	const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
	for (let node = walker.nextNode(); node; node = walker.nextNode()) {
		if (node.data.length > 0) {
			nodes.push({ node, text: node.data, block: node.parentElement?.closest(blockSelector) ?? null });
		}
	}
	return nodes;
}

// Конец слова: проявление не обрывает слово посередине
function extend_ToWordEnd(text, index) {
	let end = index;
	while (end < text.length && !/\s/.test(text[end])) {
		end += 1;
	}
	return end;
}

// body - тело реплики. Текст проявляется в блоках .prose вне карточек документов,
// остальные части тела ждут конца текста. onProgress() вызывается на каждом шаге - экран чата держит низ ленты
export function reveal_Message(body, { onProgress } = {}) {
	if (prefers_ReducedMotion() || document.hidden) {
		return;
	}
	const textRoots = [...body.querySelectorAll('.prose')].filter((root) => !root.closest('.doc-card'));
	const nodes = textRoots.flatMap(collect_TextNodes);
	const total = nodes.reduce((sum, item) => sum + item.text.length, 0);
	// Части тела без проявляемого текста появляются после него
	const later = [...body.children].filter((child) => !textRoots.some((root) => child === root || child.contains(root)));
	for (const child of later) {
		child.classList.add('reveal-later');
	}
	if (total === 0) {
		requestAnimationFrame(() => show_Later(later));
		return;
	}
	for (const item of nodes) {
		item.node.data = '';
		item.block?.classList.add('reveal-hidden');
	}
	body.setAttribute('aria-busy', 'true');
	const duration = Math.max(minDurationMs, Math.min(maxDurationMs, total * msPerCharacter));
	const started = performance.now();

	let done = false;
	const finish = () => {
		if (done) {
			return;
		}
		done = true;
		document.removeEventListener('visibilitychange', on_Visibility);
		for (const item of nodes) {
			item.node.data = item.text;
			item.block?.classList.remove('reveal-hidden');
		}
		body.removeAttribute('aria-busy');
		show_Later(later);
		onProgress?.();
	};

	const frame = (now) => {
		// Реплику перерисовали или убрали: новая копия уже показана целиком
		if (!body.isConnected) {
			document.removeEventListener('visibilitychange', on_Visibility);
			return;
		}
		let budget = Math.ceil(((now - started) / duration) * total);
		if (budget >= total || document.hidden) {
			finish();
			return;
		}
		for (const item of nodes) {
			if (budget <= 0) {
				break;
			}
			const shown = Math.min(item.text.length, extend_ToWordEnd(item.text, budget));
			item.node.data = item.text.slice(0, shown);
			if (shown > 0) {
				item.block?.classList.remove('reveal-hidden');
			}
			budget -= item.text.length;
		}
		onProgress?.();
		requestAnimationFrame(frame);
	};
	// Вкладку свернули посреди проявления: кадров не будет, ответ показывается целиком сразу
	function on_Visibility() {
		if (document.hidden) {
			finish();
		}
	}
	document.addEventListener('visibilitychange', on_Visibility);
	requestAnimationFrame(frame);
}

function show_Later(elements) {
	for (const element of elements) {
		element.classList.add('reveal-shown');
	}
}
