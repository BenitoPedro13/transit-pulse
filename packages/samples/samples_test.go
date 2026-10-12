package samples_test

import (
	"testing"

	"github.com/BenitoPedro13/transit-pulse/packages/samples"
)

func TestMustReadRealSamples(t *testing.T) {
	// Sizes on disk, 2026-10-12.
	tests := []struct {
		name string
		size int
	}{
		{"smtr-its-20261012-veiculos-conecta.json", 26048},
		{"sppo-20261012-gtfsrt-vehicle-positions.pb", 126988},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := samples.MustRead(t, tt.name)
			if len(b) != tt.size {
				t.Fatalf("len = %d bytes, want %d", len(b), tt.size)
			}
		})
	}
}

func TestReadErrors(t *testing.T) {
	for _, name := range []string{"does-not-exist.json", "../CLAUDE.md", "a/b.json", "", `a\b.json`} {
		t.Run(name, func(t *testing.T) {
			if _, err := samples.Read(name); err == nil {
				t.Fatalf("Read(%q) returned no error", name)
			}
		})
	}
}
