package clientgen

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/dave/jennifer/jen"
	"github.com/vektah/gqlparser/v2/ast"
)

// typeGenerator generates Go types from GraphQL schema.
type typeGenerator struct {
	schema *ast.Schema
	gen    *codeGenerator
	opts   Options
	errs   []error
}

// newTypeGenerator creates a new type generator.
func newTypeGenerator(schema *ast.Schema, gen *codeGenerator, opts Options) *typeGenerator {
	return &typeGenerator{
		schema: schema,
		gen:    gen,
		opts:   opts,
	}
}

// GenerateAllTypes generates all types from the schema.
func (tg *typeGenerator) GenerateAllTypes() error {
	// generate the Optional wrapper type first (used by input objects)
	tg.generateOptionalType()

	// collect all type names and sort them for consistent output
	typeNames := make([]string, 0, len(tg.schema.Types))
	for _, def := range tg.schema.Types {
		if def.BuiltIn || def == tg.schema.Query || def == tg.schema.Mutation || def == tg.schema.Subscription {
			continue
		}

		typeNames = append(typeNames, def.Name)
	}

	slices.Sort(typeNames)

	for _, name := range typeNames {
		def := tg.schema.Types[name]
		tg.generateDefinition(def)

		// generate interface implementations
		for _, typ := range tg.schema.GetImplements(def) {
			tg.generateInterfaceImplementation(def, typ)
		}
	}

	return errors.Join(tg.errs...)
}

// addErr records a generation error; generation continues so all problems are
// reported in one run.
func (tg *typeGenerator) addErr(err error) {
	tg.errs = append(tg.errs, err)
}

// fileComment emits a description as line comments, splitting multi-line
// descriptions so every line carries the // prefix.
func (tg *typeGenerator) fileComment(text string) {
	for line := range strings.SplitSeq(text, "\n") {
		tg.gen.file.Comment(line)
	}
}

// descriptionComment starts a statement with a description rendered as line
// comments, one per line, for use before struct fields or const definitions.
func descriptionComment(text string) *jen.Statement {
	stmt := jen.Empty()

	for line := range strings.SplitSeq(text, "\n") {
		stmt = stmt.Comment(line).Line()
	}

	return stmt
}

