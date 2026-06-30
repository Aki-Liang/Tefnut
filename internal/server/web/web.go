package web

import (
	"embed"
	"html/template"
	"io/fs"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static
var staticFS embed.FS

// Templates holds the base layout template only. Page-specific block templates
// are parsed per-request by render() in pages.go, so that each page's
// {{define}} blocks override the layout's defaults correctly.
var Templates = template.Must(template.ParseFS(templatesFS, "templates/layout.html"))

// FS exposes the embedded template files for per-request cloning.
var FS = templatesFS

// Static is the embedded static asset filesystem rooted at "static".
var Static fs.FS = mustSub(staticFS, "static")

func mustSub(f embed.FS, dir string) fs.FS {
	sub, err := fs.Sub(f, dir)
	if err != nil {
		panic(err)
	}
	return sub
}
