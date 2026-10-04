{{- define "permwatch.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "permwatch.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else if contains (include "permwatch.name" .) .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name (include "permwatch.name" .) | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}

{{- define "permwatch.selectorLabels" -}}
app.kubernetes.io/name: {{ include "permwatch.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "permwatch.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version }}
{{ include "permwatch.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "permwatch.secretName" -}}
{{- default (include "permwatch.fullname" .) .Values.alerts.existingSecret }}
{{- end }}

{{- define "permwatch.hasAlertSecret" -}}
{{- if or .Values.alerts.existingSecret .Values.alerts.webhookUrl .Values.alerts.telegramBotToken }}true{{ end }}
{{- end }}
