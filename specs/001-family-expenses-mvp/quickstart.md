# Quickstart: проверка сквозных сценариев

## Предпосылки

- Docker + docker compose.
- `.env` по образцу `.env.example`:
  - `POSTGRES_*` — доступ к PostgreSQL (в compose задаётся сам);
  - `LISTEN_ADDR=:8080`;
  - `LLM_BASE_URL`, `LLM_API_KEY`, `LLM_MODEL`, `LLM_TEMPERATURE` — доступ к LLM (нужен для сценариев бота и evals);
  - `TELEGRAM_BOT_TOKEN` — может быть пустым (режим Web-API only);
  - `EXPENSES_LIST_LIMIT` (лимит списка панели), `DIALOG_WINDOW=16`, `DIALOG_TTL=30m`.

## Запуск

```sh
docker compose up --build
```

Поднимаются: `postgres:16`, `server` (REST `:8080` + health; применяет goose-миграции и seeding при пустом `users`), `web` (панель `:3000`, прокси `/api` → server). Замер SC-007: от команды до старта всех контейнеров ~52 с (тёплый кэш сборки; норматив — не более 5 минут с чистого клона).

## Сценарии проверки

1. **Health check и seeding**: `GET /healthz` → 200; `GET /api/v1/users` → демонстрационные члены семьи (если база была пуста).
2. **REST CRUD** (`curl`, см. `api/openapi.yaml`):
   - `POST /api/v1/expenses` `{"user_id":1,"amount_rub":"350.50","category":"food","spent_at":"2026-10-17"}` → 201, в ответе `amount_minor: 35050`, `category_name: "еда"`;
   - `POST` с `"category":"жилье"` → сохранено как `housing` (е/ё-нормализация);
   - `GET /api/v1/expenses?date_from=2026-10-01&date_to=2026-10-31&category=food&user_id=1` → только еда пользователя 1 за октябрь (включительно);
   - `PATCH /api/v1/expenses/{id}` `{"amount_rub":"450"}` → 200, `amount_minor: 45000`, остальные поля без изменений;
   - `DELETE /api/v1/expenses/{id}` → 204; повторный `GET /api/v1/expenses/{id}` → 404 `not_found`;
   - `POST` с `"amount_rub":"0"` или `"amount_rub":"350.505"` (три знака) → 400 `invalid_amount`; с неизвестной категорией → 400 `invalid_category`.
3. **Изоляция (бот, два Telegram-аккаунта)**: у каждого видны только свои траты; попытка изменить/удалить чужой `expense_id` → `not_found`, данные другого не раскрываются (FR-011).
4. **Бот** (нужны `TELEGRAM_BOT_TOKEN` + LLM), по одному сообщению:
   - «кофе 350» → трата: 350 ₽, «еда», сегодня; пользователь появился в `/api/v1/users`;
   - «такси 1200 вчера» → 1200 ₽, «транспорт», вчера;
   - «сколько потратил на еду в этом месяце» → сумма совпадает с `get_summary` (арифметика в Go);
   - «последнее было не 350, а 450» → изменена именно последняя трата; после паузы > TTL — изменяется самая свежая по `spent_at`;
   - «удали такси» при 2+ совпадениях → 2–3 последние кандидата и ровно один уточняющий вопрос;
   - «привет» → ответ без операций с тратами.
5. **Web-API only**: `TELEGRAM_BOT_TOKEN=""` → в логах `WARN starting in Web-API only mode`, бот не запущен, REST и панель полностью работают (не ошибка конфигурации).
6. **Тесты и линт**: `make lint && make test` — unit + интеграционные против PG из compose (scoping, конвертация денег 350/350.5/350.50/0.01/350.505→reject, е/ё, TTL памяти).
7. **Evals**: `make eval` — прогон `evals/dataset.yaml` (25–30 кейсов) на фиксированном `reference_time`; ожидание ≥ 90% (SC-002). Требует живой LLM, вне CI.

Ссылки: контракт REST — `api/openapi.yaml`; тулы агента — `contracts/agent-tools.md`; коды ошибок — `contracts/errors.md`; модель данных — `data-model.md`.
