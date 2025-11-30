{{/*
Expand the name of the chart.
*/}}
{{- define "supabase.auth.name" -}}
{{- default (print .Chart.Name "-auth") .Values.auth.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "supabase.auth.fullname" -}}
{{- printf "%s-auth" (include "supabase.name" .) -}}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "supabase.auth.selectorLabels" -}}
app.kubernetes.io/name: {{ include "supabase.auth.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "supabase.auth.serviceAccountName" -}}
{{- if .Values.auth.serviceAccount.create }}
{{- default (include "supabase.auth.fullname" .) .Values.auth.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.auth.serviceAccount.name }}
{{- end }}
{{- end }}