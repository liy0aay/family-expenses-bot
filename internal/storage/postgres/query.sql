-- name: UpsertUser :one
INSERT INTO users (id, username, first_name, created_at, updated_at)
VALUES (@id, @username, @first_name, now(), now())
ON CONFLICT (id) DO UPDATE
SET username   = EXCLUDED.username,
    first_name = EXCLUDED.first_name,
    updated_at = now()
RETURNING id, username, first_name, created_at, updated_at;

-- name: CreateExpense :one
INSERT INTO expenses (user_id, amount_minor, category, spent_at, comment, created_at, updated_at)
VALUES (@user_id, @amount_minor, @category, @spent_at, @comment, now(), now())
RETURNING id, user_id, amount_minor, category, spent_at, comment, created_at, updated_at;

-- name: ListExpenses :many
SELECT id, user_id, amount_minor, category, spent_at, comment, created_at, updated_at
FROM expenses
WHERE (sqlc.narg('user_id')::BIGINT IS NULL OR user_id = sqlc.narg('user_id'))
  AND (sqlc.narg('date_from')::DATE IS NULL OR spent_at >= sqlc.narg('date_from'))
  AND (sqlc.narg('date_to')::DATE IS NULL OR spent_at <= sqlc.narg('date_to'))
  AND (sqlc.narg('category')::TEXT IS NULL OR category = sqlc.narg('category'))
  -- @text arrives pre-escaped: %, _, \ are made literal by service textFilter
  AND (sqlc.narg('text')::TEXT IS NULL OR comment ILIKE '%' || sqlc.narg('text') || '%')
-- порядок трат, а не ввода; id — тайбрейк порядка вставки внутри дня (openapi: spent_at DESC, id DESC)
ORDER BY spent_at DESC, id DESC
LIMIT @lim::BIGINT;

-- name: GetExpense :one
SELECT id, user_id, amount_minor, category, spent_at, comment, created_at, updated_at
FROM expenses
WHERE id = @id
  AND (sqlc.narg('user_id')::BIGINT IS NULL OR user_id = sqlc.narg('user_id'));

-- name: UpdateExpense :one
UPDATE expenses
SET amount_minor = COALESCE(sqlc.narg('amount_minor'), amount_minor),
    category     = COALESCE(sqlc.narg('category'), category),
    spent_at     = COALESCE(sqlc.narg('spent_at'), spent_at),
    comment      = NULLIF(COALESCE(sqlc.narg('comment'), comment), ''),
    updated_at   = now()
WHERE id = @id
  AND (sqlc.narg('user_id')::BIGINT IS NULL OR user_id = sqlc.narg('user_id'))
RETURNING id, user_id, amount_minor, category, spent_at, comment, created_at, updated_at;

-- name: DeleteExpense :execrows
DELETE FROM expenses
WHERE id = @id
  AND (sqlc.narg('user_id')::BIGINT IS NULL OR user_id = sqlc.narg('user_id'));

-- name: SummaryByCategory :many
SELECT
    category,
    SUM(amount_minor)::BIGINT AS total_minor,
    COUNT(*)::BIGINT          AS expenses_count
FROM expenses
WHERE (sqlc.narg('user_id')::BIGINT IS NULL OR user_id = sqlc.narg('user_id'))
  AND (sqlc.narg('date_from')::DATE IS NULL OR spent_at >= sqlc.narg('date_from'))
  AND (sqlc.narg('date_to')::DATE IS NULL OR spent_at <= sqlc.narg('date_to'))
GROUP BY category
ORDER BY category;

-- name: LastExpense :one
SELECT id, user_id, amount_minor, category, spent_at, comment, created_at, updated_at
FROM expenses
WHERE (sqlc.narg('user_id')::BIGINT IS NULL OR user_id = sqlc.narg('user_id'))
ORDER BY created_at DESC, id DESC
LIMIT 1;

-- name: ListUsers :many
SELECT id, username, first_name, created_at, updated_at
FROM users
WHERE (sqlc.narg('user_id')::BIGINT IS NULL OR id = sqlc.narg('user_id'))
ORDER BY id;
