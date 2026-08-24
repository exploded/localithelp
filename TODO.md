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

14. [ ] **Calendar sync** — an ICS feed of the admin diary (`/admin/calendar.ics?token=…`)
    to subscribe from Google Calendar / iPhone, or two-way Google Calendar sync
    of `booked` visits. Today only outbound `.ics` invites go to customers.
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
