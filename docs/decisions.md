# Решения (ADR)

Правило - конституция VII: нетривиальное решение фиксируется в этом же коммите.

## ADR-001. Единый Go-сервис вместо раздельных бэкендов

- **Context**: REST для панели и Telegram-бот должны оперировать одними тратами, правами и правилами валидации; нагрузка — одна семья.
- **Decision**: один процесс (`cmd/server/main.go`): HTTP REST + Long Polling бота поверх общего `internal/service`; оркестратор агента — отдельный пакет `internal/agent` (вне `adapter/`).
- **Consequences**: единый домен и один compose-сервис; изоляция сбоев — `recover()` в горутине бота. Минус: общий процесс перезапускается целиком (приемлемо).

## ADR-002. ogen вместо oapi-codegen

- **Context**: REST веб-панели генерируется из контракта `api/openapi.yaml` (OpenAPI 3.0.3), а не описывается кодом вручную.
- **Decision**: `ogen` (версия в `go.mod`): типизированный сервер, свой роутер и валидация запросов, сгенерированная по схеме (required, enum, минимум/максимум, pattern) — без загрузки схемы в рантайме. Повторная генерация даёт пустой diff (CI проверяет `git diff --exit-code`). Маппинг доменных ошибок в HTTP — один хелпер.
- **Consequences**: не нужны chi/echo и внешние middleware валидации (kin-openapi в рантайме); JSON — без рефлексии. Цена: ogen требователен к строгости описания `openapi.yaml`. oapi-codegen (включая strict-server) отклонён: валидация всё равно внешним middleware, роутер — отдельный.

## ADR-003. pgx/v5 + sqlc, миграции goose

- **Context**: PostgreSQL 16; нужна типобезопасная работа с SQL без ORM и прозрачные миграции.
- **Decision**: pgx/v5 (пул), запросы — sqlc (типобезопасно, проверяется компилятором), миграции — goose SQL (embed). `amount_minor BIGINT` + CHECK по категории на уровне БД.
- **Consequences**: SQL явный, домен чистый. GORM/ent отклонены (утечка модели БД в домен, рефлексия); golang-migrate равноценен, но goose проще в go-тулинге.

## ADR-004. Telegram: минимальный Long Polling клиент на net/http

- **Context**: нужны только `getMe`, `getUpdates`, `sendMessage`; публичный хост для webhook недоступен локально.
- **Decision**: собственный минимальный клиент Bot API (long poll), горутина с `recover()`; пустой `TELEGRAM_BOT_TOKEN` — WARN и режим Web-API only.
- **Consequences**: нет SDK-зависимости, полный контроль таймаутов. SDK (go-telegram-bot-api/telebot) отклонены: лишние абстракции ради трёх методов.

## ADR-005. LLM: минимальный OpenAI-совместимый клиент на net/http

- **Context**: конфиг `LLM_BASE_URL`/`LLM_API_KEY`/`LLM_MODEL`; требуется tool calling с контролем парсинга аргументов (`json.Number` для `amount_rub`).
- **Decision**: собственный клиент `chat/completions` + tools; каждый вызов логируется slog. Цикл оркестратора — до 4 итераций.
- **Consequences**: нет зависимости, точный контроль границы «рубли -> копейки». go-openai отклонён: промежуточные типы и слабый контроль парсинга аргументов тулов.

## ADR-006. Память диалога in-memory

- **Context**: ссылка «последнее» и кандидаты удаления требуют краткосрочного контекста; процесс один (ADR-001).
- **Decision**: in-memory map по chat_id: окно 8 сообщений (включая результаты тулов), TTL 30 мин, параметры настраиваются. Потеря при рестарте допустима — fallback: последняя трата по `created_at`.
- **Consequences**: нулевая инфраструктура. Redis отклонён (лишний сервис ради несуществующей персистентности).

## ADR-007. Интеграционные тесты против PostgreSQL из docker compose

- **Context**: scoping, upsert, CHECK-ограничения требуют реального SQL; БД и так есть в compose.
- **Decision**: `make test` использует PostgreSQL из docker compose (миграции применяет тестовый раннер); testcontainers не добавляем.
- **Consequences**: меньше зависимостей (конституция VII). Минус: для тестов нужен запущенный compose-сервис БД (принято, скрипт в Makefile).

## ADR-008. Монтирование ogen-сервера под /api/v1 через stdlib mux

- **Context**: контракт объявляет `servers[0].url: /api/v1`. Спайк на `api/openapi.yaml`: сгенерированные роуты регистрируются дословно по `paths` (`pathPattern = "/expenses"`, `"/users"`), строки `api/v1` в сгенерированном коде нет — `servers` для серверных роутов ogen игнорирует. Smoke test монтирования: `/api/v1/users` -> 200, `/api/v1/expenses/1` -> 200, `/expenses` (без префикса) -> 404, `/healthz` -> 200.
- **Decision**: ogen-хендлер монтируется в stdlib mux: `mux.Handle("/api/v1/", http.StripPrefix("/api/v1", ogenServer))`; служебные эндпоинты (`GET /healthz`) регистрируются там же на stdlib mux — ровно его роль по AGENTS.md.
- **Consequences**: наружу видны только `/api/v1/*` и `/healthz`; префикс в openapi.yaml остаётся контрактом для клиентов (типы web, прокси панели). При обновлении ogen, если появится поддержка префикса из `servers`, — перепроверить спайком и упростить монтирование.
