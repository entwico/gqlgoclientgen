package clientgen

import (
	"bytes"
	_ "embed"
	"fmt"
	"go/format"
	"text/template"
)

// The client scaffolding (connection handling, options, reconnection loop) is
// identical for every schema, so it lives as real Go code in a template
// instead of being built statement by statement. Only the package name is
// substituted.
//
//go:embed templates/client.go.tmpl
var clientTemplateText string

var clientTemplate = template.Must(template.New("client").Parse(clientTemplateText))

// renderClientTemplate renders the static client file for a package.
func renderClientTemplate(packageName, header string) ([]byte, error) {
	var buf bytes.Buffer

	err := clientTemplate.Execute(&buf, struct{ PackageName, Header string }{PackageName: packageName, Header: header})
	if err != nil {
		return nil, fmt.Errorf("failed to render client template: %w", err)
	}

	// the template is formatted Go, but guard against drift
	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("client template produced invalid Go: %w", err)
	}

	return formatted, nil
}
