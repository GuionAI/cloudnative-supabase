{{/*
Expand the name of the chart.
*/}}
{{- define "supabase.realtime.name" -}}
{{- default (print .Chart.Name "-realtime") .Values.realtime.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "supabase.realtime.fullname" -}}
{{- printf "%s-realtime" (include "supabase.name" .) -}}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "supabase.realtime.selectorLabels" -}}
app.kubernetes.io/name: {{ include "supabase.realtime.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "supabase.realtime.serviceAccountName" -}}
{{- if .Values.realtime.serviceAccount.create }}
{{- default (include "supabase.realtime.fullname" .) .Values.realtime.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.realtime.serviceAccount.name }}
{{- end }}
{{- end }}
