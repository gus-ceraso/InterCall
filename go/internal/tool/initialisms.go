package tool

import "strings"

// initialisms is the fixed initialism set from SPEC.md "Names and native
// overrides", in the exact order given there. The set is fixed: it is not
// configurable and never grows, and conversion is ASCII-only.
var initialisms = [...]string{
	"ACL", "API", "ASCII", "CPU", "CSS", "DNS", "EOF", "GUID", "HTML",
	"HTTP", "HTTPS", "ID", "IP", "JSON", "QPS", "RAM", "RPC", "SLA",
	"SMTP", "SQL", "SSH", "TCP", "TLS", "TTL", "UDP", "UI", "UID", "URI",
	"URL", "UTF8", "UUID", "VM", "XML", "XMPP", "XSRF", "XSS",
}

// initialismSet indexes the initialisms for O(1) membership checks.
var initialismSet = func() map[string]bool {
	m := make(map[string]bool, len(initialisms))
	for _, s := range initialisms {
		m[s] = true
	}
	return m
}()

// isInitialism reports whether s is one of the fixed initialisms.
func isInitialism(s string) bool { return initialismSet[s] }

// longestInitialism returns the longest fixed initialism that is a prefix
// of s, or "" when no initialism matches. Length, not table order, decides
// between overlapping initialisms such as HTTP and HTTPS or UI and UID,
// so HTTPS wins for "HTTPSClient" and UUID wins for "UUID".
func longestInitialism(s string) string {
	best := ""
	for _, init := range initialisms {
		if len(init) > len(best) && strings.HasPrefix(s, init) {
			best = init
		}
	}
	return best
}
