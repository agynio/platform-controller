{{- define "platform-controller.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "platform-controller.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name (include "platform-controller.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "platform-controller.labels" -}}
app.kubernetes.io/name: {{ include "platform-controller.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "platform-controller.selectorLabels" -}}
app.kubernetes.io/name: {{ include "platform-controller.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}
