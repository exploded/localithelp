-- Quotes

-- name: InsertQuote :exec
INSERT INTO quotes (user_id, name, email, mobile, address, description, total_cost, features, ai_estimate, stripe_session_id, status)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetLastQuote :one
SELECT id, user_id, name, email, mobile, address, description, total_cost, features, ai_estimate, stripe_session_id, status, created_at
FROM quotes WHERE id = (SELECT MAX(id) FROM quotes);

-- name: GetQuote :one
SELECT id, user_id, name, email, mobile, address, description, total_cost, features, ai_estimate, stripe_session_id, status, created_at
FROM quotes WHERE id = ?;

-- name: ListDraftsByUser :many
SELECT id, user_id, name, email, description, total_cost, status, created_at
FROM quotes WHERE user_id = ? AND status = 'draft'
ORDER BY created_at DESC;

-- name: ListPaidByUser :many
SELECT id, user_id, name, email, mobile, address, description, total_cost, features, ai_estimate, stripe_session_id, status, created_at
FROM quotes WHERE user_id = ? AND status = 'paid'
ORDER BY created_at DESC;

-- name: UpdateQuoteStatus :exec
UPDATE quotes SET status = ?, stripe_session_id = ?, ai_estimate = ?
WHERE id = ?;

-- name: ListQuotes :many
SELECT id, user_id, name, email, mobile, address, description, total_cost, features, ai_estimate, stripe_session_id, status, created_at
FROM quotes ORDER BY created_at DESC;

-- Users

-- name: UpsertUser :exec
INSERT INTO users (google_id, email, name, picture)
VALUES (?, ?, ?, ?)
ON CONFLICT(google_id) DO UPDATE SET
    email = excluded.email,
    name = excluded.name,
    picture = excluded.picture;

-- name: GetUserByGoogleID :one
SELECT id, google_id, email, name, picture, created_at
FROM users WHERE google_id = ?;

-- Option Groups

-- name: ListOptionGroups :many
SELECT id, name, label, hint, sort_order
FROM quote_option_groups ORDER BY sort_order, id;

-- name: ListOptionsByGroupID :many
SELECT id, group_id, value, name, cost, cost_label, is_default, sort_order
FROM quote_options WHERE group_id = ? ORDER BY sort_order, id;

-- name: DeleteAllOptions :exec
DELETE FROM quote_options;

-- name: DeleteAllOptionGroups :exec
DELETE FROM quote_option_groups;

-- name: InsertOptionGroup :exec
INSERT INTO quote_option_groups (name, label, hint, sort_order) VALUES (?, ?, ?, ?);

-- name: GetLastOptionGroup :one
SELECT id, name, label, hint, sort_order
FROM quote_option_groups WHERE id = (SELECT MAX(id) FROM quote_option_groups);

-- name: InsertOption :exec
INSERT INTO quote_options (group_id, value, name, cost, cost_label, is_default, sort_order) VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: CountOptionGroups :one
SELECT COUNT(*) FROM quote_option_groups;

-- Settings

-- name: GetSetting :one
SELECT value FROM quote_settings WHERE key = ?;

-- name: UpsertSetting :exec
INSERT OR REPLACE INTO quote_settings (key, value) VALUES (?, ?);

-- name: CountSettingByKey :one
SELECT COUNT(*) FROM quote_settings WHERE key = ?;

-- Bookings

-- name: InsertBooking :exec
INSERT INTO bookings (name, phone, email, suburb, service_slug, mode, issue, preferred_time, ip)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetLastBooking :one
SELECT id, name, phone, email, suburb, service_slug, mode, issue, preferred_time, status, ip, created_at
FROM bookings WHERE id = (SELECT MAX(id) FROM bookings);

-- name: ListBookings :many
SELECT id, name, phone, email, suburb, service_slug, mode, issue, preferred_time, status, ip, created_at
FROM bookings ORDER BY id DESC;

-- name: UpdateBookingStatus :exec
UPDATE bookings SET status = ? WHERE id = ?;
