package clientgen

import (
	"strings"
	"testing"

	"github.com/dave/jennifer/jen"
	"github.com/stretchr/testify/assert"
	"github.com/vektah/gqlparser/v2/ast"
)

// helper to render jen.Code to string for comparison.
func renderCode(t *testing.T, code jen.Code) string {
	t.Helper()
	// wrap the code in a statement to use GoString()
	stmt, ok := code.(*jen.Statement)
	if !ok {
		t.Fatal("expected *jen.Statement")
	}

	return stmt.GoString()
}

// helper to create a minimal schema for testing.
func testSchema() *ast.Schema {
	return &ast.Schema{
		Types: map[string]*ast.Definition{
			"String": {
				Kind: ast.Scalar,
				Name: "String",
			},
			"Int": {
				Kind: ast.Scalar,
				Name: "Int",
			},
			"Float": {
				Kind: ast.Scalar,
				Name: "Float",
			},
			"Boolean": {
				Kind: ast.Scalar,
				Name: "Boolean",
			},
			"ID": {
				Kind: ast.Scalar,
				Name: "ID",
			},
			"CustomScalar": {
				Kind: ast.Scalar,
				Name: "CustomScalar",
			},
			"MyInput": {
				Kind: ast.InputObject,
				Name: "MyInput",
			},
			"MyEnum": {
				Kind: ast.Enum,
				Name: "MyEnum",
			},
		},
	}
}

func TestGoTypeFromGraphQLType(t *testing.T) {
	schema := testSchema()
	og := newOperationsGenerator(schema, nil, nil, Options{})

	tests := []struct {
		name     string
		gqlType  *ast.Type
		expected string
	}{
		// nullable scalars (should be pointers)
		{
			name:     "nullable String",
			gqlType:  &ast.Type{NamedType: "String"},
			expected: "*string",
		},
		{
			name:     "nullable Int",
			gqlType:  &ast.Type{NamedType: "Int"},
			expected: "*int",
		},
		{
			name:     "nullable Float",
			gqlType:  &ast.Type{NamedType: "Float"},
			expected: "*float64",
		},
		{
			name:     "nullable Boolean",
			gqlType:  &ast.Type{NamedType: "Boolean"},
			expected: "*bool",
		},
		{
			// ID has a recognizable zero value and stays a value even when nullable
			name:     "nullable ID",
			gqlType:  &ast.Type{NamedType: "ID"},
			expected: "string",
		},

		// non-null scalars (no pointer)
		{
			name:     "non-null String (String!)",
			gqlType:  &ast.Type{NamedType: "String", NonNull: true},
			expected: "string",
		},
		{
			name:     "non-null Int (Int!)",
			gqlType:  &ast.Type{NamedType: "Int", NonNull: true},
			expected: "int",
		},
		{
			name:     "non-null Boolean (Boolean!)",
			gqlType:  &ast.Type{NamedType: "Boolean", NonNull: true},
			expected: "bool",
		},

		// nullable list of nullable elements [String]
		{
			name: "nullable list of nullable String ([String])",
			gqlType: &ast.Type{
				Elem: &ast.Type{NamedType: "String"},
			},
			expected: "[]*string",
		},

		// nullable list of non-null elements [String!]
		{
			name: "nullable list of non-null String ([String!])",
			gqlType: &ast.Type{
				Elem: &ast.Type{NamedType: "String", NonNull: true},
			},
			expected: "[]string",
		},

		// non-null list of nullable elements [String]!
		{
			name: "non-null list of nullable String ([String]!)",
			gqlType: &ast.Type{
				NonNull: true,
				Elem:    &ast.Type{NamedType: "String"},
			},
			expected: "[]*string",
		},

		// non-null list of non-null elements [String!]! - the bug case
		{
			name: "non-null list of non-null String ([String!]!)",
			gqlType: &ast.Type{
				NonNull: true,
				Elem:    &ast.Type{NamedType: "String", NonNull: true},
			},
			expected: "[]string",
		},

		// non-null list of non-null Int [Int!]!
		{
			name: "non-null list of non-null Int ([Int!]!)",
			gqlType: &ast.Type{
				NonNull: true,
				Elem:    &ast.Type{NamedType: "Int", NonNull: true},
			},
			expected: "[]int",
		},

		// custom scalars
		{
			name:     "nullable custom scalar",
			gqlType:  &ast.Type{NamedType: "CustomScalar"},
			expected: "*CustomScalar",
		},
		{
			name:     "non-null custom scalar",
			gqlType:  &ast.Type{NamedType: "CustomScalar", NonNull: true},
			expected: "CustomScalar",
		},
		{
			name: "non-null list of non-null custom scalar ([CustomScalar!]!)",
			gqlType: &ast.Type{
				NonNull: true,
				Elem:    &ast.Type{NamedType: "CustomScalar", NonNull: true},
			},
			expected: "[]CustomScalar",
		},

		// input objects
		{
			name:     "nullable input object",
			gqlType:  &ast.Type{NamedType: "MyInput"},
			expected: "*MyInput",
		},
		{
			name:     "non-null input object",
			gqlType:  &ast.Type{NamedType: "MyInput", NonNull: true},
			expected: "MyInput",
		},
		{
			name: "non-null list of non-null input objects ([MyInput!]!)",
			gqlType: &ast.Type{
				NonNull: true,
				Elem:    &ast.Type{NamedType: "MyInput", NonNull: true},
			},
			expected: "[]MyInput",
		},

		// enums
		{
			name:     "nullable enum",
			gqlType:  &ast.Type{NamedType: "MyEnum"},
			expected: "*MyEnum",
		},
		{
			name:     "non-null enum",
			gqlType:  &ast.Type{NamedType: "MyEnum", NonNull: true},
			expected: "MyEnum",
		},
		{
			name: "non-null list of non-null enums ([MyEnum!]!)",
			gqlType: &ast.Type{
				NonNull: true,
				Elem:    &ast.Type{NamedType: "MyEnum", NonNull: true},
			},
			expected: "[]MyEnum",
		},

		// ID type variations
		{
			name: "non-null list of non-null ID ([ID!]!)",
			gqlType: &ast.Type{
				NonNull: true,
				Elem:    &ast.Type{NamedType: "ID", NonNull: true},
			},
			expected: "[]string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := og.goTypeFromGraphQLType(tt.gqlType)
			rendered := renderCode(t, result)
			assert.Equal(t, tt.expected, rendered)
		})
	}
}

