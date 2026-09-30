package clientgen

import (
	"errors"
	"fmt"
	"go/token"
	"maps"
	"slices"
	"strings"

	"github.com/dave/jennifer/jen"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/formatter"
)

// Constants for operations generation.
const (
	interfaceType     = "interface{}"
	typenameField     = "__typename"
	intType           = "Int"
	floatType         = "Float"
	stringType        = "String"
	booleanType       = "Boolean"
	idType            = "ID"
	timeType          = "Time"
	dateTimeType      = "DateTime"
	localDateTimeType = "LocalDateTime"
	mapType           = "Map"
	anyType           = "Any"
	uploadType        = "Upload"
	omitEmptyTag      = ",omitempty"
	omitZeroTag       = ",omitzero"
)

// operationsGenerator generates Go code for GraphQL operations.
type operationsGenerator struct {
	schema          *ast.Schema
	doc             *ast.QueryDocument
	gen             *codeGenerator
	opts            Options
	fragmentStructs map[string]string // maps fragment name to struct name
	nestedStructs   map[string]bool   // tracks already resolved nested struct types
	errs            []error
}

// newOperationsGenerator creates a new operations generator for the combined,
// validated query document.
func newOperationsGenerator(schema *ast.Schema, doc *ast.QueryDocument, gen *codeGenerator, opts Options) *operationsGenerator {
	return &operationsGenerator{
		schema:          schema,
		doc:             doc,
		gen:             gen,
		opts:            opts,
		fragmentStructs: make(map[string]string),
		nestedStructs:   make(map[string]bool),
	}
}

// GenerateOperations generates all operations from the combined document.
func (og *operationsGenerator) GenerateOperations() error {
	if og.doc == nil {
		return errors.New("no parsed document found")
	}

	// generate fragment map for easy lookup
	fragmentMap := make(map[string]*ast.FragmentDefinition)
	for _, frag := range og.doc.Fragments {
		fragmentMap[frag.Name] = frag
	}

	// generate structs for all fragments first (skip union type fragments)
	for _, frag := range og.doc.Fragments {
		if !og.isUnionTypeFragment(frag) {
			og.generateFragmentStruct(frag, fragmentMap)
		}
	}

	for _, op := range og.doc.Operations {
		err := og.generateOperation(op, fragmentMap)
		if err != nil {
			return fmt.Errorf("failed to generate operation %s: %w", op.Name, err)
		}
	}

	return errors.Join(og.errs...)
}

// addErr records a generation error; generation continues so all problems are
// reported in one run.
func (og *operationsGenerator) addErr(err error) {
	og.errs = append(og.errs, err)
}

// generateFragmentStruct generates a reusable struct for a fragment.
func (og *operationsGenerator) generateFragmentStruct(frag *ast.FragmentDefinition, fragments map[string]*ast.FragmentDefinition) {
	structName := og.formatFieldName(frag.Name) + "Fragment"
	og.fragmentStructs[frag.Name] = structName

	fragType := og.schema.Types[frag.TypeCondition]

	rs := og.resolveStruct(structName, structName, frag.SelectionSet, fragType, fragments)

	og.emitRespStruct(rs, fmt.Sprintf("%s represents the %s fragment", structName, frag.Name))
}

// generateOperation generates code for a single operation.
func (og *operationsGenerator) generateOperation(op *ast.OperationDefinition, fragments map[string]*ast.FragmentDefinition) error {
	// generate the query string including fragments
	queryStr := og.buildQueryString(op, fragments)

	// generate request/response types based on the expanded query
	og.generateOperationTypes(op, fragments)

	// generate the appropriate method based on operation type
	switch op.Operation {
	case ast.Query:
		og.generateQueryMethod(op, queryStr)
	case ast.Mutation:
		og.generateMutationMethod(op, queryStr)
	case ast.Subscription:
		og.generateSubscriptionMethod(op, queryStr)
	default:
		return fmt.Errorf("unsupported operation type: %v", op.Operation)
	}

	return nil
}

// buildQueryString builds the complete query string including used fragments.
func (og *operationsGenerator) buildQueryString(op *ast.OperationDefinition, fragments map[string]*ast.FragmentDefinition) string {
	// collect all fragments used by this operation
	usedFragments := og.collectUsedFragments(op.SelectionSet, fragments, make(map[string]bool))

	// build the query document
	var queryDoc ast.QueryDocument

	queryDoc.Operations = ast.OperationList{op}

	// add used fragments in sorted order, map iteration would reorder them between runs
	for _, fragName := range slices.Sorted(maps.Keys(usedFragments)) {
		if frag, ok := fragments[fragName]; ok {
			queryDoc.Fragments = append(queryDoc.Fragments, frag)
		}
	}

	// format to string
	var sb strings.Builder
	formatter.NewFormatter(&sb).FormatQueryDocument(&queryDoc)

	return sb.String()
}

