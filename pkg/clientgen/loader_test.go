package clientgen

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const loaderTestSchema = `
type Query {
  hello: String
}
`

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()

	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
}

func TestParseIntrospectionJSON_AcceptsWrappers(t *testing.T) {
	bare := `{"types": [{"kind": "OBJECT", "name": "Query"}], "queryType": {"name": "Query"}}`

	tests := []struct {
		name string
		json string
	}{
		{"bare schema object", bare},
		{"__schema wrapper", `{"__schema": ` + bare + `}`},
		{"data.__schema wrapper", `{"data": {"__schema": ` + bare + `}}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := parseIntrospectionJSON([]byte(tt.json))
			require.NoError(t, err)
			require.Len(t, parsed.Types, 1)
			assert.Equal(t, "Query", *parsed.Types[0].Name)
		})
	}
}

func TestParseIntrospectionJSON_EmptySchemaIsError(t *testing.T) {
	_, err := parseIntrospectionJSON([]byte(`{"types": []}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no types")
}

func TestLoadDocuments_RejectsAnonymousOperations(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "query.gql", `query { hello }`)

	gqlSchema, err := parseSchema(loaderTestSchema)
	require.NoError(t, err)

	_, _, err = loadDocumentsFromPath(dir, gqlSchema)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "anonymous operation")
}

func TestLoadDocuments_ResolvesCrossFileFragments(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "fragments.gql", "fragment HelloFields on Query { hello }")
	writeFile(t, dir, "query.gql", "query GetHello { ...HelloFields }")

	gqlSchema, err := parseSchema(loaderTestSchema)
	require.NoError(t, err)

	documents, combined, err := loadDocumentsFromPath(dir, gqlSchema)
	require.NoError(t, err)
	assert.Len(t, documents, 2)
	require.Len(t, combined.Operations, 1)
	assert.Equal(t, "GetHello", combined.Operations[0].Name)
	assert.Len(t, combined.Fragments, 1)
}

func TestLoadDocuments_UnusedFragmentIsAllowed(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "docs.gql", "query GetHello { hello }\nfragment Unused on Query { hello }")

	gqlSchema, err := parseSchema(loaderTestSchema)
	require.NoError(t, err)

	_, _, err = loadDocumentsFromPath(dir, gqlSchema)
	require.NoError(t, err)
}

func TestLoadDocuments_InvalidFieldIsError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "query.gql", "query Bad { nonexistent }")

	gqlSchema, err := parseSchema(loaderTestSchema)
	require.NoError(t, err)

	_, _, err = loadDocumentsFromPath(dir, gqlSchema)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
}
