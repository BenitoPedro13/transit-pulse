// gtfs captures SMTR's static feed and publishes a rebuildable web projection.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/BenitoPedro13/transit-pulse/packages/gtfs"
	"github.com/BenitoPedro13/transit-pulse/packages/raw"
)

type config struct{ rawDir, statePath, outputDir, url, contact string }
type state struct {
	LastAttempt time.Time `json:"last_attempt"`
	ETag        string    `json:"etag"`
	Modified    string    `json:"modified"`
	Raw         *raw.Loc  `json:"raw"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) < 2 || (os.Args[1] != "fetch" && os.Args[1] != "export") {
		return errors.New("usage: gtfs fetch|export [-state var/gtfs/state.json] [-out apps/web/public/gtfs]")
	}
	flags := flag.NewFlagSet("gtfs", flag.ContinueOnError)
	c := config{rawDir: os.Getenv("RAW_DIR"), url: gtfs.SourceURL, contact: os.Getenv("GTFS_CONTACT")}
	if c.rawDir == "" {
		c.rawDir = "var/raw"
	}
	if c.contact == "" {
		c.contact = os.Getenv("ITS_CONTACT")
	}
	flags.StringVar(&c.statePath, "state", "var/gtfs/state.json", "saved fetch state and raw location")
	flags.StringVar(&c.outputDir, "out", "apps/web/public/gtfs", "static output directory")
	if err := flags.Parse(os.Args[2:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional argument")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return syncFeed(ctx, &http.Client{Timeout: 2 * time.Minute}, c, time.Now().UTC(), os.Args[1] == "fetch")
}

func loadState(path string) (state, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return state{}, nil
	}
	if err != nil {
		return state{}, err
	}
	var s state
	err = json.Unmarshal(b, &s)
	return s, err
}
func saveState(path string, s state) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".state-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer func() { _ = os.Remove(name) }()
	if err := f.Chmod(0600); err != nil {
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
	return os.Rename(name, path)
}

func syncFeed(ctx context.Context, client *http.Client, c config, now time.Time, fetch bool) error {
	// Mac development and Linux production both support an advisory process lock.
	// Keep the lock file's inode: closing releases the lock even if the command crashes.
	if err := os.MkdirAll(filepath.Dir(c.statePath), 0755); err != nil {
		return err
	}
	lock, err := os.OpenFile(c.statePath+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fmt.Errorf("gtfs: another export holds the state lock: %w", err)
	}
	s, err := loadState(c.statePath)
	if err != nil {
		return err
	}
	log := raw.Open(c.rawDir, "gtfs")
	if fetch && (s.LastAttempt.IsZero() || now.Sub(s.LastAttempt) >= 24*time.Hour) {
		s.LastAttempt = now
		if err := saveState(c.statePath, s); err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
		if err != nil {
			return err
		}
		ua := "transit-pulse/0.1.0"
		if c.contact != "" {
			ua += " (+" + c.contact + ")"
		}
		req.Header.Set("User-Agent", ua)
		if s.Raw != nil {
			if s.ETag != "" {
				req.Header.Set("If-None-Match", s.ETag)
			}
			if s.Modified != "" {
				req.Header.Set("If-Modified-Since", s.Modified)
			}
		}
		start := time.Now()
		res, requestErr := client.Do(req)
		h := raw.Header{URL: c.url, MinuteUTC: now.Truncate(time.Minute).Format(time.RFC3339), FetchedAt: now, Attempt: 1}
		if requestErr != nil {
			h.Error = requestErr.Error()
			h.DurationMS = time.Since(start).Milliseconds()
			if _, err := log.Append(h, nil); err != nil {
				return errors.Join(requestErr, err)
			}
			return requestErr
		}
		body, readErr := io.ReadAll(io.LimitReader(res.Body, gtfs.MaxZIPBytes+1))
		closeErr := res.Body.Close()
		h.Status = res.StatusCode
		h.ContentType = res.Header.Get("Content-Type")
		h.DurationMS = time.Since(start).Milliseconds()
		if len(body) > gtfs.MaxZIPBytes {
			readErr = errors.New("gtfs response exceeds 64 MiB; stored body is truncated")
		}
		if readErr != nil {
			h.Error = readErr.Error()
		}
		loc, err := log.Append(h, body)
		if err != nil {
			return err
		}
		if err := errors.Join(readErr, closeErr); err != nil {
			return err
		}
		switch res.StatusCode {
		case http.StatusOK:
			p, err := gtfs.Parse(body, gtfs.Feed{FetchedAt: now.Format(time.RFC3339Nano), ModifiedAt: res.Header.Get("Last-Modified")})
			if err != nil {
				return err
			}
			s.Raw = &loc
			s.ETag = res.Header.Get("ETag")
			s.Modified = res.Header.Get("Last-Modified")
			if err := saveState(c.statePath, s); err != nil {
				return err
			}
			return publish(c.outputDir, p)
		case http.StatusNotModified:
			if s.Raw == nil {
				return errors.New("gtfs: 304 without a retained response")
			}
		default:
			return fmt.Errorf("gtfs: GET answered %d; previous export retained", res.StatusCode)
		}
	}
	if s.Raw == nil {
		return errors.New("gtfs: no retained feed; run make gtfs (fetch attempts are limited to once per 24 h)")
	}
	e, err := log.Read(*s.Raw)
	if err != nil {
		return err
	}
	p, err := gtfs.Parse(e.Body, gtfs.Feed{FetchedAt: e.FetchedAt.Format(time.RFC3339Nano), ModifiedAt: s.Modified})
	if err != nil {
		return err
	}
	return publish(c.outputDir, p)
}
func publish(dir string, p *gtfs.Projection) error {
	if err := gtfs.Publish(dir, p); err != nil {
		return err
	}
	patterns := 0
	for _, l := range p.Lines {
		patterns += len(l.Patterns)
	}
	fmt.Printf("GTFS %s: %d lines, %d patterns, %d routes without a short name; exported to %s\n", p.Manifest.Feed.SHA256, len(p.Lines), patterns, p.ExcludedRoutes, dir)
	return nil
}