func TestResolveNestedStruct_SchemaTypeCollision(t *testing.T) {
	queryDef := &ast.Definition{Kind: ast.Object, Name: "Query"}
	schema := &ast.Schema{
		Types: map[string]*ast.Definition{
			"Query": queryDef,
			"Pagination": {
				Kind: ast.Object,
				Name: "Pagination",
				Fields: ast.FieldList{
					{Name: "page", Type: &ast.Type{NamedType: "Int", NonNull: true}},
					{Name: "totalPages", Type: &ast.Type{NamedType: "Int", NonNull: true}},
				},
			},
			"String": {Kind: ast.Scalar, Name: "String"},
			"Int":    {Kind: ast.Scalar, Name: "Int"},
		},
		Query: queryDef,
	}

	gen := newCodeGenerator("test")
	og := newOperationsGenerator(schema, nil, gen, Options{})

	parentType := &ast.Definition{
		Kind: ast.Object,
		Name: "SomeResponse",
		Fields: ast.FieldList{
			{Name: "page", Type: &ast.Type{NamedType: "Int", NonNull: true}},
		},
	}

	selSet := ast.SelectionSet{
		&ast.Field{
			Name:       "page",
			Definition: &ast.FieldDefinition{Name: "page", Type: &ast.Type{NamedType: "Int", NonNull: true}},
		},
	}

	// when the name matches a schema type generated in types.go, reuse it
	result, nested := og.resolveNestedStruct("Pagination", selSet, parentType, nil)
	assert.Equal(t, "Pagination", result)
	assert.Nil(t, nested, "should not resolve a struct model for a schema type collision")
	assert.False(t, og.nestedStructs["Pagination"], "should not register a struct for a schema type collision")
}

func TestResolveNestedStruct_NoCollision(t *testing.T) {
	queryDef := &ast.Definition{Kind: ast.Object, Name: "Query"}
	schema := &ast.Schema{
		Types: map[string]*ast.Definition{
			"Query":  queryDef,
			"String": {Kind: ast.Scalar, Name: "String"},
			"Int":    {Kind: ast.Scalar, Name: "Int"},
		},
		Query: queryDef,
	}

	gen := newCodeGenerator("test")
	og := newOperationsGenerator(schema, nil, gen, Options{})

	parentType := &ast.Definition{
		Kind: ast.Object,
		Name: "SomeResponse",
		Fields: ast.FieldList{
			{Name: "page", Type: &ast.Type{NamedType: "Int", NonNull: true}},
		},
	}

	selSet := ast.SelectionSet{
		&ast.Field{
			Name:       "page",
			Definition: &ast.FieldDefinition{Name: "page", Type: &ast.Type{NamedType: "Int", NonNull: true}},
		},
	}

	// when the name does NOT match any schema type, resolve the struct normally
	result, nested := og.resolveNestedStruct("UniqueNestedType", selSet, parentType, nil)
	assert.Equal(t, "UniqueNestedType", result)
	assert.NotNil(t, nested, "should resolve a struct model for non-colliding names")
	assert.True(t, og.nestedStructs["UniqueNestedType"], "should register non-colliding names")
}

