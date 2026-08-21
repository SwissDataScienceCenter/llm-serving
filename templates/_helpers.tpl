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
JWKS URI: use authentik if enabled, otherwise configurable
*/}}
{{- define "envoy.jwksUri" -}}
  {{- if .Values.envoy.security.jwksUri -}}
{{ .Values.envoy.security.jwksUri }}
  {{- else if .Values.authentik.enabled -}}
    {{- printf "https://authentik.%s/application/o/%s/jwks/" .Values.envoy.baseDomain .Values.authentik.oauthApp.name -}}
  {{- end -}}
{{- end -}}

{{/*
Key OpenWebUI signs its forwarded per-user identity JWT with, empty when the feature
is off. Also gated on openwebui.enabled.
*/}}
{{- define "openwebui.userJwtSecret" -}}
  {{- if and .Values.openwebui.enabled .Values.openwebui.forwardUserJwtSecret -}}
{{ .Values.openwebui.forwardUserJwtSecret }}
  {{- end -}}
{{- end -}}
