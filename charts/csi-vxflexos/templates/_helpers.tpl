{{/*
Return true if storage capacity tracking is enabled and is supported based on k8s version
*/}}
{{- define "csi-vxflexos.isStorageCapacitySupported" -}}
  {{- if eq .Values.storageCapacity.enabled true -}}
    {{- if and (eq .Capabilities.KubeVersion.Major "1") (ge (trimSuffix "+" .Capabilities.KubeVersion.Minor) "24") -}}
        {{- true -}}
    {{- end -}}
  {{- end -}}
{{- end -}}