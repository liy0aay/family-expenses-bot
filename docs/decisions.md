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

## ADR-009. golangci-lint как tool-зависимость go.mod

- **Context**: `make lint` должен давать одинаковый результат локально и в CI (T032); системный бинарник линтера может быть собран более старым Go — установленный v1.64.4 (go1.26.3) отказывается анализировать модули `go 1.27`.
- **Decision**: версия golangci-lint v2 зафиксирована tool-директивой в `go.mod`, запуск — `go tool golangci-lint run` (цель `make lint`); бинарник собирается локальным тулчейном.
- **Consequences**: версия линтера воспроизводима без шага установки в CI и не зависит от PATH; go.mod пополняется dev-зависимостями линтера (принято, конституция VII).

## ADR-010. Веб-панель: Vite + React + TS, типы из openapi через openapi-typescript

- **Context**: панель — потребитель REST (ADR-008); нужен single-page UI c простой аналитико расходов без тяжёлых фреймворков, с типами, следующими контракту `api/openapi.yaml`.
- **Decision**: Vite + React + TypeScript. Типы ответов генерируются `openapi-typescript` (npm script `gen:api`, вход `../api/openapi.yaml`, выход `web/src/api/schema.d.ts`, коммитится). Скрипт сознательно **вне** `make generate`: CI-гейт пустого diff (T032) проверяет Go-кодогенерацию и не должен требовать node. HTTP — ручной типизированный fetch-слой `web/src/api/client.ts` над `/api/v1` с разбором `{error:{code,message}}`, без кодогенератора клиента. Без router и react-query: одна страница, состояние — useState/useEffect, перезагрузка списка после мутаций. Разработка — vite dev с прокси `/api` → `http://localhost:8080` (дефолтный `LISTEN_ADDR` из `internal/config`).
- **Consequences**: минимум зависимостей (react, react-dom + devDeps vite/@vitejs/plugin-react/typescript/@types/openapi-typescript). Правки `openapi.yaml` не ловятся Go-гейтом — после изменения контракта нужен `npm run gen:api` (иначе типы панели отстают). Список категорий дублируется в `web/src/api/categories.ts` — синхронизировать с `internal/domain/category.go`.

## ADR-011. Evals: датасет в YAML, парсинг yaml.v3, свой загрузчик .env

- **Context**: T029/T030 — декларативный датасет `evals/dataset.yaml` и раннер с тегом `eval`; нужен разбор YAML без ручного парсинга, конфиг LLM берётся из `.env`.
- **Decision**: парсинг датасета — `gopkg.in/yaml.v3` (строгий режим `KnownFields`, опечатки в ключах кейса ловятся на старте); пакет уже был в графе зависимостей, стал прямым. `.env` читает свой мини-загрузчик в раннере (~20 строк, setdefault-семантика) — gotenv/viper не добавляем. Прогон подмножества кейсов — `EVALS_ONLY=id1,id2`. reference_time (2026-10-15, полдень UTC) задаётся в раннере; относительные даты кейсов резолвятся от него. Датасет — упорядоченный сценарий: add-кейсы chat 1001 строят фикстуру для update/delete/summary, порядок кейсов в файле менять нельзя. Retry транспортного сбоя LLM (`LLMFailText`) повторяет кейс до 2 раз с зачисткой строк, созданных неудачной попыткой (`id > max` на старте попытки); обновления существующих строк и память диалога не откатываются.
- **Consequences**: одна новая прямая зависимость (go.mod); загрузчик .env поддерживает только плоские `KEY=VALUE` — этого достаточно для формата `.env.example`.

## ADR-012. Доставка: один Dockerfile с target'ами server/web, nginx-прокси панели

- **Context**: FR-020/SC-007 — `docker compose up` с чистого клона без ручных шагов; web — статика Vite, server — Go-бинарник с embed-миграциями.
- **Decision**: один `Dockerfile` с именованными target'ами: `server` (golang:1.27-alpine → CGO_ENABLED=0 → runtime alpine + ca-certificates/tzdata, non-root) и `web` (node:20-alpine, `npm ci` + `npm run build` → nginx:alpine со статикой и прокси `/api` → `server:8080`; путь сохраняется — server монтирует ogen по `/api/v1`, ADR-008). Миграции работают из образа без копирования каталога — goose SQL вкомпилирован через `embed` (`internal/storage/migrations`). Compose читает `.env` (опционально, чистый клон работает без него) и перекрывает внутрисетевые параметры (`POSTGRES_HOST=postgres`); host-порт PostgreSQL — `55450` по умолчанию, переопределяется `POSTGRES_HOST_PORT` (порт занят dev-БД локально). Пустой `TELEGRAM_BOT_TOKEN` — штатный режим Web-API only (FR-022).
- **Consequences**: один файл сборки на два образа; `POSTGRES_HOST_PORT` — единственная переменная, существующая только для compose (в конфиг приложения не попадает). Сид демо-семьи выполняется только на пустой БД (`internal/storage/postgres/seed.go`), volume `pgdata` переживает `compose down`.
