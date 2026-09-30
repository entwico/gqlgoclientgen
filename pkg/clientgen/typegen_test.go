package clientgen

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vektah/gqlparser/v2/ast"
)

func TestGenerateEnum_MapWithDescriptions(t *testing.T) {
	schema := testSchema()
	gen := newCodeGenerator("test")
	tg := newTypeGenerator(schema, gen, Options{})

	def := &ast.Definition{
		Kind: ast.Enum,
		Name: "VideoProvider",
		EnumValues: ast.EnumValueList{
			{Name: "SELFHOSTED", Description: "Self-hosted"},
			{Name: "YOUTUBE", Description: "YouTube"},
		},
	}

	tg.generateEnum(def)

	buf, err := gen.WriteToBuffer()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "VideoProviderSelfhosted VideoProvider")
	assert.Contains(t, output, "VideoProviderYoutube VideoProvider")
	assert.Contains(t, output, "var VideoProviderDescriptions = map[VideoProvider]string{")
	assert.Contains(t, output, `VideoProviderSelfhosted: "Self-hosted"`)
	assert.Contains(t, output, `VideoProviderYoutube:    "YouTube"`)
}

func TestGenerateEnum_MapWithoutDescriptions(t *testing.T) {
	schema := testSchema()
	gen := newCodeGenerator("test")
	tg := newTypeGenerator(schema, gen, Options{})

	def := &ast.Definition{
		Kind: ast.Enum,
		Name: "Status",
		EnumValues: ast.EnumValueList{
			{Name: "SUCCESS"},
			{Name: "ERROR"},
		},
	}

	tg.generateEnum(def)

	buf, err := gen.WriteToBuffer()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, `StatusSuccess Status = "SUCCESS"`)
	assert.Contains(t, output, `StatusError   Status = "ERROR"`)
	// an enum without any described value gets no descriptions map
	assert.NotContains(t, output, "StatusDescriptions")
}

func TestGenerateEnum_MapOmitsDeprecated(t *testing.T) {
	schema := testSchema()
	gen := newCodeGenerator("test")
	tg := newTypeGenerator(schema, gen, Options{OmitDeprecated: true})

	def := &ast.Definition{
		Kind: ast.Enum,
		Name: "Color",
		EnumValues: ast.EnumValueList{
			{Name: "RED", Description: "Red color"},
			{Name: "GREEN", Description: "Green color", Directives: ast.DirectiveList{
				{Name: "deprecated"},
			}},
			{Name: "BLUE", Description: "Blue color"},
		},
	}

	tg.generateEnum(def)

	buf, err := gen.WriteToBuffer()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, `ColorRed:  "Red color"`)
	assert.Contains(t, output, `ColorBlue: "Blue color"`)
	assert.NotContains(t, output, "ColorGreen")
}

func TestGenerateEnum_EmptyEnumValues(t *testing.T) {
	schema := testSchema()
	gen := newCodeGenerator("test")
	tg := newTypeGenerator(schema, gen, Options{})

	def := &ast.Definition{
		Kind:       ast.Enum,
		Name:       "Empty",
		EnumValues: ast.EnumValueList{},
	}

	tg.generateEnum(def)

	buf, err := gen.WriteToBuffer()
	require.NoError(t, err)

	output := buf.String()
	// no descriptions map should be generated for an empty enum
	assert.NotContains(t, output, "EmptyDescriptions")
}

func TestGenerateScalar_DateTimeHelpers(t *testing.T) {
	schema := testSchema()
	gen := newCodeGenerator("test")
	tg := newTypeGenerator(schema, gen, Options{})

	tg.generateScalar(&ast.Definition{Kind: ast.Scalar, Name: "DateTime"})

	buf, err := gen.WriteToBuffer()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "func NewDateTime(t time.Time) DateTime")
	assert.Contains(t, output, "func (dt DateTime) AsTime() (time.Time, error)")
	assert.Contains(t, output, "func (dt DateTime) TimeOrZero() time.Time")
	assert.Contains(t, output, "func (dt DateTime) String() string")
	assert.Contains(t, output, "func (dt DateTime) IsZero() bool")
	// DateTime carries a timezone offset, so it uses RFC3339
	assert.Contains(t, output, "time.RFC3339")
}

func TestGenerateScalar_LocalDateTimeHelpers(t *testing.T) {
	schema := testSchema()
	gen := newCodeGenerator("test")
	tg := newTypeGenerator(schema, gen, Options{})

	tg.generateScalar(&ast.Definition{Kind: ast.Scalar, Name: "LocalDateTime"})

	buf, err := gen.WriteToBuffer()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "func NewLocalDateTime(t time.Time) LocalDateTime")
	assert.Contains(t, output, "func (dt LocalDateTime) AsTime() (time.Time, error)")
	assert.Contains(t, output, "func (dt LocalDateTime) TimeOrZero() time.Time")
	assert.Contains(t, output, "func (dt LocalDateTime) String() string")
	assert.Contains(t, output, "func (dt LocalDateTime) IsZero() bool")
	// LocalDateTime formats without an offset but AsTime also tolerates a
	// trailing Z or offset by falling back to RFC3339 when parsing.
	assert.Contains(t, output, `"2006-01-02T15:04:05"`)
	assert.Contains(t, output, "time.RFC3339")
}

