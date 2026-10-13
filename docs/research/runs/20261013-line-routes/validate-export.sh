#!/bin/sh
# Validate every generated payload against the repo's JSON Schemas, without adding dependencies.
set -eu
cd "$(dirname "$0")/../../../.."
task_gtfs_validation_dir=$(mktemp -d)
trap 'rm -rf "$task_gtfs_validation_dir"' EXIT
cat > "$task_gtfs_validation_dir/validate.go" <<'GO'
package main

import (
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    "strings"

    "github.com/BenitoPedro13/transit-pulse/packages/contracts"
    "github.com/BenitoPedro13/transit-pulse/packages/gtfs"
)

func main() {
    dir := "apps/web/public/gtfs"
    b, err := os.ReadFile(filepath.Join(dir, "index.json")); must(err)
    must(contracts.Validate("line-routes-index.v1.schema.json", b))
    var index gtfs.Manifest; must(json.Unmarshal(b, &index))
    for _, entry := range index.Lines {
        b, err := os.ReadFile(filepath.Join(dir, strings.TrimPrefix(entry.File, "/gtfs/"))); must(err)
        must(contracts.Validate("line-routes.v1.schema.json", b))
    }
    fmt.Printf("Validated manifest and %d line files\n", len(index.Lines))
}
func must(err error) { if err != nil { panic(err) } }
GO
go run "$task_gtfs_validation_dir/validate.go"
