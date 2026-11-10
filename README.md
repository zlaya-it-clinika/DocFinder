# DocFinder

DocFinder - корпоративный юридический ассистент с поиском по внутренним документам и нормативной базе. Сервис отвечает только на основании найденных положений, показывает источники, учитывает редакции документов и сообщает, когда данных для ответа недостаточно.

## Возможности

- юридический чат с проверяемыми цитатами;
- гибридный поиск BM25, embeddings и reranking;
- переход из ответа к конкретному положению документа;
- загрузка TXT, PDF и DOCX через админ-панель;
- автоматическая переиндексация базы знаний;
- Ollama и OpenAI-compatible провайдеры;
- пользователи, роли, журнал действий, LDAP и Kerberos;
- адаптивный веб-интерфейс и встраиваемый виджет;
- локальная работа без передачи документов сторонним сервисам при использовании Ollama.

## Архитектура

```text
Браузер
   |
   v
web, Node.js BFF
   |
   v
core, Go API ----------> PostgreSQL + pgvector
   |
   v
engine, Python --------> knowledge-base + кэш векторов
   |
   v
Ollama или OpenAI-compatible API
```

- `web` хранит защищённую cookie-сессию, проверяет CSRF и проксирует API;
- `core` управляет пользователями, ролями, чатами, документами и настройками;
- `engine` разбирает документы, выполняет поиск и формирует проверенный ответ;
- `knowledge-base` содержит стартовую базу документов;
- `compose.yaml` является единственной конфигурацией полного запуска.

Браузер не получает `ENGINE_TOKEN`, пароль ядра и ключ внешнего API.

## Структура репозитория

| Путь | Назначение |
|---|---|
| `web/` | интерфейс, BFF и встраиваемый виджет |
| `core/` | Go API, PostgreSQL, права и управление документами |
| `engine/` | поиск, работа с моделями и проверка ответов |
| `knowledge-base/` | стартовые документы |
| `compose.yaml` | запуск всех сервисов |
| `.env.example` | пример конфигурации |
| `CodeStyle.md` | правила разработки проекта |

## Требования

Для полного запуска нужны:

- Docker Engine или Docker Desktop;
- Docker Compose v2;
- минимум 8 ГБ RAM для небольших локальных моделей;
- Ollama на хосте, Ollama в compose или внешний OpenAI-compatible API.

Для разработки без Docker:

- Go 1.24 или новее;
- Node.js 22 или новее;
- Python 3.12;
- PostgreSQL 16 с pgvector для live-режима core.

## Быстрый запуск

Linux и macOS:

```bash
cp .env.example .env
```

PowerShell:

```powershell
Copy-Item .env.example .env
```

Заполните в `.env` три разные случайные строки:

```env
DOCS_PASSWORD=
ENGINE_TOKEN=
POSTGRES_PASSWORD=
```

Запустите сервисы:

```bash
docker compose up -d --build
docker compose ps
```

Откройте `http://localhost:3000`. Первая созданная учётная запись получает права администратора.

Внешние порты по умолчанию:

- web: `http://localhost:3000`;
- core debug API: `http://127.0.0.1:8080`;
- PostgreSQL и engine доступны только внутри compose-сети.

## Настройка моделей

### Ollama на компьютере

```env
AI_PROVIDER=ollama
OLLAMA_URL=http://host.docker.internal:11434
OLLAMA_CHAT_MODEL=qwen2.5:7b
OLLAMA_EMBED_MODEL=bge-m3
OLLAMA_EMBED_DIMENSIONS=1024
```

Загрузите модели до запуска DocFinder:

```bash
ollama pull qwen2.5:7b
ollama pull bge-m3
```

### Ollama в Docker

```env
AI_PROVIDER=ollama
OLLAMA_URL=http://ollama:11434
```

```bash
docker compose --profile ollama up -d --build
docker compose logs -f ollama-models
```

### OpenAI-compatible API

