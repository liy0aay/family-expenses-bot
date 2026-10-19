# Tasks: Сервис учёта семейных расходов (бот + веб-панель)

**Input**: Design documents from `specs/001-family-expenses-mvp/` (plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md)

**Prerequisites**: plan.md, spec.md; конституция (`.specify/memory/constitution.md`) — каждый дифф сверять с принципами I–VII.

**Format**: `- [ ] T### [P?] [US?] Описание (путь к файлу — закрывает: …)`; **[P]** = параллелизуемо, **[USx]** = user story из spec.md.

Группировка — по этапам из запроса; US-разметка сохранена для трассировки (US1–US4 — бот, US5–US6 — панель).

## Этап 1. Каркас, миграции goose, sqlc и домен

**Цель**: компилируемый каркас + домен с деньгами/категориями/временем + хранилище + сервис с правами. Блокирует всё остальное.

- [x] T001 Создать каркас по plan.md: `go.mod` (go 1.27), каталоги `cmd/server/`, `internal/{config,clock,domain,service,agent,adapter,storage}`, `.env.example` со всеми переменными и `Makefile` (build/lint/test/generate/eval/run) — закрывает: базу для FR-020, FR-021
- [x] T002 [P] Конфиг из ENV: `POSTGRES_*`, `LISTEN_ADDR`, `LLM_BASE_URL/LLM_API_KEY/LLM_MODEL/LLM_TEMPERATURE`, `TELEGRAM_BOT_TOKEN`, `EXPENSES_LIST_LIMIT`, `DIALOG_WINDOW=8`, `DIALOG_TTL=30m` в `internal/config/config.go` — закрывает: FR-021, FR-022
- [x] T003 [P] Домен категорий: справочник 7 slug↔русское название и `NormalizeCategory` (trim → lower → е→ё → slug; неизвестное → ErrInvalidCategory) в `internal/domain/category.go` — закрывает: FR-005, FR-006
- [x] T004 [P] Деньги: `RublesToKopecks` — парсинг `json.Number`/строки (рубли и копейки отдельно, без float64 и math.Round; >2 знаков после точки или ≤0 → ErrInvalidAmount) в `internal/domain/money.go` — закрывает: FR-004
- [x] T005 [P] Ошибки домена (ErrInvalidAmount/ErrInvalidCategory/ErrInvalidDate/ErrNotFound/ErrValidation/ErrInternal по contracts/errors.md) в `internal/domain/errors.go` + интерфейс `Clock` с RealClock/FixedClock в `internal/clock/clock.go` — закрывает: FR-014; принципы V, VII
- [x] T006 Миграция goose `internal/storage/migrations/0001_init.sql`: `users` + `expenses` (`amount_minor BIGINT CHECK (amount_minor > 0)`, `category TEXT CHECK IN 7 slug'ов`, `spent_at DATE`, индексы `(user_id, spent_at)` и `(user_id, created_at DESC)`) — закрывает: FR-001 (data-model.md)
- [x] T007 sqlc: `internal/storage/postgres/query.sql` + `sqlc.yaml` — upsert пользователей (ON CONFLICT), CRUD трат, фильтры (user_id/date_from/date_to/category), сводка (SUM/COUNT), «последняя» по created_at LIMIT 1 — закрывает: FR-001, FR-007, FR-008, FR-009
- [x] T008 Пул pgx/v5 и применение goose-миграций при старте в `internal/storage/postgres/postgres.go` — закрывает: FR-020
- [x] T009 [US1] [P] Тесты `internal/domain/money_test.go` и `category_test.go`: суммы 350, 350.5, 350.50, 0.01 — ок; 350.505 → invalid_amount; е/ё («жилье»/«Жильё» → housing); неизвестная категория → ошибка — закрывает: US1, FR-004, FR-006; SC-003
- [x] T010 [US1] `internal/service/expense.go` (+`user.go`): Create/List/Get/Patch/Delete/Summary/Last с валидацией домена и правами в одном месте: `UserScope` (бот — строго свой user_id из ctx) и `FamilyHeadScope` (панель — все траты, user_id как опциональный фильтр); чужая трата → not_found — закрывает: US1–US6, FR-011, FR-019; принцип I
- [x] T011 [US1] [P] Тесты scoping `internal/service/expense_test.go` (PG из compose): бот видит только своё, панель — всё; обращение к чужому id → not_found без раскрытия данных — закрывает: FR-011, FR-019; SC-004
- [x] T012 ogen в прод-режиме: зафиксировать версию в `go.mod`, таргет `make generate` из `api/openapi.yaml`, повторная генерация даёт пустой diff; монтирование по итогам спайка (ADR-008): `mux.Handle("/api/v1/", http.StripPrefix("/api/v1", ogenServer))` + `GET /healthz` на stdlib mux — закрывает: FR-017; ADR-002, ADR-008

