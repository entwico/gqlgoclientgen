package clientgen

import (
	"fmt"

	"github.com/dave/jennifer/jen"
	"github.com/vektah/gqlparser/v2/ast"
)

// This file holds the resolved model of generated response structs: the
// resolver turns a validated selection set into respStruct/respField values,
// and the emitter renders that model with jennifer. Keeping analysis and
// emission separate makes both testable in isolation and keeps the AST walk
// free of code-generation concerns.

// respStruct is the resolved model of one generated response struct.
type respStruct struct {
	name   string
	fields []*respField
}

// respField is the resolved model of one response struct field. Exactly one
// of schemaType, structName, isTypename or invalid describes the field's type.
type respField struct {
	goName   string
	jsonName string
	nonNull  bool // whether the schema declares the field non-null

	schemaType *ast.Type // scalar, enum or custom scalar type straight from the schema
	structName string    // name of a generated struct (fragment, union or nested type)
	isTypename bool      // __typename resolved without a schema definition
	invalid    bool      // type could not be resolved; emitted as interface{}

	slice       bool // structName field backed by a list type
	elemNonNull bool // innermost list element non-null (drives []T vs []*T)

	// nested is set when this field introduced a new nested struct that must
	// be emitted before its parent
	nested *respStruct
}

// ----- resolver -----

// resolveStruct resolves a selection set into a response struct model.
// prefix namespaces the names of nested structs introduced below this one.
func (og *operationsGenerator) resolveStruct(name, prefix string, selSet ast.SelectionSet, parentType *ast.Definition, fragments map[string]*ast.FragmentDefinition) *respStruct {
	rs := &respStruct{name: name}

	// fields from sibling spreads and inline fragments dedupe into one map
	processed := make(map[string]bool)

	for _, sel := range selSet {
		rs.fields = append(rs.fields, og.resolveSelection(sel, prefix, parentType, fragments, processed)...)
	}

	return rs
}

// resolveSelection resolves one selection into zero or more fields. Fragment
// spreads and inline fragments expand into the fields they select.
func (og *operationsGenerator) resolveSelection(sel ast.Selection, prefix string, parentType *ast.Definition, fragments map[string]*ast.FragmentDefinition, processed map[string]bool) []*respField {
	switch s := sel.(type) {
	case *ast.Field:
		field := og.resolveField(s, prefix, parentType, fragments, processed)
		if field == nil {
			return nil
		}

		return []*respField{field}

	case *ast.FragmentSpread:
		frag, ok := fragments[s.Name]
		if !ok || og.isUnionTypeFragment(frag) {
			// union type fragments are handled as field types, not expanded
			return nil
		}

		fragType := og.schema.Types[frag.TypeCondition]

		var fields []*respField
		for _, fragSel := range frag.SelectionSet {
			fields = append(fields, og.resolveSelection(fragSel, prefix, fragType, fragments, processed)...)
		}

		return fields

	case *ast.InlineFragment:
		inlineType := parentType
		if s.TypeCondition != "" {
			inlineType = og.schema.Types[s.TypeCondition]
		}

		var fields []*respField
		for _, inlineSel := range s.SelectionSet {
			fields = append(fields, og.resolveSelection(inlineSel, prefix, inlineType, fragments, processed)...)
		}

		return fields

	default:
		return nil
	}
}

// resolveField resolves a single field selection, or returns nil when the
// field was already produced by an overlapping fragment.
func (og *operationsGenerator) resolveField(s *ast.Field, prefix string, parentType *ast.Definition, fragments map[string]*ast.FragmentDefinition, processed map[string]bool) *respField {
	fieldKey := s.Name
	if s.Alias != "" {
		fieldKey = s.Alias
	}

	if processed[fieldKey] {
		return nil
	}

	processed[fieldKey] = true

	field := &respField{
		goName:   og.formatFieldName(fieldKey),
		jsonName: fieldKey,
	}

	// prefer the validator-resolved definition; fall back to a lookup on the
	// parent type
	fieldDef := s.Definition
	if fieldDef == nil && parentType != nil {
		for _, f := range parentType.Fields {
			if f.Name == s.Name {
				fieldDef = f
				break
			}
		}
	}

	field.nonNull = fieldDef != nil && fieldDef.Type.NonNull

	if len(s.SelectionSet) == 0 {
		return og.resolveLeafField(s, field, fieldDef)
	}

	return og.resolveCompositeField(s, field, fieldDef, prefix, fragments)
}

