{{ range .Hostnames }}
route {{.Name}} {{- range .Alternatives }} {{ . }}{{ end }}
{{range .Containers }}{{ if .ShouldProxy }}
    upstream {{ .Name }}:{{ .Port }}
{{ end }}{{ end }}
    # You can add or remove headers for all routes here, e.g.:
    # header default Strict-Transport-Security max-age=15768000
    # header delete Server
{{ range $k, $v := .Headers }}
    header replace {{ $k }} {{ $v }}
{{ end }}
{{ end }}