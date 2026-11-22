// Package detector finds performance anti-patterns in recorded traces. It depends only on the
// standard library and internal/trace.
package detector

import "strings"

// Normalize collapses whitespace runs into one space, trims the ends and drops a trailing ";".
func Normalize(sql string) string {
	s := strings.Join(strings.Fields(sql), " ")
	s = strings.TrimSuffix(s, ";")
	return strings.TrimSpace(s)
}
