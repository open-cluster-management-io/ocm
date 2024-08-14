{{/* Create secret to access docker registry */}}
{{- define "imagePullSecret" }}
{{- with .Values.images }}
{{- if and .imageCredentials.userName .imageCredentials.password }}
{{- printf "{\"auths\": {\"%s\": {\"auth\": \"%s\"}}}" .registry (printf "%s:%s" .imageCredentials.userName .imageCredentials.password | b64enc) | b64enc }}
{{- else }}
{{- printf "{}" | b64enc }}
{{- end }}
{{- end }}
{{- end }}


{{/* Create random bootstrap token secret. */}}
{{- define "tokenID" }}
{{- printf "ocmhub" }}
{{- end }}
{{- define "tokenSecret" }}
{{- printf "%s" (randAlphaNum 6) }}
{{- end }}

{{/* Define the image tag. */}}
{{- define "imageTag" }}
{{- if .Values.images.tag }}
{{- printf "%s" .Values.images.tag }}
{{- else }}
{{- printf "%s" .Chart.AppVersion }}
{{- end }}
{{- end }}