// collectUsedFragments recursively collects all fragments used in a selection set.
func (og *operationsGenerator) collectUsedFragments(selSet ast.SelectionSet, fragments map[string]*ast.FragmentDefinition, visited map[string]bool) map[string]bool {
	used := make(map[string]bool)

	for _, sel := range selSet {
		switch s := sel.(type) {
		case *ast.Field:
			// recursively check field selections
			childFrags := og.collectUsedFragments(s.SelectionSet, fragments, visited)
			for frag := range childFrags {
				used[frag] = true
			}

		case *ast.FragmentSpread:
			// mark this fragment as used
			fragName := s.Name
			if !visited[fragName] {
				visited[fragName] = true
				used[fragName] = true

				// recursively check the fragment's selections
				if frag, ok := fragments[fragName]; ok {
					childFrags := og.collectUsedFragments(frag.SelectionSet, fragments, visited)
					for f := range childFrags {
						used[f] = true
					}
				}
			}

		case *ast.InlineFragment:
			// recursively check inline fragment selections
			childFrags := og.collectUsedFragments(s.SelectionSet, fragments, visited)
			for frag := range childFrags {
				used[frag] = true
			}

		default:
			// ignore unknown selection types
		}
	}

	return used
}

// generateOperationTypes generates request and response types for an operation.
func (og *operationsGenerator) generateOperationTypes(op *ast.OperationDefinition, fragments map[string]*ast.FragmentDefinition) {
	// we no longer generate variables struct since we use individual parameters
	// only generate response struct based on the expanded selection set
	og.generateResponseStructExpanded(op, fragments)
}

// generateResponseStructExpanded generates a struct for operation response with expanded fragments.
func (og *operationsGenerator) generateResponseStructExpanded(op *ast.OperationDefinition, fragments map[string]*ast.FragmentDefinition) {
	structName := og.operationResponseName(op.Name)

	// determine the root type based on operation type
	var rootType *ast.Definition

	switch op.Operation {
	case ast.Mutation:
		rootType = og.schema.Mutation
	case ast.Subscription:
		rootType = og.schema.Subscription
	default:
		rootType = og.schema.Query
	}

	// nested types are namespaced with the operation method name so two
	// operations selecting the same root field never collide
	rs := og.resolveStruct(structName, og.formatMethodName(op.Name), op.SelectionSet, rootType, fragments)

	og.emitRespStruct(rs, fmt.Sprintf("%s represents the response for the %s operation.", structName, op.Name))
}

// listElemNonNull reports whether the innermost element of a list type is non-null.
func listElemNonNull(t *ast.Type) bool {
	for t.Elem != nil {
		t = t.Elem
	}

	return t.NonNull
}

// getBaseTypeName extracts the base type name from a Type (unwrapping List and NonNull).
func (og *operationsGenerator) getBaseTypeName(t *ast.Type) string {
	if t.NamedType != "" {
		return t.NamedType
	}

	if t.Elem != nil {
		return og.getBaseTypeName(t.Elem)
	}

	return ""
}

// isListType checks if a type is a list.
func (og *operationsGenerator) isListType(t *ast.Type) bool {
	if t.Elem != nil && t.NamedType == "" {
		return true
	}

	if t.Elem != nil {
		return og.isListType(t.Elem)
	}

	return false
}

// generateQueryMethod generates a method on Queries for executing a query.
func (og *operationsGenerator) generateQueryMethod(op *ast.OperationDefinition, queryStr string) {
	og.generateExecMethod(op, queryStr, "q", "Queries", "query")
}

// generateMutationMethod generates a method on Mutations for executing a mutation.
func (og *operationsGenerator) generateMutationMethod(op *ast.OperationDefinition, queryStr string) {
	og.generateExecMethod(op, queryStr, "m", "Mutations", "mutation")
}

// generateExecMethod emits a request/response method: queries and mutations
// share the exact same execution shape and differ only in receiver and wording.
func (og *operationsGenerator) generateExecMethod(op *ast.OperationDefinition, queryStr, receiver, receiverType, kind string) {
	methodName := og.formatMethodName(op.Name)
	responseName := og.operationResponseName(op.Name)

	// build method signature with individual parameters
	methodParams := make([]jen.Code, 0, len(op.VariableDefinitions)+1)
	methodParams = append(methodParams, jen.Id("ctx").Qual("context", "Context"))

	// add individual parameters for each variable
	for _, varDef := range op.VariableDefinitions {
		paramName := og.formatParameterName(varDef.Variable)
		paramType := og.goTypeFromGraphQLType(varDef.Type)
		methodParams = append(methodParams, jen.Id(paramName).Add(paramType))
	}

	// build method body
	var methodBody []jen.Code

	// check if client is configured
	methodBody = append(methodBody,
		jen.If(jen.Id(receiver).Dot("client").Dot("client").Op("==").Nil()).Block(
			jen.Return(jen.Nil(), jen.Qual("errors", "New").Call(jen.Lit("graphql client is not configured"))),
		),
		jen.Line(),
	)

	// define the query string with all fragments expanded as multiline using backticks
	methodBody = append(methodBody,
		jen.Id("query").Op(":=").Op("`"+queryStr+"`"),
		jen.Line(),
	)

	// prepare variables map from individual parameters if needed
	varsParam := jen.Nil()

	if len(op.VariableDefinitions) > 0 {
		methodBody = append(methodBody, og.buildVariablesMapFromParams(op))
		varsParam = jen.Id("vars")
	}

	methodBody = append(methodBody,
		jen.Id("rawData").Op(",").Err().Op(":=").Id(receiver).Dot("client").Dot("client").Dot("ExecRaw").Call(
			jen.Id("ctx"),
			jen.Id("query"),
			varsParam,
		),
		jen.If(jen.Err().Op("!=").Nil()).Block(
			jen.Return(jen.Nil(), jen.Err()),
		),
		jen.Line(),
		// unmarshal the response
		jen.Var().Id("resp").Id(responseName),
		jen.If(
			jen.Err().Op(":=").Qual("encoding/json", "Unmarshal").Call(
				jen.Id("rawData"),
				jen.Op("&").Id("resp"),
			),
			jen.Err().Op("!=").Nil(),
		).Block(
			jen.Return(jen.Nil(), jen.Err()),
		),
		jen.Line(),
		jen.Return(jen.Op("&").Id("resp"), jen.Nil()),
	)

	og.gen.file.Comment(fmt.Sprintf("%s executes the %s %s.", methodName, op.Name, kind))
	og.gen.file.Func().Params(
		jen.Id(receiver).Op("*").Id(receiverType),
	).Id(methodName).Params(methodParams...).Params(
		jen.Op("*").Id(responseName),
		jen.Error(),
	).Block(methodBody...)
	og.gen.file.Line()
}

