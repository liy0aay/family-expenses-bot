-- +goose Up
CREATE TABLE users (
    id         BIGINT PRIMARY KEY,
    username   TEXT NULL,
    first_name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE expenses (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id      BIGINT NOT NULL REFERENCES users (id),
    amount_minor BIGINT NOT NULL CHECK (amount_minor > 0),
    category     TEXT NOT NULL CHECK (category IN ('food', 'transport', 'housing', 'entertainment', 'health', 'shopping', 'other')),
    spent_at     DATE NOT NULL,
    comment      TEXT NULL,
    created_at   TIMESTAMPTZ NOT NULL,
    updated_at   TIMESTAMPTZ NOT NULL
);

CREATE INDEX expenses_user_id_spent_at_idx ON expenses (user_id, spent_at);
CREATE INDEX expenses_user_id_created_at_idx ON expenses (user_id, created_at DESC);

-- +goose Down
DROP TABLE expenses;
DROP TABLE users;