// generateOptionalType generates the Optional wrapper type for nullable fields.
func (tg *typeGenerator) generateOptionalType() {
	tg.gen.file.Comment("Optional is a wrapper type to distinguish between null, unset, and valued fields in GraphQL mutations.")
	tg.gen.file.Comment("This solves the problem where Go's omitempty can't distinguish between \"not set\" and \"explicitly set to null/empty\".")
	tg.gen.file.Type().Id("Optional").Types(jen.Id("T").Any()).Struct(
		jen.Id("value").Id("T"),
		jen.Id("isSet").Bool(),
		jen.Id("isNull").Bool(),
	)
	tg.gen.file.Line()

	// NewOptional constructor
	tg.gen.file.Comment("NewOptional creates an Optional with a value set.")
	tg.gen.file.Func().Id("NewOptional").Types(jen.Id("T").Any()).Params(
		jen.Id("value").Id("T"),
	).Id("Optional").Types(jen.Id("T")).Block(
		jen.Return(jen.Id("Optional").Types(jen.Id("T")).Values(jen.Dict{
			jen.Id("value"):  jen.Id("value"),
			jen.Id("isSet"):  jen.True(),
			jen.Id("isNull"): jen.False(),
		})),
	)
	tg.gen.file.Line()

	// NullOptional constructor
	tg.gen.file.Comment("NullOptional creates an Optional with an explicit null value.")
	tg.gen.file.Func().Id("NullOptional").Types(jen.Id("T").Any()).Params().Id("Optional").Types(jen.Id("T")).Block(
		jen.Var().Id("zero").Id("T"),
		jen.Return(jen.Id("Optional").Types(jen.Id("T")).Values(jen.Dict{
			jen.Id("value"):  jen.Id("zero"),
			jen.Id("isSet"):  jen.True(),
			jen.Id("isNull"): jen.True(),
		})),
	)
	tg.gen.file.Line()

	// IsZero method for omitzero support
	tg.gen.file.Comment("IsZero returns true if the Optional is not set (for omitzero support).")
	tg.gen.file.Func().Params(
		jen.Id("o").Id("Optional").Types(jen.Id("T")),
	).Id("IsZero").Params().Bool().Block(
		jen.Return(jen.Op("!").Id("o").Dot("isSet")),
	)
	tg.gen.file.Line()

	// MarshalJSON method
	tg.gen.file.Comment("MarshalJSON implements json.Marshaler for Optional.")
	tg.gen.file.Comment("When set to null, it marshals to JSON null.")
	tg.gen.file.Comment("When set with value, it marshals the value.")
	tg.gen.file.Func().Params(
		jen.Id("o").Id("Optional").Types(jen.Id("T")),
	).Id("MarshalJSON").Params().Params(
		jen.Index().Byte(),
		jen.Error(),
	).Block(
		jen.Comment("Note: if not set, IsZero returns true and omitempty will exclude this field"),
		jen.If(jen.Id("o").Dot("isNull")).Block(
			jen.Return(jen.Index().Byte().Parens(jen.Lit("null")), jen.Nil()),
		),
		jen.Return(jen.Qual("encoding/json", "Marshal").Call(jen.Id("o").Dot("value"))),
	)
	tg.gen.file.Line()

	// UnmarshalJSON method
	tg.gen.file.Comment("UnmarshalJSON implements json.Unmarshaler for Optional.")
	tg.gen.file.Func().Params(
		jen.Id("o").Op("*").Id("Optional").Types(jen.Id("T")),
	).Id("UnmarshalJSON").Params(
		jen.Id("data").Index().Byte(),
	).Error().Block(
		jen.If(jen.String().Call(jen.Id("data")).Op("==").Lit("null")).Block(
			jen.Id("o").Dot("isSet").Op("=").True(),
			jen.Id("o").Dot("isNull").Op("=").True(),
			jen.Return(jen.Nil()),
		),
		jen.If(jen.Err().Op(":=").Qual("encoding/json", "Unmarshal").Call(
			jen.Id("data"),
			jen.Op("&").Id("o").Dot("value"),
		), jen.Err().Op("!=").Nil()).Block(
			jen.Return(jen.Err()),
		),
		jen.Id("o").Dot("isSet").Op("=").True(),
		jen.Id("o").Dot("isNull").Op("=").False(),
		jen.Return(jen.Nil()),
	)
	tg.gen.file.Line()

	// Value method
	tg.gen.file.Comment("Value returns the value and whether it's set and non-null.")
	tg.gen.file.Func().Params(
		jen.Id("o").Id("Optional").Types(jen.Id("T")),
	).Id("Value").Params().Params(
		jen.Id("T"),
		jen.Bool(),
	).Block(
		jen.If(jen.Id("o").Dot("isSet").Op("&&").Op("!").Id("o").Dot("isNull")).Block(
			jen.Return(jen.Id("o").Dot("value"), jen.True()),
		),
		jen.Var().Id("zero").Id("T"),
		jen.Return(jen.Id("zero"), jen.False()),
	)
	tg.gen.file.Line()
}

// generateDefinition generates a single type definition.
func (tg *typeGenerator) generateDefinition(def *ast.Definition) {
	switch def.Kind {
	case ast.Scalar:
		tg.generateScalar(def)
	case ast.Enum:
		tg.generateEnum(def)
	case ast.Object, ast.InputObject:
		tg.generateObject(def)
	case ast.Interface, ast.Union:
		tg.generateInterfaceOrUnion(def)
	default:
		// skip unknown definition kinds
	}
}

