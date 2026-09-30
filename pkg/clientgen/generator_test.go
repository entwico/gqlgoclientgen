package clientgen

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// generateFixture runs the full generation pipeline and returns the generated files by name.
func generateFixture(t *testing.T, schemaPath, documentsPath string) map[string]string {
	t.Helper()

	outDir := t.TempDir()

	g, err := New(NewConfig(schemaPath, "testclient", outDir, documentsPath))
	require.NoError(t, err)
	require.NoError(t, g.LoadSchema())
	require.NoError(t, g.LoadDocuments())
	require.NoError(t, g.Generate())

	entries, err := os.ReadDir(outDir)
	require.NoError(t, err)

	files := make(map[string]string, len(entries))

	for _, entry := range entries {
		content, err := os.ReadFile(filepath.Join(outDir, entry.Name()))
		require.NoError(t, err)

		files[entry.Name()] = string(content)
	}

	return files
}

func TestGenerate_OutputIsValidGo(t *testing.T) {
	schemaPath := filepath.Join("testdata", "determinism", "schema.graphql")
	documentsPath := filepath.Join("testdata", "determinism", "documents")

	files := generateFixture(t, schemaPath, documentsPath)
	require.NotEmpty(t, files)

	for name, content := range files {
		fset := token.NewFileSet()

		parsed, err := parser.ParseFile(fset, name, content, 0)
		require.NoError(t, err, "generated file %s is not valid Go", name)

		// overlapping fragment spreads and inline fragments must dedupe into a
		// single field per struct, otherwise the output does not compile
		ast.Inspect(parsed, func(n ast.Node) bool {
			structType, ok := n.(*ast.StructType)
			if !ok {
				return true
			}

			seen := make(map[string]bool)

			for _, field := range structType.Fields.List {
				for _, fieldName := range field.Names {
					assert.False(t, seen[fieldName.Name], "duplicate field %s in a struct in %s", fieldName.Name, name)
					seen[fieldName.Name] = true
				}
			}

			return true
		})
	}
}

func TestGenerate_OutputIsDeterministic(t *testing.T) {
	schemaPath := filepath.Join("testdata", "determinism", "schema.graphql")
	documentsPath := filepath.Join("testdata", "determinism", "documents")

	expected := generateFixture(t, schemaPath, documentsPath)
	require.NotEmpty(t, expected)

	// map iteration order varies per run, so repeated runs expose any dependency on it
	for range 10 {
		actual := generateFixture(t, schemaPath, documentsPath)

		require.Len(t, actual, len(expected))

		for name, content := range expected {
			assert.Equal(t, content, actual[name], "generated file %s differs between runs", name)
		}
	}
}
