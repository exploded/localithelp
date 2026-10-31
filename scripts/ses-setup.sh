#!/usr/bin/env bash
# One-time Amazon SES setup for localithelp.com.au (run from your machine, not
# the server). Originally written for mchugh.com.au; the send policy keeps the
# old domain's identities so the pre-rebrand sender keeps working during the
# transition.
#
# What it does (idempotent — safe to re-run):
#   1. Creates IAM user `localithelp-mailer` with a send-only policy limited to
#      the localithelp.com.au + mchugh.com.au SES identities (plus any
#      configuration set, since a default set attached to an identity is also
#      authorised on send), and an access key for it.
#   2. Ensures the SES domain identity `localithelp.com.au` exists (Easy DKIM).
#   3. If CF_TOKEN is set (Cloudflare API token with Zone:DNS:Edit on
#      localithelp.com.au), adds the 3 DKIM CNAME records; otherwise prints them.
#   4. Creates the `localithelp` configuration set, which publishes bounces and
#      complaints to the SNS topic `localithelp-ses-events`, and subscribes
#      https://localithelp.com.au/webhooks/ses to the topic once the live site
#      is ready for it.
#   5. Prints the lines to append to /var/www/localithelp/.env on the server.
#
# Requirements: aws CLI v2 with an admin profile, curl, jq.
# Usage:
#   AWS_PROFILE=aws-admin CF_TOKEN=... scripts/ses-setup.sh
#
# Step 4 needs two runs: the site only accepts messages from the topic named in
# SES_EVENTS_TOPIC_ARN and answers 404 until it's set. Run once, add the two
# SES_* lines to the server .env, restart, then run again to subscribe.
set -euo pipefail

REGION=${AWS_REGION:-ap-southeast-2}
DOMAIN=localithelp.com.au
FROM_ADDR=james@localithelp.com.au
OLD_DOMAIN=mchugh.com.au
OLD_FROM_ADDR=james@mchugh.com.au
USER_NAME=localithelp-mailer
POLICY_NAME=ses-send-localithelp-com-au
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

# ── 4. Bounce + complaint events → SNS → /webhooks/ses ─────────────────────
CONFIG_SET=localithelp
TOPIC_NAME=localithelp-ses-events
DEST_NAME=sns-bounces
WEBHOOK=https://${DOMAIN}/webhooks/ses

TOPIC_ARN=$(aws sns create-topic --region "$REGION" --name "$TOPIC_NAME" --query TopicArn --output text)
echo "SNS topic $TOPIC_ARN"
# The app only accepts SHA256 signatures.
aws sns set-topic-attributes --region "$REGION" --topic-arn "$TOPIC_ARN" \
  --attribute-name SignatureVersion --attribute-value 2
# Owner keeps full control; SES may publish, but only for this account's set.
TOPIC_POLICY=$(cat <<JSON
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "owner",
      "Effect": "Allow",
      "Principal": {"AWS": "arn:aws:iam::${ACCOUNT}:root"},
      "Action": ["SNS:GetTopicAttributes", "SNS:SetTopicAttributes", "SNS:AddPermission",
                 "SNS:RemovePermission", "SNS:DeleteTopic", "SNS:Subscribe",
                 "SNS:ListSubscriptionsByTopic", "SNS:Publish"],
      "Resource": "${TOPIC_ARN}"
    },
    {
      "Sid": "ses-publish",
      "Effect": "Allow",
      "Principal": {"Service": "ses.amazonaws.com"},
      "Action": "SNS:Publish",
      "Resource": "${TOPIC_ARN}",
      "Condition": {
        "StringEquals": {"AWS:SourceAccount": "${ACCOUNT}"},
        "ArnLike": {"AWS:SourceArn": "arn:aws:ses:${REGION}:${ACCOUNT}:configuration-set/${CONFIG_SET}"}
      }
    }
  ]
}
JSON
)
aws sns set-topic-attributes --region "$REGION" --topic-arn "$TOPIC_ARN" \
  --attribute-name Policy --attribute-value "$TOPIC_POLICY"

if ! aws sesv2 get-configuration-set --region "$REGION" --configuration-set-name "$CONFIG_SET" >/dev/null 2>&1; then
  aws sesv2 create-configuration-set --region "$REGION" --configuration-set-name "$CONFIG_SET" \
    --tags Key=app,Value=$DOMAIN >/dev/null
  echo "created SES configuration set $CONFIG_SET"
else
  echo "SES configuration set $CONFIG_SET exists"
fi
DEST="{\"Enabled\":true,\"MatchingEventTypes\":[\"BOUNCE\",\"COMPLAINT\"],\"SnsDestination\":{\"TopicArn\":\"$TOPIC_ARN\"}}"
NDEST=$(aws sesv2 get-configuration-set-event-destinations --region "$REGION" --configuration-set-name "$CONFIG_SET" \
  --query "length(EventDestinations[?Name=='$DEST_NAME'])" --output text)
if [ "$NDEST" = "0" ]; then
  aws sesv2 create-configuration-set-event-destination --region "$REGION" --configuration-set-name "$CONFIG_SET" \
    --event-destination-name "$DEST_NAME" --event-destination "$DEST"
  echo "created event destination $DEST_NAME (bounces + complaints → $TOPIC_NAME)"
else
  aws sesv2 update-configuration-set-event-destination --region "$REGION" --configuration-set-name "$CONFIG_SET" \
    --event-destination-name "$DEST_NAME" --event-destination "$DEST"
  echo "event destination $DEST_NAME up to date"
fi

# An empty POST gets 400 from a configured site, 404 before SES_EVENTS_TOPIC_ARN is set.
STATUS=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$WEBHOOK" || true)
if [ "$STATUS" = "404" ]; then
  echo "  $WEBHOOK answers 404: the server doesn't have SES_EVENTS_TOPIC_ARN yet (or the code isn't deployed)."
  echo "  Add the SES_* lines below, restart, then re-run this script to subscribe."
else
  SUB=$(aws sns list-subscriptions-by-topic --region "$REGION" --topic-arn "$TOPIC_ARN" \
    --query "Subscriptions[?Endpoint=='$WEBHOOK'].SubscriptionArn | [0]" --output text)
  if [ "$SUB" = "None" ] || [ "$SUB" = "PendingConfirmation" ]; then
    aws sns subscribe --region "$REGION" --topic-arn "$TOPIC_ARN" --protocol https \
      --notification-endpoint "$WEBHOOK" >/dev/null
    echo "  subscribed $WEBHOOK; the site confirms it itself (re-run to check it's no longer pending)"
  else
    echo "  $WEBHOOK subscribed ($SUB)"
  fi
fi

# ── 5. Server .env lines ────────────────────────────────────────────────────
cat <<ENV

On the server, /var/www/localithelp/.env needs (then: sudo systemctl restart localithelp):

# Email notifications via Amazon SES
AWS_REGION=$REGION
AWS_ACCESS_KEY_ID=$AKID
AWS_SECRET_ACCESS_KEY=$SECRET
# Sender + notifications use CONTACT_EMAIL — flip to $FROM_ADDR once DKIM shows SUCCESS
CONTACT_EMAIL=$FROM_ADDR
# Bounce + complaint tracking (POST /webhooks/ses)
SES_CONFIGURATION_SET=$CONFIG_SET
SES_EVENTS_TOPIC_ARN=$TOPIC_ARN
ENV
