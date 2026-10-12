// Package raw is the append-only raw log of provider responses (invariant 2).
//
// Layout: one file per source per UTC hour of the minute fetched, named like an S3 key
// (its/v1/2026/10/12/13.raw.gz). A file is a series of gzip members, one per HTTP attempt. Each
// member holds one JSON header line, a newline, and exactly Header.BodyLen bytes of the response
// body as received. The body is never put inside JSON, so its bytes stay exact.
//
// Files are only ever appended to. The one exception is Log.Scan with repair, which cuts off a
// last member that a crash left incomplete (it holds no data that was ever acknowledged).
package raw

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

var (
	// ErrTruncated means the last gzip member of a file is incomplete (a crash mid-write). The
	// envelopes before it are still returned.
	ErrTruncated = errors.New("raw: truncated last member")
	// ErrCorrupt means a member is damaged somewhere other than the tail. Nothing is repaired.
	ErrCorrupt = errors.New("raw: corrupt member")
)

// Header is the JSON line that starts every member.
type Header struct {
	V           int       `json:"v"`
	Source      string    `json:"source"`
	URL         string    `json:"url"`
	MinuteUTC   string    `json:"minute_utc"`
	Page        int       `json:"page"`
	Attempt     int       `json:"attempt"`
	Cursor      *string   `json:"cursor"`
	FetchedAt   time.Time `json:"fetched_at"`
	Status      int       `json:"status"`
	Error       string    `json:"error"`
	DurationMS  int64     `json:"duration_ms"`
	ContentType string    `json:"content_type"`
	BodyLen     int       `json:"body_len"`
	NextCursor  *string   `json:"next_cursor"`
}

// Envelope is one stored HTTP attempt: the header and the body as received.
type Envelope struct {
	Header
	Body []byte
}

// Loc says where a member sits inside its file, so it can be read back on its own.
type Loc struct {
	Key    string
	Offset int64
	Len    int64
}

// HourKey is the object key of the file that holds the minutes of the hour containing t.
func HourKey(source string, t time.Time) string {
	return source + "/v1/" + t.UTC().Format("2006/01/02/15") + ".raw.gz"
}

// Log is the raw log of one source under a root directory. It is safe for concurrent use.
type Log struct {
	dir    string
	source string
	mu     sync.Mutex
}

// Open returns the log for a source. It creates nothing until the first Append.
func Open(dir, source string) *Log { return &Log{dir: dir, source: source} }

// Dir is the root directory.
func (l *Log) Dir() string { return l.dir }

// Path is the file for a key.
func (l *Log) Path(key string) string { return filepath.Join(l.dir, filepath.FromSlash(key)) }

