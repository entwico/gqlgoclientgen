package schema

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToSDL_NilSchema(t *testing.T) {
	_, err := ToSDL(nil)
	require.Error(t, err)
	assert.Equal(t, "schema is nil", err.Error())
}

func TestToSDL_EmptySchema(t *testing.T) {
	schema := &Schema{}
	sdl, err := ToSDL(schema)
	require.NoError(t, err)
	assert.Empty(t, sdl)
}

func TestToSDL_ScalarType(t *testing.T) {
	name := "DateTime"
	schema := &Schema{
		Types: []Type{
			{
				Kind: TypeKindScalar,
				Name: &name,
			},
		},
	}

	sdl, err := ToSDL(schema)
	require.NoError(t, err)
	assert.Contains(t, sdl, "scalar DateTime")
}

func TestToSDL_EnumType(t *testing.T) {
	name := "Status"
	schema := &Schema{
		Types: []Type{
			{
				Kind: TypeKindEnum,
				Name: &name,
				EnumValues: []EnumValue{
					{Name: "ACTIVE"},
					{Name: "INACTIVE"},
				},
			},
		},
	}

	sdl, err := ToSDL(schema)
	require.NoError(t, err)
	assert.Contains(t, sdl, "enum Status")
	assert.Contains(t, sdl, "ACTIVE")
	assert.Contains(t, sdl, "INACTIVE")
}

func TestToSDL_ObjectType(t *testing.T) {
	name := "User"
	fieldName := "id"
	fieldTypeName := "ID"
	schema := &Schema{
		Types: []Type{
			{
				Kind: TypeKindObject,
				Name: &name,
				Fields: []Field{
					{
						Name: fieldName,
						Type: &Type{
							Kind: TypeKindNonNull,
							OfType: &Type{
								Kind: TypeKindScalar,
								Name: &fieldTypeName,
							},
						},
					},
				},
			},
		},
	}

	sdl, err := ToSDL(schema)
	require.NoError(t, err)
	assert.Contains(t, sdl, "type User")
	assert.Contains(t, sdl, "id: ID!")
}

func TestToSDL_SkipsBuiltInTypes(t *testing.T) {
	stringName := "String"
	intName := "Int"
	boolName := "Boolean"
	customName := "Custom"

	schema := &Schema{
		Types: []Type{
			{Kind: TypeKindScalar, Name: &stringName},
			{Kind: TypeKindScalar, Name: &intName},
			{Kind: TypeKindScalar, Name: &boolName},
			{Kind: TypeKindScalar, Name: &customName},
		},
	}

	sdl, err := ToSDL(schema)
	require.NoError(t, err)

	// should not contain built-in types
	assert.NotContains(t, sdl, "scalar String")
	assert.NotContains(t, sdl, "scalar Int")
	assert.NotContains(t, sdl, "scalar Boolean")

	// should contain custom type
	assert.Contains(t, sdl, "scalar Custom")
}

func TestToSDL_SkipsIntrospectionTypes(t *testing.T) {
	schemaTypeName := "__Schema"
	typeName := "__Type"
	customName := "Query"

	schema := &Schema{
		Types: []Type{
			{Kind: TypeKindObject, Name: &schemaTypeName},
			{Kind: TypeKindObject, Name: &typeName},
			{Kind: TypeKindObject, Name: &customName, Fields: []Field{}},
		},
	}

	sdl, err := ToSDL(schema)
	require.NoError(t, err)

	// should not contain introspection types
	assert.NotContains(t, sdl, "__Schema")
	assert.NotContains(t, sdl, "__Type")

	// should contain custom type
	assert.Contains(t, sdl, "type Query")
}

func TestToSDL_WithSchemaDefinition(t *testing.T) {
	queryName := "RootQuery"
	mutationName := "RootMutation"

	schema := &Schema{
		QueryType:    &Type{Name: &queryName},
		MutationType: &Type{Name: &mutationName},
		Types: []Type{
			{Kind: TypeKindObject, Name: &queryName, Fields: []Field{}},
			{Kind: TypeKindObject, Name: &mutationName, Fields: []Field{}},
		},
	}

	sdl, err := ToSDL(schema)
	require.NoError(t, err)

	// should contain schema definition for non-default names
	assert.Contains(t, sdl, "schema {")
	assert.Contains(t, sdl, "query: RootQuery")
	assert.Contains(t, sdl, "mutation: RootMutation")
}

func TestToSDL_NoSchemaDefinitionForDefaultNames(t *testing.T) {
	queryName := "Query"
	mutationName := "Mutation"

	schema := &Schema{
		QueryType:    &Type{Name: &queryName},
		MutationType: &Type{Name: &mutationName},
		Types: []Type{
			{Kind: TypeKindObject, Name: &queryName, Fields: []Field{}},
			{Kind: TypeKindObject, Name: &mutationName, Fields: []Field{}},
		},
	}

	sdl, err := ToSDL(schema)
	require.NoError(t, err)

	// should not contain schema definition for default names
	assert.NotContains(t, sdl, "schema {")
}

func TestToSDL_InputObjectType(t *testing.T) {
	name := "CreateUserInput"
	fieldTypeName := "String"
	schema := &Schema{
		Types: []Type{
			{
				Kind: TypeKindInputObject,
				Name: &name,
				InputFields: []InputValue{
					{
						Name: "name",
						Type: &Type{
							Kind: TypeKindScalar,
							Name: &fieldTypeName,
						},
					},
				},
			},
		},
	}

	sdl, err := ToSDL(schema)
	require.NoError(t, err)
	assert.Contains(t, sdl, "input CreateUserInput")
	assert.Contains(t, sdl, "name: String")
}

