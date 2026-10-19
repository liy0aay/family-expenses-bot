# Контракт тулов агента (internal/agent/tools)

5 тулов, схемы — OpenAI function calling (JSON Schema). Вызов — по одному за итерацию; цикл оркестратора — до 4 итераций (лимит на сообщение).

## Общие правила

1. **Права (конституция I)**: `user_id` отсутствует во всех схемах и в промпте. Владелец передаётся адаптером бота через `context.Context` (`UserScope`); проверка прав — только в `internal/service`.
2. **Деньги (конституция II)**: в схемах сумма — `amount_rub` (number). Адаптер конвертирует единственной функцией `RublesToKopecks` (парсинг `json.Number`, рубли и копейки отдельно, без float64 и math.Round; > 2 знаков → `invalid_amount`).
3. **Категории (конституция IV)**: в схемах — slug (enum). Если LLM всё же прислала русское название, адаптер пропускает его через `NormalizeCategory`. Наружу — только русские названия.
4. **Даты**: текущая дата (из `Clock`) передаётся в системном промпте; LLM конвертирует относительные выражения («вчера», «в этом месяце», «с 1 по 15 октября») в конкретные даты `YYYY-MM-DD`; диапазоны — включительно; service валидирует формат.
5. **Ошибки тулов** возвращаются LLM как результат `{error: {code, message}}` (коды из [errors.md](./errors.md)) — агент вежливо пересказывает пользователю, не повторяя слепо вызов.
6. **Сообщения не о тратах** («привет», «спасибо») — агент отвечает финальным текстом без вызова тулов (FR-012).

## add_expense

Создать трату. Результат: созданная трата (`id`, `amount_minor`, `category`, `spent_at`, `comment`).

```json
{
  "type": "object",
  "properties": {
    "amount_rub":  { "type": "number", "description": "Сумма в рублях, не более двух знаков после запятой" },
    "category":    { "type": "string", "enum": ["food", "transport", "housing", "entertainment", "health", "shopping", "other"] },
    "spent_at":    { "type": "string", "description": "Дата траты YYYY-MM-DD; относительные слова приведи к дате от текущей даты" },
    "comment":     { "type": "string", "description": "Короткий комментарий, опционально" }
  },
  "required": ["amount_rub", "category", "spent_at"]
}
```

## find_expenses

Найти траты пользователя. Сортировка: `spent_at DESC, id DESC`. Результат: список трат (ограничен `limit`, по умолчанию 10).

```json
{
  "type": "object",
  "properties": {
    "date_from": { "type": "string", "description": "YYYY-MM-DD, включительно; опционально" },
    "date_to":   { "type": "string", "description": "YYYY-MM-DD, включительно; опционально" },
    "category":  { "type": "string", "enum": ["food", "transport", "housing", "entertainment", "health", "shopping", "other"], "description": "Опционально" },
    "text":      { "type": "string", "description": "Поиск по комментарию/слову пользователя (например, «такси»); детерминированный матчинг в service; опционально" },
    "limit":     { "type": "integer", "minimum": 1, "maximum": 10, "description": "По умолчанию 10" }
  }
}
```

Использование: «покажи траты за неделю»; подбор кандидатов для изменения/удаления; резолвинг «последнее» при пустой памяти (`limit: 1`).

## update_expense

Изменить трату по `id`. Переданные поля меняются, остальные сохраняются. Результат: обновлённая трата.

```json
{
  "type": "object",
  "properties": {
    "expense_id":  { "type": "integer", "description": "ID траты (из контекста диалога или результата find_expenses)" },
    "amount_rub":  { "type": "number", "description": "Новая сумма в рублях; опционально" },
    "category":    { "type": "string", "enum": ["food", "transport", "housing", "entertainment", "health", "shopping", "other"], "description": "Опционально" },
    "spent_at":    { "type": "string", "description": "YYYY-MM-DD; опционально" },
    "comment":     { "type": "string", "description": "Опционально" }
  },
  "required": ["expense_id"]
}
```

## delete_expense

Удалить трату по `id`. Результат: удалённая трата.

```json
{
  "type": "object",
  "properties": {
    "expense_id": { "type": "integer", "description": "ID траты" }
  },
  "required": ["expense_id"]
}
```

## get_summary

Сводка за период. Результат: `{total_minor, count}` (арифметика — детерминированный Go, конституция III).

```json
{
  "type": "object",
  "properties": {
    "date_from": { "type": "string", "description": "YYYY-MM-DD, включительно" },
    "date_to":   { "type": "string", "description": "YYYY-MM-DD, включительно" },
    "category":  { "type": "string", "enum": ["food", "transport", "housing", "entertainment", "health", "shopping", "other"], "description": "Опционально" }
  },
  "required": ["date_from", "date_to"]
}
```

## Диалоговые правила оркестратора

- **«Последнее» (FR-009)**: `expense_id` из памяти диалога (последняя упомянутая трата); память пуста/истекла → `find_expenses` с `limit: 1` (фолбэк возвращает последнюю трату по дате spent_at; при истёкшей памяти и задатированной задним числом последней траты это отличается от последней введённой — осознанный компромисс), найденный id используется для update/delete.
- **Удаление с несколькими совпадениями (FR-010)**: `find_expenses(text/категория)` → если одна — `delete_expense` сразу; если несколько — показать 2–3 последние (в порядке выдачи find_expenses) и задать **ровно один** уточняющий вопрос; выбор участника резолвится в `expense_id` через память диалога. Ни одной — сообщить, тул не вызывать.
- **Составляющие сводки (US2)**: на вопрос «из чего / за счёт чего» после `get_summary` — `find_expenses` с тем же периодом (и категорией, если прозвучала); короткий список трат, сумму заново не считать — арифметика уже в Go (конституция III).
- **Память**: после каждого тул-вызова упомянутые/возвращённые `expense_id` фиксируются в памяти диалога (окно 8, TTL 30 мин).
- **Переспрос (FR-014)**: не хватает суммы/даты/категории — один короткий вопрос, тул не вызывается; ошибка тулов (`invalid_amount`, `invalid_category`, `invalid_date`, `not_found`) — пересказать понятно пользователю.
