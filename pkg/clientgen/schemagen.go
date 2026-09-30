package clientgen

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"github.com/dave/jennifer/jen"
	"github.com/vektah/gqlparser/v2/ast"
)

// schemaGenerator emits JSON Schema documents for GraphQL input types so the
// resulting JSON produced by an LLM can be unmarshaled directly into the
// generated Go input structs.
type schemaGenerator struct {
	schema *ast.Schema
	gen    *codeGenerator
	opts   Options
}

// newSchemaGenerator creates a new schema generator.
func newSchemaGenerator(schema *ast.Schema, gen *codeGenerator, opts Options) *schemaGenerator {
	return &schemaGenerator{
		schema: schema,
		gen:    gen,
		opts:   opts,
	}
}

// GenerateAllInputSchemas walks the schema and emits a JSON Schema document per
// input object type, wired to a JSONSchema method on the generated struct and a
// package-level JSONSchemaFor dispatch.
func (sg *schemaGenerator) GenerateAllInputSchemas() error {
	names := make([]string, 0)

	for _, def := range sg.schema.Types {
		if def.BuiltIn {
			continue
		}

		if def.Kind != ast.InputObject {
			continue
		}

		names = append(names, def.Name)
	}

	slices.Sort(names)

	sg.emitHelpers()

	for _, name := range names {
		def := sg.schema.Types[name]

		if err := sg.emitForInput(def); err != nil {
			return err
		}
	}

	sg.emitDispatch(names)

	return nil
}

// emitHelpers emits the inline option types that JSONSchema methods use. They
// live in the generated package so the output has no external dependency on
// gqlgoclientgen itself.
func (sg *schemaGenerator) emitHelpers() {
	sg.gen.file.Comment("SchemaOption mutates schemaOptions to select a JSON Schema variant.")
	sg.gen.file.Type().Id("SchemaOption").Func().Params(jen.Op("*").Id("schemaOptions"))
	sg.gen.file.Line()

	sg.gen.file.Comment("schemaOptions holds the resolved JSON Schema selection flags.")
	sg.gen.file.Type().Id("schemaOptions").Struct(
		jen.Comment("Strict selects the strict JSON Schema variant."),
		jen.Id("Strict").Bool(),
	)
	sg.gen.file.Line()

	sg.gen.file.Comment("Strict selects the strict JSON Schema variant: every field is required,")
	sg.gen.file.Comment("nullable fields are wrapped in null unions, and additionalProperties is false.")
	sg.gen.file.Func().Id("Strict").Params().Id("SchemaOption").Block(
		jen.Return(jen.Func().Params(jen.Id("o").Op("*").Id("schemaOptions")).Block(
			jen.Id("o").Dot("Strict").Op("=").True(),
		)),
	)
	sg.gen.file.Line()

	sg.gen.file.Comment("applySchemaOptions folds a slice of SchemaOption values into a resolved schemaOptions.")
	sg.gen.file.Func().Id("applySchemaOptions").
		Params(jen.Id("opts").Index().Id("SchemaOption")).
		Id("schemaOptions").
		Block(
			jen.Var().Id("o").Id("schemaOptions"),
			jen.For(jen.List(jen.Id("_"), jen.Id("opt")).Op(":=").Range().Id("opts")).Block(
				jen.If(jen.Id("opt").Op("!=").Nil()).Block(
					jen.Id("opt").Call(jen.Op("&").Id("o")),
				),
			),
			jen.Return(jen.Id("o")),
		)
	sg.gen.file.Line()
}

