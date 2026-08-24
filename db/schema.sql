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
    status         TEXT    NOT NULL DEFAULT 'new',      -- new | contacted | booked | done | invoiced | paid | cancelled | spam
    ip             TEXT    NOT NULL DEFAULT '',
    created_at     TEXT    NOT NULL DEFAULT (datetime('now')),
    -- Added later: keep in sync with the ALTER TABLE list in db.Open (constant defaults only).
    customer_id       INTEGER NOT NULL DEFAULT 0,
    start_at          TEXT    NOT NULL DEFAULT '',   -- 'YYYY-MM-DD HH:MM' Australia/Melbourne local; '' = unscheduled
    duration_min      INTEGER NOT NULL DEFAULT 60,
    admin_notes       TEXT    NOT NULL DEFAULT '',
    parent_booking_id INTEGER NOT NULL DEFAULT 0,   -- follow-up visits point at the original booking
    updated_at        TEXT    NOT NULL DEFAULT '',
    address           TEXT    NOT NULL DEFAULT '',  -- full street address, e.g. '12 Smith St, Donvale VIC 3111'
    reminder_sent_at    TEXT  NOT NULL DEFAULT '',  -- UTC datetime the day-before reminder was emailed to the customer
    admin_alert_sent_at TEXT  NOT NULL DEFAULT ''   -- UTC datetime the 1-hour heads-up was emailed to the admin
);

CREATE INDEX IF NOT EXISTS idx_bookings_start_at ON bookings(start_at);
CREATE INDEX IF NOT EXISTS idx_bookings_customer ON bookings(customer_id);

-- Customers: the people we do work for (no login). Linked from bookings and invoices.
CREATE TABLE IF NOT EXISTS customers (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT    NOT NULL DEFAULT '',
    email      TEXT    NOT NULL DEFAULT '',   -- stored lower-cased
    phone      TEXT    NOT NULL DEFAULT '',
    phone_norm TEXT    NOT NULL DEFAULT '',   -- digits only, for matching
    address    TEXT    NOT NULL DEFAULT '',
    suburb     TEXT    NOT NULL DEFAULT '',
    notes      TEXT    NOT NULL DEFAULT '',
    created_at TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_customers_email      ON customers(email) WHERE email <> '';
CREATE INDEX        IF NOT EXISTS idx_customers_phone_norm ON customers(phone_norm);

-- Invoices: numbers start at 1000 (see InsertInvoice); money is integer cents; no GST.
CREATE TABLE IF NOT EXISTS invoices (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    number         INTEGER NOT NULL UNIQUE,
    booking_id     INTEGER NOT NULL DEFAULT 0,
    customer_id    INTEGER NOT NULL DEFAULT 0,
    status         TEXT    NOT NULL DEFAULT 'draft',  -- draft | sent | paid | void
    issued_at      TEXT    NOT NULL DEFAULT '',       -- 'YYYY-MM-DD' local
    due_at         TEXT    NOT NULL DEFAULT '',
    paid_at        TEXT    NOT NULL DEFAULT '',
    payment_method TEXT    NOT NULL DEFAULT '',       -- zeller_link | bank_transfer | card_on_day | cash | other
    payment_ref    TEXT    NOT NULL DEFAULT '',
    payment_link   TEXT    NOT NULL DEFAULT '',       -- Zeller payment link pasted by admin
    total_cents    INTEGER NOT NULL DEFAULT 0,
    notes          TEXT    NOT NULL DEFAULT '',
    view_token     TEXT    NOT NULL DEFAULT '',       -- public /invoice/{token}
    review_asked_at TEXT   NOT NULL DEFAULT '',       -- 'YYYY-MM-DD' a Google review was requested
    created_at     TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_at     TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_invoices_view_token ON invoices(view_token);
CREATE INDEX IF NOT EXISTS idx_invoices_customer   ON invoices(customer_id);
CREATE INDEX IF NOT EXISTS idx_invoices_booking    ON invoices(booking_id);

CREATE TABLE IF NOT EXISTS invoice_items (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    invoice_id  INTEGER NOT NULL REFERENCES invoices(id) ON DELETE CASCADE,
    description TEXT    NOT NULL DEFAULT '',
    qty         REAL    NOT NULL DEFAULT 1,
    unit_cents  INTEGER NOT NULL DEFAULT 0,
    line_cents  INTEGER NOT NULL DEFAULT 0,   -- round(qty * unit_cents), computed at save
    sort_order  INTEGER NOT NULL DEFAULT 0
);

-- Scheduler: one row per (job, local date) the job has run, so once-a-day jobs
-- (the morning digest) survive restarts without double-sending.
CREATE TABLE IF NOT EXISTS scheduler_runs (
    job    TEXT NOT NULL,
    ran_on TEXT NOT NULL,   -- 'YYYY-MM-DD' Melbourne local
    PRIMARY KEY (job, ran_on)
);
