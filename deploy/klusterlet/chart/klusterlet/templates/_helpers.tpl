
{{/*
Create secret to access docker registry
*/}}
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

{{/* Define the image tag. */}}
{{- define "imageTag" }}
{{- if .Values.images.tag }}
{{- printf "%s" .Values.images.tag }}
{{- else }}
{{- printf "%s" .Chart.AppVersion }}
{{- end }}
{{- end }}