// emitForInput emits _schema<Name>_full and _schema<Name>_strict vars plus the
// JSONSchema method for a single input type.
func (sg *schemaGenerator) emitForInput(def *ast.Definition) error {
	fullBuilder := newSchemaBuilder(sg.schema, false, sg.opts)
	fullDoc := fullBuilder.buildRoot(def)

	fullRaw, err := json.Marshal(fullDoc)
	if err != nil {
		return fmt.Errorf("failed to marshal full schema for %s: %w", def.Name, err)
	}

	strictBuilder := newSchemaBuilder(sg.schema, true, sg.opts)
	strictDoc := strictBuilder.buildRoot(def)

	strictRaw, err := json.Marshal(strictDoc)
	if err != nil {
		return fmt.Errorf("failed to marshal strict schema for %s: %w", def.Name, err)
	}

	fullVar := schemaVarName(def.Name, false)
	strictVar := schemaVarName(def.Name, true)

	sg.gen.file.Commentf("%s is the permissive JSON Schema document for %s (draft 2020-12).", fullVar, def.Name)
	sg.gen.file.Var().Id(fullVar).Op("=").Qual("encoding/json", "RawMessage").Call(jen.Lit(string(fullRaw)))
	sg.gen.file.Line()

	sg.gen.file.Commentf("%s is the strict JSON Schema document for %s.", strictVar, def.Name)
	sg.gen.file.Comment("All fields are required; nullable fields use union types; additionalProperties is false.")
	sg.gen.file.Var().Id(strictVar).Op("=").Qual("encoding/json", "RawMessage").Call(jen.Lit(string(strictRaw)))
	sg.gen.file.Line()

	sg.gen.file.Commentf("JSONSchema returns the JSON Schema document describing %s.", def.Name)
	sg.gen.file.Comment("Pass Strict() for the strict variant.")
	sg.gen.file.Func().
		Params(jen.Id(def.Name)).
		Id("JSONSchema").
		Params(jen.Id("opts").Op("...").Id("SchemaOption")).
		Qual("encoding/json", "RawMessage").
		Block(
			jen.Id("o").Op(":=").Id("applySchemaOptions").Call(jen.Id("opts")),
			jen.If(jen.Id("o").Dot("Strict")).Block(
				jen.Return(jen.Id(strictVar)),
			),
			jen.Return(jen.Id(fullVar)),
		)
	sg.gen.file.Line()

	return nil
}

// emitDispatch emits the package-level JSONSchemaFor lookup.
func (sg *schemaGenerator) emitDispatch(names []string) {
	sg.gen.file.Comment("JSONSchemaFor returns the JSON Schema document for the named GraphQL input type.")
	sg.gen.file.Comment("The second return value is false when no input type with the given name exists.")
	sg.gen.file.Comment("Pass Strict() for the strict variant.")

	cases := make([]jen.Code, 0, len(names)+1)
	for _, name := range names {
		fullVar := schemaVarName(name, false)
		strictVar := schemaVarName(name, true)
		cases = append(cases, jen.Case(jen.Lit(name)).Block(
			jen.If(jen.Id("o").Dot("Strict")).Block(
				jen.Return(jen.Id(strictVar), jen.True()),
			),
			jen.Return(jen.Id(fullVar), jen.True()),
		))
	}

	cases = append(cases, jen.Default().Block(
		jen.Return(jen.Nil(), jen.False()),
	))

	body := make([]jen.Code, 0, 2)
	body = append(body,
		jen.Id("o").Op(":=").Id("applySchemaOptions").Call(jen.Id("opts")),
		jen.Switch(jen.Id("name")).Block(cases...),
	)

	sg.gen.file.Func().
		Id("JSONSchemaFor").
		Params(
			jen.Id("name").String(),
			jen.Id("opts").Op("...").Id("SchemaOption"),
		).
		Params(
			jen.Qual("encoding/json", "RawMessage"),
			jen.Bool(),
		).
		Block(body...)
	sg.gen.file.Line()
}

// schemaVarName computes the package-private var name for a generated schema.
func schemaVarName(typeName string, strict bool) string {
	if strict {
		return "_schema" + typeName + "Strict"
	}

	return "_schema" + typeName + "Full"
}

// ----- schemaBuilder -----

// schemaBuilder builds a single JSON Schema document rooted at one input type.
// It collects nested input types into $defs and resolves them through $ref.
type schemaBuilder struct {
	schema *ast.Schema
	strict bool
	opts   Options

	// defs holds nested input-object schemas by type name.
	defs map[string]map[string]any

	// inProgress tracks input types currently being built to break recursion.
	inProgress map[string]bool
}

func newSchemaBuilder(schema *ast.Schema, strict bool, opts Options) *schemaBuilder {
	return &schemaBuilder{
		schema:     schema,
		strict:     strict,
		opts:       opts,
		defs:       make(map[string]map[string]any),
		inProgress: make(map[string]bool),
	}
}