// goTypeFromGraphQLType converts a GraphQL type to a Go type, tracking
// nullability at every nesting level so [String] and [String!] produce
// []*string and []string. The mapping matches the one used for schema types.
func (og *operationsGenerator) goTypeFromGraphQLType(t *ast.Type) jen.Code {
	if t.Elem != nil {
		// a slice is already nilable, so a nullable list needs no extra pointer
		return jen.Index().Add(og.goTypeFromGraphQLType(t.Elem))
	}

	def, ok := og.schema.Types[t.NamedType]
	if !ok {
		og.addErr(fmt.Errorf("%sunknown type %q", atPos(t.Position), t.NamedType))

		return jen.Interface()
	}

	base, err := goBaseType(def, og.opts.ScalarMappings)
	if err != nil {
		og.addErr(err)
	}

	if !t.NonNull && nullableNeedsPointer(def.Name) {
		return jen.Op("*").Add(base)
	}

	return base
}

// operationResponseName generates a response struct name for an operation.
func (og *operationsGenerator) operationResponseName(opName string) string {
	return og.formatMethodName(opName) + "Response"
}

// formatMethodName formats an operation name as a Go method name.
func (og *operationsGenerator) formatMethodName(name string) string {
	return toPascalCase(name)
}

// isOnlyFragmentSpread reports whether a selection set is effectively a single
// fragment spread: exactly one spread, optionally accompanied by __typename.
// The spread's generated struct can then be used as the field type directly.
func (og *operationsGenerator) isOnlyFragmentSpread(selSet ast.SelectionSet) bool {
	spreads := 0

	for _, sel := range selSet {
		switch s := sel.(type) {
		case *ast.FragmentSpread:
			spreads++
		case *ast.Field:
			if s.Name != typenameField {
				return false
			}
		default:
			return false
		}
	}

	return spreads == 1
}

// isUnionTypeFragment checks if a fragment is a union type fragment (contains only inline fragments and fragment spreads).
func (og *operationsGenerator) isUnionTypeFragment(frag *ast.FragmentDefinition) bool {
	for _, sel := range frag.SelectionSet {
		switch s := sel.(type) {
		case *ast.Field:
			// if it has regular fields, it's not a pure union type fragment
			// unless it's __typename which is used for type discrimination
			if s.Name != typenameField {
				return false
			}
		case *ast.InlineFragment, *ast.FragmentSpread:
			// these are expected in union type fragments
			continue
		default:
			return false
		}
	}

	return true
}

// formatFieldName formats a GraphQL field name as a Go struct field name.
func (og *operationsGenerator) formatFieldName(name string) string {
	return fieldNameToGoName(name)
}

// formatParameterName formats a GraphQL variable name as a Go parameter name.
func (og *operationsGenerator) formatParameterName(name string) string {
	result := toLowerCamel(name)

	// a parameter name must not collide with a Go keyword
	if token.IsKeyword(result) {
		return result + "Param"
	}

	return result
}

// buildVariablesMapFromParams builds the variables map from individual parameters.
func (og *operationsGenerator) buildVariablesMapFromParams(op *ast.OperationDefinition) jen.Code {
	if len(op.VariableDefinitions) > 0 {
		// build map from individual parameters
		return jen.Id("vars").Op(":=").Map(jen.String()).Any().Values(
			jen.DictFunc(func(d jen.Dict) {
				for _, varDef := range op.VariableDefinitions {
					paramName := og.formatParameterName(varDef.Variable)
					d[jen.Lit(varDef.Variable)] = jen.Id(paramName)
				}
			}),
		).Line()
	}

	// no variables, create empty map
	return jen.Id("vars").Op(":=").Map(jen.String()).Any().Values().Line()
}
