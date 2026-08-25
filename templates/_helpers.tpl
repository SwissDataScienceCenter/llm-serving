{{/*
Envoy Gateway full name.
*/}}
{{- define "envoy.fullname" -}}
  {{- printf "%s-envoy" .Release.Name -}}
{{- end -}}

{{/*
OpenWebUI full name.
*/}}
{{- define "openwebui.fullname" -}}
  {{- printf "%s-openwebui" .Release.Name -}}
{{- end -}}

{{/*

Telemetry full name.
*/}}
{{- define "telemetry.fullname" -}}
  {{- printf "%s-telemetry" .Release.Name -}}
{{- end -}}

{{/*

Init job full name.
*/}}
{{- define "initjob.fullname" -}}
  {{- printf "%s-init" .Release.Name -}}
{{- end -}}

{{/*
Base name for every resource of one model.

Usage: {{ include "model.fullname" (merge (dict "modelName" $name) $) }}
*/}}
{{- define "model.fullname" -}}
  {{- printf "%s-model-%s" .Release.Name .modelName -}}
{{- end -}}

{{/*
Base URL of this release's authentik OAuth2 provider, with a trailing slash.
Callers append the endpoint they need; authentik issues per-provider URLs, so this
prefix is also the `iss` claim on the tokens it signs.
*/}}
{{- define "authentik.providerUrl" -}}
  {{- printf "https://authentik.%s/application/o/%s/" .Values.envoy.baseDomain .Values.authentik.oauthApp.name -}}
{{- end -}}

{{/*
JWKS URI: use authentik if enabled, otherwise configurable
*/}}
{{- define "envoy.jwksUri" -}}
  {{- if .Values.envoy.security.jwksUri -}}
{{ .Values.envoy.security.jwksUri }}
  {{- else if .Values.authentik.enabled -}}
    {{- printf "%sjwks/" (include "authentik.providerUrl" .) -}}
  {{- end -}}
{{- end -}}

{{/*
Expected `iss` claim. One authentik signs every provider with the same key, so the
JWKS alone would also accept another application's tokens.
*/}}
{{- define "envoy.jwtIssuer" -}}
  {{- if .Values.envoy.security.jwksUri -}}
    {{- .Values.envoy.security.issuer | required "envoy.security.issuer is required alongside envoy.security.jwksUri, or the gateway accepts every token that JWKS validates" -}}
  {{- else if .Values.authentik.enabled -}}
    {{- include "authentik.providerUrl" . -}}
  {{- end -}}
{{- end -}}
