package cmd

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseHeaderFlags(t *testing.T) {
	headers := map[string]string{"X-From-Config": "1", "Authorization": "config-token"}

	err := parseHeaderFlags([]string{"Authorization: Bearer abc", "X-Extra: v"}, headers)
	require.NoError(t, err)

	// command-line values override config values, others are preserved
	assert.Equal(t, "Bearer abc", headers["Authorization"])
	assert.Equal(t, "v", headers["X-Extra"])
	assert.Equal(t, "1", headers["X-From-Config"])
}

func TestParseHeaderFlags_InvalidFormat(t *testing.T) {
	err := parseHeaderFlags([]string{"NoColonHere"}, map[string]string{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid header format")
}

func TestHeaderTransport_SetsHeaders(t *testing.T) {
	var got http.Header

	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
	}))
	defer server.Close()

	client := newHTTPClient(map[string]string{"Content-Type": "application/custom", "X-Key": "k"})

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL, nil)
	require.NoError(t, err)

	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	// Set semantics: the configured header replaces, never duplicates
	assert.Equal(t, []string{"application/custom"}, got.Values("Content-Type"))
	assert.Equal(t, "k", got.Get("X-Key"))
}

func TestValidateGenerateFlags(t *testing.T) {
	docsDir := t.TempDir()

	tests := []struct {
		name        string
		flags       generateFlags
		endpointURL string
		wantErr     string
	}{
		{
			name:    "no schema source",
			flags:   generateFlags{packageName: "x", outputDir: "out", documentsPath: docsDir},
			wantErr: "either schema path, URL, or config file is required",
		},
		{
			name:    "missing package name",
			flags:   generateFlags{schemaPath: "s.graphql", outputDir: "out", documentsPath: docsDir},
			wantErr: "package name is required",
		},
		{
			name:    "documents path missing",
			flags:   generateFlags{schemaPath: "s.graphql", packageName: "x", outputDir: "out", documentsPath: filepath.Join(docsDir, "nope")},
			wantErr: "documents path error",
		},
		{
			name:  "valid with schema file",
			flags: generateFlags{schemaPath: "s.graphql", packageName: "x", outputDir: "out", documentsPath: docsDir},
		},
		{
			name:        "valid with url only",
			flags:       generateFlags{packageName: "x", outputDir: "out", documentsPath: docsDir},
			endpointURL: "https://example.com/graphql",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateGenerateFlags(&tt.flags, tt.endpointURL)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.True(t, filepath.IsAbs(tt.flags.outputDir))
		})
	}
}

func TestResolveSchemaSource_Precedence(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	require.NoError(t, os.WriteFile(configPath, []byte(`{"url": "https://config.example/graphql", "headers": {"X-A": "1"}}`), 0o600))

	t.Run("config url is used when no flags are set", func(t *testing.T) {
		flags := generateFlags{configFile: configPath}

		endpointURL, headers, _, err := resolveSchemaSource(nil, &flags)
		require.NoError(t, err)
		assert.Equal(t, "https://config.example/graphql", endpointURL)
		assert.Equal(t, "1", headers["X-A"])
	})

	t.Run("url flag beats config url", func(t *testing.T) {
		flags := generateFlags{configFile: configPath, url: "https://flag.example/graphql"}

		endpointURL, _, _, err := resolveSchemaSource(nil, &flags)
		require.NoError(t, err)
		assert.Equal(t, "https://flag.example/graphql", endpointURL)
	})

	t.Run("schema flag beats config url", func(t *testing.T) {
		flags := generateFlags{configFile: configPath, schemaPath: "schema.graphql"}

		endpointURL, _, _, err := resolveSchemaSource(nil, &flags)
		require.NoError(t, err)
		assert.Empty(t, endpointURL)
		assert.Equal(t, "schema.graphql", flags.schemaPath)
	})
}

func TestLoadGenerateConfig_YAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gqlgoclientgen.yml")
	require.NoError(t, os.WriteFile(path, []byte(`
url: https://api.example.com/graphql
documents: ./queries
package: apiclient
output: ./generated
omitDeprecated: true
scalars:
  BigInt: int64
  Decimal: github.com/shopspring/decimal.Decimal
headers:
  Authorization: Bearer token
`), 0o600))

	config, err := loadGenerateConfig(path)
	require.NoError(t, err)

	assert.Equal(t, "https://api.example.com/graphql", config.URL)
	assert.Equal(t, "./queries", config.Documents)
	assert.Equal(t, "apiclient", config.Package)
	assert.Equal(t, "./generated", config.Output)
	assert.True(t, config.OmitDeprecated)
	assert.Equal(t, "int64", config.Scalars["BigInt"])
	assert.Equal(t, "Bearer token", config.Headers["Authorization"])
}

func TestResolveSchemaSource_FullConfigMergesIntoFlags(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
schema: ./schema.graphql
documents: ./queries
package: apiclient
scalars:
  BigInt: int64
`), 0o600))

	flags := generateFlags{configFile: path}

	endpointURL, _, options, err := resolveSchemaSource(nil, &flags)
	require.NoError(t, err)
	assert.Empty(t, endpointURL)
	assert.Equal(t, "./schema.graphql", flags.schemaPath)
	assert.Equal(t, "./queries", flags.documentsPath)
	assert.Equal(t, "apiclient", flags.packageName)
	assert.Equal(t, "int64", options.ScalarMappings["BigInt"])
}
