package gitcli

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
)

var shaPattern = regexp.MustCompile(`^[0-9a-f]{7,64}$`)

// ValidateSHA accepts only lower-case hexadecimal object names of 7 to 64 characters. Anything
// else (refs, options, revision expressions) is refused before git runs.
func ValidateSHA(sha string) error {
	if !shaPattern.MatchString(sha) {
		return fmt.Errorf("gitcli: invalid sha %q", sha)
	}
	return nil
}

// ValidatePath accepts only clean, relative, forward-slash paths that cannot be read by git as an
// option or escape the repository.
func ValidatePath(p string) error {
	switch {
	case p == "":
		return errors.New("gitcli: empty path")
	case strings.ContainsRune(p, 0):
		return errors.New("gitcli: path contains a NUL byte")
	case strings.Contains(p, `\`):
		return errors.New("gitcli: path contains a backslash")
	case strings.HasPrefix(p, "-"):
		return fmt.Errorf("gitcli: path %q starts with '-'", p)
	case strings.HasPrefix(p, "/"):
		return fmt.Errorf("gitcli: path %q is absolute", p)
	case path.Clean(p) != p:
		return fmt.Errorf("gitcli: path %q is not clean", p)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return fmt.Errorf("gitcli: path %q escapes the repository", p)
		}
	}
	return nil
}
