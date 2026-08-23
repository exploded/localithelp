#!/usr/bin/env bash
# One-time Amazon SES setup for localithelp.com.au (run from your machine, not
# the server). Originally written for mchugh.com.au; the send policy keeps the
# old domain's identities so the pre-rebrand sender keeps working during the
# transition.
#
# What it does (idempotent — safe to re-run):
#   1. Creates IAM user `mchugh-au-mailer` with a send-only policy limited to
#      the localithelp.com.au + mchugh.com.au SES identities (plus any
#      configuration set, since a default set attached to an identity is also
#      authorised on send), and an access key for it.
#   2. Ensures the SES domain identity `localithelp.com.au` exists (Easy DKIM).
#   3. If CF_TOKEN is set (Cloudflare API token with Zone:DNS:Edit on
#      localithelp.com.au), adds the 3 DKIM CNAME records; otherwise prints them.
#   4. Prints the lines to append to /var/www/localithelp/.env on the server.
#
# Requirements: aws CLI v2 with an admin profile, curl, jq.
# Usage:
#   AWS_PROFILE=tooltrack-admin CF_TOKEN=... scripts/ses-setup.sh
set -euo pipefail

REGION=${AWS_REGION:-ap-southeast-2}
DOMAIN=localithelp.com.au
FROM_ADDR=james@localithelp.com.au
OLD_DOMAIN=mchugh.com.au
OLD_FROM_ADDR=james@mchugh.com.au
USER_NAME=mchugh-au-mailer
POLICY_NAME=ses-send-mchugh-com-au
CF_ZONE_NAME=$DOMAIN

ACCOUNT=$(aws sts get-caller-identity --query Account --output text)
echo "AWS account $ACCOUNT, region $REGION"

# ── 1. IAM user + send-only policy ──────────────────────────────────────────
if ! aws iam get-user --user-name "$USER_NAME" >/dev/null 2>&1; then
  aws iam create-user --user-name "$USER_NAME" --tags Key=app,Value=$DOMAIN >/dev/null
  echo "created IAM user $USER_NAME"
else
  echo "IAM user $USER_NAME exists"
fi

POLICY_DOC=$(cat <<JSON
{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Action": ["ses:SendEmail", "ses:SendRawEmail"],
    "Resource": [
      "arn:aws:ses:${REGION}:${ACCOUNT}:identity/${FROM_ADDR}",
      "arn:aws:ses:${REGION}:${ACCOUNT}:identity/${DOMAIN}",
      "arn:aws:ses:${REGION}:${ACCOUNT}:identity/${OLD_FROM_ADDR}",
      "arn:aws:ses:${REGION}:${ACCOUNT}:identity/${OLD_DOMAIN}",
      "arn:aws:ses:${REGION}:${ACCOUNT}:configuration-set/*"
    ]
  }]
}
JSON
)
aws iam put-user-policy --user-name "$USER_NAME" --policy-name "$POLICY_NAME" --policy-document "$POLICY_DOC"
echo "policy $POLICY_NAME attached"

# Access key: only mint one if the user has none (keys can't be re-read later).
NKEYS=$(aws iam list-access-keys --user-name "$USER_NAME" --query 'length(AccessKeyMetadata)' --output text)
if [ "$NKEYS" = "0" ]; then
  read -r AKID SECRET < <(aws iam create-access-key --user-name "$USER_NAME" \
      --query 'AccessKey.[AccessKeyId,SecretAccessKey]' --output text)
  echo "created access key $AKID"
else
  AKID="(existing — see IAM console; create a new key if you lost the secret)"
  SECRET="(existing)"
  echo "user already has $NKEYS access key(s); not creating another"
fi

# ── 2. SES domain identity (Easy DKIM) ──────────────────────────────────────
if ! aws sesv2 get-email-identity --email-identity "$DOMAIN" >/dev/null 2>&1; then
  aws sesv2 create-email-identity --email-identity "$DOMAIN" --tags Key=app,Value=$DOMAIN >/dev/null
  echo "created SES identity $DOMAIN"
fi
TOKENS=$(aws sesv2 get-email-identity --email-identity "$DOMAIN" --query 'DkimAttributes.Tokens[]' --output text)
DKIM_STATUS=$(aws sesv2 get-email-identity --email-identity "$DOMAIN" --query 'DkimAttributes.Status' --output text)
echo "SES identity $DOMAIN DKIM status: $DKIM_STATUS"

# ── 3. DKIM CNAMEs in Cloudflare ────────────────────────────────────────────
if [ -n "${CF_TOKEN:-}" ]; then
  ZID=$(curl -fsS -H "Authorization: Bearer $CF_TOKEN" \
        "https://api.cloudflare.com/client/v4/zones?name=$CF_ZONE_NAME" | jq -r '.result[0].id')
  [ "$ZID" != "null" ] || { echo "Cloudflare zone $CF_ZONE_NAME not found for this token" >&2; exit 1; }
  for t in $TOKENS; do
    NAME="${t}._domainkey.${DOMAIN}"
    EXISTS=$(curl -fsS -H "Authorization: Bearer $CF_TOKEN" \
        "https://api.cloudflare.com/client/v4/zones/$ZID/dns_records?type=CNAME&name=$NAME" | jq -r '.result | length')
    if [ "$EXISTS" = "0" ]; then
      curl -fsS -X POST -H "Authorization: Bearer $CF_TOKEN" -H "Content-Type: application/json" \
        "https://api.cloudflare.com/client/v4/zones/$ZID/dns_records" \
        --data "{\"type\":\"CNAME\",\"name\":\"$NAME\",\"content\":\"${t}.dkim.amazonses.com\",\"ttl\":1,\"proxied\":false}" \
        | jq -r '"  added CNAME \(.result.name)"'
    else
      echo "  CNAME $NAME already present"
    fi
  done
  echo "DKIM records in place — SES verifies within ~minutes to an hour (re-run to see status)."
else
  echo
  echo "CF_TOKEN not set — add these CNAME records (DNS-only / grey cloud) in the $DOMAIN zone:"
  for t in $TOKENS; do
    echo "  ${t}._domainkey.${DOMAIN}   CNAME   ${t}.dkim.amazonses.com"
  done
fi

# ── 4. Server .env lines ────────────────────────────────────────────────────
cat <<ENV

On the server, /var/www/localithelp/.env needs (then: sudo systemctl restart localithelp):

# Email notifications via Amazon SES
AWS_REGION=$REGION
AWS_ACCESS_KEY_ID=$AKID
AWS_SECRET_ACCESS_KEY=$SECRET
# Sender + notifications use CONTACT_EMAIL — flip to $FROM_ADDR once DKIM shows SUCCESS
CONTACT_EMAIL=$FROM_ADDR
ENV
