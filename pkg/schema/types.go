// Package schema fetches GraphQL schemas via introspection and converts the
// introspection result to SDL.
package schema

// Directive describes a directive definition from an introspection result.
type Directive struct {
	Name         string              `json:"name"`
	Description  *string             `json:"description,omitempty"`
	Locations    []DirectiveLocation `json:"locations"`
	Args         []InputValue        `json:"args"`
	IsRepeatable bool                `json:"isRepeatable"`
}

// DirectiveLocation is the __DirectiveLocation enum from the introspection schema.
type DirectiveLocation string

// The directive locations defined by the GraphQL introspection schema.
const (
	DirectiveLocationQuery                DirectiveLocation = "QUERY"
	DirectiveLocationMutation             DirectiveLocation = "MUTATION"
	DirectiveLocationSubscription         DirectiveLocation = "SUBSCRIPTION"
	DirectiveLocationField                DirectiveLocation = "FIELD"
	DirectiveLocationFragmentDefinition   DirectiveLocation = "FRAGMENT_DEFINITION"
	DirectiveLocationFragmentSpread       DirectiveLocation = "FRAGMENT_SPREAD"
	DirectiveLocationInlineFragment       DirectiveLocation = "INLINE_FRAGMENT"
	DirectiveLocationVariableDefinition   DirectiveLocation = "VARIABLE_DEFINITION"
	DirectiveLocationSchema               DirectiveLocation = "SCHEMA"
	DirectiveLocationScalar               DirectiveLocation = "SCALAR"
	DirectiveLocationObject               DirectiveLocation = "OBJECT"
	DirectiveLocationFieldDefinition      DirectiveLocation = "FIELD_DEFINITION"
	DirectiveLocationArgumentDefinition   DirectiveLocation = "ARGUMENT_DEFINITION"
	DirectiveLocationInterface            DirectiveLocation = "INTERFACE"
	DirectiveLocationUnion                DirectiveLocation = "UNION"
	DirectiveLocationEnum                 DirectiveLocation = "ENUM"
	DirectiveLocationEnumValue            DirectiveLocation = "ENUM_VALUE"
	DirectiveLocationInputObject          DirectiveLocation = "INPUT_OBJECT"
	DirectiveLocationInputFieldDefinition DirectiveLocation = "INPUT_FIELD_DEFINITION"
)

// EnumValue describes one value of an enum type from an introspection result.
type EnumValue struct {
	Name              string  `json:"name"`
	Description       *string `json:"description,omitempty"`
	IsDeprecated      bool    `json:"isDeprecated"`
	DeprecationReason *string `json:"deprecationReason,omitempty"`
}

// Field describes an output field of an object or interface type.
type Field struct {
	Name              string       `json:"name"`
	Description       *string      `json:"description,omitempty"`
	Args              []InputValue `json:"args"`
	Type              *Type        `json:"type"`
	IsDeprecated      bool         `json:"isDeprecated"`
	DeprecationReason *string      `json:"deprecationReason,omitempty"`
}

// InputValue describes an argument or an input object field.
type InputValue struct {
	Name         string  `json:"name"`
	Description  *string `json:"description,omitempty"`
	Type         *Type   `json:"type"`
	DefaultValue *string `json:"defaultValue,omitempty"`
}

// Schema is the root of a GraphQL introspection result.
type Schema struct {
	Types            []Type      `json:"types"`
	QueryType        *Type       `json:"queryType"`
	MutationType     *Type       `json:"mutationType,omitempty"`
	SubscriptionType *Type       `json:"subscriptionType,omitempty"`
	Directives       []Directive `json:"directives"`
}

// Type describes a named type or a LIST/NON_NULL wrapper in an introspection result.
type Type struct {
	Kind          TypeKind     `json:"kind"`
	Name          *string      `json:"name,omitempty"`
	Description   *string      `json:"description,omitempty"`
	Fields        []Field      `json:"fields,omitempty"`
	Interfaces    []Type       `json:"interfaces,omitempty"`
	PossibleTypes []Type       `json:"possibleTypes,omitempty"`
	EnumValues    []EnumValue  `json:"enumValues,omitempty"`
	InputFields   []InputValue `json:"inputFields,omitempty"`
	OfType        *Type        `json:"ofType,omitempty"`
}

// TypeKind is the __TypeKind enum from the introspection schema.
type TypeKind string

// The type kinds defined by the GraphQL introspection schema.
const (
	TypeKindScalar      TypeKind = "SCALAR"
	TypeKindObject      TypeKind = "OBJECT"
	TypeKindInterface   TypeKind = "INTERFACE"
	TypeKindUnion       TypeKind = "UNION"
	TypeKindEnum        TypeKind = "ENUM"
	TypeKindInputObject TypeKind = "INPUT_OBJECT"
	TypeKindList        TypeKind = "LIST"
	TypeKindNonNull     TypeKind = "NON_NULL"
)
