-- Quotes

-- name: InsertQuote :one
INSERT INTO quotes (name, email, mobile, address, description, total_cost, features, verify_token, status)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'pending')
RETURNING id;

-- name: GetQuote :one
SELECT id, user_id, name, email, mobile, address, description, total_cost, features, ai_estimate, verify_token, verified_at, status, created_at
FROM quotes WHERE id = ?;

-- name: GetQuoteByVerifyToken :one
SELECT id, user_id, name, email, mobile, address, description, total_cost, features, ai_estimate, verify_token, verified_at, status, created_at
FROM quotes WHERE verify_token = ? AND verify_token <> '';

-- name: CountVerifiedByEmail :one
SELECT COUNT(*) FROM quotes WHERE email = ? AND status IN ('verified', 'paid');

-- name: DeletePendingByEmail :exec
DELETE FROM quotes WHERE email = ? AND status = 'pending';

-- name: MarkQuoteVerified :execrows
UPDATE quotes SET status = 'verified', ai_estimate = ?, verified_at = datetime('now')
WHERE id = ? AND status = 'pending';

-- name: ListQuotes :many
SELECT id, user_id, name, email, mobile, address, description, total_cost, features, ai_estimate, verify_token, verified_at, status, created_at
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

-- name: InsertBooking :one
INSERT INTO bookings (name, phone, email, suburb, address, service_slug, mode, issue, preferred_time, ip, customer_id, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
RETURNING id;

-- name: InsertFollowupBooking :one
INSERT INTO bookings (name, phone, email, suburb, address, service_slug, mode, issue, preferred_time, ip, customer_id, parent_booking_id, status, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, '', '', ?, ?, 'new', datetime('now'))
RETURNING id;

-- name: GetBooking :one
SELECT id, name, phone, email, suburb, service_slug, mode, issue, preferred_time, status, ip, created_at,
       customer_id, start_at, duration_min, admin_notes, parent_booking_id, updated_at, address, reminder_sent_at, admin_alert_sent_at
FROM bookings WHERE id = ?;

-- name: ListBookings :many
SELECT id, name, phone, email, suburb, service_slug, mode, issue, preferred_time, status, ip, created_at,
       customer_id, start_at, duration_min, admin_notes, parent_booking_id, updated_at, address, reminder_sent_at, admin_alert_sent_at
FROM bookings ORDER BY id DESC;

-- name: ListBookingsByStatus :many
SELECT id, name, phone, email, suburb, service_slug, mode, issue, preferred_time, status, ip, created_at,
       customer_id, start_at, duration_min, admin_notes, parent_booking_id, updated_at, address, reminder_sent_at, admin_alert_sent_at
FROM bookings WHERE status = ? ORDER BY id DESC;

-- name: ListBookingsBetween :many
SELECT id, name, phone, email, suburb, service_slug, mode, issue, preferred_time, status, ip, created_at,
       customer_id, start_at, duration_min, admin_notes, parent_booking_id, updated_at, address, reminder_sent_at, admin_alert_sent_at
FROM bookings
WHERE start_at >= ? AND start_at < ? AND status NOT IN ('cancelled', 'spam')
ORDER BY start_at;

-- name: ListBookingsByCustomer :many
SELECT id, name, phone, email, suburb, service_slug, mode, issue, preferred_time, status, ip, created_at,
       customer_id, start_at, duration_min, admin_notes, parent_booking_id, updated_at, address, reminder_sent_at, admin_alert_sent_at
FROM bookings WHERE customer_id = ? ORDER BY id DESC;

-- name: ListChildBookings :many
SELECT id, name, phone, email, suburb, service_slug, mode, issue, preferred_time, status, ip, created_at,
       customer_id, start_at, duration_min, admin_notes, parent_booking_id, updated_at, address, reminder_sent_at, admin_alert_sent_at
FROM bookings WHERE parent_booking_id = ? ORDER BY id;

-- name: ListUnlinkedBookings :many
SELECT id, name, phone, email, suburb, service_slug, mode, issue, preferred_time, status, ip, created_at,
       customer_id, start_at, duration_min, admin_notes, parent_booking_id, updated_at, address, reminder_sent_at, admin_alert_sent_at
FROM bookings WHERE customer_id = 0 AND status <> 'spam' ORDER BY id;

-- name: CountBookingsByStatus :many
SELECT status, COUNT(*) AS n FROM bookings GROUP BY status;

-- name: UpdateBookingStatus :exec
UPDATE bookings SET status = ?, updated_at = datetime('now') WHERE id = ?;

-- name: UpdateBookingSchedule :exec
-- Rescheduling clears the reminder stamps so the new time gets its own reminder + heads-up.
UPDATE bookings SET start_at = ?, duration_min = ?, status = 'booked', reminder_sent_at = '', admin_alert_sent_at = '', updated_at = datetime('now') WHERE id = ?;

-- name: UpdateBookingNotes :exec
UPDATE bookings SET admin_notes = ?, updated_at = datetime('now') WHERE id = ?;

-- name: SetBookingCustomer :exec
UPDATE bookings SET customer_id = ? WHERE id = ?;

-- name: UpdateBookingAddress :exec
UPDATE bookings SET address = ?, suburb = ?, updated_at = datetime('now') WHERE id = ?;

-- name: ListBookingsForReminder :many
-- Booked visits in [from, to) whose customer has an email and no reminder yet.
SELECT id, name, phone, email, suburb, service_slug, mode, issue, preferred_time, status, ip, created_at,
       customer_id, start_at, duration_min, admin_notes, parent_booking_id, updated_at, address, reminder_sent_at, admin_alert_sent_at
FROM bookings
WHERE start_at >= ? AND start_at < ? AND status = 'booked' AND email <> '' AND reminder_sent_at = ''
ORDER BY start_at;

-- name: ListBookingsForAdminAlert :many
-- Booked visits in [from, to) the admin has not been alerted about yet.
SELECT id, name, phone, email, suburb, service_slug, mode, issue, preferred_time, status, ip, created_at,
       customer_id, start_at, duration_min, admin_notes, parent_booking_id, updated_at, address, reminder_sent_at, admin_alert_sent_at
FROM bookings
WHERE start_at >= ? AND start_at < ? AND status = 'booked' AND admin_alert_sent_at = ''
ORDER BY start_at;

-- name: MarkBookingReminderSent :exec
UPDATE bookings SET reminder_sent_at = datetime('now') WHERE id = ?;

-- name: MarkBookingAdminAlertSent :exec
UPDATE bookings SET admin_alert_sent_at = datetime('now') WHERE id = ?;

-- Scheduler

-- name: InsertSchedulerRun :execrows
INSERT OR IGNORE INTO scheduler_runs (job, ran_on) VALUES (?, ?);

-- Customers

-- name: InsertCustomer :one
INSERT INTO customers (name, email, phone, phone_norm, address, suburb, notes)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: GetCustomer :one
SELECT id, name, email, phone, phone_norm, address, suburb, notes, created_at, updated_at
FROM customers WHERE id = ?;

-- name: GetCustomerByEmail :one
SELECT id, name, email, phone, phone_norm, address, suburb, notes, created_at, updated_at
FROM customers WHERE email = ? AND email <> '';

-- name: GetCustomerByPhoneNorm :one
SELECT id, name, email, phone, phone_norm, address, suburb, notes, created_at, updated_at
FROM customers WHERE phone_norm = ? AND phone_norm <> '' ORDER BY id LIMIT 1;

-- name: ListCustomers :many
SELECT id, name, email, phone, phone_norm, address, suburb, notes, created_at, updated_at
FROM customers ORDER BY name COLLATE NOCASE, id;

-- name: SearchCustomers :many
SELECT id, name, email, phone, phone_norm, address, suburb, notes, created_at, updated_at
FROM customers
WHERE name LIKE ? OR email LIKE ? OR phone_norm LIKE ? OR suburb LIKE ?
ORDER BY name COLLATE NOCASE, id;

-- name: UpdateCustomer :exec
UPDATE customers SET name = ?, email = ?, phone = ?, phone_norm = ?, address = ?, suburb = ?, notes = ?, updated_at = datetime('now')
WHERE id = ?;

-- name: TouchCustomerContact :exec
UPDATE customers SET
    name       = CASE WHEN name = ''       THEN ? ELSE name END,
    email      = CASE WHEN email = ''      THEN ? ELSE email END,
    phone      = CASE WHEN phone = ''      THEN ? ELSE phone END,
    phone_norm = CASE WHEN phone_norm = '' THEN ? ELSE phone_norm END,
    suburb     = CASE WHEN suburb = ''     THEN ? ELSE suburb END,
    updated_at = datetime('now')
WHERE id = ?;

-- Invoices

-- name: InsertInvoice :one
INSERT INTO invoices (number, booking_id, customer_id, status, issued_at, due_at, notes, view_token)
VALUES ((SELECT COALESCE(MAX(number), 999) + 1 FROM invoices), ?, ?, 'draft', ?, ?, ?, ?)
RETURNING id;

-- name: GetInvoice :one
SELECT id, number, booking_id, customer_id, status, issued_at, due_at, paid_at, payment_method, payment_ref, payment_link,
       total_cents, notes, view_token, review_asked_at, created_at, updated_at
FROM invoices WHERE id = ?;

-- name: GetInvoiceByToken :one
SELECT id, number, booking_id, customer_id, status, issued_at, due_at, paid_at, payment_method, payment_ref, payment_link,
       total_cents, notes, view_token, review_asked_at, created_at, updated_at
FROM invoices WHERE view_token = ? AND view_token <> '';

-- name: ListInvoices :many
SELECT id, number, booking_id, customer_id, status, issued_at, due_at, paid_at, payment_method, payment_ref, payment_link,
       total_cents, notes, view_token, review_asked_at, created_at, updated_at
FROM invoices ORDER BY number DESC;

-- name: ListInvoicesByStatus :many
SELECT id, number, booking_id, customer_id, status, issued_at, due_at, paid_at, payment_method, payment_ref, payment_link,
       total_cents, notes, view_token, review_asked_at, created_at, updated_at
FROM invoices WHERE status = ? ORDER BY number DESC;

-- name: ListInvoicesByCustomer :many
SELECT id, number, booking_id, customer_id, status, issued_at, due_at, paid_at, payment_method, payment_ref, payment_link,
       total_cents, notes, view_token, review_asked_at, created_at, updated_at
FROM invoices WHERE customer_id = ? ORDER BY number DESC;

-- name: ListInvoicesByBooking :many
SELECT id, number, booking_id, customer_id, status, issued_at, due_at, paid_at, payment_method, payment_ref, payment_link,
       total_cents, notes, view_token, review_asked_at, created_at, updated_at
FROM invoices WHERE booking_id = ? ORDER BY number DESC;

-- name: ListOverdueInvoices :many
-- Sent, unpaid invoices whose due date is before the given local date.
SELECT id, number, booking_id, customer_id, status, issued_at, due_at, paid_at, payment_method, payment_ref, payment_link,
       total_cents, notes, view_token, review_asked_at, created_at, updated_at
FROM invoices WHERE status = 'sent' AND due_at <> '' AND due_at < ? ORDER BY due_at;

-- name: SumOutstandingCents :one
SELECT COALESCE(SUM(total_cents), 0) FROM invoices WHERE status = 'sent';

-- name: UpdateInvoiceDraft :exec
UPDATE invoices SET due_at = ?, notes = ?, payment_link = ?, total_cents = ?, updated_at = datetime('now')
WHERE id = ? AND status = 'draft';

-- name: UpdateInvoicePaymentLink :exec
UPDATE invoices SET payment_link = ?, updated_at = datetime('now')
WHERE id = ? AND status IN ('draft', 'sent');

-- name: MarkInvoiceSent :execrows
UPDATE invoices SET status = 'sent', issued_at = ?, due_at = ?, updated_at = datetime('now')
WHERE id = ? AND status = 'draft';

-- name: MarkInvoicePaid :execrows
UPDATE invoices SET status = 'paid', paid_at = ?, payment_method = ?, payment_ref = ?,
    issued_at = CASE WHEN issued_at = '' THEN ? ELSE issued_at END, updated_at = datetime('now')
WHERE id = ? AND status IN ('draft', 'sent');

-- name: MarkInvoiceReviewAsked :exec
UPDATE invoices SET review_asked_at = ?, updated_at = datetime('now')
WHERE id = ?;

-- name: VoidInvoice :execrows
UPDATE invoices SET status = 'void', updated_at = datetime('now')
WHERE id = ? AND status IN ('draft', 'sent');

-- name: DeleteInvoiceItems :exec
DELETE FROM invoice_items WHERE invoice_id = ?;

-- name: InsertInvoiceItem :exec
INSERT INTO invoice_items (invoice_id, description, qty, unit_cents, line_cents, sort_order)
VALUES (?, ?, ?, ?, ?, ?);

-- name: ListInvoiceItems :many
SELECT id, invoice_id, description, qty, unit_cents, line_cents, sort_order
FROM invoice_items WHERE invoice_id = ? ORDER BY sort_order, id;
