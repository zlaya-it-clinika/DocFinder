# DocFinder Web

Web - интерфейс DocFinder, BFF для Go core и загрузчик встраиваемого виджета. Компонент написан на HTML, CSS и JavaScript без клиентских фреймворков и npm-зависимостей.

Ответственный: Syharik3316.

## Запуск

Требуется Node.js 22 или новее и запущенный core.

```bash
cp .env.example .env
npm start
```

PowerShell:

```powershell
Copy-Item .env.example .env
npm start
```

Откройте `http://localhost:3000`.

## Команды

| Команда | Назначение |
|---|---|
| `npm start` | запуск сервера |
| `npm run dev` | запуск с перезагрузкой при изменении server |
| `npm run check` | тесты BFF и безопасности |

## Маршруты

| Адрес | Назначение |
|---|---|
| `/login` | вход и создание первого администратора |
| `/`, `/c/<sessionId>` | юридический чат |
| `/documents` | каталог документов |
| `/documents/<documentId>` | просмотр документа по положениям |
| `/admin/*` | пользователи, роли, база знаний, модели и аудит |
| `/embed` | компактный интерфейс для iframe |
| `/widget-demo.html` | локальный пример виджета |

## BFF и безопасность

- токен core хранится только на web-сервере;
- браузер получает случайную `httpOnly` cookie;
- изменяющие запросы требуют CSRF-токен и допустимый Origin;
- статика работает со строгой CSP;
- данные API вставляются через `textContent`;
- загрузки ограничены размером и списком расширений;
- попытки входа ограничиваются по адресу клиента.

## Виджет

```html
<script src="https://docfinder.example/widget.js" defer></script>
```

Для портала на другом домене задайте `EMBED_ALLOWED_ORIGINS`, включите HTTPS и используйте:

```env
COOKIE_SECURE=true
COOKIE_SAMESITE=None
```

## Структура

| Путь | Назначение |
|---|---|
| `server/index.js` | HTTP-сервер и маршрутизация |
| `server/api.js` | сессии и прокси в core |
| `server/security.js` | CSP, CSRF, Origin и rate limit |
| `public/js/views/` | страницы приложения |
| `public/js/components/` | переиспользуемые компоненты |
| `public/js/texts.js` | фиксированные тексты интерфейса |
| `public/widget.js` | загрузчик виджета |

## Проверки

```bash
npm run check
node --check public/js/app.js
node --check server/index.js
```

Правила разработки находятся в [CodeStyle.md](../CodeStyle.md).