// generateScalar generates a scalar type.
func (tg *typeGenerator) generateScalar(def *ast.Definition) {
	// a mapped scalar uses the configured Go type directly
	if _, ok := tg.opts.ScalarMappings[def.Name]; ok {
		return
	}

	// skip convenience types that are handled specially
	switch def.Name {
	case timeType, mapType, uploadType, anyType:
		return
	}

	if def.Description != "" {
		tg.fileComment(def.Description)
	}

	tg.gen.file.Type().Id(def.Name).String()
	tg.gen.file.Line()

	// add helper methods for date/time scalar types. LocalDateTime carries no
	// timezone offset in its canonical form, so it formats without one but still
	// tolerates inputs that include a trailing Z or offset when parsing.
	switch def.Name {
	case dateTimeType:
		tg.generateDateTimeHelpers(dateTimeType,
			func() jen.Code { return jen.Qual("time", "RFC3339") },
			[]func() jen.Code{
				func() jen.Code { return jen.Qual("time", "RFC3339") },
			},
		)
	case localDateTimeType:
		tg.generateDateTimeHelpers(localDateTimeType,
			func() jen.Code { return jen.Lit("2006-01-02T15:04:05") },
			[]func() jen.Code{
				func() jen.Code { return jen.Lit("2006-01-02T15:04:05") },
				func() jen.Code { return jen.Qual("time", "RFC3339") },
			},
		)
	default:
		// other scalar types get no extra helpers
	}
}

// generateDateTimeHelpers generates helper methods for a date/time scalar type.
// formatLayout produces the layout New<TypeName> uses to serialize a time.Time.
// parseLayouts produces the layouts AsTime tries in order, so values that carry
// a trailing Z or timezone offset still parse instead of silently yielding the
// zero time.
func (tg *typeGenerator) generateDateTimeHelpers(typeName string, formatLayout func() jen.Code, parseLayouts []func() jen.Code) {
	// New<TypeName> creates the scalar from time.Time
	tg.gen.file.Comment("New" + typeName + " creates a " + typeName + " from time.Time")
	tg.gen.file.Func().Id("New" + typeName).Params(
		jen.Id("t").Qual("time", "Time"),
	).Id(typeName).Block(
		jen.Return(jen.Id(typeName).Call(
			jen.Id("t").Dot("Format").Call(formatLayout()),
		)),
	)
	tg.gen.file.Line()

	// build the slice of candidate layouts for AsTime to try
	layoutValues := make([]jen.Code, 0, len(parseLayouts))
	for _, layout := range parseLayouts {
		layoutValues = append(layoutValues, layout())
	}

	// AsTime converts the scalar to time.Time, trying each layout until one parses
	tg.gen.file.Comment("AsTime converts " + typeName + " to time.Time.")
	tg.gen.file.Comment("It tries each supported layout and returns the last parse error if none matches.")
	tg.gen.file.Func().Params(
		jen.Id("dt").Id(typeName),
	).Id("AsTime").Params().Params(jen.Qual("time", "Time"), jen.Error()).Block(
		jen.Var().Id("lastErr").Error(),
		jen.For(
			jen.List(jen.Id("_"), jen.Id("layout")).Op(":=").Range().Index().String().Values(layoutValues...),
		).Block(
			jen.List(jen.Id("res"), jen.Id("err")).Op(":=").Qual("time", "Parse").Call(
				jen.Id("layout"),
				jen.String().Call(jen.Id("dt")),
			),
			jen.If(jen.Id("err").Op("==").Nil()).Block(
				jen.Return(jen.Id("res"), jen.Nil()),
			),
			jen.Id("lastErr").Op("=").Id("err"),
		),
		jen.Return(jen.Qual("time", "Time").Values(), jen.Id("lastErr")),
	)
	tg.gen.file.Line()

	// TimeOrZero is the lenient companion to AsTime for use in expressions
	tg.gen.file.Comment("TimeOrZero converts " + typeName + " to time.Time, returning the zero time when it does not parse.")
	tg.gen.file.Func().Params(
		jen.Id("dt").Id(typeName),
	).Id("TimeOrZero").Params().Qual("time", "Time").Block(
		jen.List(jen.Id("res"), jen.Id("_")).Op(":=").Id("dt").Dot("AsTime").Call(),
		jen.Return(jen.Id("res")),
	)
	tg.gen.file.Line()

	// String method for the scalar
	tg.gen.file.Comment("String returns the string representation of " + typeName)
	tg.gen.file.Func().Params(
		jen.Id("dt").Id(typeName),
	).Id("String").Params().String().Block(
		jen.Return(jen.String().Call(jen.Id("dt"))),
	)
	tg.gen.file.Line()

	// IsZero checks if the scalar is empty/zero
	tg.gen.file.Comment("IsZero checks if " + typeName + " is empty")
	tg.gen.file.Func().Params(
		jen.Id("dt").Id(typeName),
	).Id("IsZero").Params().Bool().Block(
		jen.Return(jen.Id("dt").Op("==").Lit("")),
	)
	tg.gen.file.Line()
}

