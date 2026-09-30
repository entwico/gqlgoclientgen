package clientgen

import (
	"errors"
	"fmt"
	"go/token"
	"io/fs"
	"os"
	"strings"
)

// Config holds the configuration for the client generator.
type Config struct {
	// SchemaPath is the path to the GraphQL schema file or URL
	SchemaPath string

	// PackageName is the name of the generated Go package
	PackageName string

	// OutputDir is the directory where generated files will be saved
	OutputDir string

	// DocumentsPath is the path to directory containing .gql or .graphql files
	DocumentsPath string

	// GeneratorVersion is stamped into the DO-NOT-EDIT header of generated
	// files; the CLI sets it to its build version. Empty means "dev".
	GeneratorVersion string

	// Additional options for future extensibility
	Options Options
}

// Options contains additional configuration options.
type Options struct {
	// OmitDeprecated controls whether to omit deprecated fields and enum values
	OmitDeprecated bool

	// ScalarMappings maps custom GraphQL scalar names to Go types, e.g.
	// {"BigInt": "int64", "Decimal": "github.com/shopspring/decimal.Decimal"}.
	// Unmapped custom scalars default to a generated string type.
	ScalarMappings map[string]string
}

// NewConfig creates a new Config with default options.
func NewConfig(schemaPath, packageName, outputDir, documentsPath string) *Config {
	return &Config{
		SchemaPath:    schemaPath,
		PackageName:   packageName,
		OutputDir:     outputDir,
		DocumentsPath: documentsPath,
	}
}

// Validate checks if the configuration is valid.
func (c *Config) Validate() error {
	if c.SchemaPath == "" {
		return errors.New("schema path is required")
	}

	if c.PackageName == "" {
		return errors.New("package name is required")
	}

	// validate package name follows Go conventions
	if !isValidPackageName(c.PackageName) {
		return fmt.Errorf("invalid package name: %s", c.PackageName)
	}

	if c.OutputDir == "" {
		return errors.New("output directory is required")
	}

	// check if documents path exists if provided
	if c.DocumentsPath != "" {
		info, err := os.Stat(c.DocumentsPath)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("documents directory does not exist: %s", c.DocumentsPath)
			}

			return fmt.Errorf("error accessing documents directory: %w", err)
		}

		if !info.IsDir() {
			return fmt.Errorf("documents path must be a directory: %s", c.DocumentsPath)
		}
	}

	return nil
}

// IsSchemaURL returns true if the schema path is a URL.
func (c *Config) IsSchemaURL() bool {
	return strings.HasPrefix(c.SchemaPath, "http://") || strings.HasPrefix(c.SchemaPath, "https://")
}

// isValidPackageName checks if the package name is valid according to Go conventions.
func isValidPackageName(name string) bool {
	// the blank identifier is a valid Go identifier but not a valid package name
	return token.IsIdentifier(name) && name != "_"
}
