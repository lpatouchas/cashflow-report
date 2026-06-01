package web

import (
	_ "embed"
	"html/template"
)

//go:embed index.html
var indexHTML string

// indexTmpl renders the upload page, pre-filling the exclusion-rules editor.
var indexTmpl = template.Must(template.New("index").Parse(indexHTML))
