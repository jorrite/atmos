# atmos:template
# Sizing Plan

{{- range .Config.sizing.regions }}
- `{{ .name }}`: {{ .instance_size }}
{{- end }}