// generateEnum generates an enum type with constants.
func (tg *typeGenerator) generateEnum(def *ast.Definition) {
	if def.Description != "" {
		tg.fileComment(def.Description)
	}

	// generate the base type
	tg.gen.file.Type().Id(def.Name).String()
	tg.gen.file.Line()

	var (
		defs            []jen.Code
		constNames      = make(map[string]string)
		hasDescriptions bool
		descEntries     = jen.Dict{}
	)

	for _, val := range def.EnumValues {
		if tg.opts.OmitDeprecated && hasDeprecated(val.Directives) {
			continue
		}

		constName := enumValueToConstName(def.Name, val.Name)

		// two spellings like FOO_BAR and FooBar collapse to the same Go
		// constant, which would emit uncompilable code
		if prev, ok := constNames[constName]; ok {
			tg.addErr(fmt.Errorf("%senum %s: values %q and %q both map to Go constant %s; rename one of them", atPos(val.Position), def.Name, prev, val.Name, constName))
			continue
		}

		constNames[constName] = val.Name

		if val.Description != "" {
			hasDescriptions = true

			defs = append(defs, descriptionComment(val.Description).Id(constName).Id(def.Name).Op("=").Lit(val.Name))
		} else {
			defs = append(defs, jen.Id(constName).Id(def.Name).Op("=").Lit(val.Name))
		}

		label := val.Name
		if val.Description != "" {
			label = val.Description
		}

		descEntries[jen.Id(constName)] = jen.Lit(label)
	}

	if len(defs) > 0 {
		tg.gen.file.Const().Defs(defs...)
		tg.gen.file.Line()
	}

	// emit the list of all values so consumers can enumerate the enum
	if len(constNames) > 0 {
		values := make([]jen.Code, 0, len(def.EnumValues))

		for _, val := range def.EnumValues {
			if tg.opts.OmitDeprecated && hasDeprecated(val.Directives) {
				continue
			}

			if name := enumValueToConstName(def.Name, val.Name); constNames[name] == val.Name {
				values = append(values, jen.Id(name))
			}
		}

		tg.gen.file.Comment(def.Name + "Values lists every " + def.Name + " value.")
		tg.gen.file.Var().Id(def.Name + "Values").Op("=").Index().Id(def.Name).Values(values...)
		tg.gen.file.Line()
	}

	// emit the descriptions lookup only when at least one value documents itself;
	// values without a description fall back to their own name
	if hasDescriptions {
		tg.gen.file.Comment(def.Name + "Descriptions maps each " + def.Name + " value to its human-readable description.")
		tg.gen.file.Var().Id(def.Name + "Descriptions").Op("=").Map(jen.Id(def.Name)).String().Values(descEntries)
		tg.gen.file.Line()
	}
}