```env
AI_PROVIDER=openai
LLM_BASE_URL=https://provider.example/v1
LLM_API_KEY=replace-me
LLM_MODEL=model-name
EMB_MODEL=embedding-model-name
RERANKER_MODEL=
```

Провайдер должен поддерживать `POST /chat/completions` и `POST /embeddings`. Endpoint `/rerank` необязателен. Настройки также доступны в разделе `Админ-панель -> Модели ИИ`. Сохранённый API-ключ не возвращается в браузер.

## Работа с документами

Стартовые документы читаются из `knowledge-base/`. Документы из админ-панели сохраняются в PostgreSQL и передаются engine для фоновой переиндексации.

Поддерживаемые форматы:

- `.txt`;
- `.pdf`;
- `.docx`.

Максимальный размер задаётся переменной `MAX_UPLOAD_MB`.

Базовый сценарий проверки:

1. Создайте первого администратора.
2. Откройте новый чат.
3. Задайте вопрос по документу из базы.
4. Проверьте прямой ответ и список источников.
5. Откройте цитату и перейдите к подсвеченному положению.
6. Задайте вопрос, ответа на который в базе нет, и проверьте обоснованный отказ.

## Разработка

### Web

```bash
cd web
npm start
npm run dev
npm run check
```

### Core

```bash
cd core
go run ./cmd/api
go test ./...
go build ./...
```

Для разработки интерфейса без PostgreSQL и модели запустите core с `API_MODE=stub`.

### Engine

```bash
cd engine
python -m venv .venv
```

Linux и macOS:

```bash
. .venv/bin/activate
pip install -r requirements.txt
python -m unittest discover -s tests -t .
```

PowerShell:

```powershell
.venv\Scripts\Activate.ps1
pip install -r requirements.txt
python -m unittest discover -s tests -t .
```

## Проверка состояния

```bash
docker compose ps
docker compose logs -f engine api web
```

Health endpoints:

- web: `http://localhost:3000/healthz`;
- core: `http://127.0.0.1:8080/healthz`;
- engine: `http://engine:8000/health` внутри compose-сети.

## Основные переменные

| Переменная | Назначение |
|---|---|
| `DOCS_PASSWORD` | пароль отладочной страницы core |
| `ENGINE_TOKEN` | внутренний токен core -> engine |
| `POSTGRES_PASSWORD` | пароль PostgreSQL |
| `AI_PROVIDER` | `ollama` или `openai` |
| `LLM_BASE_URL`, `LLM_API_KEY` | внешний OpenAI-compatible API |
| `LLM_MODEL`, `EMB_MODEL`, `RERANKER_MODEL` | модели ответа, embeddings и reranking |
| `OLLAMA_*` | адрес и модели Ollama |
| `WEB_PORT` | внешний порт web |
| `ENABLE_SIGNUP`, `DEFAULT_USER_ROLE` | регистрация и роль новых пользователей |
| `ENABLE_LDAP`, `LDAP_*` | подключение LDAP или Active Directory |
| `COOKIE_SECURE`, `COOKIE_SAMESITE` | параметры cookie |

Полный список находится в `.env.example`.

## Безопасность

- изменяющие запросы web требуют CSRF-токен и допустимый Origin;
- сессия браузера хранится в `httpOnly` cookie;
- секреты не попадают в web и не должны коммититься;
- контейнеры приложения работают без root и без Linux capabilities;
- база знаний монтируется в engine только для чтения;
- внешние API-ключи хранятся в core и передаются engine по внутренней сети;
- перед коммитом проверяйте `git status` и отсутствие локальных `.env` файлов.

## Контрибьюторы

- `PotJoke` - Go core, API и хранение данных;
- `Syharik3316` - web, интерфейс и интеграция;
- `rodyaisdead` - поиск и обработка документов.

## Правила разработки

Перед изменениями прочитайте [CodeStyle.md](CodeStyle.md). Коммит создаётся только после успешных тестов и только от лица пользователя. Сообщение коммита должно содержать список изменений.
