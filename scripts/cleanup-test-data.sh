#!/usr/bin/env bash
# Remove test data from the live database:
#
#   1. Attribution rows left behind by testing the Google Ads tracking setup -
#      ad clicks with no gclid, which only a test fetch produces while
#      auto-tagging is on. Real ad clicks always carry one.
#   2. Bookings #1 and #2, which were entered by hand as tests, along with
#      anything hanging off them: invoices, invoice lines, and the test
#      customer if nothing else of theirs remains.
#   3. Software quote #1, another test. Quotes carry their own data and own
#      nothing else, so the row is the whole of it.
#
# Dry run by default - it prints exactly what it would delete and stops. Pass
# --apply to go ahead. Applying stops the service, backs the database up, and
# does the whole delete in one transaction.
#
#   sudo bash cleanup-test-data.sh            # look
#   sudo bash cleanup-test-data.sh --apply    # delete
set -euo pipefail

APP=localithelp
DB=/var/www/$APP/app.db
BOOKINGS="1,2"                     # test bookings to remove
QUOTES="1"                         # test software quotes to remove
BACKUP_DIR=/var/www/$APP/backups
APPLY=false
[ "${1:-}" = "--apply" ] && APPLY=true

if [ "$(id -u)" -ne 0 ]; then
    echo "Run me with sudo - the database belongs to www-data." >&2
    exit 1
fi
if ! command -v sqlite3 >/dev/null; then
    echo "sqlite3 is not installed. Run: sudo apt-get install -y sqlite3" >&2
    exit 1
fi
if [ ! -f "$DB" ]; then
    echo "No database at $DB" >&2
    exit 1
fi

q() { sqlite3 -header -column "$DB" "$1"; }

echo "=== Bookings to delete ==="
q "SELECT id, name, suburb, status, source, created_at,
          CASE WHEN gcal_event_id = '' THEN '-' ELSE gcal_event_id END AS gcal
   FROM bookings WHERE id IN ($BOOKINGS);"

echo
echo "=== Their invoices (deleted with them) ==="
q "SELECT id, number, status, total_cents FROM invoices WHERE booking_id IN ($BOOKINGS);"

echo
echo "=== Their customers (deleted only if nothing else of theirs remains) ==="
q "SELECT c.id, c.name, c.email,
          (SELECT COUNT(*) FROM bookings b WHERE b.customer_id = c.id) AS bookings,
          (SELECT COUNT(*) FROM invoices i WHERE i.customer_id = c.id) AS invoices
   FROM customers c
   WHERE c.id IN (SELECT customer_id FROM bookings WHERE id IN ($BOOKINGS) AND customer_id <> 0);"

echo
echo "=== Software quotes to delete ==="
q "SELECT id, name, email, total_cost, status, user_id, created_at
   FROM quotes WHERE id IN ($QUOTES);"

echo
echo "=== Test ad clicks to delete (no gclid, never booked) ==="
q "SELECT id, source, keyword, campaign, landing, created_at
   FROM ad_clicks WHERE booking_id = 0 AND gclid = '';"

echo
echo "=== Ad clicks that will survive (real ones) ==="
q "SELECT COUNT(*) AS kept, MIN(created_at) AS oldest, MAX(created_at) AS newest
   FROM ad_clicks WHERE NOT (booking_id = 0 AND gclid = '');"

# A calendar event outlives its booking: Google keeps it, and deleting the row
# here leaves nothing to cancel it with.
GCAL=$(sqlite3 "$DB" "SELECT COUNT(*) FROM bookings WHERE id IN ($BOOKINGS) AND gcal_event_id <> '';")
if [ "$GCAL" != "0" ]; then
    echo
    echo "!! $GCAL of these bookings still has a Google Calendar event."
    echo "!! Delete it in Google Calendar first, or it will linger with nothing"
    echo "!! in the app pointing at it."
fi

if [ "$APPLY" != true ]; then
    echo
    echo "Dry run - nothing deleted. Re-run with --apply to go ahead."
    exit 0
fi

echo
echo "Stopping $APP..."
systemctl stop "$APP"

mkdir -p "$BACKUP_DIR"
BACKUP="$BACKUP_DIR/app-before-cleanup-$(date +%Y%m%d-%H%M%S).db"
sqlite3 "$DB" ".backup '$BACKUP'"
echo "Backed up to $BACKUP"

sqlite3 "$DB" <<SQL
PRAGMA foreign_keys = ON;
BEGIN;

-- Remember whose bookings these were before the rows go.
CREATE TEMP TABLE victims AS
SELECT DISTINCT customer_id FROM bookings WHERE id IN ($BOOKINGS) AND customer_id <> 0;

DELETE FROM invoice_items WHERE invoice_id IN (SELECT id FROM invoices WHERE booking_id IN ($BOOKINGS));
DELETE FROM invoices WHERE booking_id IN ($BOOKINGS);
DELETE FROM ad_clicks WHERE booking_id IN ($BOOKINGS);
DELETE FROM bookings WHERE id IN ($BOOKINGS);

-- Only take the customer if the test bookings were all they had.
DELETE FROM customers WHERE id IN (SELECT customer_id FROM victims)
  AND id NOT IN (SELECT customer_id FROM bookings)
  AND id NOT IN (SELECT customer_id FROM invoices);

-- Test fetches of the tracking setup: no gclid, no booking.
DELETE FROM ad_clicks WHERE booking_id = 0 AND gclid = '';

-- Test software quotes. Any login account behind one is left alone: users are
-- the Google sign-ins, and yours is in there too.
DELETE FROM quotes WHERE id IN ($QUOTES);

COMMIT;
VACUUM;
SQL

chown www-data:www-data "$DB"*
echo "Starting $APP..."
systemctl start "$APP"
sleep 2

echo
echo "=== What's left ==="
q "SELECT (SELECT COUNT(*) FROM bookings)  AS bookings,
          (SELECT COUNT(*) FROM invoices)  AS invoices,
          (SELECT COUNT(*) FROM customers) AS customers,
          (SELECT COUNT(*) FROM quotes)    AS quotes,
          (SELECT COUNT(*) FROM ad_clicks) AS ad_clicks;"

echo
echo -n "Health check: "
curl -sS -o /dev/null -w "%{http_code}\n" https://localithelp.com.au/health
echo "Backup kept at $BACKUP"
