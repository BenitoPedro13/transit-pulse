// Package contracts holds the JSON Schema files that define the messages on the wire, plus a
// validation helper for tests. No production binary imports it.
package contracts

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Dir returns the absolute directory holding the schema files.
func Dir() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("contracts: cannot locate source file")
	}
	return filepath.Dir(file), nil
}

// Schema returns the absolute path of a schema file, e.g. "position.v1.schema.json".
func Schema(name string) (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	p := filepath.Join(dir, filepath.Base(name))
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("contracts: %w", err)
	}
	return p, nil
}

// Validate checks one JSON document against the named schema file.
func Validate(schemaName string, doc []byte) error {
	p, err := Schema(schemaName)
	if err != nil {
		return err
	}
	sch, err := jsonschema.NewCompiler().Compile(p)
	if err != nil {
		return fmt.Errorf("contracts: compile %s: %w", schemaName, err)
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(doc))
	if err != nil {
		return fmt.Errorf("contracts: decode document: %w", err)
	}
	return sch.Validate(inst)
}

// MustValidate is Validate for tests.
func MustValidate(t testing.TB, schemaName string, doc []byte) {
	t.Helper()
	if err := Validate(schemaName, doc); err != nil {
		t.Fatalf("document does not match %s: %v\n%s", schemaName, err, doc)
	}
}