// buildRoot returns a complete draft-2020-12 document for an input object.
func (b *schemaBuilder) buildRoot(def *ast.Definition) map[string]any {
	body := b.buildInputObject(def)

	doc := make(map[string]any, len(body)+2)
	doc["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	doc["title"] = def.Name
	maps.Copy(doc, body)

	if len(b.defs) > 0 {
		// remove the root entry from $defs if it landed there via recursion
		delete(b.defs, def.Name)

		if len(b.defs) > 0 {
			doc["$defs"] = b.defs
		}
	}

	return doc
}

// buildInputObject builds the JSON Schema body (type/properties/required/...) for
// an input object, without the $schema header or $defs.
func (b *schemaBuilder) buildInputObject(def *ast.Definition) map[string]any {
	properties := make(map[string]any)
	required := make([]string, 0)

	for _, field := range def.Fields {
		if b.opts.OmitDeprecated && hasDeprecated(field.Directives) {
			continue
		}

		fieldSchema := b.buildType(field.Type)
		if field.Description != "" {
			// merge description only if the field schema is an object we own
			if m, ok := fieldSchema.(map[string]any); ok {
				// do not clobber a description already set (e.g. from an enum)
				if _, exists := m["description"]; !exists {
					m["description"] = field.Description
				}
			}
		}

		properties[field.Name] = fieldSchema

		// strict mode: every property is required; otherwise only non-null fields
		if b.strict || field.Type.NonNull {
			required = append(required, field.Name)
		}
	}

	out := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if def.Description != "" {
		out["description"] = def.Description
	}

	if len(required) > 0 {
		slices.Sort(required)
		out["required"] = required
	}

	if b.strict {
		out["additionalProperties"] = false
	}

	return out
}

// buildType returns the JSON Schema node for a GraphQL type reference. The
// result is either a map[string]any schema or a map[string]any with "$ref".
//
// Nullability handling:
//   - In strict mode, nullable nodes are wrapped via nullableStrict.
//   - In full mode, nullability is encoded only by whether the field is in the
//     parent object's "required" list; the node itself stays unchanged.
func (b *schemaBuilder) buildType(t *ast.Type) any {
	// list types
	if t.Elem != nil {
		item := b.buildType(t.Elem)

		arr := map[string]any{
			"type":  "array",
			"items": item,
		}
		if !t.NonNull && b.strict {
			return nullableStrict(arr)
		}

		return arr
	}

	// named types
	named := b.schema.Types[t.NamedType]
	if named == nil {
		// unknown type name — fall back to permissive string
		return map[string]any{
			"type":        "string",
			"description": fmt.Sprintf("unknown GraphQL type %q", t.NamedType),
		}
	}

	node := b.buildNamed(named)
	if !t.NonNull && b.strict {
		return nullableStrict(node)
	}

	return node
}

// buildNamed dispatches by named-type kind.
func (b *schemaBuilder) buildNamed(def *ast.Definition) any {
	switch def.Kind {
	case ast.Scalar:
		return b.buildScalar(def)
	case ast.Enum:
		return b.buildEnum(def)
	case ast.InputObject:
		return b.buildInputRef(def)
	case ast.Object, ast.Interface, ast.Union:
		// GraphQL does not allow these as input; treat defensively as an opaque
		// object so we never crash on a bad schema.
		return map[string]any{"type": "object"}
	default:
		return map[string]any{}
	}
}

// buildInputRef emits a $ref to a nested input type's $defs entry, filling
// $defs on demand.
func (b *schemaBuilder) buildInputRef(def *ast.Definition) map[string]any {
	if _, already := b.defs[def.Name]; !already && !b.inProgress[def.Name] {
		b.inProgress[def.Name] = true
		b.defs[def.Name] = b.buildInputObject(def)
		delete(b.inProgress, def.Name)
	}

	return map[string]any{"$ref": "#/$defs/" + def.Name}
}

// buildEnum returns an inline string+enum schema.
func (b *schemaBuilder) buildEnum(def *ast.Definition) map[string]any {
	values := make([]any, 0, len(def.EnumValues))
	for _, v := range def.EnumValues {
		if b.opts.OmitDeprecated && hasDeprecated(v.Directives) {
			continue
		}

		values = append(values, v.Name)
	}

	out := map[string]any{
		"type": "string",
		"enum": values,
	}
	if def.Description != "" {
		out["description"] = def.Description
	}

	return out
}

// buildScalar returns the JSON Schema node for a named scalar.
func (b *schemaBuilder) buildScalar(def *ast.Definition) map[string]any {
	if goType, ok := b.opts.ScalarMappings[def.Name]; ok {
		return map[string]any{"type": jsonSchemaTypeForGoType(goType)}
	}

	mapping, ok := scalarDefaults[def.Name]
	if !ok {
		// unknown custom scalar — fall back to string
		return map[string]any{
			"type":        "string",
			"description": fmt.Sprintf("custom scalar %q", def.Name),
		}
	}

	if b.strict {
		return cloneMap(mapping.strict)
	}

	return cloneMap(mapping.full)
}

// ----- scalar table -----

type scalarMapping struct {
	full   map[string]any
	strict map[string]any
}

var scalarDefaults = map[string]scalarMapping{
	stringType: {
		full:   map[string]any{"type": "string"},
		strict: map[string]any{"type": "string"},
	},
	idType: {
		full:   map[string]any{"type": "string"},
		strict: map[string]any{"type": "string"},
	},
	intType: {
		full:   map[string]any{"type": "integer"},
		strict: map[string]any{"type": "integer"},
	},
	floatType: {
		full:   map[string]any{"type": "number"},
		strict: map[string]any{"type": "number"},
	},
	booleanType: {
		full:   map[string]any{"type": "boolean"},
		strict: map[string]any{"type": "boolean"},
	},
	dateTimeType: {
		full:   map[string]any{"type": "string", "format": "date-time"},
		strict: map[string]any{"type": "string", "format": "date-time"},
	},
	localDateTimeType: {
		full:   map[string]any{"type": "string", "format": "date-time"},
		strict: map[string]any{"type": "string", "format": "date-time"},
	},
	timeType: {
		full:   map[string]any{"type": "string", "format": "date-time"},
		strict: map[string]any{"type": "string", "format": "date-time"},
	},
	mapType: {
		full:   map[string]any{"type": "object", "additionalProperties": true},
		strict: map[string]any{"type": "string", "description": "JSON-encoded object (lossy downgrade for strict mode)"},
	},
	anyType: {
		full:   map[string]any{},
		strict: map[string]any{"type": "string", "description": "arbitrary value encoded as string (lossy downgrade for strict mode)"},
	},
	uploadType: {
		full:   map[string]any{"type": "string", "description": "base64-encoded file content"},
		strict: map[string]any{"type": "string", "description": "base64-encoded file content"},
	},
}

// ----- helpers -----

// nullableStrict wraps a schema node with a null union. Works on both plain
// schemas ({"type":"string"} -> {"type":["string","null"]}) and $ref nodes
// ({"$ref":"..."} -> {"anyOf":[{"$ref":"..."},{"type":"null"}]}).
func nullableStrict(node any) map[string]any {
	m, ok := node.(map[string]any)
	if !ok {
		return map[string]any{"anyOf": []any{node, map[string]any{"type": "null"}}}
	}

	if _, isRef := m["$ref"]; isRef {
		return map[string]any{"anyOf": []any{m, map[string]any{"type": "null"}}}
	}

	if t, hasType := m["type"]; hasType {
		switch tv := t.(type) {
		case string:
			m["type"] = []any{tv, "null"}
			return m
		case []any:
			for _, v := range tv {
				if s, ok := v.(string); ok && s == "null" {
					return m
				}
			}

			m["type"] = append(tv, "null")

			return m
		}
	}
	// empty or enum-only schema — wrap via anyOf so we don't lose info
	return map[string]any{"anyOf": []any{m, map[string]any{"type": "null"}}}
}

// cloneMap makes a shallow copy of a map[string]any so mutations by callers
// don't leak back into the scalar defaults table.
func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	maps.Copy(out, m)

	return out
}
