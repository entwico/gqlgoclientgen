package clientgen

import (
	"fmt"

	"github.com/vektah/gqlparser/v2/ast"
)

// atPos renders a "file:line: " prefix for generation errors when the AST
// node carries a source position, so errors point at the schema or document
// line that caused them.
func atPos(pos *ast.Position) string {
	if pos == nil || pos.Src == nil || pos.Src.Name == "" {
		return ""
	}

	return fmt.Sprintf("%s:%d: ", pos.Src.Name, pos.Line)
}
