package detector

import "regexp"

// rangeSuffix is the suffix the runtime gives a range-over-func body (Lines-range1). It is the same
// rule the code index uses to find the declared function.
var rangeSuffix = regexp.MustCompile(`-range\d+`)

// closureSuffix is the chain the runtime appends to a closure: .funcN, plus .M per nesting level.
var closureSuffix = regexp.MustCompile(`\.func\d+(\.\d+)*$`)

// packageLevel matches what is left of a package-level closure (pkg.glob., pkg.init or pkg.init.N
// when the package has several init functions), which is not a function anyone declared. It looks
// only at the end of the name, because the package path itself may contain dots (yaml.v3); a method
// named init, (*T).init, is a declared function and does not match.
var packageLevel = regexp.MustCompile(`(^|[^)])\.(glob\.|init(\.\d+)?)$`)

// declaredFunction names the function a human looks for in the code: it removes every -rangeN and
// the trailing closure chain, keeping the frame name when the closure is package-level. File and
// line still point at the body (ADR-0019).
func declaredFunction(name string) string {
	name = rangeSuffix.ReplaceAllString(name, "")
	declared := closureSuffix.ReplaceAllString(name, "")
	if packageLevel.MatchString(declared) {
		return name
	}
	return declared
}