// generateObject generates an object or input object type.
func (tg *typeGenerator) generateObject(def *ast.Definition) {
	if def.Description != "" {
		tg.fileComment(def.Description)
	}

	fields := make([]jen.Code, 0, len(def.Fields))
	goNames := make(map[string]string)

	for _, field := range def.Fields {
		if tg.opts.OmitDeprecated && hasDeprecated(field.Directives) {
			continue
		}

		// skip introspection fields
		if field.Name == "__schema" || field.Name == "__type" {
			continue
		}

		// names like user_id and userId collapse to the same Go field, which
		// would emit uncompilable code
		goName := fieldNameToGoName(field.Name)
		if prev, ok := goNames[goName]; ok {
			tg.addErr(fmt.Errorf("%stype %s: fields %q and %q both map to Go field %s; rename one of them", atPos(field.Position), def.Name, prev, field.Name, goName))
			continue
		}

		goNames[goName] = field.Name

		// use different field generation for input objects vs regular objects
		var fieldCode jen.Code
		if def.Kind == ast.InputObject {
			fieldCode = tg.generateInputField(field)
		} else {
			fieldCode = tg.generateField(field)
		}

		fields = append(fields, fieldCode)
	}

	tg.gen.file.Type().Id(def.Name).Struct(fields...)
	tg.gen.file.Line()
}

// generateField generates a struct field.
func (tg *typeGenerator) generateField(field *ast.FieldDefinition) jen.Code {
	name := fieldNameToGoName(field.Name)
	typ := tg.generateType(field.Type)

	// build JSON tag
	jsonTag := field.Name
	if !field.Type.NonNull {
		jsonTag += omitEmptyTag
	}

	tag := jen.Tag(map[string]string{"json": jsonTag})

	// add description if present
	if field.Description != "" {
		return descriptionComment(field.Description).Id(name).Add(typ).Add(tag)
	}

	return jen.Id(name).Add(typ).Add(tag)
}

// generateInputField generates a struct field for input objects using Optional wrapper for nullable fields.
func (tg *typeGenerator) generateInputField(field *ast.FieldDefinition) jen.Code {
	name := fieldNameToGoName(field.Name)

	// for input objects, wrap nullable fields in Optional to distinguish between null and unset
	var typ jen.Code

	if !field.Type.NonNull {
		// wrap the type in Optional[T] (value type)
		// the zero Optional value reports IsZero()==true so ",omitzero" omits unset fields
		innerType := tg.generateTypeForOptional(field.Type)
		typ = jen.Id("Optional").Types(innerType)
	} else {
		// required fields don't need Optional wrapper
		typ = tg.generateType(field.Type)
	}

	// build JSON tag - nullable fields use omitzero so Optional.IsZero() controls omission
	jsonTag := field.Name
	if !field.Type.NonNull {
		jsonTag += omitZeroTag
	}

	tag := jen.Tag(map[string]string{"json": jsonTag})

	// add description if present
	if field.Description != "" {
		return descriptionComment(field.Description).Id(name).Add(typ).Add(tag)
	}

	return jen.Id(name).Add(typ).Add(tag)
}

// generateTypeForOptional generates the Go type for use inside Optional[T].
// The Optional wrapper itself carries the null/unset distinction, so no pointer
// is added at the top level; nested list elements keep their nullability.
func (tg *typeGenerator) generateTypeForOptional(t *ast.Type) jen.Code {
	if t.Elem != nil {
		// a nil slice already represents null, Optional distinguishes unset
		return jen.Index().Add(tg.generateTypeAtLevel(t.Elem, false))
	}

	def, ok := tg.schema.Types[t.NamedType]
	if !ok {
		tg.addErr(fmt.Errorf("%sunknown type %q", atPos(t.Position), t.NamedType))

		return jen.Interface()
	}

	base := tg.baseGoType(def)

	// a self-referential input type without indirection would not compile,
	// so composite types are held by pointer inside Optional
	if def.Kind == ast.InputObject {
		return jen.Op("*").Add(base)
	}

	return base
}

