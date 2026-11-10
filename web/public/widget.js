// Загрузчик попап-виджета DocFinder для внутреннего портала.
// Подключение одной строкой:
//   <script src="https://docs-ai.corp.local/widget.js" defer></script>
// Необязательные атрибуты: data-position="left" и data-label="Подпись кнопки".
// Скрипт рисует кнопку в Shadow DOM, чат открывается в iframe с нашего сервера:
// стили портала и виджета не пересекаются, токен в страницу портала не попадает.
(function () {
	'use strict';

	// Подписи загрузчика. Файл работает на чужой странице без модулей, поэтому тексты здесь
	var loaderTexts = {
		open: 'Помощник по документам',
		close: 'Свернуть помощника',
		frameTitle: 'DocFinder - помощник по документам',
	};

	var closeMessageType = 'docfinder:close';
	var currentScript = document.currentScript;
	if (!currentScript || window.__docFinderWidgetLoaded) {
		return;
	}
	window.__docFinderWidgetLoaded = true;

	var serverOrigin = new URL(currentScript.src, window.location.href).origin;
	var position = currentScript.dataset.position === 'left' ? 'left' : 'right';
	var buttonLabel = currentScript.dataset.label || loaderTexts.open;

	var styles = [
		':host { all: initial; }',
		'.launcher { position: fixed; bottom: 20px; ' + position + ': 20px; z-index: 2147483000; width: 52px; height: 52px;',
		'  border: 0; border-radius: 50%; background: #171717; color: #fff; cursor: pointer;',
		'  display: flex; align-items: center; justify-content: center; box-shadow: 0 0 0 1px rgba(255,255,255,.16), 0 6px 20px rgba(0,0,0,.18); }',
		'.launcher:hover { background: #333; }',
		'.launcher:focus-visible { outline: 2px solid #4c7ce0; outline-offset: 3px; }',
		'.launcher-logo { width: 34px; height: 34px; display: block; image-rendering: pixelated; pointer-events: none; }',
		'.launcher svg { width: 24px; height: 24px; fill: none; stroke: currentColor; stroke-width: 1.75; stroke-linecap: round; stroke-linejoin: round; }',
		'.panel { position: fixed; bottom: 84px; ' + position + ': 20px; z-index: 2147483000; width: 400px; height: 640px;',
		'  max-width: calc(100vw - 40px); max-height: calc(100vh - 110px); border-radius: 18px; overflow: hidden;',
		'  background: #fff; box-shadow: 0 12px 40px rgba(0,0,0,.18), 0 2px 8px rgba(0,0,0,.08); border: 1px solid rgba(0,0,0,.08); }',
		'.panel[hidden] { display: none; }',
		'.panel iframe { width: 100%; height: 100%; border: 0; display: block; }',
		'@media (max-width: 520px) { .panel { inset: 0; width: 100%; height: 100%; max-width: none; max-height: none; border-radius: 0; } }',
	].join('\n');

	// Запасная иконка, если портал не разрешает картинки с нашего сервера
	var chatIcon = '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M6 3h8l4 4v14H6z"/><path d="M14 3v4h4"/><circle cx="11.5" cy="13.5" r="2.75"/><path d="m13.5 15.5 2 2"/></svg>';
	var closeIcon = '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M6 6l12 12M18 6 6 18"/></svg>';

	var host = document.createElement('div');
	host.setAttribute('data-docfinder-widget', '');
	var shadow = host.attachShadow({ mode: 'closed' });

	// Конструируемые таблицы стилей не зависят от CSP портала на встроенные стили
	try {
		var sheet = new CSSStyleSheet();
		sheet.replaceSync(styles);
		shadow.adoptedStyleSheets = [sheet];
	} catch (error) {
		var styleElement = document.createElement('style');
		styleElement.textContent = styles;
		shadow.appendChild(styleElement);
	}

	var launcher = document.createElement('button');
	launcher.className = 'launcher';
	launcher.type = 'button';
	launcher.setAttribute('aria-expanded', 'false');

	var panel = document.createElement('div');
	panel.className = 'panel';
	panel.hidden = true;
	panel.setAttribute('role', 'dialog');
	panel.setAttribute('aria-label', loaderTexts.frameTitle);

	var frame = null;

	// Пиксельный логотип DocFinder на кнопке
	var logoFailed = false;
	var logoImage = document.createElement('img');
	logoImage.className = 'launcher-logo';
	logoImage.src = serverOrigin + '/logo-192.png';
	logoImage.alt = '';
	logoImage.setAttribute('aria-hidden', 'true');
	logoImage.addEventListener('error', function () {
		logoFailed = true;
		if (launcher.getAttribute('aria-expanded') === 'false') {
			launcher.innerHTML = chatIcon;
		}
	});

	function render_Launcher(isOpen) {
		// Иконки - статичная разметка загрузчика, данных пользователя здесь нет
		if (isOpen) {
			launcher.innerHTML = closeIcon;
		} else if (logoFailed) {
			launcher.innerHTML = chatIcon;
		} else {
			launcher.replaceChildren(logoImage);
		}
		launcher.setAttribute('aria-label', isOpen ? loaderTexts.close : buttonLabel);
		launcher.title = isOpen ? loaderTexts.close : buttonLabel;
		launcher.setAttribute('aria-expanded', isOpen ? 'true' : 'false');
	}

	function open_Panel() {
		// iframe создаём при первом открытии, чтобы не грузить чат на каждой странице портала
		if (!frame) {
			frame = document.createElement('iframe');
			frame.src = serverOrigin + '/embed';
			frame.title = loaderTexts.frameTitle;
			frame.setAttribute('allow', 'clipboard-write');
			frame.setAttribute('referrerpolicy', 'no-referrer');
			panel.appendChild(frame);
		}
		panel.hidden = false;
		render_Launcher(true);
		frame.focus();
	}

	function close_Panel() {
		panel.hidden = true;
		render_Launcher(false);
		launcher.focus();
	}

	launcher.addEventListener('click', function () {
		if (panel.hidden) {
			open_Panel();
		} else {
			close_Panel();
		}
	});

	document.addEventListener('keydown', function (event) {
		if (event.key === 'Escape' && !panel.hidden) {
			close_Panel();
		}
	});

	// Команды принимаем только от нашего iframe
	window.addEventListener('message', function (event) {
		if (event.origin !== serverOrigin || !frame || event.source !== frame.contentWindow) {
			return;
		}
		if (event.data && event.data.type === closeMessageType) {
			close_Panel();
		}
	});

	render_Launcher(false);
	shadow.appendChild(panel);
	shadow.appendChild(launcher);

	function mount_Widget() {
		document.body.appendChild(host);
	}

	if (document.body) {
		mount_Widget();
	} else {
		document.addEventListener('DOMContentLoaded', mount_Widget);
	}
})();