**Checkpoint**: `make test` зелёный на домене и scoping; схема БД и кодогенерация воспроизводимы.

## Этап 2. REST-хендлеры поверх ogen-сервера

**Цель**: панель получает рабочий REST: список с фильтрами и полный CRUD. Зависит от T010, T012.

- [x] T013 [US5] Единый хелпер маппинга ошибок домена → HTTP (статус + `{error:{code,message}}` по contracts/errors.md; ogen-типы `*Error`) в `internal/adapter/rest/errors.go` — закрывает: FR-014, FR-019
- [x] T014 [US5] Хендлеры чтения: `GET /users` и `GET /expenses` (фильтры user_id/date_from/date_to/category + limit из конфига; FamilyHeadScope; `category_name` из справочника домена) в `internal/adapter/rest/handlers.go` — закрывает: US5, FR-015, FR-016
- [x] T015 [US6] Хендлеры записи: `POST /expenses` (обязательный user_id — выбор члена семьи), `GET/PATCH/DELETE /expenses/{expense_id}` через FamilyHeadScope в `internal/adapter/rest/handlers.go` — закрывает: US6, FR-017, FR-018
- [x] T016 [US5] [P] Контракт-тесты `internal/adapter/rest/rest_test.go` (httptest + PG из compose): CRUD e2e, комбинация фильтров, е/ё в query `category`, `amount_rub` «0» и «350.505» → 400 invalid_amount, неизвестная категория → 400, чужой/несуществующий id → 404 not_found; фильтры и лимит — в SQL, без постфильтрации (SC-005: code review запросов sqlc + разовый EXPLAIN) — закрывает: US5, US6, FR-016–FR-019; SC-003, SC-005

**Checkpoint**: по quickstart.md сценарий 2 проходит целиком; панель может быть написана только против этого API.

## Этап 3. LLM-провайдер, агент, память, Telegram

**Цель**: бот на естественном языке: добавление/просмотр/изменение/удаление, изоляция, сообщения не о тратах. Зависит от T010 (service).

