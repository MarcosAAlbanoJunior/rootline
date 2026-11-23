package detector

import (
	"regexp"
)

// rangeSuffix is the suffix the runtime gives a range-over-func body (Lines-range1). It is the same
// rule the code index uses to find the declared function.
var rangeSuffix = regexp.MustCompile(`-range\d+`)

// declaredFunction removes every -rangeN suffix, so the location names the function a human looks
// for in the code; file and line still point at the body (ADR-0016).
func declaredFunction(name string) string {
	return rangeSuffix.ReplaceAllString(name, "")
}
