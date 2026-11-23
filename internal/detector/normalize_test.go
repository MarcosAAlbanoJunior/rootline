package detector_test

import (
	"testing"

	"github.com/MarcosAAlbanoJunior/rootline/internal/detector"
)

func TestNormalize(t *testing.T) {
	tests := []struct{ in, want string }{
		{"SELECT 1", "SELECT 1"},
		{"  SELECT   *\n\tFROM  items\n WHERE order_id = $1  ", "SELECT * FROM items WHERE order_id = $1"},
		{"SELECT 1;", "SELECT 1"},
		{"SELECT 1 ;  ", "SELECT 1"},
		{"", ""},
	}
	for _, tc := range tests {
		if got := detector.Normalize(tc.in); got != tc.want {
			t.Errorf("Normalize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
