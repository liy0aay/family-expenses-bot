# Implementation Plan: Сервис учёта семейных расходов (бот + веб-панель)

**Branch**: `001-family-expenses-mvp` | **Date**: 2026-10-18 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/001-family-expenses-mvp/spec.md`

## Summary

Единый Go-сервис (`cmd/server/main.go`): REST API веб-панели главы семьи (сервер и роутер генерируются ogen'ом из `api/openapi.yaml`, OpenAPI 3.0.3) + Telegram-бот на Long Polling с ИИ-агентом (OpenAI-совместимый LLM, цикл tool calling ≤ 4 итераций, 5 тулов) поверх общего доменного слоя `internal/service`. Хранилище — PostgreSQL 16 (pgx/v5 + sqlc, миграции goose): `users` и `expenses` (деньги — `amount_minor BIGINT`, категория — slug с CHECK). Диалоговая память in-memory (окно 16 сообщений, TTL 30 мин). Время — инжектируемый `Clock`. Веб-панель — React + Vite + TypeScript, типы из openapi.yaml. Всё поднимается `docker compose up`; пустой `TELEGRAM_BOT_TOKEN` — режим Web-API only (WARN, не ошибка). Качество агента проверяется eval-набором (25–30 кейсов, фиксированный reference_time).

## Technical Context

**Language/Version**: Go 1.27 (backend); TypeScript 5 + React 18 (web)

**Primary Dependencies**: pgx/v5, sqlc, ogen (сервер+роутер REST из openapi), goose (миграции), slog (логи); web: Vite, React, openapi-typescript (типы клиента из openapi.yaml)

**Storage**: PostgreSQL 16

**Testing**: `go test` — unit рядом с пакетами + интеграционные против реального PostgreSQL из docker compose (без testcontainers, ADR-007); evals — пакет `evals/` с тегом `//go:build eval` (`make eval`, вне CI)

**Target Platform**: Linux, docker compose (postgres + server + web), один процесс Go

**Project Type**: monorepo: web-service (REST) + telegram-бот + SPA-панель

**Performance Goals**: фильтрация панели — в SQL с индексами, список с настраиваемым лимитом (SC-005); ответ агента — не более 4 итераций tool calling

**Constraints**: деньги только в копейках (целые, `amount_minor`); `time.Now()` запрещён в `internal/agent` и `internal/service`; `user_id` не входит в схемы тулов и промпт; без пагинации — настраиваемый лимит списка; без авторизации; без фич сверх спеки

**Scale/Scope**: одна семья, десятки пользователей, порядка 10⁴–10⁵ трат

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| # | Принцип | Статус | Обоснование |
|---|---------|--------|-------------|
| I | Права решает домен | ✅ PASS | Проверка прав только в `internal/service`: `UserScope` (бот — строго свой `user_id` из ctx) и `FamilyHeadScope` (панель — все траты, `user_id` как опциональный query-фильтр). `user_id` отсутствует в схемах тулов и промпте ([contracts/agent-tools.md](./contracts/agent-tools.md)); хендлеры и LLM доступ не решают. |
| II | Деньги — целые | ✅ PASS | БД/домен/ответы REST — `amount_minor` BIGINT/integer ([data-model.md](./data-model.md), [api/openapi.yaml](../../api/openapi.yaml)). Запросы REST — `amount_rub` (decimal-строка) и схемы тулов LLM — `amount_rub` (number) → единственная функция `RublesToKopecks` (парсинг `json.Number`, без float64 и math.Round). |
| III | LLM не доверяем в детерминированных задачах | ✅ PASS | LLM только извлекает намерение и параметры; валидация, права и арифметика (суммы, диапазоны) — детерминированный Go-код в service. |
| IV | Категории: slug внутри, русский снаружи | ✅ PASS | В БД и схемах тулов — slug + CHECK constraint ([data-model.md](./data-model.md)); вход REST и тулов — через `NormalizeCategory` (trim, lower, е→ё); наружу — только русские названия (`category_name` в REST, тексты бота). |
| V | Время — через Clock | ✅ PASS | Инжектируемый `internal/clock.Clock` (`Now() time.Time`); `time.Now()` запрещён в `internal/agent` и `internal/service`; evals выполняются на фиксированном reference_time. |
| VI | Один запуск | ✅ PASS | `docker compose up` поднимает PG, server и web; пустой `TELEGRAM_BOT_TOKEN` — WARN «starting in Web-API only mode», бот не стартует, REST работает ([quickstart.md](./quickstart.md), сценарий 5). |
| VII | Прагматичный стек, решения записаны | ✅ PASS | Зависимости минимальны (без SDK для Telegram/LLM, без ORM, без Redis); ключевые решения зафиксированы в ADR-001…ADR-008 ([docs/decisions.md](../../docs/decisions.md)). |

