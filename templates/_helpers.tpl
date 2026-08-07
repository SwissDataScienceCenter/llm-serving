{{/*
Envoy Gateway full name
*/}}
{{- define "envoy.fullname" -}}
  {{- printf "%s-envoy" .Release.Name -}}
{{- end -}}

{{/*
Base name for every resource of one model. Fails when too long.

The cap is the 63-character DNS label limit minus the revision suffix Knative
appends to derive names from the Service.

Usage: {{ include "model.fullname" (merge (dict "modelName" $name) $) }}
*/}}
{{- define "model.fullname" -}}
  {{- $full := printf "%s-model-%s" .Release.Name .modelName -}}
  {{- $cap := sub 63 (len "-00001") -}}
  {{- if gt (len $full) (int $cap) -}}
    {{- fail (printf "model resource name %q exceeds the %d characters available: shorten the release name or the model key" $full $cap) -}}
  {{- end -}}
  {{- $full -}}
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
