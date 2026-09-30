package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/entwico/gqlgoclientgen/pkg/clientgen"
	"github.com/entwico/gqlgoclientgen/pkg/schema"
)

// generateFlags holds the flag values for the generate command; keeping them
// in a struct avoids mutable package state and keeps the command reentrant.
type generateFlags struct {
	schemaPath    string
	packageName   string
	outputDir     string
	documentsPath string
	url           string
	headers       []string
	configFile    string
	check         bool
}

var genFlags generateFlags

// GenerateConfig describes the config file accepted by the generate command
// via --config (YAML or JSON, detected by extension). Command-line flags take
// precedence over config file values.
type GenerateConfig struct {
	URL            string            `json:"url,omitempty"            yaml:"url"`
	Headers        map[string]string `json:"headers,omitempty"        yaml:"headers"`
	Schema         string            `json:"schema,omitempty"         yaml:"schema"`
	Documents      string            `json:"documents,omitempty"      yaml:"documents"`
	Package        string            `json:"package,omitempty"        yaml:"package"`
	Output         string            `json:"output,omitempty"         yaml:"output"`
	OmitDeprecated bool              `json:"omitDeprecated,omitempty" yaml:"omitDeprecated"`

	// Scalars maps custom GraphQL scalar names to Go types, e.g.
	// {"BigInt": "int64", "Decimal": "github.com/shopspring/decimal.Decimal"}
	Scalars map[string]string `json:"scalars,omitempty" yaml:"scalars"`
}

var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate Go client code from GraphQL schema and documents",
	Long: `Generate type-safe Go client code from a GraphQL schema file or URL,
along with query/mutation/subscription documents.

The command can obtain the schema in multiple ways:
1. From a local schema file (--schema)
2. Via introspection from a GraphQL endpoint (--url)
3. From a config file containing URL and/or schema path (--config)

Priority order: command-line flags > config file values`,
	Example: `  # Generate from local schema file
  gqlgoclientgen generate --schema ./schema.graphql --pkg client --out ./generated --documents ./queries

  # Generate via introspection from URL
  gqlgoclientgen generate --url https://api.example.com/graphql --pkg api --out ./api/generated --documents ./queries

  # Generate with authentication headers
  gqlgoclientgen generate --url https://api.example.com/graphql -H "Authorization: Bearer token" --pkg api --out ./api/generated --documents ./queries

  # Generate using config file
  gqlgoclientgen generate --config ./config.json --pkg client --out ./generated --documents ./queries`,
	RunE:         runGenerate,
	SilenceUsage: true,
}

func init() {
	rootCmd.AddCommand(generateCmd)

	generateCmd.Flags().StringVar(&genFlags.schemaPath, "schema", "", "Path to GraphQL schema file (SDL or introspection JSON)")
	generateCmd.Flags().StringVar(&genFlags.packageName, "pkg", "generated", "Name of the generated Go package")
	generateCmd.Flags().StringVar(&genFlags.outputDir, "out", "./generated", "Output directory for generated files")
	generateCmd.Flags().StringVar(&genFlags.documentsPath, "documents", "", "Path to directory containing .gql or .graphql files with operations (required)")
	generateCmd.Flags().StringVarP(&genFlags.url, "url", "u", "", "GraphQL endpoint URL for introspection")
	generateCmd.Flags().StringArrayVarP(&genFlags.headers, "header", "H", nil, "HTTP headers for introspection (format: 'Key: Value')")
	generateCmd.Flags().StringVarP(&genFlags.configFile, "config", "c", "", "Config file path (YAML or JSON; flags take precedence)")
	generateCmd.Flags().BoolVar(&genFlags.check, "check", false, "Verify the generated files are up to date without writing; exits non-zero when stale")
}

func runGenerate(cmd *cobra.Command, _ []string) error {
	// work on a copy so runs never leak state into the package-level flags
	flags := genFlags

	// merge the config file before validating so its schema path or URL counts
	endpointURL, headers, options, err := resolveSchemaSource(cmd, &flags)
	if err != nil {
		return err
	}

	// validate everything local before any network call
	if err := validateGenerateFlags(&flags, endpointURL); err != nil {
		return fmt.Errorf("validation error: %w", err)
	}

	if endpointURL != "" {
		schemaFile, cleanup, err := introspectToTempFile(cmd.Context(), endpointURL, headers)
		if err != nil {
			return err
		}
		defer cleanup()

		flags.schemaPath = schemaFile
	}

	fmt.Fprintln(os.Stderr, "Generating Go client code...")

	if endpointURL != "" {
		fmt.Fprintf(os.Stderr, "  Schema from: %s (via introspection)\n", endpointURL)
	} else {
		fmt.Fprintf(os.Stderr, "  Schema: %s\n", flags.schemaPath)
	}

	fmt.Fprintf(os.Stderr, "  Package: %s\n", flags.packageName)
	fmt.Fprintf(os.Stderr, "  Output: %s\n", flags.outputDir)
	fmt.Fprintf(os.Stderr, "  Documents: %s\n", flags.documentsPath)

	config := clientgen.NewConfig(flags.schemaPath, flags.packageName, flags.outputDir, flags.documentsPath)
	config.Options = options
	config.GeneratorVersion = Version

	generator, err := clientgen.New(config)
	if err != nil {
		return fmt.Errorf("failed to create generator: %w", err)
	}

	fmt.Fprintln(os.Stderr, "\nLoading schema...")

	if err := generator.LoadSchema(); err != nil {
		return fmt.Errorf("failed to load schema: %w", err)
	}

	fmt.Fprintln(os.Stderr, "Loading documents...")

	if err := generator.LoadDocuments(); err != nil {
		return fmt.Errorf("failed to load documents: %w", err)
	}

	if flags.check {
		stale, err := generator.Check()
		if err != nil {
			return fmt.Errorf("check failed: %w", err)
		}

		if len(stale) > 0 {
			return fmt.Errorf("generated code is stale, re-run gqlgoclientgen generate: %s", strings.Join(stale, ", "))
		}

		fmt.Fprintln(os.Stderr, "Generated code is up to date.")

		return nil
	}

	fmt.Fprintln(os.Stderr, "Generating client code...")

	if err := generator.Generate(); err != nil {
		return fmt.Errorf("generation failed: %w", err)
	}

	fmt.Fprintf(os.Stderr, "\nClient code generated successfully in %s\n", flags.outputDir)

	return nil
}

