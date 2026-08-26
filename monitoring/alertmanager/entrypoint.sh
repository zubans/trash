#!/bin/sh
# Alertmanager does not expand environment variables in its config, and the
# Telegram bot token must not be committed. The template is rendered at start
# into a file that only lives in the container's writable layer.
set -eu

if [ -z "${TELEGRAM_BOT_TOKEN:-}" ] || [ -z "${TELEGRAM_CHAT_ID:-}" ]; then
    echo "alertmanager: TELEGRAM_BOT_TOKEN and TELEGRAM_CHAT_ID are required" >&2
    echo "alertmanager: set them in .env — see doc/monitoring.md" >&2
    exit 1
fi

sed -e "s|\${TELEGRAM_BOT_TOKEN}|${TELEGRAM_BOT_TOKEN}|g" \
    -e "s|\${TELEGRAM_CHAT_ID}|${TELEGRAM_CHAT_ID}|g" \
    /etc/alertmanager/alertmanager.tmpl.yml > /tmp/alertmanager.yml

exec /bin/alertmanager \
    --config.file=/tmp/alertmanager.yml \
    --storage.path=/alertmanager \
    "$@"
