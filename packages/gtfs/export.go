package gtfs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Publish writes a feed directory before atomically replacing its manifest. Existing exports
// remain usable if a fetch, parse or write fails; older feed directories are retained.
func Publish(dir string, p *Projection) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(dir, ".staging-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(stage) }()
	for i, line := range p.Lines {
		b, err := json.Marshal(line)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(stage, filepath.Base(p.Manifest.Lines[i].File)), b, 0644); err != nil {
			return err
		}
	}
	feedDir := filepath.Join(dir, p.Manifest.Feed.SHA256)
	if _, err := os.Stat(feedDir); os.IsNotExist(err) {
		if err := os.Rename(stage, feedDir); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else {
		// A content-addressed directory must agree byte-for-byte, including on a second export.
		for _, entry := range p.Manifest.Lines {
			name := filepath.Base(entry.File)
			expected, err := os.ReadFile(filepath.Join(stage, name))
			if err != nil {
				return err
			}
			existing, err := os.ReadFile(filepath.Join(feedDir, name))
			if err != nil {
				return err
			}
			if !bytes.Equal(expected, existing) {
				return fmt.Errorf("gtfs: existing feed differs: %s", name)
			}
		}
	}
	b, err := json.Marshal(p.Manifest)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".index-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer func() { _ = os.Remove(name) }()
	if err := f.Chmod(0644); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(name, filepath.Join(dir, "index.json"))
}
