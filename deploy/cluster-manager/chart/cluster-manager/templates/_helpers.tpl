{{/* Create secret to access docker registry */}}
{{- define "imagePullSecret" }}
{{- with .Values.images }}
{{- if and .imageCredentials.userName .imageCredentials.password }}
{{- printf "{\"auths\": {\"%s\": {\"auth\": \"%s\"}}}" .registry (printf "%s:%s" .imageCredentials.userName .imageCredentials.password | b64enc) | b64enc }}
{{- else if .imageCredentials.dockerConfigJson }}
{{- printf "%s" .imageCredentials.dockerConfigJson | b64enc }}
{{- else }}
{{- printf "{}" | b64enc }}
{{- end }}
{{- end }}
{{- end }}



{{- define "registrationImage" }}
{{- if .Values.images.overrides.registrationImage }}
{{- printf "%s" .Values.images.overrides.registrationImage }}
{{- else if .Values.images.tag }}
{{- printf "%s/registration:%s" .Values.images.registry .Values.images.tag }}
{{- else }}
{{- printf "%s/registration:%s" .Values.images.registry .Chart.AppVersion }}
{{- end }}
{{- end }}

{{- define "workImage" }}
{{- if .Values.images.overrides.workImage }}
{{- printf "%s" .Values.images.overrides.workImage }}
{{- else if .Values.images.tag }}
{{- printf "%s/work:%s" .Values.images.registry .Values.images.tag }}
{{- else }}
{{- printf "%s/work:%s" .Values.images.registry .Chart.AppVersion }}
{{- end }}
{{- end }}

{{- define "addOnManagerImage" }}
{{- if .Values.images.overrides.addOnManagerImage }}
{{- printf "%s" .Values.images.overrides.addOnManagerImage }}
{{- else if .Values.images.tag }}
{{- printf "%s/addon-manager:%s" .Values.images.registry .Values.images.tag }}
{{- else }}
{{- printf "%s/addon-manager:%s" .Values.images.registry .Chart.AppVersion }}
{{- end }}
{{- end }}

{{- define "placementImage" }}
{{- if .Values.images.overrides.placementImage }}
{{- printf "%s" .Values.images.overrides.placementImage }}
{{- else if .Values.images.tag }}
{{- printf "%s/placement:%s" .Values.images.registry .Values.images.tag }}
{{- else }}
{{- printf "%s/placement:%s" .Values.images.registry .Chart.AppVersion }}
{{- end }}
{{- end }}

{{- define "operatorImage" }}
{{- if .Values.images.overrides.operatorImage }}
{{- printf "%s" .Values.images.overrides.operatorImage }}
{{- else if .Values.images.tag }}
{{- printf "%s/registration-operator:%s" .Values.images.registry .Values.images.tag }}
{{- else }}
{{- printf "%s/registration-operator:%s" .Values.images.registry .Chart.AppVersion }}
{{- end }}
{{- end }}
