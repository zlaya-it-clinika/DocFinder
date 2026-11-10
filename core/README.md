# DocFinder Core

Core - HTTP API DocFinder на Go. Компонент отвечает за пользователей, роли, сессии чата, документы, настройки моделей, журнал действий и связь с engine.

Ответственный: PotJoke.

## Запуск

Полный проект запускается из корня:

```bash
docker compose up -d --build
```

Отдельный запуск для разработки:

```bash
cp .env.example .env
go run ./cmd/api
```

PowerShell:

```powershell
Copy-Item .env.example .env
go run ./cmd/api
```

Требуется Go 1.24 или новее.

## Режимы

| `API_MODE` | Назначение |
|---|---|
| `live` | PostgreSQL, документы, engine и модель |
| `stub` | данные в памяти и фиксированные ответы для разработки web |

В stub-режиме доступны пользователи `admin` / `admin` и `user` / `user`, если `STUB_SEED_USERS=true`.

## Основные endpoints

- `/auth/*` - вход и настройки аутентификации;
- `/sessions/*` - чаты и сообщения;
- `/documents/*` - загрузка, редакции и структура документов;
- `/ai/*` - настройки и проверка провайдера моделей;
- `/users`, `/roles`, `/permissions` - доступ пользователей;
- `/audit` - журнал действий;
- `/organization`, `/sources` - настройки организации и источников;
- `/healthz` - состояние API;
- `/docs` - ручная проверка API с паролем `DOCS_PASSWORD`.

## PostgreSQL

Core использует PostgreSQL 16 и pgvector. Миграции из `migrations/` идемпотентны и применяются при запуске.

```env
API_MODE=live
DATABASE_URL=postgres://docs:password@localhost:5432/docfinder?sslmode=disable
ENGINE_URL=http://localhost:8000
ENGINE_TOKEN=replace-me
```

## Engine

Обычный юридический вопрос передаётся в Python engine вместе с историей диалога и настройками модели. После загрузки, импорта или удаления документа core запускает переиндексацию.

Если engine временно недоступен, core возвращает безопасный ответ без выдуманных фактов и пишет ошибку в debug-лог.

## Проверки

```bash
gofmt -w internal cmd
go test ./...
go build ./...
```

## Правила

Код и комментарии должны соответствовать [CodeStyle.md](../CodeStyle.md). Секреты нельзя хранить в исходниках и логах.
