{{/*
Return the appropriate sidecar images based on k8s version
*/}}
{{- define "csi-vxflexos.attacherImage" -}}
  {{- if eq .Capabilities.KubeVersion.Major "1" }}
    {{- if and (ge (trimSuffix "+" .Capabilities.KubeVersion.Minor) "21") (le (trimSuffix "+" .Capabilities.KubeVersion.Minor) "26") -}}
      {{- print "k8s.gcr.io/sig-storage/csi-attacher:v4.0.0" -}}
    {{- end -}}
  {{- end -}}
{{- end -}}

{{- define "csi-vxflexos.provisionerImage" -}}
  {{- if eq .Capabilities.KubeVersion.Major "1" }}
    {{- if and (ge (trimSuffix "+" .Capabilities.KubeVersion.Minor) "21") (le (trimSuffix "+" .Capabilities.KubeVersion.Minor) "26") -}}
      {{- print "k8s.gcr.io/sig-storage/csi-provisioner:v3.3.0" -}}
    {{- end -}}
  {{- end -}}
{{- end -}}

{{- define "csi-vxflexos.snapshotterImage" -}}
  {{- if eq .Capabilities.KubeVersion.Major "1" }}
    {{- if and (ge (trimSuffix "+" .Capabilities.KubeVersion.Minor) "21") (le (trimSuffix "+" .Capabilities.KubeVersion.Minor) "26") -}}
      {{- print "k8s.gcr.io/sig-storage/csi-snapshotter:v6.1.0" -}}
    {{- end -}}
  {{- end -}}
{{- end -}}

{{- define "csi-vxflexos.resizerImage" -}}
  {{- if eq .Capabilities.KubeVersion.Major "1" }}
    {{- if and (ge (trimSuffix "+" .Capabilities.KubeVersion.Minor) "21") (le (trimSuffix "+" .Capabilities.KubeVersion.Minor) "26") -}}
      {{- print "k8s.gcr.io/sig-storage/csi-resizer:v1.6.0" -}}
    {{- end -}}
  {{- end -}}
{{- end -}}

{{- define "csi-vxflexos.registrarImage" -}}
  {{- if eq .Capabilities.KubeVersion.Major "1" }}
    {{- if and (ge (trimSuffix "+" .Capabilities.KubeVersion.Minor) "21") (le (trimSuffix "+" .Capabilities.KubeVersion.Minor) "26") -}}
      {{- print "k8s.gcr.io/sig-storage/csi-node-driver-registrar:v2.6.0" -}}
    {{- end -}}
  {{- end -}}
{{- end -}}

{{- define "csi-vxflexos.healthmonitorImage" -}}
  {{- if eq .Capabilities.KubeVersion.Major "1" }}
    {{- if and (ge (trimSuffix "+" .Capabilities.KubeVersion.Minor) "21") (le (trimSuffix "+" .Capabilities.KubeVersion.Minor) "26") -}}
      {{- print "gcr.io/k8s-staging-sig-storage/csi-external-health-monitor-controller:v0.7.0" -}}
    {{- end -}}
  {{- end -}}
{{- end -}}
