// Package analysis runs the detectors-only analysis: it selects the traces of the symptom, runs
// the detectors, correlates candidates with commits and builds the Result. It imports only the
// standard library and internal domain packages; I/O goes through the ports in ports.go.
package analysis

import "github.com/MarcosAAlbanoJunior/rootline/internal/trace"

// Symptom identifies what is analyzed: an operation of a service in a bad version, optionally
// compared with a baseline version.
type Symptom struct {
	Service         string
	Operation       trace.Operation
	BadVersion      string
	BaselineVersion string // "" = no reference
}