// resolveSchemaSource merges the config file into the flags and returns the
// introspection endpoint (empty when a schema file is used), the headers and
// the generation options.
func resolveSchemaSource(cmd *cobra.Command, flags *generateFlags) (string, map[string]string, clientgen.Options, error) {
	var (
		endpointURL string
		options     clientgen.Options
	)

	headers := make(map[string]string)

	if flags.configFile != "" {
		config, err := loadGenerateConfig(flags.configFile)
		if err != nil {
			return "", nil, options, fmt.Errorf("failed to load config file: %w", err)
		}

		if config.Schema != "" && flags.schemaPath == "" {
			flags.schemaPath = config.Schema
		}

		if config.Documents != "" && flags.documentsPath == "" {
			flags.documentsPath = config.Documents
		}

		// pkg and out carry defaults, so only an explicitly set flag wins
		if config.Package != "" && !flagChanged(cmd, "pkg") {
			flags.packageName = config.Package
		}

		if config.Output != "" && !flagChanged(cmd, "out") {
			flags.outputDir = config.Output
		}

		if config.URL != "" {
			endpointURL = config.URL
		}

		options.OmitDeprecated = config.OmitDeprecated
		options.ScalarMappings = config.Scalars

		maps.Copy(headers, config.Headers)
	}

	// command-line URL flag takes precedence over the config file
	if flags.url != "" {
		endpointURL = flags.url
	}

	// an explicit --schema flag beats a URL that came from the config file
	if flags.schemaPath != "" && flags.url == "" {
		endpointURL = ""
	}

	// command-line headers override config file headers
	if err := parseHeaderFlags(flags.headers, headers); err != nil {
		return "", nil, options, err
	}

	return endpointURL, headers, options, nil
}

// flagChanged reports whether the user set a flag explicitly. A nil command
// (as in tests) means no flag was set.
func flagChanged(cmd *cobra.Command, name string) bool {
	return cmd != nil && cmd.Flags().Changed(name)
}

// introspectToTempFile fetches the schema from the endpoint and stores it in a
// temporary JSON file the generator can load. The returned cleanup removes it.
func introspectToTempFile(ctx context.Context, endpointURL string, headers map[string]string) (string, func(), error) {
	fmt.Fprintf(os.Stderr, "Introspecting GraphQL endpoint: %s\n", endpointURL)

	schemaData, err := schema.FetchSchema(ctx, endpointURL, newHTTPClient(headers))
	if err != nil {
		return "", nil, fmt.Errorf("failed to fetch schema: %w", err)
	}

	tempFile, err := os.CreateTemp("", "schema-*.json")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temp schema file: %w", err)
	}

	cleanup := func() { _ = os.Remove(tempFile.Name()) }

	encoder := json.NewEncoder(tempFile)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(schemaData); err != nil {
		tempFile.Close()
		cleanup()

		return "", nil, fmt.Errorf("failed to write schema to temp file: %w", err)
	}

	if err := tempFile.Close(); err != nil {
		cleanup()

		return "", nil, fmt.Errorf("failed to write schema to temp file: %w", err)
	}

	return tempFile.Name(), cleanup, nil
}

func validateGenerateFlags(flags *generateFlags, endpointURL string) error {
	if flags.schemaPath == "" && endpointURL == "" {
		return errors.New("either schema path, URL, or config file is required")
	}

	if flags.packageName == "" {
		return errors.New("package name is required")
	}

	if flags.outputDir == "" {
		return errors.New("output directory is required")
	}

	if flags.documentsPath == "" {
		return errors.New("documents path is required (flag --documents or config file)")
	}

	info, err := os.Stat(flags.documentsPath)
	if err != nil {
		return fmt.Errorf("documents path error: %w", err)
	}

	if !info.IsDir() {
		return errors.New("documents path must be a directory")
	}

	absPath, err := filepath.Abs(flags.outputDir)
	if err != nil {
		return fmt.Errorf("failed to resolve output directory: %w", err)
	}

	flags.outputDir = absPath

	return nil
}

// loadGenerateConfig loads the configuration from a YAML or JSON file,
// detected by extension.
func loadGenerateConfig(path string) (*GenerateConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config GenerateConfig

	switch strings.ToLower(filepath.Ext(path)) {
	case ".yml", ".yaml":
		if err := yaml.Unmarshal(data, &config); err != nil {
			return nil, fmt.Errorf("failed to parse config file: %w", err)
		}
	default:
		if err := json.Unmarshal(data, &config); err != nil {
			return nil, fmt.Errorf("failed to parse config file: %w", err)
		}
	}

	return &config, nil
}