- [ ] T017 [US1] [P] OpenAI-совместимый клиент `chat/completions` + tools на `net/http` (конфиг LLM_*; логирование slog: модель, длительность, число тул-вызовов; парсинг аргументов как `json.Number`) в `internal/agent/llm.go` — закрывает: FR-021; ADR-005, принцип II
- [ ] T018 [US3] [P] In-memory память диалога по chat_id: окно 8 сообщений (включая результаты тулов), TTL 30 мин (из конфига), упомянутые expense_id («последнее», кандидаты удаления) в `internal/agent/memory/memory.go` — закрывает: US3, US4, FR-009, FR-013; ADR-006
- [ ] T019 [US3] Тесты памяти `internal/agent/memory/memory_test.go`: вытеснение за окном 8, истечение TTL → сброс, поведение «последнее» после сброса — закрывает: FR-013, FR-009
- [ ] T020 [US2] [P] Адаптеры 5 тулов по contracts/agent-tools.md → вызовы service (user_id строго через ctx): add (amount_rub → RublesToKopecks), find (текст/фильтры, created_at DESC), update, delete, get_summary (арифметика в Go); ошибки тулов назад к LLM как `{error:{code,message}}` в `internal/agent/tools/tools.go` — закрывает: US1–US4, FR-002, FR-004, FR-009, FR-010; принципы I–III
- [ ] T021 [US1] Оркестратор `internal/agent/agent.go`: системный промпт (текущая дата из Clock; без user_id), цикл tool calling ≤ 4 итераций, диалоговые правила: «последнее» через память → fallback find limit=1, удаление — 1 совпадение сразу / 2–3 последние + ровно один уточняющий вопрос, сообщения не о тратах без вызова тулов, переспрос при нехватке данных — закрывает: US1–US4, FR-002, FR-003, FR-008, FR-009, FR-010, FR-012, FR-014; принцип III
- [ ] T022 [US4] [P] Тесты оркестратора `internal/agent/agent_test.go` (fake LLM): остановка после 4 итераций с вежливым ответом; «привет/спасибо» → ни одного тул-вызова; «последнее» из памяти и по fallback; invalid_amount → траты нет, понятный ответ; удаление при 2+ совпадениях -> 2–3 кандидата и ровно один уточняющий вопрос — закрывает: US1–US4, FR-009, FR-010, FR-012; SC-006, SC-008
- [ ] T023 [US1] Telegram long polling: `getUpdates`/`sendMessage` на `net/http`, upsert пользователя на каждое сообщение, вызов агента, `recover()` в горутине polling, пустой `TELEGRAM_BOT_TOKEN` → WARN «starting in Web-API only mode» и без запуска бота в `internal/adapter/telegram/bot.go` — закрывает: US1–US4, FR-007, FR-022; ADR-004
- [ ] T024 [US1] Тесты транспорта `internal/adapter/telegram/bot_test.go` (fake Telegram API + fake agent): update → агент → ответ; isolation: чужой id → «Такой траты нет» без деталей — закрывает: US1–US4, FR-011; SC-004

**Checkpoint**: по quickstart.md сценарии 3–4 проходят: бот добавляет/показывает/меняет/удаляет, чужое — not_found, сообщения не о тратах — без операций (MVP готов).

## Этап 4. Веб-панель и seeding

**Цель**: глава семьи видит и правит траты всей семьи. Зависит от этапа 2 (REST).

- [ ] T025 [US5] Скелет `web/`: Vite + React + TS, генерация типов из `api/openapi.yaml` (openapi-typescript), минимальный fetch-слой на `/api/v1` — закрывает: US5, FR-015
- [ ] T026 [US5] Список трат всех членов с именем/username владельца, фильтры (член семьи + период date_from/date_to + категория, комбинируются), русские названия категорий и суммы в рублях в `web/src/pages/ExpensesPage.tsx` — закрывает: US5, FR-015, FR-016; SC-005
- [ ] T027 [US6] Формы создания (выбор члена семьи), редактирования и удаления + обработка ошибок по `code` из contracts/errors.md в `web/src/components/ExpenseForm.tsx` — закрывает: US6, FR-017, FR-018
- [ ] T028 [US5] [P] Seeding при старте, только если таблица `users` пуста (несколько пользователей и трат за текущий месяц), в `internal/storage/postgres/seed.go`; тест: повторный запуск не создаёт дублей — закрывает: US5, FR-020; quickstart сценарий 1

**Checkpoint**: по quickstart.md сценарии 1 и панель из браузера: фильтры работают, CRUD виден и в боте того же пользователя.

## Этап 5. Evals, docker compose, CI, README

**Цель**: воспроизводимое качество агента и «одна команда» запуска.

