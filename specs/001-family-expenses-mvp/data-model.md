# Data Model: Сервис учёта семейных расходов

## users

| Поле | Тип (PG) | Ограничения |
|------|----------|-------------|
| id | BIGINT | PK; совпадает с Telegram ID |
| username | TEXT | NULL |
| first_name | TEXT | NOT NULL |
| created_at | TIMESTAMPTZ | NOT NULL |
| updated_at | TIMESTAMPTZ | NOT NULL |

**Upsert** (при каждом сообщении боту, FR-007): `INSERT … ON CONFLICT (id) DO UPDATE SET username = EXCLUDED.username, first_name = EXCLUDED.first_name, updated_at = now()`.

## expenses

| Поле | Тип (PG) | Ограничения |
|------|----------|-------------|
| id | BIGINT | PK, `GENERATED ALWAYS AS IDENTITY` |
| user_id | BIGINT | NOT NULL, FK → `users(id)` |
| amount_minor | BIGINT | NOT NULL, `CHECK (amount_minor > 0)` — целые копейки |
| category | TEXT | NOT NULL, `CHECK (category IN ('food','transport','housing','entertainment','health','shopping','other'))` |
| spent_at | DATE | NOT NULL — календарная дата, без времени |
| comment | TEXT | NULL; домен ограничивает 500 символами |
| created_at | TIMESTAMPTZ | NOT NULL |
| updated_at | TIMESTAMPTZ | NOT NULL |

**Индексы**:
- `(user_id, spent_at)` — фильтры панели и сводки по периоду/категории;
- `(user_id, created_at DESC)` — «последняя трата» (fallback памяти диалога, FR-009).

## Category — справочник домена (не таблица)

| slug | русское название |
|------|------------------|
| food | еда |
| transport | транспорт |
| housing | жильё |
| entertainment | развлечения |
| health | здоровье |
| shopping | покупки |
| other | другое |

- Внутрь (БД, домен, схемы тулов) — только slug; наружу (ответы бота, `category_name` в REST) — только русское название.
- `NormalizeCategory`: trim → lower → е→ё → словарь синонимов → slug; нераспознанное → `invalid_category`.

## DialogMemory — in-memory (не в БД)

| Аспект | Значение |
|--------|----------|
| Ключ | chat_id Telegram |
| Состав | окно 8 последних сообщений (роль + содержимое; результаты тулов считаются) + упомянутые `expense_id` (последняя трата, кандидаты удаления) |
| TTL | 30 мин с последней активности (настраивается); истёк — окно сбрасывается |
| Назначение | резолвинг «последнее» (FR-009) и выбор кандидата при удалении с несколькими совпадениями (FR-010) |
| Fallback | память пуста/истекла → последняя трата пользователя по `created_at DESC LIMIT 1` |

## Связи

- `users 1 — N expenses` (по `user_id`).

## Правила валидации (домен, едины для REST, тулов и бота)

1. **Деньги**: рубли → копейки только через `RublesToKopecks` (парсинг `json.Number`/строки: рубли и копейки отдельно, без float64 и math.Round). Больше двух знаков после точки, ноль или отрицательное → `invalid_amount`. Границы: REST-запросы (POST/PATCH) — `amount_rub` десятичной строкой; схемы тулов LLM — `amount_rub` (number); хранение и REST-ответы — `amount_minor` (int64).
2. **Категория**: `NormalizeCategory` → slug из 7; иначе → `invalid_category`.
3. **Дата**: строгий формат `YYYY-MM-DD`; диапазоны трактуются включительно; будущие даты не отклоняются (assumption спеки).
4. **Комментарий**: опционален, ≤ 500 символов.
5. **Права**: бот — строго свои траты (`user_id` из ctx, чужой id → `not_found`); панель — все траты семьи, `user_id` как опциональный фильтр. Проверка — только в `internal/service`.

## Состояния

Жизненный цикл траты отсутствует (нет статусов): только создание/изменение/удаление с служебными `created_at`/`updated_at`. Переходы состояний не применимы.
