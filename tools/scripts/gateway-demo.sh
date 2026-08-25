#!/usr/bin/env bash
# Smoke-test the model gateway end to end: list the models it serves, then send one
# message to the first of them.
set -euo pipefail

BASE_DOMAIN="${BASE_DOMAIN:-}"
if [[ -z "$BASE_DOMAIN" ]]; then
    cat >&2 <<'USAGE'
usage: BASE_DOMAIN=<envoy.baseDomain> CLIENT_ID=<authentik.oauthApp.clientId> gateway-demo.sh

Export GATEWAY_TOKEN to reuse a token and skip the browser approval, and MODEL to
pick a model other than the first one listed.
USAGE
    exit 2
fi

here=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
TOKEN="${GATEWAY_TOKEN:-$("$here/gateway-token.sh")}"

gateway="https://gateway.$BASE_DOMAIN/v1"
auth=(-H "Authorization: Bearer $TOKEN")

echo "== models ==" >&2
models=$(curl -sS "$gateway/models" "${auth[@]}")
if ! jq -e '.data' >/dev/null 2>&1 <<<"$models"; then
    printf 'could not list models: %s\n' "$models" >&2
    exit 1
fi
jq -r '.data[].id' <<<"$models"

model="${MODEL:-$(jq -r '.data[0].id // empty' <<<"$models")}"
if [[ -z "$model" ]]; then
    echo 'no models are configured on the gateway' >&2
    exit 1
fi

printf '\n== completion (%s) ==\n' "$model" >&2
# A model that has scaled to zero takes minutes to answer the first request.
curl -sS --max-time 600 "$gateway/chat/completions" "${auth[@]}" \
    -H 'Content-Type: application/json' \
    -d "$(jq -nc --arg m "$model" \
        '{model: $m, messages: [{role: "user", content: "Reply with exactly: hello"}]}')" |
    jq -r '.choices[0].message.content // .'