// baseGoType maps a named GraphQL definition to its Go representation without
// any pointer or slice wrapping.
func (tg *typeGenerator) baseGoType(def *ast.Definition) jen.Code {
	code, err := goBaseType(def, tg.opts.ScalarMappings)
	if err != nil {
		tg.addErr(err)
	}

	return code
}

// goBaseType maps a named GraphQL definition to its Go representation without
// any pointer or slice wrapping. It is shared by the type and operations
// generators so both emit identical Go types for the same schema type.
// A scalar mapping takes precedence over every built-in rule.
func goBaseType(def *ast.Definition, mappings map[string]string) (jen.Code, error) {
	if goType, ok := mappings[def.Name]; ok {
		return goTypeFromString(goType), nil
	}

	switch def.Name {
	// standard GraphQL scalars
	case intType:
		return jen.Int(), nil
	case floatType:
		return jen.Float64(), nil
	case stringType, idType:
		return jen.String(), nil
	case booleanType:
		return jen.Bool(), nil
	// convenience types
	case timeType:
		return jen.Qual("time", "Time"), nil
	case mapType:
		return jen.Map(jen.String()).Interface(), nil
	case uploadType:
		return jen.Qual(hasuraClientPkg, "Upload"), nil
	case anyType:
		return jen.Interface(), nil
	default:
		if def.BuiltIn {
			return jen.Interface(), fmt.Errorf("unsupported built-in type: %s", def.Name)
		}

		return jen.Id(def.Name), nil
	}
}

// nullableNeedsPointer reports whether a nullable occurrence of the named type
// is represented as a Go pointer. Types with a recognizable zero value are not.
func nullableNeedsPointer(name string) bool {
	switch name {
	case idType, timeType, mapType, anyType:
		return false
	default:
		return true
	}
}

// generateInterfaceOrUnion generates an interface or union type.
func (tg *typeGenerator) generateInterfaceOrUnion(def *ast.Definition) {
	possibleTypes := tg.schema.GetPossibleTypes(def)

	typeNames := make([]string, 0, len(possibleTypes))
	for _, typ := range possibleTypes {
		typeNames = append(typeNames, typ.Name)
	}

	// generate the main struct
	fields := make([]jen.Code, 0)

	// add interface fields
	for _, field := range def.Fields {
		if tg.opts.OmitDeprecated && hasDeprecated(field.Directives) {
			continue
		}

		if field.Name == "__schema" || field.Name == "__type" {
			continue
		}

		fieldCode := tg.generateField(field)
		fields = append(fields, fieldCode)
	}

	// add Value field for storing the concrete type
	if len(fields) > 0 {
		fields = append(fields, jen.Line())
	}

	fields = append(fields,
		jen.Comment("Value holds the concrete type implementing this "+strings.ToLower(string(def.Kind))),
		jen.Id("Value").Id(def.Name+"Value").Tag(map[string]string{"json": "-"}),
	)

	if def.Description != "" {
		tg.fileComment(def.Description)
	}

	tg.gen.file.Type().Id(def.Name).Struct(fields...)
	tg.gen.file.Line()

	// generate UnmarshalJSON method
	tg.generateUnmarshalJSON(def, possibleTypes)

	// generate interface type
	tg.gen.file.Comment(def.Name + "Value is implemented by: " + strings.Join(typeNames, ", "))
	tg.gen.file.Type().Id(def.Name + "Value").Interface(
		jen.Id("is" + def.Name).Params(),
	)
	tg.gen.file.Line()
}

