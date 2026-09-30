package clientgen

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vektah/gqlparser/v2/ast"
)

// buildSchemaForInput builds a JSON Schema document for the given input
// definition using a fresh schemaBuilder. Returns the decoded root document.
func buildSchemaForInput(t *testing.T, schema *ast.Schema, def *ast.Definition, strict bool) map[string]any {
	t.Helper()

	b := newSchemaBuilder(schema, strict, Options{})
	doc := b.buildRoot(def)
	raw, err := json.Marshal(doc)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(raw, &parsed))

	return parsed
}

// asObj asserts that v is a map[string]any and returns it, failing the test
// with a helpful message otherwise.
func asObj(t *testing.T, v any) map[string]any {
	t.Helper()

	m, ok := v.(map[string]any)
	require.True(t, ok, "expected map[string]any, got %T", v)

	return m
}

// asArr asserts that v is a []any and returns it, failing the test with a
// helpful message otherwise.
func asArr(t *testing.T, v any) []any {
	t.Helper()

	arr, ok := v.([]any)
	require.True(t, ok, "expected []any, got %T", v)

	return arr
}

// schemagenTestSchema returns a small GraphQL schema with enough variety to
// exercise the builder: an input with required/optional scalars, an enum, a
// list, and a nested input type.
func schemagenTestSchema() *ast.Schema {
	authorInput := &ast.Definition{
		Kind:        ast.InputObject,
		Name:        "AuthorInput",
		Description: "Author of a post",
		Fields: ast.FieldList{
			{Name: "name", Type: &ast.Type{NamedType: "String", NonNull: true}},
			{Name: "email", Type: &ast.Type{NamedType: "String"}},
		},
	}

	statusEnum := &ast.Definition{
		Kind: ast.Enum,
		Name: "PostStatus",
		EnumValues: ast.EnumValueList{
			{Name: "DRAFT"},
			{Name: "PUBLISHED"},
		},
	}

	createPostInput := &ast.Definition{
		Kind:        ast.InputObject,
		Name:        "CreatePostInput",
		Description: "Input for creating a post",
		Fields: ast.FieldList{
			{
				Name:        "title",
				Description: "Post title",
				Type:        &ast.Type{NamedType: "String", NonNull: true},
			},
			{
				Name: "body",
				Type: &ast.Type{NamedType: "String"},
			},
			{
				Name: "tags",
				Type: &ast.Type{
					Elem:    &ast.Type{NamedType: "String", NonNull: true},
					NonNull: true,
				},
			},
			{
				Name: "status",
				Type: &ast.Type{NamedType: "PostStatus", NonNull: true},
			},
			{
				Name: "author",
				Type: &ast.Type{NamedType: "AuthorInput", NonNull: true},
			},
			{
				Name: "publishedAt",
				Type: &ast.Type{NamedType: "DateTime"},
			},
			{
				Name: "metadata",
				Type: &ast.Type{NamedType: "Map"},
			},
			{
				Name: "extra",
				Type: &ast.Type{NamedType: "Any"},
			},
		},
	}

	return &ast.Schema{
		Types: map[string]*ast.Definition{
			"String":          {Kind: ast.Scalar, Name: "String"},
			"Int":             {Kind: ast.Scalar, Name: "Int"},
			"Float":           {Kind: ast.Scalar, Name: "Float"},
			"Boolean":         {Kind: ast.Scalar, Name: "Boolean"},
			"ID":              {Kind: ast.Scalar, Name: "ID"},
			"DateTime":        {Kind: ast.Scalar, Name: "DateTime"},
			"Map":             {Kind: ast.Scalar, Name: "Map"},
			"Any":             {Kind: ast.Scalar, Name: "Any"},
			"PostStatus":      statusEnum,
			"AuthorInput":     authorInput,
			"CreatePostInput": createPostInput,
		},
	}
}

