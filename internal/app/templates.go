package app

import (
	"embed"
	"html/template"
)

// The HTML for every screen lives in templates/*.html, one file per feature
// (shell.html, payments.html, request_detail.html, …), and is compiled into
// the binary. Each {{define "name"}} in those files is a page or a partial;
// a handler renders a page with a.render(w, r, "name", data). Every full page
// starts with {{template "top" .}} (head, sidebar, flash messages) and ends
// with {{template "bottom" .}}, both in shell.html. The functions a template
// can call are listed in templateFuncs.
//
//go:embed templates/*.html
var templateFiles embed.FS

// parseTemplates compiles every template file into one set. It panics on a
// syntax error or an unknown function, so a broken template stops the server
// at startup rather than on the first request for that page.
func parseTemplates() *template.Template {
	return template.Must(template.New("base").Funcs(templateFuncs()).ParseFS(templateFiles, "templates/*.html"))
}
