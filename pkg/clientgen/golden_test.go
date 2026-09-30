package clientgen

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "rewrite golden files with the current generator output")

// TestGenerate_MatchesGolden compares the full generator output byte-for-byte
// against the committed golden files, so every change to the generator shows
// up as a reviewable diff of the generated code.
//
// After an intentional change, refresh the goldens with:
//
//	go test ./pkg/clientgen -run TestGenerate_MatchesGolden -update
func TestGenerate_MatchesGolden(t *testing.T) {
	schemaPath := filepath.Join("testdata", "determinism", "schema.graphql")
	documentsPath := filepath.Join("testdata", "determinism", "documents")
	goldenDir := filepath.Join("testdata", "determinism", "golden")

	files := generateFixture(t, schemaPath, documentsPath)
	require.NotEmpty(t, files)

	if *update {
		require.NoError(t, os.RemoveAll(goldenDir))
		require.NoError(t, os.MkdirAll(goldenDir, 0o755))

		for name, content := range files {
			// golden files are committed and meant to be world-readable
			require.NoError(t, os.WriteFile(filepath.Join(goldenDir, name), []byte(content), 0o644)) //nolint:gosec
		}
	}

	entries, err := os.ReadDir(goldenDir)
	require.NoError(t, err, "golden files missing; run with -update to create them")
	require.Len(t, files, len(entries), "generated file set differs from golden set; run with -update if intended")

	for _, entry := range entries {
		golden, err := os.ReadFile(filepath.Join(goldenDir, entry.Name()))
		require.NoError(t, err)

		assert.Equal(t, string(golden), files[entry.Name()],
			"%s differs from its golden file; run with -update if the change is intended", entry.Name())
	}
}
