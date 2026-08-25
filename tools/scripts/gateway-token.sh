#!/usr/bin/env bash
# Obtain an authentik access token for the model gateway via the OAuth2 device code
# flow. The token is the only thing on stdout, so this works as
# `TOKEN=$(gateway-token.sh)`; prompts and progress go to stderr.
set -euo pipefail

if [[ -z "${BASE_DOMAIN:-}" || -z "${CLIENT_ID:-}" ]]; then
    cat >&2 <<'USAGE'
usage: BASE_DOMAIN=<envoy.baseDomain> CLIENT_ID=<authentik.oauthApp.clientId> gateway-token.sh

Prints an access token for https://gateway.$BASE_DOMAIN/v1 to stdout.
USAGE
    exit 2
fi

authentik="https://authentik.$BASE_DOMAIN"

# offline_access required for authentik to return a refresh token
init=$(curl -sS --max-time 30 "$authentik/application/o/device/" \
    -d client_id="$CLIENT_ID" \
    --data-urlencode "scope=openid profile email offline_access")

# Check for non-JSON body or empty code.
device_code=$(jq -r '.device_code // empty' 2>/dev/null <<<"$init" || true)
if [[ -z "$device_code" ]]; then
    printf '%s/application/o/device/ returned no device code:\n%s\n' "$authentik" "$init" >&2
    exit 1
fi

interval=$(jq -r '.interval // 5' <<<"$init")
expires_in=$(jq -r '.expires_in // 600' <<<"$init")

printf 'Approve this device, then leave this running:\n\n  %s\n\n' \
    "$(jq -r '.verification_uri_complete' <<<"$init")" >&2

deadline=$((SECONDS + expires_in))
while ((SECONDS < deadline)); do
    sleep "$interval"
    response=$(curl -sS --max-time 30 "$authentik/application/o/token/" \
        -d grant_type=urn:ietf:params:oauth:grant-type:device_code \
        -d client_id="$CLIENT_ID" \
        --data-urlencode "device_code=$device_code")

    case "$(jq -r '.error // "ok"' <<<"$response")" in
    ok)
        jq -r '.access_token' <<<"$response"
        exit 0
        ;;
    authorization_pending) ;;
    *)
        printf 'token request failed: %s\n' "$response" >&2
        exit 1
        ;;
    esac
done

printf 'timed out after %ss without approval\n' "$expires_in" >&2
exit 1
