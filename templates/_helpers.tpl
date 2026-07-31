{{- define "sanitizeEnvVarName" -}}
{{ regexReplaceAll "[^A-Z0-9]+" (upper .) "_" }}
{{- end -}}

{{/*
Envoy Gateway full name
*/}}
{{- define "envoy.fullname" -}}
  {{- printf "%s-envoy" .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Backend name for a specific model
*/}}
{{- define "envoy.backendName" -}}
  {{- printf "%s-%s" (include "envoy.fullname" .) .modelName | trunc 63 | trimSuffix "-" -}}
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
