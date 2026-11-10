# DocFinder Engine

Engine - поисковый и генеративный контур DocFinder на Python. Компонент разбирает юридические документы, строит индекс, находит применимые положения, формирует ответ модели и проверяет ссылки и числа.

Ответственный за поисковую часть: rodyaisdead.

## Возможности

- TXT, Markdown, DOCX и ZIP с документами;
- разбор договоров, законов, дополнительных соглашений и редакций;
- BM25, embeddings, RRF и необязательный reranking;
- защита от инструкций внутри документов;
- проверка чисел и ссылок в ответе;
- обоснованный отказ при недостатке данных;
- HTTP API для core;

## Установка

```bash
python -m venv .venv
```

Linux и macOS:

```bash
. .venv/bin/activate
pip install -r requirements.txt
```

PowerShell:

```powershell
.venv\Scripts\Activate.ps1
pip install -r requirements.txt
```

## Запуск HTTP API

```env
ENGINE_HOST=127.0.0.1
ENGINE_PORT=8000
ENGINE_TOKEN=replace-me
ENGINE_KB_PATH=../knowledge-base
ENGINE_CACHE_DIR=./cache
LLM_BASE_URL=http://127.0.0.1:11434/v1
LLM_MODEL=qwen2.5:7b
EMB_MODEL=bge-m3
```

```bash
python server.py
```

Endpoints:

- `GET /health`;
- `POST /answer`;
- `POST /reindex`;
- `GET /documents`;
- `GET /documents/<documentId>/structure`.

При заданном `ENGINE_TOKEN` запросы, кроме healthcheck, требуют `Authorization: Bearer <token>`.

## Основные модули

| Файл | Назначение |
|---|---|
| `kb_loader.py`, `kb_parse.py` | загрузка и разбор документов |
| `index.py`, `retrieval.py` | индекс и поиск |
| `llm_client.py` | OpenAI-compatible API |
| `pipeline.py` | полный цикл ответа |
| `verify.py`, `formatter.py` | проверка и форматирование |
| `server.py` | HTTP API |

## Проверки

```bash
python -m unittest discover -s tests -t .
```

Дополнительные `debug_*.py` предназначены для локальной диагностики поиска и ответов. Секреты задаются только через переменные окружения и не должны попадать в логи.

Правила разработки находятся в [CodeStyle.md](../CodeStyle.md).
