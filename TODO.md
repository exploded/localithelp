# TODO

Ideas for rounding out the site, in descending priority. Status: `[ ]` idea ·
`[x]` approved · `[-]` rejected · ✅ built.

## 1. Scheduler + reminders — in progress

Base is built: `cmd/server/scheduler.go`, 15-minute ticker, on when `PROD` or
`SCHEDULER=1`. Each job stamps a column or a `scheduler_runs` row so restarts
never double-send. See "Scheduled reminders" in the README.

- ✅ 1a. Day-before visit reminder (customer, 4–8 pm) — `bookings.reminder_sent_at`
- ✅ 1e. 1-hour heads-up (admin, 45–60 min before) — `bookings.admin_alert_sent_at`
- ✅ 1d. Morning digest (admin, first tick after 7:30, only if non-empty) — `scheduler_runs`
- [ ] 1b. Overdue-invoice nudge (customer) — `status=sent`, `due_at` 7+ and 14+
  days past; polite reminder with invoice link + pay options. New column
  `invoices.reminded_at` (+ `remind_count`).
- [ ] 1c. Review-not-asked reminder (admin) — paid invoices with
  `review_asked_at` empty for 2+ days; one admin email listing them. New column
  `invoices.review_nudged_at`.

## Backlog

2. ✅ **DB backups** — nightly `VACUUM INTO` + gzip → `s3://localithelp-backups`
   from the in-app scheduler (no cron); put-only IAM user via
   `scripts/s3-backup-setup.sh`; 90-day lifecycle; restore steps in README.
   Needs `BACKUP_*` in the server `.env` to switch on.
3. [ ] **Public reviews + AggregateRating** — `reviews` table, admin CRUD, display
   on home/service/area pages, `Review`/`AggregateRating` JSON-LD for star snippets.
4. [ ] **/healthz + uptime monitor** — trivial route (DB ping), UptimeRobot notes
   in README.
5. [ ] **Dated posts + RSS + IndexNow ping** — embedded Markdown posts, `/posts`,
   `/feed.xml`, auto-submit new URLs to IndexNow (key file exists, no submitter).
6. [ ] **Maintenance plan / annual check-up** — `/maintenance` page,
   `customers.next_checkup_at`, scheduled reminder with prefilled `/book` link.
7. [ ] **Referral voucher on receipt** — `vouchers` table, `?ref=` on `/book`,
   code printed in receipt email/PDF, admin redeem.
8. [ ] **Per-service FAQ + /faq page** — FAQ blocks with `FAQPage` schema per
   service (only 5 FAQs exist today, on `/pricing`).
9. [ ] **On-site job quote** — quote document for hardware jobs with accept link →
   converts to invoice (token pattern like invoices).
10. [ ] **Revenue reports + booking source** — `/admin/reports` (revenue/month,
    jobs by suburb/service, avg invoice); capture `utm_*`/referrer on bookings.
11. [ ] **Stripe payment links + paid webhook** — replace hand-pasted Zeller
    links; auto-mark paid.
12. [ ] **Customer self-service slot picking on /book** — expose free slots from
    the admin calendar.
13. [ ] **Google Business Profile cadence** — runbook note only (monthly
    post/photo), no code.

## Also noted (gaps from the site survey, not yet ranked)

14. ✅ **Calendar sync** — two-way Google Calendar sync with james67@gmail.com.
    Built 2026-08-24 (plan below). Bookings push to a dedicated "Local IT Help"
    calendar; other calendars show as busy times on `/admin/calendar`. Needs the
    one-off Google Cloud Console steps in the README, then Connect on
    `/admin/calendar/settings`.
15. [ ] **SMS** — day-before reminder and heads-up by SMS (Twilio/ClickSend) for
    customers without email, or as a second channel. None today.
16. [ ] **Overdue-invoice nudge** — see 1b.
17. [ ] **Newsletter / email list** — signup + occasional tips mailout; no list
    or capture beyond bookings today.
18. [ ] **Gift vouchers** — "an hour of help" voucher, sold via payment link,
    redeemed on an invoice; separate from the referral voucher (7).