func TestResolveNestedStruct_ScalarCollisionStillGenerates(t *testing.T) {
	// scalar types should NOT trigger the collision skip (only Object/InputObject/Interface)
	queryDef := &ast.Definition{Kind: ast.Object, Name: "Query"}
	schema := &ast.Schema{
		Types: map[string]*ast.Definition{
			"Query":  queryDef,
			"String": {Kind: ast.Scalar, Name: "String"},
			"Int":    {Kind: ast.Scalar, Name: "Int"},
		},
		Query: queryDef,
	}

	gen := newCodeGenerator("test")
	og := newOperationsGenerator(schema, nil, gen, Options{})

	parentType := &ast.Definition{
		Kind: ast.Object,
		Name: "SomeResponse",
		Fields: ast.FieldList{
			{Name: "value", Type: &ast.Type{NamedType: "String", NonNull: true}},
		},
	}

	selSet := ast.SelectionSet{
		&ast.Field{
			Name:       "value",
			Definition: &ast.FieldDefinition{Name: "value", Type: &ast.Type{NamedType: "String", NonNull: true}},
		},
	}

	// "Int" is a scalar in the schema, so the collision check should NOT skip resolution
	result, nested := og.resolveNestedStruct("Int", selSet, parentType, nil)
	assert.Equal(t, "Int", result)
	assert.NotNil(t, nested, "scalar collision should still resolve the struct")
	assert.True(t, og.nestedStructs["Int"], "scalar collision should still register the struct")
}

func TestIsListType(t *testing.T) {
	schema := testSchema()
	og := newOperationsGenerator(schema, nil, nil, Options{})

	tests := []struct {
		name     string
		gqlType  *ast.Type
		expected bool
	}{
		{
			name:     "simple scalar is not a list",
			gqlType:  &ast.Type{NamedType: "String"},
			expected: false,
		},
		{
			name:     "non-null scalar is not a list",
			gqlType:  &ast.Type{NamedType: "String", NonNull: true},
			expected: false,
		},
		{
			name: "nullable list is a list",
			gqlType: &ast.Type{
				Elem: &ast.Type{NamedType: "String"},
			},
			expected: true,
		},
		{
			name: "non-null list is a list ([String]!)",
			gqlType: &ast.Type{
				NonNull: true,
				Elem:    &ast.Type{NamedType: "String"},
			},
			expected: true,
		},
		{
			name: "non-null list of non-null elements is a list ([String!]!)",
			gqlType: &ast.Type{
				NonNull: true,
				Elem:    &ast.Type{NamedType: "String", NonNull: true},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := og.isListType(tt.gqlType)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBuildQueryString_FragmentOrderIsStable(t *testing.T) {
	gen := newCodeGenerator("test")
	og := newOperationsGenerator(testSchema(), nil, gen, Options{})

	fragments := make(map[string]*ast.FragmentDefinition)
	selSet := make(ast.SelectionSet, 0, 5)

	for _, name := range []string{"Delta", "Alpha", "Echo", "Charlie", "Bravo"} {
		fragments[name] = &ast.FragmentDefinition{
			Name:          name,
			TypeCondition: "Post",
			SelectionSet:  ast.SelectionSet{&ast.Field{Name: "id", Alias: "id"}},
		}
		selSet = append(selSet, &ast.FragmentSpread{Name: name})
	}

	op := &ast.OperationDefinition{
		Operation:    ast.Query,
		Name:         "getPost",
		SelectionSet: ast.SelectionSet{&ast.Field{Name: "post", Alias: "post", SelectionSet: selSet}},
	}

	expected := og.buildQueryString(op, fragments)

	positions := make([]int, 0, len(fragments))
	for _, name := range []string{"Alpha", "Bravo", "Charlie", "Delta", "Echo"} {
		positions = append(positions, strings.Index(expected, "fragment "+name+" on"))
	}

	assert.NotContains(t, positions, -1)
	assert.IsIncreasing(t, positions)

	// map iteration order varies per call, so repeated calls expose any dependency on it
	for range 50 {
		assert.Equal(t, expected, og.buildQueryString(op, fragments))
	}
}
