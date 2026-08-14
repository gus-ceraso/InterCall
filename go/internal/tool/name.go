package tool

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ASCII byte predicates. All conversion is ASCII-only; any byte outside
// these classes makes an identifier or wire name invalid.
func isLower(b byte) bool       { return 'a' <= b && b <= 'z' }
func isUpper(b byte) bool       { return 'A' <= b && b <= 'Z' }
func isDigit(b byte) bool       { return '0' <= b && b <= '9' }
func isASCIILetter(b byte) bool { return isLower(b) || isUpper(b) }
func isWireChar(b byte) bool    { return isASCIILetter(b) || isDigit(b) || b == '_' }

// Case selects the default Go identifier projection for one generated
// identifier: PascalCase for declarations and fields, camelCase for
// procedure parameters.
type Case int

const (
	// PascalCase projects declarations and fields.
	PascalCase Case = iota
	// CamelCase projects procedure parameters.
	CamelCase
)

// String renders the case for diagnostics.
func (c Case) String() string {
	if c == CamelCase {
		return "camel"
	}
	return "Pascal"
}

// goKeywords are the 25 Go language keywords (Go spec, "Keywords").
var goKeywords = map[string]bool{
	"break": true, "case": true, "chan": true, "const": true, "continue": true,
	"default": true, "defer": true, "else": true, "fallthrough": true,
	"for": true, "func": true, "go": true, "goto": true, "if": true,
	"import": true, "interface": true, "map": true, "package": true,
	"range": true, "return": true, "select": true, "struct": true,
	"switch": true, "type": true, "var": true,
}

// IsGoKeyword reports whether name is one of the 25 Go keywords.
func IsGoKeyword(name string) bool { return goKeywords[name] }