func TestGenerateType_NullabilityMatrix(t *testing.T) {
	schema := testSchema()
	gen := newCodeGenerator("test")
	tg := newTypeGenerator(schema, gen, Options{})

	tests := []struct {
		name     string
		gqlType  *ast.Type
		expected string
	}{
		{"T", &ast.Type{NamedType: "String"}, "*string"},
		{"T!", &ast.Type{NamedType: "String", NonNull: true}, "string"},
		{"[T]", &ast.Type{Elem: &ast.Type{NamedType: "String"}}, "[]*string"},
		{"[T!]", &ast.Type{Elem: &ast.Type{NamedType: "String", NonNull: true}}, "[]string"},
		{"[T]!", &ast.Type{NonNull: true, Elem: &ast.Type{NamedType: "String"}}, "[]*string"},
		{"[T!]!", &ast.Type{NonNull: true, Elem: &ast.Type{NamedType: "String", NonNull: true}}, "[]string"},
		{"[[Int!]!]!", &ast.Type{NonNull: true, Elem: &ast.Type{NonNull: true, Elem: &ast.Type{NamedType: "Int", NonNull: true}}}, "[][]int"},
		{"[[Int]]", &ast.Type{Elem: &ast.Type{Elem: &ast.Type{NamedType: "Int"}}}, "[][]*int"},
		{"nullable ID stays a value", &ast.Type{NamedType: "ID"}, "string"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, renderCode(t, tg.generateType(tt.gqlType)))
		})
	}

	require.Empty(t, tg.errs)
}

func TestGenerateEnum_CollidingValuesError(t *testing.T) {
	schema := testSchema()
	gen := newCodeGenerator("test")
	tg := newTypeGenerator(schema, gen, Options{})

	tg.generateEnum(&ast.Definition{
		Kind: ast.Enum,
		Name: "Color",
		EnumValues: ast.EnumValueList{
			{Name: "FOO_BAR"},
			{Name: "FooBar"},
		},
	})

	require.Len(t, tg.errs, 1)
	assert.Contains(t, tg.errs[0].Error(), "ColorFooBar")
}

func TestGenerateObject_CollidingFieldsError(t *testing.T) {
	schema := testSchema()
	gen := newCodeGenerator("test")
	tg := newTypeGenerator(schema, gen, Options{})

	tg.generateObject(&ast.Definition{
		Kind: ast.Object,
		Name: "User",
		Fields: ast.FieldList{
			{Name: "user_id", Type: ast.NamedType("String", nil)},
			{Name: "userId", Type: ast.NamedType("String", nil)},
		},
	})

	require.Len(t, tg.errs, 1)
	assert.Contains(t, tg.errs[0].Error(), "UserID")
}

func TestGenerateObject_MultiLineDescription(t *testing.T) {
	schema := testSchema()
	gen := newCodeGenerator("test")
	tg := newTypeGenerator(schema, gen, Options{})

	tg.generateObject(&ast.Definition{
		Kind:        ast.Object,
		Name:        "Doc",
		Description: "first line\nsecond line",
		Fields: ast.FieldList{
			{Name: "body", Type: ast.NamedType("String", nil), Description: "field line one\nfield line two"},
		},
	})

	buf, err := gen.WriteToBuffer()
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "// first line\n// second line")
	assert.Contains(t, output, "// field line one\n\t// field line two")
}

func TestScalarMappings(t *testing.T) {
	schema := testSchema()
	schema.Types["BigInt"] = &ast.Definition{Kind: ast.Scalar, Name: "BigInt"}
	schema.Types["Decimal"] = &ast.Definition{Kind: ast.Scalar, Name: "Decimal"}

	gen := newCodeGenerator("test")
	tg := newTypeGenerator(schema, gen, Options{ScalarMappings: map[string]string{
		"BigInt":  "int64",
		"Decimal": "github.com/shopspring/decimal.Decimal",
	}})

	assert.Equal(t, "int64", renderCode(t, tg.generateType(&ast.Type{NamedType: "BigInt", NonNull: true})))
	assert.Equal(t, "*int64", renderCode(t, tg.generateType(&ast.Type{NamedType: "BigInt"})))
	assert.Equal(t, "decimal.Decimal", renderCode(t, tg.generateType(&ast.Type{NamedType: "Decimal", NonNull: true})))

	// a mapped scalar gets no generated named type
	tg.generateScalar(schema.Types["BigInt"])

	buf, err := gen.WriteToBuffer()
	require.NoError(t, err)
	assert.NotContains(t, buf.String(), "type BigInt")
	require.Empty(t, tg.errs)
}

func TestJSONSchemaTypeForGoType(t *testing.T) {
	assert.Equal(t, "integer", jsonSchemaTypeForGoType("int64"))
	assert.Equal(t, "number", jsonSchemaTypeForGoType("float32"))
	assert.Equal(t, "boolean", jsonSchemaTypeForGoType("bool"))
	assert.Equal(t, "string", jsonSchemaTypeForGoType("time.Time"))
	assert.Equal(t, "string", jsonSchemaTypeForGoType("github.com/shopspring/decimal.Decimal"))
}