// generateUnmarshalJSON generates the UnmarshalJSON method for interfaces/unions.
func (tg *typeGenerator) generateUnmarshalJSON(def *ast.Definition, possibleTypes []*ast.Definition) {
	// build switch cases for each possible type
	switchCases := make([]jen.Code, 0, len(possibleTypes)+2)
	for _, typ := range possibleTypes {
		switchCases = append(switchCases, jen.Case(jen.Lit(typ.Name)).Block(
			jen.Id("base").Dot("Value").Op("=").New(jen.Id(typ.Name)),
		))
	}

	// handle empty typename
	errPrefix := fmt.Sprintf("%s %s: ", strings.ToLower(string(def.Kind)), def.Name)
	switch def.Kind {
	case ast.Interface:
		switchCases = append(switchCases, jen.Case(jen.Lit("")).Block(
			jen.Return(jen.Nil()),
		))
	case ast.Union:
		switchCases = append(switchCases, jen.Case(jen.Lit("")).Block(
			jen.Return(jen.Qual("fmt", "Errorf").Call(jen.Lit(errPrefix+"missing __typename field"))),
		))
	default:
		// other definition kinds don't need special handling for empty typename
	}

	// default case for unknown typename
	switchCases = append(switchCases,
		jen.Default().Block(
			jen.Return(jen.Qual("fmt", "Errorf").Call(
				jen.Lit(errPrefix+"unknown __typename %q"),
				jen.Id("data").Dot("TypeName"),
			)),
		),
	)

	// generate the method
	tg.gen.file.Func().Params(
		jen.Id("base").Op("*").Id(def.Name),
	).Id("UnmarshalJSON").Params(
		jen.Id("b").Index().Byte(),
	).Error().Block(
		jen.Type().Id("Raw").Id(def.Name),
		jen.Var().Id("data").Struct(
			jen.Op("*").Id("Raw"),
			jen.Id("TypeName").String().Tag(map[string]string{"json": "__typename"}),
		),
		jen.Id("data").Dot("Raw").Op("=").Parens(jen.Op("*").Id("Raw")).Parens(jen.Id("base")),
		jen.Id("err").Op(":=").Qual("encoding/json", "Unmarshal").Call(
			jen.Id("b"),
			jen.Op("&").Id("data"),
		),
		jen.If(jen.Id("err").Op("!=").Nil()).Block(
			jen.Return(jen.Id("err")),
		),
		jen.Switch(jen.Id("data").Dot("TypeName")).Block(switchCases...),
		jen.Return(jen.Qual("encoding/json", "Unmarshal").Call(
			jen.Id("b"),
			jen.Id("base").Dot("Value"),
		)),
	)
	tg.gen.file.Line()
}

// generateInterfaceImplementation generates the interface implementation method.
func (tg *typeGenerator) generateInterfaceImplementation(def *ast.Definition, iface *ast.Definition) {
	tg.gen.file.Func().Params(
		tg.generateType(ast.NamedType(def.Name, nil)),
	).Id("is" + iface.Name).Params().Block()
	tg.gen.file.Line()
}

// generateType generates the Go type for a GraphQL type.
func (tg *typeGenerator) generateType(t *ast.Type) jen.Code {
	return tg.generateTypeAtLevel(t, true)
}

// generateTypeAtLevel maps a GraphQL type to Go, tracking nullability at every
// nesting level so [String] and [String!] produce []*string and []string.
// toplevel marks the outermost, non-list position: only there do non-null
// object and interface fields get a pointer, to break recursive types.
func (tg *typeGenerator) generateTypeAtLevel(t *ast.Type, toplevel bool) jen.Code {
	if t.Elem != nil {
		// a slice is already nilable, so a nullable list needs no extra pointer
		return jen.Index().Add(tg.generateTypeAtLevel(t.Elem, false))
	}

	def, ok := tg.schema.Types[t.NamedType]
	if !ok {
		tg.addErr(fmt.Errorf("%sunknown type %q", atPos(t.Position), t.NamedType))

		return jen.Interface()
	}

	base := tg.baseGoType(def)

	if !t.NonNull {
		if !nullableNeedsPointer(def.Name) {
			return base
		}

		return jen.Op("*").Add(base)
	}

	if toplevel && (def.Kind == ast.Object || def.Kind == ast.Interface) {
		// required to deal with recursive types
		return jen.Op("*").Add(base)
	}

	return base
}

// hasDeprecated checks if a directive list contains a deprecated directive.
func hasDeprecated(list ast.DirectiveList) bool {
	return list.ForName("deprecated") != nil
}