// resolveLeafField resolves a field without sub-selections: a scalar, enum or
// custom scalar.
func (og *operationsGenerator) resolveLeafField(s *ast.Field, field *respField, fieldDef *ast.FieldDefinition) *respField {
	if fieldDef != nil {
		field.schemaType = fieldDef.Type

		return field
	}

	if s.Name == typenameField {
		field.isTypename = true

		return field
	}

	og.addErr(fmt.Errorf("%sfield %s: type cannot be resolved from the schema", atPos(s.Position), field.jsonName))

	field.invalid = true

	return field
}

// resolveCompositeField resolves a field with sub-selections into a reference
// to a generated struct: a union wrapper, a fragment struct or a nested type.
func (og *operationsGenerator) resolveCompositeField(s *ast.Field, field *respField, fieldDef *ast.FieldDefinition, prefix string, fragments map[string]*ast.FragmentDefinition) *respField {
	if fieldDef != nil {
		field.slice = og.isListType(fieldDef.Type)
		field.elemNonNull = listElemNonNull(fieldDef.Type)
	}

	var nestedType *ast.Definition
	if fieldDef != nil {
		nestedType = og.schema.Types[og.getBaseTypeName(fieldDef.Type)]
	}

	// union results use the types.go union wrapper, whose UnmarshalJSON
	// dispatches on __typename
	if nestedType != nil && nestedType.Kind == ast.Union {
		field.structName = nestedType.Name

		return field
	}

	if og.isOnlyFragmentSpread(s.SelectionSet) {
		og.resolveFragmentField(s, field, fragments)

		return field
	}

	baseName := field.goName
	if field.slice {
		baseName += "Item"
	}

	field.structName, field.nested = og.resolveNestedStruct(prefix+og.formatFieldName(baseName), s.SelectionSet, nestedType, fragments)

	return field
}

// resolveFragmentField types a field whose selection is a single fragment
// spread (plus optional __typename) as the fragment's generated struct.
func (og *operationsGenerator) resolveFragmentField(s *ast.Field, field *respField, fragments map[string]*ast.FragmentDefinition) {
	for _, sel := range s.SelectionSet {
		fs, ok := sel.(*ast.FragmentSpread)
		if !ok {
			continue
		}

		if structName, ok := og.fragmentStructs[fs.Name]; ok {
			field.structName = structName

			return
		}

		frag, ok := fragments[fs.Name]
		if !ok {
			field.invalid = true

			return
		}

		if og.isUnionTypeFragment(frag) {
			fragType := og.schema.Types[frag.TypeCondition]

			if fragType != nil && fragType.Kind == ast.Union {
				field.structName = fragType.Name

				return
			}

			og.addErr(fmt.Errorf("%sfragment %s: cannot determine union type for condition %s", atPos(fs.Position), fs.Name, frag.TypeCondition))

			field.invalid = true

			return
		}

		// the fragment's struct is not registered yet; reference it by its
		// derived name, the fragment loop will generate it
		field.structName = og.formatFieldName(fs.Name) + "Fragment"

		return
	}

	field.invalid = true
}

// resolveNestedStruct resolves the selection set of a nested field into its
// own named struct. It returns a nil model when the name is already taken by
// an earlier resolution or by a schema type generated in types.go.
func (og *operationsGenerator) resolveNestedStruct(structName string, selSet ast.SelectionSet, parentType *ast.Definition, fragments map[string]*ast.FragmentDefinition) (string, *respStruct) {
	if og.nestedStructs[structName] {
		return structName, nil
	}

	// if this name matches a schema type already generated in types.go, reuse it
	if schemaType, exists := og.schema.Types[structName]; exists &&
		schemaType != og.schema.Query && schemaType != og.schema.Mutation && schemaType != og.schema.Subscription &&
		(schemaType.Kind == ast.Object || schemaType.Kind == ast.InputObject || schemaType.Kind == ast.Interface) {
		return structName, nil
	}

	og.nestedStructs[structName] = true

	return structName, og.resolveStruct(structName, structName, selSet, parentType, fragments)
}

// ----- field shape helpers (shared by emitter and getter generation) -----

// isSlice reports whether the field renders as a Go slice.
func (f *respField) isSlice() bool {
	if f.schemaType != nil {
		return f.schemaType.Elem != nil
	}

	return f.slice
}

// isPointer reports whether the field renders with a top-level Go pointer.
func (f *respField) isPointer() bool {
	if f.invalid || f.isTypename {
		return false
	}

	if f.schemaType != nil {
		return !f.schemaType.NonNull && f.schemaType.Elem == nil && nullableNeedsPointer(f.baseTypeName())
	}

	return !f.slice && !f.nonNull
}

// baseTypeName returns the underlying named type: the schema type name for
// leaf fields, the generated struct name otherwise.
func (f *respField) baseTypeName() string {
	switch {
	case f.invalid:
		return interfaceType
	case f.isTypename:
		return stringType
	case f.schemaType != nil:
		t := f.schemaType
		for t.Elem != nil {
			t = t.Elem
		}

		return t.NamedType
	default:
		return f.structName
	}
}