func TestSchemagen_FullMode_Basics(t *testing.T) {
	schema := schemagenTestSchema()
	doc := buildSchemaForInput(t, schema, schema.Types["CreatePostInput"], false)

	assert.Equal(t, "https://json-schema.org/draft/2020-12/schema", doc["$schema"])
	assert.Equal(t, "CreatePostInput", doc["title"])
	assert.Equal(t, "object", doc["type"])
	assert.Equal(t, "Input for creating a post", doc["description"])

	props := asObj(t, doc["properties"])
	assert.Contains(t, props, "title")
	assert.Contains(t, props, "body")
	assert.Contains(t, props, "tags")
	assert.Contains(t, props, "status")
	assert.Contains(t, props, "author")

	// required = only non-null fields in full mode
	required := asArr(t, doc["required"])

	requiredStrs := make([]string, 0, len(required))
	for _, r := range required {
		s, ok := r.(string)
		require.True(t, ok)

		requiredStrs = append(requiredStrs, s)
	}

	assert.Contains(t, requiredStrs, "title")
	assert.Contains(t, requiredStrs, "tags")
	assert.Contains(t, requiredStrs, "status")
	assert.Contains(t, requiredStrs, "author")
	assert.NotContains(t, requiredStrs, "body", "nullable field should not be required in full mode")
	assert.NotContains(t, requiredStrs, "publishedAt")
	assert.NotContains(t, requiredStrs, "metadata")

	// additionalProperties should NOT be set in full mode
	_, hasAP := doc["additionalProperties"]
	assert.False(t, hasAP, "full mode must not emit additionalProperties")

	// nested input uses $ref + $defs
	author := asObj(t, props["author"])
	assert.Equal(t, "#/$defs/AuthorInput", author["$ref"])

	defs := asObj(t, doc["$defs"])
	assert.Contains(t, defs, "AuthorInput")

	authorDef := asObj(t, defs["AuthorInput"])
	assert.Equal(t, "object", authorDef["type"])

	// enum inlined as string + enum values
	status := asObj(t, props["status"])
	assert.Equal(t, "string", status["type"])
	assert.ElementsMatch(t, []any{"DRAFT", "PUBLISHED"}, asArr(t, status["enum"]))

	// title description flowed through
	title := asObj(t, props["title"])
	assert.Equal(t, "Post title", title["description"])
}

func TestSchemagen_FullMode_Scalars(t *testing.T) {
	schema := schemagenTestSchema()
	doc := buildSchemaForInput(t, schema, schema.Types["CreatePostInput"], false)
	props := asObj(t, doc["properties"])

	dt := asObj(t, props["publishedAt"])
	assert.Equal(t, "string", dt["type"])
	assert.Equal(t, "date-time", dt["format"])

	m := asObj(t, props["metadata"])
	assert.Equal(t, "object", m["type"])
	assert.Equal(t, true, m["additionalProperties"])

	// Any maps to an (almost) empty schema in full mode.
	extra := asObj(t, props["extra"])
	_, hasType := extra["type"]
	assert.False(t, hasType, "Any should have no type constraint in full mode")
}

func TestSchemagen_StrictMode(t *testing.T) {
	schema := schemagenTestSchema()
	doc := buildSchemaForInput(t, schema, schema.Types["CreatePostInput"], true)

	// strict mode: all fields required
	required := asArr(t, doc["required"])

	names := make([]string, 0, len(required))
	for _, r := range required {
		s, ok := r.(string)
		require.True(t, ok)

		names = append(names, s)
	}

	assert.ElementsMatch(t,
		[]string{"title", "body", "tags", "status", "author", "publishedAt", "metadata", "extra"},
		names,
	)

	// additionalProperties=false at every object level
	assert.Equal(t, false, doc["additionalProperties"])

	defs := asObj(t, doc["$defs"])
	authorDef := asObj(t, defs["AuthorInput"])
	assert.Equal(t, false, authorDef["additionalProperties"])

	props := asObj(t, doc["properties"])

	// nullable scalar gets ["string","null"] union
	body := asObj(t, props["body"])
	assert.ElementsMatch(t, []any{"string", "null"}, asArr(t, body["type"]))

	// nullable DateTime also gets a null union
	dt := asObj(t, props["publishedAt"])
	assert.ElementsMatch(t, []any{"string", "null"}, asArr(t, dt["type"]))

	// Map in strict mode is downgraded to a string
	m := asObj(t, props["metadata"])
	assert.ElementsMatch(t, []any{"string", "null"}, asArr(t, m["type"]))

	// Any in strict mode is downgraded to a string
	extra := asObj(t, props["extra"])
	assert.ElementsMatch(t, []any{"string", "null"}, asArr(t, extra["type"]))

	// non-null enum keeps single string type and enum values
	status := asObj(t, props["status"])
	assert.Equal(t, "string", status["type"])
	assert.ElementsMatch(t, []any{"DRAFT", "PUBLISHED"}, asArr(t, status["enum"]))

	// required input ref stays as direct $ref
	author := asObj(t, props["author"])
	assert.Equal(t, "#/$defs/AuthorInput", author["$ref"])
}

func TestSchemagen_StrictMode_NullableInputRef_WrapsAnyOf(t *testing.T) {
	// construct a schema where the author field is nullable to verify the
	// $ref+null anyOf wrapping path.
	base := schemagenTestSchema()
	nullableAuthorInput := &ast.Definition{
		Kind: ast.InputObject,
		Name: "DraftInput",
		Fields: ast.FieldList{
			{Name: "author", Type: &ast.Type{NamedType: "AuthorInput"}},
		},
	}
	base.Types["DraftInput"] = nullableAuthorInput

	doc := buildSchemaForInput(t, base, nullableAuthorInput, true)
	props := asObj(t, doc["properties"])
	author := asObj(t, props["author"])
	_, isRef := author["$ref"]
	assert.False(t, isRef, "nullable $ref must be wrapped in anyOf")

	anyOf := asArr(t, author["anyOf"])
	require.Len(t, anyOf, 2)

	first := asObj(t, anyOf[0])
	assert.Equal(t, "#/$defs/AuthorInput", first["$ref"])

	second := asObj(t, anyOf[1])
	assert.Equal(t, "null", second["type"])
}

