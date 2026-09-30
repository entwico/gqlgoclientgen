package schema

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// ToSDL converts an introspection schema to SDL (Schema Definition Language) format.
func ToSDL(schema *Schema) (string, error) {
	if schema == nil {
		return "", errors.New("schema is nil")
	}

	if err := validateSchema(schema); err != nil {
		return "", err
	}

	var sb strings.Builder

	// write schema definition if it has non-default types
	if needsSchemaDefinition(schema) {
		sb.WriteString("schema {\n")

		if schema.QueryType != nil && schema.QueryType.Name != nil {
			fmt.Fprintf(&sb, "  query: %s\n", *schema.QueryType.Name)
		}

		if schema.MutationType != nil && schema.MutationType.Name != nil {
			fmt.Fprintf(&sb, "  mutation: %s\n", *schema.MutationType.Name)
		}

		if schema.SubscriptionType != nil && schema.SubscriptionType.Name != nil {
			fmt.Fprintf(&sb, "  subscription: %s\n", *schema.SubscriptionType.Name)
		}

		sb.WriteString("}\n\n")
	}

	// write all types
	for _, t := range schema.Types {
		if isBuiltInType(t) {
			continue
		}

		typeSDL := printType(t)
		sb.WriteString(typeSDL)
	}

	// write custom directive definitions; without them a schema using custom
	// directives would not round-trip through introspection and SDL
	for _, d := range schema.Directives {
		if isBuiltInDirective(d.Name) {
			continue
		}

		sb.WriteString(printDirective(d))
	}

	return sb.String(), nil
}

// validateSchema rejects malformed introspection data before printing, so the
// printers never dereference a nil type.
func validateSchema(schema *Schema) error {
	for _, t := range schema.Types {
		if t.Name == nil {
			continue
		}

		name := *t.Name

		if t.Kind == TypeKindUnion && !strings.HasPrefix(name, "__") && len(t.PossibleTypes) == 0 {
			return fmt.Errorf("union %s has no member types", name)
		}

		for _, f := range t.Fields {
			if f.Type == nil {
				return fmt.Errorf("type %s: field %s has no type", name, f.Name)
			}

			for _, a := range f.Args {
				if a.Type == nil {
					return fmt.Errorf("type %s: argument %s of field %s has no type", name, a.Name, f.Name)
				}
			}
		}

		for _, f := range t.InputFields {
			if f.Type == nil {
				return fmt.Errorf("input %s: field %s has no type", name, f.Name)
			}
		}
	}

	for _, d := range schema.Directives {
		for _, a := range d.Args {
			if a.Type == nil {
				return fmt.Errorf("directive @%s: argument %s has no type", d.Name, a.Name)
			}
		}
	}

	return nil
}

// isBuiltInDirective reports whether a directive is defined by the GraphQL
// spec and therefore must not be re-declared in SDL.
func isBuiltInDirective(name string) bool {
	switch name {
	case "skip", "include", "deprecated", "specifiedBy", "oneOf", "defer":
		return true
	default:
		return false
	}
}

// printDirective generates the SDL definition for a custom directive.
func printDirective(d Directive) string {
	var sb strings.Builder

	if d.Description != nil && *d.Description != "" {
		sb.WriteString(printDescription(*d.Description, ""))
	}

	fmt.Fprintf(&sb, "directive @%s", d.Name)

	if len(d.Args) > 0 {
		sb.WriteString("(")

		for idx, a := range d.Args {
			if idx > 0 {
				sb.WriteString(", ")
			}

			fmt.Fprintf(&sb, "%s: %s", a.Name, typeReference(*a.Type))

			if a.DefaultValue != nil {
				fmt.Fprintf(&sb, " = %s", *a.DefaultValue)
			}
		}

		sb.WriteString(")")
	}

	if d.IsRepeatable {
		sb.WriteString(" repeatable")
	}

	sb.WriteString(" on ")

	for idx, loc := range d.Locations {
		if idx > 0 {
			sb.WriteString(" | ")
		}

		sb.WriteString(string(loc))
	}

	sb.WriteString("\n\n")

	return sb.String()
}

// needsSchemaDefinition checks if we need an explicit schema definition.
func needsSchemaDefinition(schema *Schema) bool {
	// only need schema definition if root types have non-default names
	hasNonDefaultQuery := schema.QueryType != nil && schema.QueryType.Name != nil && *schema.QueryType.Name != "Query"
	hasNonDefaultMutation := schema.MutationType != nil && schema.MutationType.Name != nil && *schema.MutationType.Name != "Mutation"
	hasNonDefaultSubscription := schema.SubscriptionType != nil && schema.SubscriptionType.Name != nil && *schema.SubscriptionType.Name != "Subscription"

	return hasNonDefaultQuery || hasNonDefaultMutation || hasNonDefaultSubscription
}

// isBuiltInType checks if a type is a built-in GraphQL type that shouldn't be printed.
func isBuiltInType(t Type) bool {
	if t.Name == nil {
		return true
	}

	// skip introspection types
	if strings.HasPrefix(*t.Name, "__") {
		return true
	}

	return builtInScalars[*t.Name]
}

// builtInScalars lists the scalars defined by the GraphQL spec, which must not
// be re-declared in SDL.
var builtInScalars = map[string]bool{
	"Int":     true,
	"Float":   true,
	"String":  true,
	"Boolean": true,
	"ID":      true,
}

