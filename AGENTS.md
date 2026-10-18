# family-expenses-bot

## Правила коммуникации

- Общайся сухо и лаконично. 
- Комментарии объясняют «почему», а не «что». Не комментировать очевидный код.
- Ошибки: конкретные коды домена (`invalid_amount`, `invalid_category`, `invalid_date`, `not_found`, `bad_request`, `internal`), тексты для пользователя - из `specs/001-family-expenses-mvp/contracts/errors.md`. Никаких абстрактных «something went wrong».

## Ограничения

- **Сгенерированный код - только для чтения.** Никогда не править вручную вывод `ogen` и `sqlc`. Запускать `make generate`.
- **Деньги - только `int64` копейки (`amount_minor`).** Никаких `float64` и `math.Round`. Конвертация входных рублей - исключительно через `domain.RublesToKopecks`; REST-ответ отдаёт `amount_minor` (int).
- **Права проверяет только `internal/service`.** `user_id` не передаётся в промпт и JSON-схемы тулов - в боте он идёт строго через `context.Context` (`UserScope`), в веб-панели действует `FamilyHeadScope` (вся семья, `user_id` - опциональный фильтр).
- **Категории - slug внутри, русский снаружи.** В БД, домене и схемах тулов - 7 английских slug'ов. Любой вход проходит через `domain.NormalizeCategory` (`е` -> `ё`). В ответах бота и UI только русские названия.
- **Время - только через `Clock`.** Вызов `time.Now()` в `internal/service` и `internal/agent` запрещён.
- **Ручной DI.** Сборка зависимостей в `cmd/server/main.go`, без `wire`/`fx`. Новые внешние пакеты - только с записью в `docs/decisions.md`.

## Правила разработки

- Изменения сфокусированные и в рамках текущей задачи из `specs/001-family-expenses-mvp/tasks.md`.
- Контракт (openapi, SQL, схемы тулов) меняется через спеку: сначала правка в `specs/` или ADR в `docs/decisions.md`, потом код.
- Никогда не делать коммиты автоматически. Не запускать `git commit` и `git push` - коммитит только человек после ревью диффа. Перед передачей диффа - `make lint && make test`.
- Тесты проверяют поведение и граничные случаи (scoping прав, копейки `350.505` -> reject, `е`/`ё`, TTL памяти), а не мокают всё ради покрытия.
- Падение горутины Telegram-поллинга перехватывается `recover()` и не роняет HTTP-сервер. Пустой `TELEGRAM_BOT_TOKEN` - штатный старт в режиме Web-API only (`WARN` в лог).

## Команды

```
make build       # сборка
make test        # unit/integration тесты
make lint        # golangci-lint
make generate    # кодогенерация (ogen, sqlc)
make eval        # evals с реальным LLM, тег //go:build eval
make run         # локальный запуск
```