func TestToSDL_UnionType(t *testing.T) {
	name := "SearchResult"
	userType := "User"
	postType := "Post"
	schema := &Schema{
		Types: []Type{
			{
				Kind: TypeKindUnion,
				Name: &name,
				PossibleTypes: []Type{
					{Name: &userType},
					{Name: &postType},
				},
			},
		},
	}

	sdl, err := ToSDL(schema)
	require.NoError(t, err)
	assert.Contains(t, sdl, "union SearchResult = User | Post")
}

func TestToSDL_InterfaceType(t *testing.T) {
	name := "Node"
	fieldTypeName := "ID"
	schema := &Schema{
		Types: []Type{
			{
				Kind: TypeKindInterface,
				Name: &name,
				Fields: []Field{
					{
						Name: "id",
						Type: &Type{
							Kind: TypeKindNonNull,
							OfType: &Type{
								Kind: TypeKindScalar,
								Name: &fieldTypeName,
							},
						},
					},
				},
			},
		},
	}

	sdl, err := ToSDL(schema)
	require.NoError(t, err)
	assert.Contains(t, sdl, "interface Node")
	assert.Contains(t, sdl, "id: ID!")
}

func TestToSDL_TypeWithDescription(t *testing.T) {
	name := "User"
	description := "A user in the system"
	schema := &Schema{
		Types: []Type{
			{
				Kind:        TypeKindObject,
				Name:        &name,
				Description: &description,
				Fields:      []Field{},
			},
		},
	}

	sdl, err := ToSDL(schema)
	require.NoError(t, err)
	assert.True(t, strings.Contains(sdl, "A user in the system") || strings.Contains(sdl, `"""A user in the system"""`))
}

func TestToSDL_DeprecatedField(t *testing.T) {
	name := "User"
	fieldTypeName := "String"
	deprecationReason := "Use newField instead"
	schema := &Schema{
		Types: []Type{
			{
				Kind: TypeKindObject,
				Name: &name,
				Fields: []Field{
					{
						Name: "oldField",
						Type: &Type{
							Kind: TypeKindScalar,
							Name: &fieldTypeName,
						},
						IsDeprecated:      true,
						DeprecationReason: &deprecationReason,
					},
				},
			},
		},
	}

	sdl, err := ToSDL(schema)
	require.NoError(t, err)
	assert.Contains(t, sdl, "@deprecated")
}

func TestToSDL_ListType(t *testing.T) {
	name := "Query"
	stringType := "String"
	schema := &Schema{
		Types: []Type{
			{
				Kind: TypeKindObject,
				Name: &name,
				Fields: []Field{
					{
						Name: "items",
						Type: &Type{
							Kind: TypeKindList,
							OfType: &Type{
								Kind: TypeKindScalar,
								Name: &stringType,
							},
						},
					},
				},
			},
		},
	}

	sdl, err := ToSDL(schema)
	require.NoError(t, err)
	assert.Contains(t, sdl, "items: [String]")
}

func TestToSDL_NonNullListType(t *testing.T) {
	name := "Query"
	stringType := "String"
	schema := &Schema{
		Types: []Type{
			{
				Kind: TypeKindObject,
				Name: &name,
				Fields: []Field{
					{
						Name: "items",
						Type: &Type{
							Kind: TypeKindNonNull,
							OfType: &Type{
								Kind: TypeKindList,
								OfType: &Type{
									Kind: TypeKindNonNull,
									OfType: &Type{
										Kind: TypeKindScalar,
										Name: &stringType,
									},
								},
							},
						},
					},
				},
			},
		},
	}

	sdl, err := ToSDL(schema)
	require.NoError(t, err)
	assert.Contains(t, sdl, "items: [String!]!")
}

func TestToSDL_CustomDirective(t *testing.T) {
	desc := "Restricts access."
	name := "Query"
	fieldType := "String"

	schema := &Schema{
		Types: []Type{
			{
				Kind: TypeKindObject,
				Name: &name,
				Fields: []Field{
					{Name: "hello", Type: &Type{Kind: TypeKindScalar, Name: &fieldType}},
				},
			},
		},
		Directives: []Directive{
			{Name: "include"},
			{
				Name:         "auth",
				Description:  &desc,
				IsRepeatable: true,
				Locations:    []DirectiveLocation{DirectiveLocationFieldDefinition, DirectiveLocationObject},
				Args: []InputValue{
					{Name: "role", Type: &Type{Kind: TypeKindScalar, Name: &fieldType}},
				},
			},
		},
	}

	sdl, err := ToSDL(schema)
	require.NoError(t, err)
	assert.Contains(t, sdl, "directive @auth(role: String) repeatable on FIELD_DEFINITION | OBJECT")
	// built-in directives must not be re-declared
	assert.NotContains(t, sdl, "directive @include")
}

func TestToSDL_EmptyUnionIsError(t *testing.T) {
	name := "SearchResult"

	schema := &Schema{
		Types: []Type{
			{Kind: TypeKindUnion, Name: &name},
		},
	}

	_, err := ToSDL(schema)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SearchResult")
}

func TestToSDL_NilFieldTypeIsError(t *testing.T) {
	name := "Query"

	schema := &Schema{
		Types: []Type{
			{
				Kind: TypeKindObject,
				Name: &name,
				Fields: []Field{
					{Name: "broken", Type: nil},
				},
			},
		},
	}

	_, err := ToSDL(schema)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "broken")
}