**Re-check после Phase 1**: дизайн (data-model, контракты, quickstart) нарушений не добавляет — все гейты остаются PASS.

## Project Structure

### Documentation (this feature)

```text
specs/001-family-expenses-mvp/
├── plan.md              # Этот файл
├── research.md          # Phase 0: решения и альтернативы
├── data-model.md        # Phase 1: сущности, ограничения, валидации
├── quickstart.md        # Phase 1: сквозные сценарии проверки
├── contracts/
│   ├── agent-tools.md   # Контракт тулов агента (JSON-схемы, правила диалога)
│   └── errors.md        # Маппинг ошибок домена → HTTP / тексты бота
└── tasks.md             # Phase 2 (/speckit.tasks) — здесь не создаётся
```

### Source Code (repository root)

```text
api/
└── openapi.yaml            # Контракт REST (OpenAPI 3.0.3); ogen генерирует из него сервер
cmd/
└── server/
    └── main.go             # Конфиг, ручной DI, запуск REST (+ бот при заданном токене), seeding
internal/
├── config/                 # ENV: POSTGRES_*, LLM_BASE_URL/LLM_API_KEY/LLM_MODEL, TELEGRAM_BOT_TOKEN,
│                           #      LISTEN_ADDR, лимит списка, окно/TTL памяти
├── clock/                  # Интерфейс Clock + RealClock / FixedClock
├── domain/                 # Expense, User, Category (slug↔русское), ошибки домена,
│                           # RublesToKopecks, NormalizeCategory
├── service/                # ExpenseService, UserService: права (UserScope/FamilyHeadScope), валидация
├── agent/                  # Оркестратор LLM: системный промпт (дата из Clock), цикл tool calling ≤ 4
│   ├── memory/             # In-memory память диалога (окно 16, TTL 30 мин)
│   └── tools/              # Адаптеры 5 тулов → вызовы service (user_id только через ctx)
├── adapter/
│   ├── rest/               # Сгенерированный ogen-сервер, хендлеры, маппинг ошибок (один хелпер);
│                           #      монтирование под /api/v1 — через stdlib mux (ADR-008)
│   └── telegram/           # Long Polling (getUpdates/sendMessage), upsert пользователя, recover()
└── storage/
    ├── postgres/           # Пул pgx/v5, sqlc-запросы, seeding при пустом users
    └── migrations/         # SQL-миграции goose (embed)
web/                        # React + Vite + TS; типы клиента генерируются из api/openapi.yaml
evals/                      # //go:build eval; dataset.yaml (25–30 кейсов), FixedClock reference_time
docs/
└── decisions.md            # ADR-001…ADR-008
docker-compose.yml          # postgres:16 + server + web
Dockerfile, Makefile
```

Тесты — рядом с пакетами (`*_test.go`): scoping (бот видит только своё, панель — всё), конвертация денег (350 / 350.5 / 350.50 / 0.01 / 350.505 → reject), е/ё-нормализация, TTL памяти диалога.

**Structure Decision**: монолит-монорепо (ADR-001): один Go-процесс с общим доменом для REST и бота; оркестратор агента — отдельный пакет `internal/agent` (вне `adapter/`), транспорты — минимальные адаптеры; фронтенд панели — изолированный `web/`, связанный с бэкендом только контрактом `api/openapi.yaml`.

## Complexity Tracking

> Заполняется только при нарушениях Constitution Check.

Нарушений нет — таблица не заполняется.
