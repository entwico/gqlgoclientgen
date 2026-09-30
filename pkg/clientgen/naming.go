package clientgen

import "strings"

// goInitialisms maps lowercase word segments to their idiomatic Go spelling.
var goInitialisms = map[string]string{
	"api":  "API",
	"html": "HTML",
	"http": "HTTP",
	"id":   "ID",
	"json": "JSON",
	"sql":  "SQL",
	"uri":  "URI",
	"url":  "URL",
	"uuid": "UUID",
}

// toPascalCase converts camelCase, snake_case, SCREAMING_SNAKE_CASE or mixed
// input to PascalCase. Whole word segments are matched against known
// initialisms (id → ID) so words that merely contain one (Identifier, Paid)
// are never mangled. An uppercase run inside a mixed-case name is treated as
// an acronym and preserved (streamHLS → StreamHLS); only in an all-uppercase
// identifier like an enum value is it title-cased (FOO_BAR → FooBar).
func toPascalCase(name string) string {
	var b strings.Builder

	screaming := isAllUpper(name)

	for _, word := range splitWords(name) {
		lower := strings.ToLower(word)

		if initialism, ok := goInitialisms[lower]; ok {
			b.WriteString(initialism)
			continue
		}

		if isAllUpper(word) {
			if screaming {
				// SCREAMING_SNAKE_CASE segment: FOO → Foo
				b.WriteString(strings.ToUpper(lower[:1]))
				b.WriteString(lower[1:])
			} else {
				// acronym inside a mixed-case name: HLS stays HLS
				b.WriteString(word)
			}

			continue
		}

		b.WriteString(strings.ToUpper(word[:1]))
		b.WriteString(word[1:])
	}

	return b.String()
}

// fieldNameToGoName converts a GraphQL field name to an exported Go field
// name, preserving leading underscores by moving them to the end so the name
// stays exported (__typename → Typename__).
func fieldNameToGoName(name string) string {
	if name == "" {
		return ""
	}

	trimmed := strings.TrimLeft(name, "_")
	leadingUnderscores := len(name) - len(trimmed)

	return toPascalCase(trimmed) + strings.Repeat("_", leadingUnderscores)
}

// toLowerCamel converts an identifier to lowerCamelCase for parameter names.
// A leading initialism is lowercased as a whole (ID → id, not iD).
func toLowerCamel(name string) string {
	words := splitWords(name)
	if len(words) == 0 {
		return ""
	}

	var b strings.Builder

	b.WriteString(strings.ToLower(words[0]))

	for _, word := range words[1:] {
		lower := strings.ToLower(word)

		if initialism, ok := goInitialisms[lower]; ok {
			b.WriteString(initialism)
			continue
		}

		b.WriteString(strings.ToUpper(word[:1]))
		b.WriteString(strings.ToLower(word[1:]))
	}

	return b.String()
}

// enumValueToConstName converts an enum value to a Go constant name, prefixed
// with the enum type name to avoid collisions between enums.
func enumValueToConstName(typeName, value string) string {
	return typeName + toPascalCase(value)
}

// splitWords splits an identifier into word segments on underscores and
// camelCase boundaries, keeping ALLCAPS runs together (HTTPServer → HTTP, Server).
func splitWords(name string) []string {
	var (
		words []string
		cur   []rune
	)

	runes := []rune(name)

	flush := func() {
		if len(cur) > 0 {
			words = append(words, string(cur))
			cur = nil
		}
	}

	for i, r := range runes {
		switch {
		case r == '_':
			flush()
		case i > 0 && isUpperRune(r) && !isUpperRune(runes[i-1]):
			// lowercase or digit followed by uppercase starts a new word
			flush()

			cur = append(cur, r)
		case i > 0 && isUpperRune(r) && isUpperRune(runes[i-1]) && i+1 < len(runes) && isLowerRune(runes[i+1]):
			// last letter of an ALLCAPS run followed by lowercase starts a new word
			flush()

			cur = append(cur, r)
		default:
			cur = append(cur, r)
		}
	}

	flush()

	return words
}

func isAllUpper(s string) bool {
	hasUpper := false

	for _, r := range s {
		if isLowerRune(r) {
			return false
		}

		if isUpperRune(r) {
			hasUpper = true
		}
	}

	return hasUpper
}

func isUpperRune(r rune) bool {
	return r >= 'A' && r <= 'Z'
}

func isLowerRune(r rune) bool {
	return r >= 'a' && r <= 'z'
}
