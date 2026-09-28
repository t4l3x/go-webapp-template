THIRD-PARTY SOFTWARE NOTICES AND LICENSES

This project is licensed under the Apache License 2.0 (see LICENSE). It
includes the third-party Go modules listed below, each distributed under
its own license, reproduced in full.

The Go standard library and runtime linked into every binary are
licensed under the BSD-3-Clause license: https://go.dev/LICENSE

GENERATED FILE - do not edit. Regenerate with `make licenses`.
{{- $prev := "" }}
{{ range . }}
================================================================================
Module:  {{ .Name }}
Version: {{ .Version }}
License: {{ .LicenseName }}
Source:  {{ .LicenseURL }}
================================================================================
{{ if eq .LicenseURL $prev }}
(Same license file as the entry above.)
{{ else }}
{{ .LicenseText }}
{{ end }}
{{- $prev = .LicenseURL }}
{{- end }}