// ----- emitter -----

// emitRespStruct renders a resolved struct model: nested structs first, depth
// first in field order, then the struct itself and its getters. comment is
// placed directly above the struct so it survives the nested emission.
func (og *operationsGenerator) emitRespStruct(rs *respStruct, comment string) {
	for _, field := range rs.fields {
		if field.nested != nil {
			og.emitRespStruct(field.nested, "")
		}
	}

	if comment != "" {
		og.gen.file.Comment(comment)
	}

	fields := make([]jen.Code, 0, len(rs.fields))
	for _, field := range rs.fields {
		fields = append(fields, og.renderField(field))
	}

	og.gen.file.Type().Id(rs.name).Struct(fields...)
	og.gen.file.Line()

	og.generateGetters(rs.name, rs.fields)
}

// renderField renders one struct field with its JSON tag.
func (og *operationsGenerator) renderField(f *respField) jen.Code {
	jsonTag := f.jsonName
	if !f.nonNull {
		jsonTag += ",omitempty"
	}

	return jen.Id(f.goName).Add(og.renderFieldType(f)).Tag(map[string]string{"json": jsonTag})
}

// renderFieldType renders the Go type of a resolved field.
func (og *operationsGenerator) renderFieldType(f *respField) jen.Code {
	switch {
	case f.invalid:
		return jen.Interface()
	case f.isTypename:
		return jen.String()
	case f.schemaType != nil:
		return og.goTypeFromGraphQLType(f.schemaType)
	case f.slice && f.elemNonNull:
		return jen.Index().Id(f.structName)
	case f.slice:
		return jen.Index().Op("*").Id(f.structName)
	case f.nonNull:
		return jen.Id(f.structName)
	default:
		return jen.Op("*").Id(f.structName)
	}
}

// generateGetters generates getter methods for nullable (pointer) fields.
func (og *operationsGenerator) generateGetters(structName string, fields []*respField) {
	// build a map of all field names for collision detection
	fieldNames := make(map[string]bool)
	for _, field := range fields {
		fieldNames[field.goName] = true
	}

	for _, field := range fields {
		// interface{} and __typename fields get no getter, slices need none
		if field.invalid || field.isTypename || field.isSlice() {
			continue
		}

		getterName := "Get" + field.goName
		if fieldNames[getterName] {
			getterName += "_" // append underscore if there's a collision
		}

		// pointer fields dereference with a zero-value fallback; value fields
		// get a plain accessor so the getter API is uniform across nullability
		body := []jen.Code{jen.Return(jen.Id("f").Dot(field.goName))}
		if field.isPointer() {
			body = []jen.Code{
				jen.If(jen.Id("f").Dot(field.goName).Op("!=").Nil()).Block(
					jen.Return(jen.Op("*").Id("f").Dot(field.goName)),
				),
				jen.Return(og.getterZeroValue(field)),
			}
		}

		// use value receiver to allow chaining
		og.gen.file.Func().Params(
			jen.Id("f").Id(structName),
		).Id(getterName).Params().Add(og.getterReturnType(field)).Block(body...)
		og.gen.file.Line()
	}
}

// getterReturnType returns the non-pointer type a getter returns.
func (og *operationsGenerator) getterReturnType(f *respField) jen.Code {
	name := f.baseTypeName()

	// schema types (scalars, enums, unions) map exactly like field types do,
	// including convenience types and configured scalar mappings
	if def, ok := og.schema.Types[name]; ok {
		code, err := goBaseType(def, og.opts.ScalarMappings)
		if err != nil {
			og.addErr(err)
		}

		return code
	}

	// generated struct types are referenced by name
	return jen.Id(name)
}

// getterZeroValue returns the zero value a getter falls back to.
func (og *operationsGenerator) getterZeroValue(f *respField) jen.Code {
	name := f.baseTypeName()

	// *new(T) is the zero value for any mapped Go type
	if goType, ok := og.opts.ScalarMappings[name]; ok {
		return jen.Op("*").New(goTypeFromString(goType))
	}

	switch name {
	case stringType, dateTimeType, localDateTimeType:
		return jen.Lit("")
	case intType:
		return jen.Lit(0)
	case floatType:
		return jen.Lit(0.0)
	case booleanType:
		return jen.False()
	default:
		if typeDef, ok := og.schema.Types[name]; ok && (typeDef.Kind == ast.Scalar || typeDef.Kind == ast.Enum) {
			// custom scalars and enums are strings underneath
			return jen.Lit("")
		}

		return jen.Id(name).Values()
	}
}
