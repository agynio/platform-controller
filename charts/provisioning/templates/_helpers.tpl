{{- define "provisioning.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "provisioning.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name (include "provisioning.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "provisioning.labels" -}}
app.kubernetes.io/name: {{ include "provisioning.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "provisioning.selectorLabels" -}}
app.kubernetes.io/name: {{ include "provisioning.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}
