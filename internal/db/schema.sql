CREATE TABLE IF NOT EXISTS books (
    id         BIGSERIAL PRIMARY KEY,
    title      TEXT        NOT NULL,
    author     TEXT        NOT NULL,
    year       INT         NOT NULL DEFAULT 0,
    isbn       TEXT UNIQUE,
    total      INT         NOT NULL DEFAULT 1 CHECK (total >= 0),
    available  INT         NOT NULL DEFAULT 1 CHECK (available >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS readers (
    id         BIGSERIAL PRIMARY KEY,
    name       TEXT        NOT NULL,
    email      TEXT        NOT NULL UNIQUE,
    phone      TEXT        NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS loans (
    id               BIGSERIAL PRIMARY KEY,
    book_id          BIGINT      NOT NULL REFERENCES books (id) ON DELETE CASCADE,
    reader_id        BIGINT      NOT NULL REFERENCES readers (id) ON DELETE CASCADE,
    issued_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    due_at           TIMESTAMPTZ NOT NULL,
    returned_at      TIMESTAMPTZ,
    overdue_notified BOOLEAN     NOT NULL DEFAULT FALSE
);

CREATE INDEX IF NOT EXISTS idx_loans_open ON loans (due_at) WHERE returned_at IS NULL;