// IsValidGoIdentifier reports whether name is a usable Go identifier
// under Go's Unicode-aware lexical rules (SPEC.md "Names and native
// overrides"): the first character is a Unicode letter or "_", the
// remaining characters are Unicode letters, Unicode digits, or "_", and
// the name is neither the blank identifier "_" nor a Go keyword.
func IsValidGoIdentifier(name string) bool {
	if name == "" || name == "_" || IsGoKeyword(name) {
		return false
	}
	for i, r := range name {
		if r == '_' {
			continue
		}
		if i == 0 {
			if !unicode.IsLetter(r) {
				return false
			}
			continue
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// IsExportedGoIdentifier reports whether name is a valid Go identifier
// whose first character is a Unicode uppercase letter, Go's exported
// visibility rule.
func IsExportedGoIdentifier(name string) bool {
	if !IsValidGoIdentifier(name) {
		return false
	}
	r, _ := utf8.DecodeRuneInString(name)
	return unicode.IsUpper(r)
}

// isASCIIIdentifier reports whether name matches the ASCII identifier
// shape [A-Za-z_][A-Za-z0-9_]*. It is the --package rule and the wire
// grammar's Go side; Unicode identifiers never match it.
func isASCIIIdentifier(name string) bool {
	if name == "" {
		return false
	}
	if !isASCIILetter(name[0]) && name[0] != '_' {
		return false
	}
	for i := 1; i < len(name); i++ {
		if !isWireChar(name[i]) {
			return false
		}
	}
	return true
}

// IsValidWireName reports whether name is a valid InterCall identifier:
// the C-like ASCII form [A-Za-z_][A-Za-z0-9_]* from README.md "Grammar".
func IsValidWireName(name string) bool {
	if name == "" {
		return false
	}
	if !isASCIILetter(name[0]) && name[0] != '_' {
		return false
	}
	for i := 1; i < len(name); i++ {
		if !isWireChar(name[i]) {
			return false
		}
	}
	return true
}

// IsCanonicalWireName reports whether name matches the canonical snake
// form [a-z][a-z0-9]*(_[a-z][a-z0-9]*)*. A valid but noncanonical wire
// name needs an import --go-name override before it can be projected.
func IsCanonicalWireName(name string) bool {
	if name == "" || !isLower(name[0]) {
		return false
	}
	i := 1
	for i < len(name) && (isLower(name[i]) || isDigit(name[i])) {
		i++
	}
	for i < len(name) {
		if name[i] != '_' {
			return false
		}
		i++
		if i == len(name) || !isLower(name[i]) {
			return false
		}
		i++
		for i < len(name) && (isLower(name[i]) || isDigit(name[i])) {
			i++
		}
	}
	return true
}

// WireToGo projects one canonical wire name to its default Go identifier
// for the given case, following SPEC.md "Names and native overrides":
// split the canonical name at underscores; a complete word whose
// uppercase spelling is a fixed initialism becomes that initialism, every
// other word has only its first letter uppercased; PascalCase
// concatenates the results and camelCase leaves the first word lowercase.
//
// WireToGo accepts only canonical wire names; a valid but noncanonical
// name is an error because it requires an import --go-name override.
func WireToGo(wire string, c Case) (string, error) {
	if !IsCanonicalWireName(wire) {
		return "", fmt.Errorf("wire name %q is not canonical (expected [a-z][a-z0-9]*(_[a-z][a-z0-9]*)*); a noncanonical wire name requires a --go-name override", wire)
	}
	words := strings.Split(wire, "_")
	var b strings.Builder
	for i, w := range words {
		if c == CamelCase && i == 0 {
			b.WriteString(w)
			continue
		}
		if up := strings.ToUpper(w); isInitialism(up) {
			b.WriteString(up)
		} else {
			b.WriteByte(w[0] - 'a' + 'A')
			b.WriteString(w[1:])
		}
	}
	return b.String(), nil
}

// GoToWire converts one Go identifier back to its canonical wire name
// with the exact checked inverse from SPEC.md "Names and native
// overrides":
//
//  1. Reject non-ASCII and underscore bytes.
//  2. Scanning left to right, split before an uppercase letter whose
//     predecessor is lowercase or a digit, and before the final uppercase
//     letter of a run when that letter is followed by lowercase.
//  3. An all-uppercase-or-digit run that is one uppercase letter with
//     optional trailing digits stays one ordinary word; any other run
//     must decompose completely into fixed initialisms, repeatedly taking
//     the longest complete match at the start and rejecting any
//     remainder.
//  4. Lowercase the words, join them with underscores, project the result
//     back to the required Pascal or camel form, and require byte-for-byte
//     equality with the source identifier.
//
// The complete identifier is converted and no affix such as "Err" or
// "Error" is ever stripped. A rejected identifier requires a declaration
// directive, @intercall param, or field tag bypass.
func GoToWire(goName string, c Case) (string, error) {
	if !IsValidGoIdentifier(goName) {
		return "", fmt.Errorf("Go identifier %q is not a usable Go identifier", goName)
	}
	if !isASCIIIdentifier(goName) {
		return "", fmt.Errorf("Go identifier %q contains a non-ASCII character, which is rejected by the ASCII-only source-to-wire projection; a declaration directive, @intercall param, or field tag can override the wire name", goName)
	}
	if strings.ContainsRune(goName, '_') {
		return "", fmt.Errorf("Go identifier %q contains an underscore, which is rejected by the source-to-wire projection", goName)
	}
	words := splitGoWords(goName)
	wireWords := make([]string, 0, len(words))
	for _, w := range words {
		more, err := goWordToWire(w)
		if err != nil {
			return "", fmt.Errorf("Go identifier %q: %v", goName, err)
		}
		wireWords = append(wireWords, more...)
	}
	wire := strings.Join(wireWords, "_")
	projected, err := WireToGo(wire, c)
	if err != nil {
		return "", fmt.Errorf("Go identifier %q does not project to a canonical wire name: %v", goName, err)
	}
	if projected != goName {
		return "", fmt.Errorf("Go identifier %q does not survive the %s round trip (it projects back as %q); it requires a declaration directive, @intercall param, or field tag", goName, c, projected)
	}
	return wire, nil
}

// splitGoWords splits a Go identifier into projection words: scanning
// left to right, split before an uppercase letter whose predecessor is
// lowercase or a digit, and before the final uppercase letter of a run
// when that letter is followed by lowercase.
func splitGoWords(s string) []string {
	var words []string
	start := 0
	for i := 1; i < len(s); i++ {
		if !isUpper(s[i]) {
			continue
		}
		prev := s[i-1]
		switch {
		case isLower(prev) || isDigit(prev):
			words = append(words, s[start:i])
			start = i
		case isUpper(prev) && i+1 < len(s) && isLower(s[i+1]):
			words = append(words, s[start:i])
			start = i
		}
	}
	return append(words, s[start:])
}

// goWordToWire projects one split word to its lowercase wire words. An
// all-uppercase-or-digit run that is one uppercase letter with optional
// trailing digits stays one ordinary word; any other such run must
// decompose completely into fixed initialisms, taking the longest match
// at every step — each match is a separate wire word — and any remainder
// is an error. Every other word is ordinary.
func goWordToWire(w string) ([]string, error) {
	allUpperOrDigit := true
	for i := 0; i < len(w); i++ {
		if !isUpper(w[i]) && !isDigit(w[i]) {
			allUpperOrDigit = false
			break
		}
	}
	if !allUpperOrDigit {
		return []string{lowerASCII(w)}, nil
	}
	if isUpper(w[0]) && allDigits(w[1:]) {
		return []string{lowerASCII(w)}, nil
	}
	var words []string
	rest := w
	for rest != "" {
		init := longestInitialism(rest)
		if init == "" {
			return nil, fmt.Errorf("uppercase run %q does not decompose into fixed initialisms (remainder %q)", w, rest)
		}
		words = append(words, lowerASCII(init))
		rest = rest[len(init):]
	}
	return words, nil
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if !isDigit(s[i]) {
			return false
		}
	}
	return true
}

// lowerASCII lowercases every uppercase ASCII byte of s.
func lowerASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if isUpper(c) {
			b[i] = c - 'A' + 'a'
		}
	}
	return string(b)
}

// ValidGoPackageName checks the --package rule from SPEC.md "Commands":
// the name must match [A-Za-z_][A-Za-z0-9_]* and cannot be "_", "main",
// or a Go keyword. The tool never sanitizes a package name.
func ValidGoPackageName(name string) error {
	if !isASCIIIdentifier(name) || name == "_" || name == "main" || IsGoKeyword(name) {
		return fmt.Errorf("invalid Go package name %q: must match [A-Za-z_][A-Za-z0-9_]*, and must not be %q, %q, or a Go keyword", name, "_", "main")
	}
	return nil
}
