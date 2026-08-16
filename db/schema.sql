CREATE TABLE IF NOT EXISTS users (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    google_id  TEXT    NOT NULL UNIQUE,
    email      TEXT    NOT NULL DEFAULT '',
    name       TEXT    NOT NULL DEFAULT '',
    picture    TEXT    NOT NULL DEFAULT '',
    created_at TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS quotes (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id           INTEGER NOT NULL DEFAULT 0,
    name              TEXT    NOT NULL DEFAULT '',
    email             TEXT    NOT NULL DEFAULT '',
    mobile            TEXT    NOT NULL DEFAULT '',
    address           TEXT    NOT NULL DEFAULT '',
    description       TEXT    NOT NULL DEFAULT '',
    total_cost        REAL    NOT NULL DEFAULT 0,
    features          TEXT    NOT NULL DEFAULT '{}',
    ai_estimate       TEXT    NOT NULL DEFAULT '',
    verify_token      TEXT    NOT NULL DEFAULT '',
    verified_at       TEXT    NOT NULL DEFAULT '',
    status            TEXT    NOT NULL DEFAULT 'pending',   -- pending (awaiting email verification) | verified
    created_at        TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_quotes_email        ON quotes(email);
CREATE INDEX IF NOT EXISTS idx_quotes_verify_token ON quotes(verify_token);

CREATE TABLE IF NOT EXISTS quote_option_groups (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT    NOT NULL UNIQUE,
    label      TEXT    NOT NULL,
    hint       TEXT    NOT NULL DEFAULT '',
    sort_order INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS quote_options (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    group_id   INTEGER NOT NULL REFERENCES quote_option_groups(id) ON DELETE CASCADE,
    value      TEXT    NOT NULL,
    name       TEXT    NOT NULL,
    cost       INTEGER NOT NULL DEFAULT 0,
    cost_label TEXT    NOT NULL DEFAULT '$0',
    is_default INTEGER NOT NULL DEFAULT 0,
    sort_order INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS quote_settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS bookings (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    name           TEXT    NOT NULL DEFAULT '',
    phone          TEXT    NOT NULL DEFAULT '',
    email          TEXT    NOT NULL DEFAULT '',
    suburb         TEXT    NOT NULL DEFAULT '',
    service_slug   TEXT    NOT NULL DEFAULT '',
    mode           TEXT    NOT NULL DEFAULT 'onsite',
    issue          TEXT    NOT NULL DEFAULT '',
    preferred_time TEXT    NOT NULL DEFAULT '',
    status         TEXT    NOT NULL DEFAULT 'new',
    ip             TEXT    NOT NULL DEFAULT '',
    created_at     TEXT    NOT NULL DEFAULT (datetime('now'))
);
