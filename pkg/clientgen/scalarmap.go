package clientgen

import (
	"strings"

	"github.com/dave/jennifer/jen"
)

// goTypeFromString renders a Go type reference from its string form as used
// in scalar mappings: a builtin ("int64"), a standard library type
// ("time.Time") or a fully qualified type ("github.com/shopspring/decimal.Decimal").
func goTypeFromString(s string) jen.Code {
	slash := strings.LastIndex(s, "/")

	dot := strings.LastIndex(s, ".")
	if dot > slash {
		return jen.Qual(s[:dot], s[dot+1:])
	}

	return jen.Id(s)
}

// jsonSchemaTypeForGoType maps a Go type from a scalar mapping to the JSON
// Schema primitive used for that scalar in the generated input schemas.
func jsonSchemaTypeForGoType(goType string) string {
	base := goType[strings.LastIndex(goType, ".")+1:]

	switch {
	case strings.HasPrefix(base, "int") || strings.HasPrefix(base, "uint"):
		return "integer"
	case strings.HasPrefix(base, "float"):
		return "number"
	case base == "bool":
		return "boolean"
	default:
		return "string"
	}
}
