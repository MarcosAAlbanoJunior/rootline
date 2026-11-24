// Package explain holds the rules shared by the execution-plan adapters (ADR-0012, ADR-0025). It
// depends only on the standard library.
package explain

import "strings"

// Unexplainable says why sql must not be explained, or "" when it may: a single statement that
// starts with SELECT or WITH. It is a heuristic, not a SQL parser (ADR-0006): a WITH holding a
// DELETE passes it. The recorded adapter only turns a refusal into a reason; the live adapter's
// guarantee is its READ ONLY transaction, and this is just its first barrier.
func Unexplainable(sql string) string {
	s := strings.TrimSpace(sql)
	s = strings.TrimSpace(strings.TrimSuffix(s, ";"))
	if s == "" {
		return "empty statement"
	}
	if strings.Contains(s, ";") {
		return "multiple statements"
	}
	first := strings.ToUpper(strings.Fields(s)[0])
	if first != "SELECT" && first != "WITH" {
		return "not a SELECT: " + first
	}
	return ""
}