19. [ ] **Post-visit follow-up email** — automatic "how's it going?" 7–14 days
    after a `paid` booking, with the review ask if not already asked.
20. [ ] **Booking-source capture** — see 10; store `utm_*`, referrer and landing
    page on every booking so Ads spend can be judged per keyword.
21. [ ] **Quote → invoice conversion** — see 9; also let the software `/quote`
    flow produce an invoice for the deposit.
22. [ ] **Admin: mark `contacted` from the digest** — one-click links in the
    digest/heads-up emails (tokenised) to change status without signing in.

## 14. Calendar sync — plan

Goal: every `booked` visit appears in James's Google Calendar
(james67@gmail.com) and follows reschedules and cancellations; anything else in
that Google account shows as grey "busy" blocks on `/admin/calendar`.

### Approach: Google Calendar API, not an ICS feed

An ICS subscription URL is read-only and Google only refetches it every 12–24 h,
so changes and deletions would lag by up to a day, and it can't read James's
other events. The Calendar API does both directions and the app already has a
Google OAuth client (`GOOGLE_CLIENT_ID`), so the marginal setup is one extra
scope and one API enablement.

### Google Cloud Console (one-off, by hand)

1. APIs & Services → enable **Google Calendar API**.
2. OAuth consent screen → add scope `https://www.googleapis.com/auth/calendar`
   (events + calendar list + free/busy on the user's own calendars).
3. **Publishing status must be "In production"**, not "Testing". A Testing app's
   refresh tokens expire after 7 days, which would silently stop the sync every
   week. Publishing without verification is fine for a single owner account —
   Google shows an "unverified app" interstitial once at connect time.
4. Redirect URI `https://localithelp.com.au/auth/google/calendar/callback` (and
   the localhost one for dev).

### Connect flow (`/admin/calendar/settings`)

- Separate from admin sign-in. Sign-in keeps `openid email profile`; the
  calendar scope is only requested from a **Connect Google Calendar** button so
  the login flow never prompts for it.
- Button → `/auth/google/calendar` → consent with `access_type=offline` and
  `prompt=consent` (forces a refresh token) → `/auth/google/calendar/callback`.
- Callback rejects any account other than `ADMIN_EMAIL`, then stores the
  refresh token and lets James pick a target calendar.
- **Use a dedicated secondary calendar** ("Local IT Help"), created by the app
  on first connect if it doesn't exist. Reasons: bookings get their own colour,
  sharing/hiding is one click, and the busy query can exclude it cleanly so the
  app's own events never come back as "busy".
- Settings page shows: connected account, target calendar, last sync time,
  last error, **Disconnect** (deletes the row; leaves events in place) and
  **Resync all** (re-pushes every future `booked` visit).

### Storage

New table `google_calendar` (single row):

| column | notes |
|---|---|
| `refresh_token` | plaintext in SQLite. Backups go to S3; acceptable for a one-user app — note in README |
| `account_email` | must equal `ADMIN_EMAIL` |
| `calendar_id` | the secondary calendar's id |
| `connected_at`, `last_sync_at`, `last_error` | shown on the settings page |

New columns on `bookings`:

| column | notes |
|---|---|
| `gcal_event_id TEXT NOT NULL DEFAULT ''` | empty = no event in Google |
| `gcal_synced_at TEXT NOT NULL DEFAULT ''` | UTC; `updated_at > gcal_synced_at` means dirty |

Both via the existing `ALTER TABLE` list in `db/db.go` + `schema.sql` + sqlc.

### Outbound: bookings → Google

Event mapping (`cmd/server/gcal.go`):

- **summary** `Visit: {Name} — {service title}` (`Remote: …` when `mode=remote`)
- **start/end** `StartAt` / `EndAt()` with `timeZone: Australia/Melbourne`
- **location** full `Address`, falling back to `Suburb`
- **description** phone, email, issue, admin notes, link to
  `/admin/bookings/{id}`
- **extendedProperties.private.localithelp_booking_id** = id, so events can be
  matched even if `gcal_event_id` is lost
- **reminders.useDefault=false, overrides=[]** — the app already sends the
  1-hour heads-up; don't double up

Rules (one function, `syncBooking(b)`):

| booking state | action |
|---|---|
| `booked`/`done`/`invoiced`/`paid` with `StartAt` set, no event | `events.insert`, store id |
| same, event exists | `events.patch` |
| `cancelled`/`spam`, or `StartAt` cleared, event exists | `events.delete`, clear id (404 = already gone, treat as success) |
| anything else | no-op |

Then stamp `gcal_synced_at = now`.

Triggers:

1. **Immediately** — `handleAdminBookingSchedule`, `handleAdminBookingStatus`,
   `handleAdminBookingNotes`, `handleAdminBookingAddress` and the invoice status
   changes call `go syncBookingAsync(id)` after a successful DB write. Failures
   are logged, not surfaced, because…
2. **Reconcile every tick** — new scheduler job `syncCalendar(now)` lists
   bookings with `updated_at > gcal_synced_at` (and `StartAt` within
   −30 / +365 days) and runs `syncBooking` on each. This catches anything the
   immediate call missed (network blip, server restart mid-request) within
   15 minutes. Check first that every mutating query bumps `updated_at`;
   `UpdateBookingStatus` from the invoice paths must too.

The app is the source of truth: if James drags an event in Google, the next
reconcile pass does **not** pull that back (one-way for event content). That's
deliberate — reschedules must go through `/admin/bookings/{id}` so the customer
gets the confirmation email.

### Inbound: Google → busy blocks on `/admin/calendar`

- `freebusy.query` for the week shown, over every calendar in
  `calendarList` **except** `calendar_id` (the app's own), plus any calendar
  James unticks on the settings page. Returns only busy intervals — no titles,
  which matches "appear as busy times" and keeps personal details out of the
  app.
- Render as grey hatched `calEvent`s labelled "Busy" behind the booking
  events; not clickable. When the calendar is opened with `?for=` (placing a
  booking), busy slots still get the `Href` so James can override, but they're
  visibly occupied.
- Cached in memory per week for 2 minutes; 3-second timeout; on error show a
  one-line banner ("Google Calendar busy times unavailable: …") and render the
  week without them. Never block the page on Google.
- Same busy data is what #12 (customer self-service slot picking) will need,
  so keep `busyIntervals(from, to)` as a reusable function.

### Code layout

- `cmd/server/gcal.go` — OAuth connect/callback, token source from the stored
  refresh token (`golang.org/x/oauth2`), a small `calendarAPI` interface
  (`Insert`, `Patch`, `Delete`, `FreeBusy`, `ListCalendars`, `CreateCalendar`)
  implemented with plain `net/http` + JSON against `www.googleapis.com/calendar/v3`
  — no need to pull in `google.golang.org/api`.
- `cmd/server/gcal_sync.go` — `eventForBooking`, `syncBooking`, scheduler job.
- `cmd/server/gcal_test.go` — fake `calendarAPI`; tests for the mapping table
  above, the dirty-row reconcile, delete-on-cancel, 404-is-ok, busy overlay
  rendering, and that the login flow's scopes are unchanged.
- `db/`: `google_calendar` table + queries; `ListBookingsDirtyForCalendar`,
  `MarkBookingCalendarSynced`, `SetBookingCalendarEventID`.
- `templates/admin-calendar-settings.html`; busy blocks in `admin-calendar.html`;
  a "Calendar sync: connected / not connected" line on the admin dashboard.
- README: new "Google Calendar sync" section (console steps, publishing-status
  caveat, refresh-token-in-DB note, disconnect/resync).

### Order of work

1. DB: table, columns, queries, `updated_at` audit.
2. Connect/disconnect flow + settings page (no sync yet). Verify a refresh
   token survives a restart.
3. Outbound sync + immediate triggers + backfill on connect.
4. Scheduler reconcile job + tests.
5. Busy overlay on `/admin/calendar`.
6. README + ship.

### Out of scope (for now)

- Pulling edits made in Google back into bookings.
- Attendee invites from Google (customers already get the `.ics` by email).
- Push notifications (webhook channels) — the 15-minute reconcile and
  immediate trigger cover it without a public webhook endpoint.
