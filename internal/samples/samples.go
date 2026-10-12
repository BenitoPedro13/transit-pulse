// Package samples loads the real provider captures in docs/research/samples for parser tests.
// It does no decoding: parsers belong to the adapters.
package samples

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Path returns the absolute path of docs/research/samples/<name>. Samples are flat files named
// <operator>-<YYYYMMDD>-<what>.<ext>, so a name with a path separator or ".." is rejected.
func Path(name string) (string, error) {
	if name == "" || strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return "", fmt.Errorf("samples: invalid sample name %q", name)
	}
	root, err := repoRoot()
	if err != nil {
		return "", err
	}
	p := filepath.Join(root, "docs", "research", "samples", name)
	info, err := os.Stat(p)
	if err != nil {
		return "", fmt.Errorf("samples: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("samples: %q is not a regular file", name)
	}
	return p, nil
}

// Read returns the bytes of the named sample.
func Read(name string) ([]byte, error) {
	p, err := Path(name)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(p)
}

// MustRead is Read for tests. A missing sample fails the test; it never skips, because a skipped
// parser test reads as green.
func MustRead(t testing.TB, name string) []byte {
	t.Helper()
	b, err := Read(name)
	if err != nil {
		t.Fatalf("read sample %q: %v", name, err)
	}
	return b
}

// repoRoot walks up from this source file's directory to the directory holding go.mod.
func repoRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("samples: cannot locate source file")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("samples: go.mod not found above " + filepath.Dir(file))
		}
		dir = parent
	}
}
