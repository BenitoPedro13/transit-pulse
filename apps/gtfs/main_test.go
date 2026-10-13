package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/BenitoPedro13/transit-pulse/packages/raw"
	"github.com/BenitoPedro13/transit-pulse/packages/samples"
)

func TestCaptureRebuildAndFailures(t *testing.T) {
	body := samples.MustRead(t, "smtr-20261012-gtfs-trimmed.zip")
	requests := 0
	status := http.StatusOK
	invalid := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("User-Agent") != "transit-pulse/0.1.0 (+test-contact)" {
			t.Error("User-Agent")
		}
		if requests > 1 && r.Header.Get("If-None-Match") != "\"fixture\"" {
			t.Error("conditional ETag missing")
		}
		w.Header().Set("ETag", "\"fixture\"")
		w.Header().Set("Last-Modified", "Wed, 10 Jun 2026 17:51:42 GMT")
		w.WriteHeader(status)
		if status == http.StatusOK {
			if invalid {
				_, _ = w.Write([]byte("invalid zip"))
			} else {
				_, _ = w.Write(body)
			}
		}
	}))
	defer srv.Close()
	dir := t.TempDir()
	c := config{rawDir: filepath.Join(dir, "raw"), statePath: filepath.Join(dir, "state.json"), outputDir: filepath.Join(dir, "public"), url: srv.URL, contact: "test-contact"}
	now := time.Date(2026, 10, 13, 15, 0, 0, 0, time.UTC)
	if err := syncFeed(context.Background(), srv.Client(), c, now, true); err != nil {
		t.Fatal(err)
	}
	s, err := loadState(c.statePath)
	if err != nil || s.Raw == nil {
		t.Fatalf("state %v", err)
	}
	e, err := raw.Open(c.rawDir, "gtfs").Read(*s.Raw)
	if err != nil || !bytes.Equal(e.Body, body) {
		t.Fatalf("raw not byte exact: %v", err)
	}
	first, err := os.ReadFile(filepath.Join(c.outputDir, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := syncFeed(context.Background(), srv.Client(), c, now.Add(time.Hour), true); err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatal("requested within 24h")
	}
	if err := syncFeed(context.Background(), srv.Client(), c, now.Add(time.Hour), false); err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatal("offline export requested network")
	}
	status = http.StatusNotModified
	if err := syncFeed(context.Background(), srv.Client(), c, now.Add(24*time.Hour), true); err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatal("no conditional refresh")
	}
	next, _ := os.ReadFile(filepath.Join(c.outputDir, "index.json"))
	if !bytes.Equal(first, next) {
		t.Fatal("304 changed export")
	}
	status = http.StatusServiceUnavailable
	if err := syncFeed(context.Background(), srv.Client(), c, now.Add(48*time.Hour), true); err == nil {
		t.Fatal("accepted 503")
	}
	status = http.StatusOK
	invalid = true
	if err := syncFeed(context.Background(), srv.Client(), c, now.Add(72*time.Hour), true); err == nil {
		t.Fatal("accepted invalid ZIP")
	}
	next, _ = os.ReadFile(filepath.Join(c.outputDir, "index.json"))
	if !bytes.Equal(first, next) {
		t.Fatal("failure changed export")
	}
	if err := syncFeed(context.Background(), srv.Client(), c, now.Add(73*time.Hour), false); err != nil {
		t.Fatal(err)
	}
	next, _ = os.ReadFile(filepath.Join(c.outputDir, "index.json"))
	if !bytes.Equal(first, next) {
		t.Fatal("failed capture replaced retained good feed")
	}
}