func TestSchemagen_OmitDeprecatedEnumValues(t *testing.T) {
	schema := schemagenTestSchema()
	statusEnum := schema.Types["PostStatus"]
	statusEnum.EnumValues = append(statusEnum.EnumValues, &ast.EnumValueDefinition{
		Name: "ARCHIVED",
		Directives: ast.DirectiveList{
			{Name: "deprecated"},
		},
	})

	b := newSchemaBuilder(schema, false, Options{OmitDeprecated: true})
	doc := b.buildRoot(schema.Types["CreatePostInput"])
	raw, err := json.Marshal(doc)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(raw, &parsed))

	props := asObj(t, parsed["properties"])
	status := asObj(t, props["status"])
	values := asArr(t, status["enum"])
	assert.ElementsMatch(t, []any{"DRAFT", "PUBLISHED"}, values)
}

func TestSchemagen_Emit_ProducesCompilableOutput(t *testing.T) {
	schema := schemagenTestSchema()
	gen := newCodeGenerator("test")

	sg := newSchemaGenerator(schema, gen, Options{})
	require.NoError(t, sg.GenerateAllInputSchemas())

	buf, err := gen.WriteToBuffer()
	require.NoError(t, err)

	out := buf.String()

	// inline helpers emitted once at the top
	assert.Contains(t, out, "type SchemaOption func(*schemaOptions)")
	assert.Contains(t, out, "type schemaOptions struct")
	assert.Contains(t, out, "func Strict() SchemaOption")
	assert.Contains(t, out, "func applySchemaOptions(opts []SchemaOption) schemaOptions")

	// generated output must not import the gqlgoclientgen repo
	assert.NotContains(t, out, "github.com/entwico/gqlgoclientgen")

	// vars for both input types in both modes
	assert.Contains(t, out, "_schemaCreatePostInputFull")
	assert.Contains(t, out, "_schemaCreatePostInputStrict")
	assert.Contains(t, out, "_schemaAuthorInputFull")
	assert.Contains(t, out, "_schemaAuthorInputStrict")

	// JSONSchema method on each input type — local SchemaOption, not qualified
	assert.Contains(t, out, "func (CreatePostInput) JSONSchema(opts ...SchemaOption) json.RawMessage")
	assert.Contains(t, out, "func (AuthorInput) JSONSchema(opts ...SchemaOption) json.RawMessage")

	// dispatch function
	assert.Contains(t, out, "func JSONSchemaFor(name string, opts ...SchemaOption) (json.RawMessage, bool)")
	assert.Contains(t, out, `case "CreatePostInput":`)
	assert.Contains(t, out, `case "AuthorInput":`)
}

func TestSchemagen_EmittedLiteralsAreValidJSON(t *testing.T) {
	schema := schemagenTestSchema()
	gen := newCodeGenerator("test")

	sg := newSchemaGenerator(schema, gen, Options{})
	require.NoError(t, sg.GenerateAllInputSchemas())

	// Rebuild the schemas directly and verify they parse as valid JSON.
	// This catches marshaling drift vs what the emitter outputs.
	for _, name := range []string{"CreatePostInput", "AuthorInput"} {
		def := schema.Types[name]

		full := newSchemaBuilder(schema, false, Options{}).buildRoot(def)
		rawFull, err := json.Marshal(full)
		require.NoError(t, err)

		var tmp map[string]any
		require.NoError(t, json.Unmarshal(rawFull, &tmp),
			"full schema for %s must be valid JSON", name)

		strict := newSchemaBuilder(schema, true, Options{}).buildRoot(def)
		rawStrict, err := json.Marshal(strict)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(rawStrict, &tmp),
			"strict schema for %s must be valid JSON", name)
	}
}

func TestSchemagen_SchemaVarName(t *testing.T) {
	assert.Equal(t, "_schemaFooInputFull", schemaVarName("FooInput", false))
	assert.Equal(t, "_schemaFooInputStrict", schemaVarName("FooInput", true))
}

func TestSchemagen_UnknownScalarFallsBackToString(t *testing.T) {
	schema := &ast.Schema{
		Types: map[string]*ast.Definition{
			"String":      {Kind: ast.Scalar, Name: "String"},
			"CustomThing": {Kind: ast.Scalar, Name: "CustomThing"},
			"WeirdInput": {
				Kind: ast.InputObject,
				Name: "WeirdInput",
				Fields: ast.FieldList{
					{Name: "x", Type: &ast.Type{NamedType: "CustomThing", NonNull: true}},
				},
			},
		},
	}
	doc := buildSchemaForInput(t, schema, schema.Types["WeirdInput"], false)
	props := asObj(t, doc["properties"])
	x := asObj(t, props["x"])
	assert.Equal(t, "string", x["type"])

	desc, ok := x["description"].(string)
	require.True(t, ok)
	assert.Contains(t, desc, "CustomThing")
}