// printType generates SDL for a single type.
func printType(t Type) string {
	var sb strings.Builder

	// write description if present
	if t.Description != nil && *t.Description != "" {
		sb.WriteString(printDescription(*t.Description, ""))
	}

	switch t.Kind {
	case TypeKindScalar:
		fmt.Fprintf(&sb, "scalar %s\n\n", *t.Name)

	case TypeKindEnum:
		fmt.Fprintf(&sb, "enum %s {\n", *t.Name)

		for _, e := range t.EnumValues {
			if e.Description != nil && *e.Description != "" {
				sb.WriteString(printDescription(*e.Description, "  "))
			}

			fmt.Fprintf(&sb, "  %s", e.Name)

			if e.IsDeprecated {
				sb.WriteString(printDeprecated(e.DeprecationReason))
			}

			sb.WriteString("\n")
		}

		sb.WriteString("}\n\n")

	case TypeKindUnion:
		fmt.Fprintf(&sb, "union %s = ", *t.Name)

		for idx, pt := range t.PossibleTypes {
			if idx > 0 {
				sb.WriteString(" | ")
			}

			sb.WriteString(typeReference(pt))
		}

		sb.WriteString("\n\n")

	case TypeKindInputObject:
		fmt.Fprintf(&sb, "input %s {\n", *t.Name)

		for _, f := range t.InputFields {
			if f.Description != nil && *f.Description != "" {
				sb.WriteString(printDescription(*f.Description, "  "))
			}

			fmt.Fprintf(&sb, "  %s: %s", f.Name, typeReference(*f.Type))

			if f.DefaultValue != nil {
				fmt.Fprintf(&sb, " = %s", *f.DefaultValue)
			}

			sb.WriteString("\n")
		}

		sb.WriteString("}\n\n")

	case TypeKindObject, TypeKindInterface:
		// write type or interface keyword
		typeKeyword := "type"
		if t.Kind == TypeKindInterface {
			typeKeyword = "interface"
		}

		fmt.Fprintf(&sb, "%s %s", typeKeyword, *t.Name)

		// write implements clause if present
		if len(t.Interfaces) > 0 {
			sb.WriteString(" implements ")

			for idx, i := range t.Interfaces {
				if idx > 0 {
					sb.WriteString(" & ")
				}

				sb.WriteString(typeReference(i))
			}
		}

		sb.WriteString(" {\n")

		// write fields
		for _, f := range t.Fields {
			if f.Description != nil && *f.Description != "" {
				sb.WriteString(printDescription(*f.Description, "  "))
			}

			fmt.Fprintf(&sb, "  %s", f.Name)

			// write arguments if present
			if len(f.Args) > 0 {
				sb.WriteString("(")

				for idx, a := range f.Args {
					if idx > 0 {
						sb.WriteString(", ")
					}

					fmt.Fprintf(&sb, "%s: %s", a.Name, typeReference(*a.Type))

					if a.DefaultValue != nil {
						fmt.Fprintf(&sb, " = %s", *a.DefaultValue)
					}
				}

				sb.WriteString(")")
			}

			fmt.Fprintf(&sb, ": %s", typeReference(*f.Type))

			if f.IsDeprecated {
				sb.WriteString(printDeprecated(f.DeprecationReason))
			}

			sb.WriteString("\n")
		}

		sb.WriteString("}\n\n")

	default:
		// skip unknown type kinds
	}

	return sb.String()
}

// typeReference generates a type reference string with modifiers.
func typeReference(t Type) string {
	var modifiers []TypeKind

	// traverse the type tree to collect modifiers
	ofType := &t
	for ofType.OfType != nil {
		switch ofType.Kind {
		case TypeKindList, TypeKindNonNull:
			modifiers = append(modifiers, ofType.Kind)
		default:
			// invalid type structure
			return "Invalid"
		}

		ofType = ofType.OfType
	}

	// get the base type name
	if ofType.Name == nil {
		return "Unknown"
	}

	typeName := *ofType.Name

	// apply modifiers in reverse order
	if len(modifiers) > 0 {
		for _, modifier := range slices.Backward(modifiers) {
			switch modifier {
			case TypeKindList:
				typeName = "[" + typeName + "]"
			case TypeKindNonNull:
				typeName += "!"
			default:
				// skip unknown modifiers
			}
		}
	}

	return typeName
}

// printDescription formats a description as SDL comment.
func printDescription(desc string, indent string) string {
	// escape special characters
	desc = strings.ReplaceAll(desc, `\`, `\\`)

	// for single-line descriptions, use simple quotes
	if !strings.Contains(desc, "\n") {
		desc = strings.ReplaceAll(desc, `"`, `\"`)
		return fmt.Sprintf("%s\"%s\"\n", indent, desc)
	}

	// for multi-line descriptions, use triple quotes
	desc = strings.ReplaceAll(desc, `"""`, `\"""`)

	var sb strings.Builder
	fmt.Fprintf(&sb, "%s\"\"\"\n", indent)

	for line := range strings.SplitSeq(desc, "\n") {
		fmt.Fprintf(&sb, "%s%s\n", indent, line)
	}

	fmt.Fprintf(&sb, "%s\"\"\"\n", indent)

	return sb.String()
}

// printDeprecated formats a deprecation directive.
func printDeprecated(reason *string) string {
	if reason != nil && *reason != "" {
		// escape quotes in reason
		escapedReason := strings.ReplaceAll(*reason, `\`, `\\`)
		escapedReason = strings.ReplaceAll(escapedReason, `"`, `\"`)

		return fmt.Sprintf(" @deprecated(reason: \"%s\")", escapedReason)
	}

	return " @deprecated"
}