// Append stores one attempt: it sets BodyLen, writes a gzip member, fsyncs, and returns where the
// member landed. The file is chosen by the hour of h.MinuteUTC, not of the fetch time.
func (l *Log) Append(h Header, body []byte) (Loc, error) {
	minute, err := time.Parse(time.RFC3339, h.MinuteUTC)
	if err != nil {
		return Loc{}, fmt.Errorf("raw: append: minute_utc %q: %w", h.MinuteUTC, err)
	}
	h.V, h.Source, h.BodyLen = 1, l.source, len(body)
	h.FetchedAt = h.FetchedAt.UTC()
	head, err := json.Marshal(h)
	if err != nil {
		return Loc{}, fmt.Errorf("raw: append: header: %w", err)
	}

	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	for _, part := range [][]byte{head, []byte("\n"), body} {
		if _, err := zw.Write(part); err != nil {
			return Loc{}, fmt.Errorf("raw: append: compress: %w", err)
		}
	}
	if err := zw.Close(); err != nil {
		return Loc{}, fmt.Errorf("raw: append: compress: %w", err)
	}

	key := HourKey(l.source, minute)
	path := l.Path(key)
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Loc{}, fmt.Errorf("raw: append: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return Loc{}, fmt.Errorf("raw: append: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return Loc{}, fmt.Errorf("raw: append: %w", err)
	}
	loc := Loc{Key: key, Offset: info.Size(), Len: int64(buf.Len())}
	if _, err := f.Write(buf.Bytes()); err != nil {
		_ = f.Close()
		return Loc{}, fmt.Errorf("raw: append: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return Loc{}, fmt.Errorf("raw: append: fsync: %w", err)
	}
	if err := f.Close(); err != nil {
		return Loc{}, fmt.Errorf("raw: append: %w", err)
	}
	return loc, nil
}

// Read returns the envelope stored at loc, body included.
func (l *Log) Read(loc Loc) (Envelope, error) {
	f, err := os.Open(l.Path(loc.Key))
	if err != nil {
		return Envelope{}, fmt.Errorf("raw: read: %w", err)
	}
	defer func() { _ = f.Close() }()
	cr := &countReader{r: bufio.NewReader(io.NewSectionReader(f, loc.Offset, loc.Len))}
	env, err := readMember(cr, true)
	if err != nil {
		return Envelope{}, fmt.Errorf("raw: read %s@%d: %w", loc.Key, loc.Offset, err)
	}
	return env, nil
}

// countReader counts the bytes consumed. It implements io.ByteReader, so compress/flate reads
// exactly what a member needs and no more, which keeps member offsets exact.
type countReader struct {
	r *bufio.Reader
	n int64
}

func (c *countReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func (c *countReader) ReadByte() (byte, error) {
	b, err := c.r.ReadByte()
	if err == nil {
		c.n++
	}
	return b, err
}

// readMember reads the next gzip member from cr. io.EOF means a clean end of the file, before
// the first byte of a member.
func readMember(cr *countReader, keepBody bool) (Envelope, error) {
	zr, err := gzip.NewReader(cr)
	if err != nil {
		return Envelope{}, memberErr(err, true)
	}
	defer func() { _ = zr.Close() }()
	zr.Multistream(false)

	br := bufio.NewReader(zr)
	line, err := br.ReadBytes('\n')
	if err != nil {
		return Envelope{}, memberErr(err, false)
	}
	var env Envelope
	if err := json.Unmarshal(line, &env.Header); err != nil {
		return Envelope{}, fmt.Errorf("%w: header: %v", ErrCorrupt, err)
	}
	if env.BodyLen < 0 {
		return Envelope{}, fmt.Errorf("%w: body_len %d", ErrCorrupt, env.BodyLen)
	}

	// Read one byte more than body_len: that tells "too long" apart from "exactly right", and
	// reading to the end of the member forces the gzip checksum to be verified.
	lr := io.LimitReader(br, int64(env.BodyLen)+1)
	var n int64
	if keepBody {
		var buf bytes.Buffer
		buf.Grow(env.BodyLen + 1)
		n, err = io.Copy(&buf, lr)
		env.Body = buf.Bytes()
	} else {
		n, err = io.Copy(io.Discard, lr)
	}
	if err != nil {
		return Envelope{}, memberErr(err, false)
	}
	if n != int64(env.BodyLen) {
		return Envelope{}, fmt.Errorf("%w: body is %d bytes, header says %d", ErrCorrupt, n, env.BodyLen)
	}
	if keepBody {
		env.Body = env.Body[:env.BodyLen:env.BodyLen]
	}
	return env, nil
}

// memberErr maps a decoding error to ErrTruncated (the stream ended early) or ErrCorrupt. A plain
// io.EOF is only the clean end of the file when atBoundary is true (nothing of a member was read).
func memberErr(err error, atBoundary bool) error {
	switch {
	case err == io.EOF && atBoundary:
		return io.EOF
	case errors.Is(err, io.ErrUnexpectedEOF):
		return ErrTruncated
	default:
		return fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
}

// Walk streams every envelope of a file to fn, in file order. With keepBody false the bodies are
// decompressed and checked but not kept, so a whole hour never sits in memory. It returns the
// length of the valid prefix. A truncated last member gives an error satisfying
// errors.Is(err, ErrTruncated) after the earlier envelopes were delivered; damage elsewhere gives
// ErrCorrupt.
func Walk(path string, keepBody bool, fn func(Envelope, Loc)) (validLen int64, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("raw: walk: %w", err)
	}
	defer func() { _ = f.Close() }()
	cr := &countReader{r: bufio.NewReader(f)}
	for {
		start := cr.n
		env, err := readMember(cr, keepBody)
		switch {
		case errors.Is(err, io.EOF):
			return cr.n, nil
		case err != nil:
			return start, fmt.Errorf("%w (member at offset %d of %s)", err, start, path)
		}
		fn(env, Loc{Offset: start, Len: cr.n - start})
	}
}

// ReadHour returns every complete envelope of one hour file. If the last member is truncated it
// returns the earlier ones together with an error satisfying errors.Is(err, ErrTruncated); that
// is a report, not a failure, and callers should keep the envelopes.
func ReadHour(path string) ([]Envelope, error) {
	var out []Envelope
	_, err := Walk(path, true, func(e Envelope, _ Loc) { out = append(out, e) })
	return out, err
}

// Entry is an index row: an attempt's header and where to find it.
type Entry struct {
	Header Header
	Loc    Loc
}

// Index holds the headers of the raw log, grouped by minute. Bodies are not kept.
type Index struct {
	minutes map[string][]Entry
}

// NewIndex returns an empty index.
func NewIndex() *Index { return &Index{minutes: map[string][]Entry{}} }

// Add records one attempt.
func (ix *Index) Add(e Entry) {
	ix.minutes[e.Header.MinuteUTC] = append(ix.minutes[e.Header.MinuteUTC], e)
}

// Delete forgets a minute, so a long-running process does not keep the headers of minutes it no
// longer needs. The files are untouched.
func (ix *Index) Delete(minute string) { delete(ix.minutes, minute) }

// Minutes lists every minute that has at least one attempt, oldest first.
func (ix *Index) Minutes() []string {
	out := make([]string, 0, len(ix.minutes))
	for m := range ix.minutes {
		out = append(out, m)
	}
	slices.Sort(out)
	return out
}

// Attempts returns every attempt recorded for a minute, in the order they were stored.
func (ix *Index) Attempts(minute string) []Entry { return ix.minutes[minute] }

// Chain returns the pages of a minute in order, and whether they are complete: a chain from page
// 0 (no cursor) to a page whose next_cursor is null, every page a 200 response, and each page
// requested with the cursor the previous page returned. If a page was fetched more than once, the
// first 200 is used. When the chain is incomplete, the pages found so far are returned.
func (ix *Index) Chain(minute string) (pages []Entry, complete bool) {
	attempts := ix.minutes[minute]
	var cursor *string
	for page := 0; ; page++ {
		i := slices.IndexFunc(attempts, func(e Entry) bool {
			h := e.Header
			return h.Status == 200 && h.Error == "" && h.Page == page && ptrEq(h.Cursor, cursor)
		})
		if i < 0 {
			return pages, false
		}
		pages = append(pages, attempts[i])
		cursor = attempts[i].Header.NextCursor
		if cursor == nil {
			return pages, true
		}
	}
}

func ptrEq(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// ScanResult describes a Scan.
type ScanResult struct {
	Index    *Index
	Files    int
	Repaired []string // keys whose incomplete last member was cut off
}

// Scan reads every hour file from the hour of from through the hour of to and indexes it. It
// reads headers only. With repair, a truncated last member is cut off so that later appends stay
// readable; a corrupt file is always an error and is never touched.
func (l *Log) Scan(from, to time.Time, repair bool) (ScanResult, error) {
	res := ScanResult{Index: NewIndex()}
	for h := from.UTC().Truncate(time.Hour); !h.After(to); h = h.Add(time.Hour) {
		key := HourKey(l.source, h)
		path := l.Path(key)
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			continue
		}
		validLen, err := Walk(path, false, func(e Envelope, loc Loc) {
			loc.Key = key
			res.Index.Add(Entry{Header: e.Header, Loc: loc})
		})
		res.Files++
		switch {
		case err == nil:
		case errors.Is(err, ErrTruncated) && repair:
			if err := os.Truncate(path, validLen); err != nil {
				return res, fmt.Errorf("raw: repair %s: %w", key, err)
			}
			res.Repaired = append(res.Repaired, key)
		default:
			return res, fmt.Errorf("raw: scan %s: %w", key, err)
		}
	}
	return res, nil
}
