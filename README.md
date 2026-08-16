# mchugh.com.au

Site for James McHugh — local computer help around Donvale VIC (email, printers,
Wi-Fi, scams, repairs, setups…) with software development as a featured service.
Go + `html/template` + SQLite (sqlc). Live at https://mchugh.com.au

Repo: `exploded/mchugh-com-au`. Not to be confused with **https://mchugh.au** —
that is a separate static landing page (repo `exploded/mchugh-au`, served from
`/var/www/mchugh.au`, source in `C:\Projects\php\mchugh.au`). This app lives at
`/var/www/mchugh.com.au` as systemd unit `mchugh-com-au`.

## Structure

- `cmd/server/main.go` — server, routes, Google sign-in for `/admin` only, site config
- `cmd/server/quote.go` — software quote flow (public, no login/payment): Turnstile → email verification → AI estimate; `turnstile.go` — Cloudflare Turnstile check
- `cmd/server/services.go` — **the service catalogue** (titles, copy, prices, suburbs, software packages). Edit this to change what the site offers.
- `cmd/server/pages.go` — service pages + booking form handlers
- `templates/layouts/base.html` — nav/footer/meta; `partials.html` — pricing, guarantees, areas, service card, book CTA
- `templates/pages/*.html` — one file per page (`home`, `services`, `service`, `software-development`, `book`, `book-thanks`, `portfolio`, `quote*`, `admin`)
- `db/schema.sql` (embedded), `db/queries.sql` → `sqlc generate` → `db/sqlc/`; wrappers in `db/db.go`

## Run locally

```
sqlc generate
go run ./cmd/server          # http://localhost:8080
# or build.bat (deletes quotes.db, regenerates, builds, runs)
```

## Environment (`.env` in the app dir)

| Var | Default | Purpose |
|---|---|---|
| `PORT` | `8080` | listen port |
| `PROD` | – | set in production (affects BASE_URL default) |
| `BASE_URL` | `https://mchugh.com.au` if PROD else `http://localhost:PORT` | canonical origin; OAuth redirect + quote verification links |
| `PHONE` | empty | display phone; empty hides all phone UI |
| `CONTACT_EMAIL` | `james@mchugh.com.au` | contact email |
| `ONSITE_FEE` / `BLOCK_RATE` | `80` / `30` | published pricing (AUD) |
| `ADMIN_EMAIL` | – | Google account allowed into `/admin` |
| `GOOGLE_CLIENT_ID` / `GOOGLE_CLIENT_SECRET` | – | Google sign-in — admin only; customers never sign in |
| `TURNSTILE_SITE_KEY` / `TURNSTILE_SECRET_KEY` | – | Cloudflare Turnstile on the quote form (empty = check skipped, logged at startup; dev test pair `1x00000000000000000000AA` / `1x0000000000000000000000000000000AA`) |
| `ANTHROPIC_API_KEY` | – | AI estimate in quote flow (generated only after the email is verified) |
| `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` | – | Amazon SES credentials (IAM user `mchugh-au-mailer`); unset = email disabled |
| `AWS_REGION` | `ap-southeast-2` | SES region |
| `MAIL_FROM` | `James McHugh <james@mchugh.com.au>` | sender (must be a verified SES identity) |
| `NOTIFY_EMAIL` | `CONTACT_EMAIL` | where booking / verified-quote notifications go |

Booking requests are stored in the `bookings` table and listed at `/admin`.
When SES is configured, each booking emails `NOTIFY_EMAIL` (reply-to set to the
customer) and sends the customer a confirmation. The quote flow *requires* SES in
production: the customer is emailed a verification link, and once they click it the
estimate is generated, emailed to them, and `NOTIFY_EMAIL` is notified. One verified
quote per email address; a resubmission replaces an unconfirmed one. Without SES
the verification link is printed to the log (handy locally). Sends run in the
background and failures are only logged — the request is already saved. See `cmd/server/mail.go` and `mailer/`.

### Email (Amazon SES) setup

One-time, from your machine with an admin AWS profile: `AWS_PROFILE=tooltrack-admin
CF_TOKEN=<cloudflare token> scripts/ses-setup.sh`. It creates the send-only IAM
user + key, the `mchugh.com.au` SES domain identity, adds the DKIM CNAMEs to
Cloudflare (or prints them if `CF_TOKEN` is unset), and prints the `.env` lines
to add on the server. `james@mchugh.com.au` is already a verified sender so mail
flows before DKIM verification completes; DKIM just improves deliverability.

## Deploy

GitHub Actions (`.github/workflows/deploy.yml`) builds a static Linux binary and
runs `scripts/deploy-mchugh-com-au` on the server. Actions secrets needed on
this repo: `DEPLOY_HOST`, `DEPLOY_USER`, `DEPLOY_PORT`, `DEPLOY_SSH_KEY`.

### First-time provisioning (one-off, in order)

1. **Cloudflare** — zone `mchugh.com.au`: `A @` and `A www` → `172.105.178.43`,
   `AAAA @` and `AAAA www` → `2400:8907::f03c:92ff:fe4b:43ea`, all DNS-only (grey cloud).
2. **Server** —
   `curl -fsSL https://raw.githubusercontent.com/exploded/mchugh-com-au/main/scripts/server-setup.sh | sudo bash`
   (creates `/var/www/mchugh.com.au`, `.env` template, systemd unit on port 8181,
   sudoers, prints the Caddy block). It refuses to touch `/var/www/mchugh.au`.
3. **Server `.env`** — `sudo nano /var/www/mchugh.com.au/.env`: `PHONE=…`, `ADMIN_EMAIL`,
   Google/Turnstile/Anthropic keys, SES creds, pricing if different.
4. **Caddy** — add the printed block to `/etc/caddy/Caddyfile` (uses the box's shared
   `access_log` / `go_proxy` snippets; leave the existing `mchugh.au` block alone),
   `sudo systemctl reload caddy`.
5. **Google Cloud Console** — add `https://mchugh.com.au/auth/google/callback` to the
   OAuth client's authorised redirect URIs.
6. Push / `gh workflow run Deploy`, watch it go green, then check
   `https://mchugh.com.au/`, `/services/email-outlook`, `/book`, and that
   `https://mchugh.au` still serves the landing page.
