
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

{{- define "operatorImage" }}
{{- if .Values.images.overrides.operatorImage }}
{{- printf "%s" .Values.images.overrides.operatorImage }}
{{- else if .Values.images.tag }}
{{- printf "%s/registration-operator:%s" .Values.images.registry .Values.images.tag }}
{{- else }}
{{- printf "%s/registration-operator:%s" .Values.images.registry .Chart.AppVersion }}
{{- end }}
{{- end }}

{{/*
 agentNamespace is the klusterlet name in hosted mode.
 agentNamespace is the klusterlet.namespace in default mode.
 agentNamespace is open-cluster-management-agent if klusterlet.namespace is empty in default mode.
*/}}
{{- define "agentNamespace" }}
{{- if or ( eq .Values.klusterlet.mode "Hosted") (eq .Values.klusterlet.mode "SingletonHosted") }}
{{- printf "%s" .Values.klusterlet.name }}
{{- else if .Values.klusterlet.namespace }}
{{- printf "%s" .Values.klusterlet.namespace }}
{{- else }}
{{- printf "open-cluster-management-agent" }}
{{- end }}
{{- end }}


{{- define "klusterletName" }}
{{- if .Values.klusterlet.name }}
{{- printf "%s" .Values.klusterlet.name }}
{{- else if  or ( eq .Values.klusterlet.mode "Hosted") (eq .Values.klusterlet.mode "SingletonHosted") }}
{{- printf "klusterlet-%s" .Values.klusterlet.clusterName }}
{{- else }}
{{- printf "klusterlet" }}
{{- end }}
{{- end }}

{{- define "klusterletNamespace" }}
{{- if .Values.klusterlet.namespace }}
{{- printf "%s" .Values.klusterlet.namespace }}
{{- else if  or ( eq .Values.klusterlet.mode "Hosted") (eq .Values.klusterlet.mode "SingletonHosted") }}
{{- printf "open-cluster-management-%s" .Values.klusterlet.clusterName }}
{{- else }}
{{- printf "open-cluster-management-agent" }}
{{- end }}
{{- end }}
