package clientgen

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/parser"
	"github.com/vektah/gqlparser/v2/validator"
	"github.com/vektah/gqlparser/v2/validator/rules"

	"github.com/entwico/gqlgoclientgen/pkg/schema"
)

// Schema represents a parsed GraphQL schema.
type Schema struct {
	// Raw schema string
	Raw string

	// Parsed schema from gqlparser
	Parsed *ast.Schema

	// Source indicates where the schema came from
	Source string
}

// Document describes one loaded operation file. The parsed, validated AST of
// all documents combined lives on the generator, not per file, because
// fragments may be shared across files.
type Document struct {
	// Path to the document file
	Path string

	// Name of the document (filename without extension)
	Name string

	// Raw content of the document
	Raw string
}

// loadSchemaFromPath loads a GraphQL schema from a file path or URL.
func loadSchemaFromPath(path string) (*Schema, error) {
	var (
		schemaContent string
		source        string
	)

	if isURL(path) {
		// load schema via introspection
		content, err := fetchSchemaFromURL(path)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch schema from URL: %w", err)
		}

		schemaContent = content
		source = fmt.Sprintf("introspection from %s", path)
	} else {
		// load schema from file
		content, err := loadSchemaFromFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to load schema from file: %w", err)
		}

		schemaContent = content
		source = fmt.Sprintf("file %s", path)
	}

	parsedSchema, err := parseSchema(schemaContent)
	if err != nil {
		return nil, fmt.Errorf("failed to parse schema: %w", err)
	}

	return &Schema{
		Raw:    schemaContent,
		Parsed: parsedSchema,
		Source: source,
	}, nil
}

// loadDocumentsFromPath loads all GraphQL documents from a directory and
// returns the file list plus one combined, validated query document.
func loadDocumentsFromPath(dirPath string, gqlSchema *ast.Schema) ([]Document, *ast.QueryDocument, error) {
	files, err := findGraphQLFiles(dirPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to find GraphQL files: %w", err)
	}

	if len(files) == 0 {
		return nil, nil, fmt.Errorf("no .gql or .graphql files found in %s", dirPath)
	}

	documents := make([]Document, 0, len(files))
	combined := &ast.QueryDocument{}

	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read document file %s: %w", file, err)
		}

		doc, err := parseDocument(file, string(content))
		if err != nil {
			return nil, nil, err
		}

		// anonymous operations would all collide on the same generated method
		for _, op := range doc.Operations {
			if op.Name == "" {
				return nil, nil, fmt.Errorf("%sanonymous operation: every operation must be named", atPos(op.Position))
			}
		}

		combined.Operations = append(combined.Operations, doc.Operations...)
		combined.Fragments = append(combined.Fragments, doc.Fragments...)

		documents = append(documents, Document{
			Path: file,
			Name: strings.TrimSuffix(filepath.Base(file), filepath.Ext(file)),
			Raw:  string(content),
		})
	}

	// validating the merged document resolves cross-file fragment references
	// and fills in the field definitions the generators rely on
	if err := validateDocument(gqlSchema, combined); err != nil {
		return nil, nil, err
	}

	return documents, combined, nil
}

// loadSchemaFromFile reads a schema from a file.
func loadSchemaFromFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("schema file does not exist: %s", path)
		}

		return "", err
	}

	if info.IsDir() {
		return "", fmt.Errorf("schema path is a directory, not a file: %s", path)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read schema file: %w", err)
	}

	// a .json file must be an introspection result; anything else is SDL
	if strings.HasSuffix(strings.ToLower(path), ".json") {
		introspected, err := parseIntrospectionJSON(content)
		if err != nil {
			return "", fmt.Errorf("failed to parse introspection JSON from %s: %w", path, err)
		}

		sdl, err := schema.ToSDL(introspected)
		if err != nil {
			return "", fmt.Errorf("failed to convert introspection to SDL: %w", err)
		}

		return sdl, nil
	}

	return string(content), nil
}

// parseIntrospectionJSON decodes an introspection result, accepting the bare
// schema object as well as the {"__schema": ...} and {"data": {"__schema": ...}}
// wrappers that most GraphQL tools emit.
func parseIntrospectionJSON(content []byte) (*schema.Schema, error) {
	var wrapper struct {
		Data *struct {
			Schema *schema.Schema `json:"__schema"`
		} `json:"data"`
		Schema *schema.Schema `json:"__schema"`
	}

	if err := json.Unmarshal(content, &wrapper); err == nil {
		if wrapper.Data != nil && wrapper.Data.Schema != nil && len(wrapper.Data.Schema.Types) > 0 {
			return wrapper.Data.Schema, nil
		}

		if wrapper.Schema != nil && len(wrapper.Schema.Types) > 0 {
			return wrapper.Schema, nil
		}
	}

	var bare schema.Schema

	if err := json.Unmarshal(content, &bare); err != nil {
		return nil, err
	}

	if len(bare.Types) == 0 {
		return nil, errors.New("introspection result contains no types")
	}

	return &bare, nil
}

// fetchSchemaFromURL fetches a schema via introspection from a GraphQL endpoint.
func fetchSchemaFromURL(url string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	introspected, err := schema.FetchSchema(ctx, url, &http.Client{})
	if err != nil {
		return "", fmt.Errorf("introspection failed: %w", err)
	}

	sdl, err := schema.ToSDL(introspected)
	if err != nil {
		return "", fmt.Errorf("failed to convert introspection to SDL: %w", err)
	}

	return sdl, nil
}

// findGraphQLFiles finds all .gql and .graphql files in a directory.
func findGraphQLFiles(dirPath string) ([]string, error) {
	var files []string

	err := filepath.WalkDir(dirPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".gql" || ext == ".graphql" {
			files = append(files, path)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	// walk order is lexical but not guaranteed on every filesystem, and the
	// file order determines generated output order
	slices.Sort(files)

	return files, nil
}

// parseSchema parses a GraphQL schema SDL.
func parseSchema(content string) (*ast.Schema, error) {
	if content == "" {
		return nil, errors.New("empty schema")
	}

	source := &ast.Source{
		Name:  "schema",
		Input: content,
	}

	parsedSchema, gqlErr := gqlparser.LoadSchema(source)
	if gqlErr != nil {
		return nil, fmt.Errorf("failed to parse schema: %w", gqlErr)
	}

	return parsedSchema, nil
}

// parseDocument parses a single GraphQL document file without validating it.
func parseDocument(name, content string) (*ast.QueryDocument, error) {
	if strings.TrimSpace(content) == "" {
		return nil, fmt.Errorf("document %s is empty", name)
	}

	doc, gqlErr := parser.ParseQuery(&ast.Source{
		Name:  name,
		Input: content,
	})
	if gqlErr != nil {
		return nil, fmt.Errorf("failed to parse document: %w", gqlErr)
	}

	return doc, nil
}

// validateDocument validates the combined query document against the schema.
func validateDocument(gqlSchema *ast.Schema, doc *ast.QueryDocument) error {
	// a shared fragment corpus may define fragments no operation uses yet
	docRules := rules.NewDefaultRules()
	docRules.RemoveRule("NoUnusedFragments")

	if errs := validator.ValidateWithRules(gqlSchema, doc, docRules); len(errs) > 0 {
		return fmt.Errorf("document validation failed: %w", errs)
	}

	return nil
}

func isURL(str string) bool {
	return strings.HasPrefix(str, "http://") || strings.HasPrefix(str, "https://")
}
