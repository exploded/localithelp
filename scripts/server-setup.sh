#!/bin/bash
set -euo pipefail

# One-time server setup for the Local IT Help (localithelp.com.au) Go web app.
# Usage: curl -fsSL https://raw.githubusercontent.com/exploded/localithelp/main/scripts/server-setup.sh | sudo bash
#
# NOTE: mchugh.au (no .com) is a SEPARATE static site served from /var/www/mchugh.au
# by the exploded/mchugh-au repo. This script must never touch that directory.

APP_DIR="/var/www/localithelp"
SERVICE="localithelp"
DEPLOY_USER="deploy"
PORT="8181"

case "$APP_DIR" in
    /var/www/mchugh.au|/var/www/mchugh.au/*)
        echo "Refusing to run: APP_DIR points at the mchugh.au landing-page directory." >&2
        exit 1 ;;
esac

echo "Setting up $SERVICE..."

# Create app directory
mkdir -p "$APP_DIR"

# .env template (only if absent — never overwrite real secrets)
if [ ! -f "$APP_DIR/.env" ]; then
cat > "$APP_DIR/.env" <<EOF
# ── localithelp.com.au (Local IT Help) ──
PORT=$PORT
PROD=true
APP_DIR=$APP_DIR
BASE_URL=https://localithelp.com.au

# Contact details shown on the site (PHONE empty = phone UI hidden).
# CONTACT_EMAIL is also the SES sender + notification address (default james@localithelp.com.au).
PHONE=
CONTACT_EMAIL=james@localithelp.com.au

# Pricing (AUD). Defaults: 80 visit fee, 30 per 15-minute block, 20% seniors discount
ONSITE_FEE=80
BLOCK_RATE=30
SENIORS_DISCOUNT_PCT=20

# Invoices: ABN and bank transfer details (BANK_BSB empty = bank details hidden)
ABN=14 723 053 435
BANK_ACCOUNT_NAME=James McHugh
BANK_BSB=
BANK_ACCOUNT_NO=

# Google tag. Nothing loads until an ID is set. Conversion labels come from the
# conversion actions you create in Google Ads; SAME_AS is a comma-separated list
# of profile URLs (Google Business Profile, ...) for the home-page JSON-LD.
GA4_ID=
GOOGLE_ADS_ID=
GOOGLE_ADS_BOOKING_LABEL=
GOOGLE_ADS_QUOTE_LABEL=
GOOGLE_ADS_CALL_LABEL=
SAME_AS=

# Admin + integrations (ADMIN_EMAIL = Google account allowed into /admin, default james67@gmail.com)
ADMIN_EMAIL=james67@gmail.com
GOOGLE_CLIENT_ID=
GOOGLE_CLIENT_SECRET=
ANTHROPIC_API_KEY=
# Cloudflare Turnstile (bot check on the quote form). Widget: dashboard → Turnstile → add site.
TURNSTILE_SITE_KEY=
TURNSTILE_SECRET_KEY=

# Email notifications via Amazon SES (scripts/ses-setup.sh prints these). Empty = disabled.
AWS_REGION=ap-southeast-2
AWS_ACCESS_KEY_ID=
AWS_SECRET_ACCESS_KEY=
EOF
    chmod 640 "$APP_DIR/.env"
    echo "Created $APP_DIR/.env template — edit it before starting the service."
fi

chown -R www-data:www-data "$APP_DIR"

# Install deploy script if a deploy bundle is present
cp /tmp/localithelp-deploy/scripts/deploy-localithelp /usr/local/bin/deploy-localithelp 2>/dev/null || true
chmod +x /usr/local/bin/deploy-localithelp 2>/dev/null || true

# Create systemd service
cat > /etc/systemd/system/${SERVICE}.service <<EOF
[Unit]
Description=Local IT Help (localithelp.com.au) web app
After=network.target

[Service]
Type=simple
User=www-data
WorkingDirectory=$APP_DIR
ExecStart=$APP_DIR/localithelp
EnvironmentFile=$APP_DIR/.env
Environment=PORT=$PORT
Environment=PROD=true
Environment=APP_DIR=$APP_DIR
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable "$SERVICE"

# Sudoers for deploy user
cat > /etc/sudoers.d/${SERVICE} <<EOF
$DEPLOY_USER ALL=(ALL) NOPASSWD: /usr/local/bin/deploy-localithelp
$DEPLOY_USER ALL=(ALL) NOPASSWD: /usr/bin/systemctl stop $SERVICE
EOF
chmod 440 /etc/sudoers.d/${SERVICE}

cat <<EOF

Setup complete.

Next steps:
  1. Edit $APP_DIR/.env (PHONE, ADMIN_EMAIL, Google/Turnstile/Anthropic keys).
  2. Add this to /etc/caddy/Caddyfile and run: sudo systemctl reload caddy

     www.localithelp.com.au, mchugh.com.au, www.mchugh.com.au {
         redir https://localithelp.com.au{uri} permanent
     }

     localithelp.com.au {
         import access_log
         reverse_proxy 127.0.0.1:$PORT {
             import go_proxy
         }
     }

     (The app also 301s any non-canonical Host itself, so an older combined
     block still works — the split block just saves a proxy hop. The
     mchugh.com.au redirect preserves paths for the pre-rebrand domain.)

     (Leave the existing mchugh.au / www.mchugh.au block alone — that is the
     separate static landing page.)

  3. Deploy from GitHub Actions (or: sudo /usr/local/bin/deploy-localithelp), then
     sudo systemctl enable --now $SERVICE
EOF
