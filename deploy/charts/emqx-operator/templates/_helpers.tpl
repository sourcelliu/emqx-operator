{{/* vim: set filetype=mustache: */}}
{{/*
Expand the name of the chart.
*/}}
{{- define "emqx-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "emqx-operator.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "emqx-operator.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "emqx-operator.labels" -}}
helm.sh/chart: {{ include "emqx-operator.chart" . }}
{{ include "emqx-operator.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "emqx-operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "emqx-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "emqx-operator.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "emqx-operator.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Determine whether this release should manage control plane resources.
Priority: .Values.global.controlPlane > .Values.controlPlane > true.
*/}}
{{- define "emqx-operator.controlPlaneEnabled" -}}
{{- $state := dict "value" true -}}
{{- if hasKey .Values "controlPlane" -}}
  {{- $_ := set $state "value" (index .Values "controlPlane") -}}
{{- end -}}
{{- if hasKey .Values "global" -}}
  {{- $global := index .Values "global" -}}
  {{- if and $global (kindIs "map" $global) (hasKey $global "controlPlane") -}}
    {{- $_ := set $state "value" (index $global "controlPlane") -}}
  {{- end -}}
{{- end -}}
{{- if (index $state "value") -}}true{{- else -}}false{{- end -}}
{{- end -}}

{{/*
Return true when CRDs should be rendered for this release.
*/}}
{{- define "emqx-operator.renderCRDs" -}}
{{- $controlPlane := eq (include "emqx-operator.controlPlaneEnabled" .) "true" -}}
{{- $state := dict "skip" false -}}
{{- if hasKey .Values "skipCRDs" -}}
  {{- $value := index .Values "skipCRDs" -}}
  {{- if kindIs "bool" $value -}}
    {{- $_ := set $state "skip" $value -}}
  {{- end -}}
{{- end -}}
{{- if not $controlPlane -}}
  {{- $_ := set $state "skip" true -}}
{{- end -}}
{{- if not (index $state "skip") -}}true{{- else -}}false{{- end -}}
{{- end -}}