- [ ] T029 [P] Датасет `evals/dataset.yaml`: 25–30 кейсов — добавление/просмотр/изменение/удаление, относительные даты («вчера», «позавчера», «в этом месяце», «с 1 по 15 октября»), е/ё, сообщения не о тратах, некорректные суммы, изоляция — закрывает: FR-023; SC-002
- [ ] T030 Раннер `evals/runner_eval.go` с тегом `//go:build eval`: фиксированный reference_time через FixedClock, прогон датасета через агента, отчёт с процентом прохождения (порог ≥90%); таргет `make eval` в `Makefile` — закрывает: FR-023; SC-001, SC-002
- [ ] T031 `docker-compose.yml` + `Dockerfile` (multi-stage: статика web + сервер): postgres:16, server, web; web проксирует `/api` → server (как в quickstart); всё поднимается `docker compose up` без ручных шагов — закрывает: FR-020; SC-007
- [ ] T032 CI `.github/workflows/ci.yml`: `make lint && make test` + проверка пустого diff кодогенерации (`make generate` → `git diff --exit-code`) — закрывает: FR-017; процесс из AGENTS.md
- [ ] T033 [P] Финальный `README.md`: запуск `docker compose up`, заполнение `.env`, сценарии бота/панели/evals со ссылками на quickstart.md — закрывает: FR-020; SC-007

**Checkpoint**: `docker compose up` с чистого клона → все сценарии quickstart.md воспроизводимы.

## Dependencies & Execution Order

- **Этап 1**: T001 → {T002, T003, T004, T005} параллельно; T006 → T007 → T008; T009 после T003+T004; T010 после T003–T008; T011 после T010; T012 после T001.
- **Этап 2**: после T010+T012: T013 → T014 → T015; T016 после T015.
- **Этап 3**: после T010: T017 ∥ T018 ∥ T020; T019 после T018; T021 после T017+T018+T020; T022 ∥ T023 после T021; T024 после T023.
- **Этап 4**: после T016: T025 → T026 → T027; T028 параллельно (после T008).
- **Этап 5**: T029 после T021; T030 после T029; T031 после T015+T027; T032 после T031; T033 последним.

## Parallel Example

```bash
# После T001 (каркас) — домен и конфиг параллельно:
Task: "T002 конфиг в internal/config/config.go"
Task: "T003 категории в internal/domain/category.go"
Task: "T004 деньги в internal/domain/money.go"
Task: "T005 ошибки + Clock в internal/domain/errors.go и internal/clock/clock.go"

# Этап 3 после T010 — независимые компоненты агента:
Task: "T017 LLM-клиент в internal/agent/llm.go"
Task: "T018 память в internal/agent/memory/memory.go"
Task: "T020 тулы в internal/agent/tools/tools.go"

# Бэкенд и фронтенд в этапах 4–5:
Task: "T028 seeding в internal/storage/postgres/seed.go"
Task: "T033 README.md"
```

## Implementation Strategy

- **MVP (US1–US4, бот)**: Этап 1 (T001–T012) → Этап 3 (T017–T024) → проверить quickstart сценарии 3–4. Панель при этом ещё не нужна.
- **Второй инкремент (US5–US6, панель)**: Этап 2 (T013–T016) → Этап 4 (T025–T028) → quickstart сценарии 1–2 и панель в браузере.
- **Финал**: Этап 5 (T029–T033) — evals, compose, CI, README.
- Тесты-задачи писать до/вместе с реализацией соответствующего кода (T009, T011, T016, T019, T022, T024) и держать зелёными: `make lint && make test`.
- Интеграционные тесты (T011, T016) — против PostgreSQL из docker compose (ADR-007).

## Notes

- [P] = разные файлы, нет зависимости от незавершённых задач.
- [USx] — трассировка к spec.md; в скобках каждой задачи — закрываемые FR.
- Ограничения с data-model.md переносить в код дословно: `amount_minor > 0`, 2 знака после точки максимум, 7 slug'ов категорий, окно 8 / TTL 30 мин, комментарий ≤ 500 символов.
- Коммит после каждой задачи или логичной группы; `make lint && make test` перед коммитом; изменения контракта — только через `make generate` с пустым diff.
