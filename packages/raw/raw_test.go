package raw_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/BenitoPedro13/transit-pulse/packages/raw"
	"github.com/BenitoPedro13/transit-pulse/packages/samples"
)

var samplesAll = []string{
	"smtr-its-20261012-veiculos-conecta.json",
	"smtr-its-20261012-veiculos-maxtrack.json",
	"smtr-its-20261012-veiculos-sonda.json",
	"smtr-its-20261012-veiculos-zirix.json",
	"smtr-its-20261012-error-422.json",
}

func header(minute string, page, attempt int, status int, cursor, next *string) raw.Header {
	return raw.Header{
		URL: "https://example.invalid/v1", MinuteUTC: minute, Page: page, Attempt: attempt,
		Cursor: cursor, FetchedAt: time.Date(2026, 10, 12, 13, 41, 2, 123, time.UTC),
		Status: status, DurationMS: 1234, ContentType: "application/json", NextCursor: next,
	}
}

func sp(s string) *string { return &s }

// E3: every sample is stored and read back byte for byte, both through the hour reader and through
// the single-member reader.
func TestBodyIsByteExact(t *testing.T) {
	log := raw.Open(t.TempDir(), "its")
	var locs []raw.Loc
	var want [][]byte
	for i, name := range samplesAll {
		body := samples.MustRead(t, name)
		loc, err := log.Append(header("2026-10-12T13:38:00Z", i, 0, 200, nil, nil), body)
		if err != nil {
			t.Fatal(err)
		}
		locs, want = append(locs, loc), append(want, body)
	}

	envs, err := raw.ReadHour(log.Path(locs[0].Key))
	if err != nil {
		t.Fatal(err)
	}
	if len(envs) != len(samplesAll) {
		t.Fatalf("read %d envelopes, want %d", len(envs), len(samplesAll))
	}
	for i, env := range envs {
		if !bytes.Equal(env.Body, want[i]) {
			t.Errorf("%s: body differs after the round trip", samplesAll[i])
		}
		if env.BodyLen != len(want[i]) {
			t.Errorf("%s: body_len %d, want %d", samplesAll[i], env.BodyLen, len(want[i]))
		}
		one, err := log.Read(locs[i])
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(one.Body, want[i]) || one.Page != i {
			t.Errorf("%s: Read(loc) returned a different envelope", samplesAll[i])
		}
	}
}

// Bytes that JSON would rewrite must survive: invalid UTF-8, odd whitespace, CRLF and a body that
// itself contains the header separator.
func TestAwkwardBodiesSurvive(t *testing.T) {
	log := raw.Open(t.TempDir(), "its")
	bodies := [][]byte{
		{},
		{0xff, 0xfe, 0x00, '\n', '\r', '\n'},
		[]byte("{ \"a\" :\t1 ,\n\n\"b\":[ ] }\n\n"),
		bytes.Repeat([]byte("x\n"), 100_000),
	}
	var locs []raw.Loc
	for i, b := range bodies {
		loc, err := log.Append(header("2026-10-12T13:38:00Z", i, 0, 200, nil, nil), b)
		if err != nil {
			t.Fatal(err)
		}
		locs = append(locs, loc)
	}
	for i, b := range bodies {
		env, err := log.Read(locs[i])
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(env.Body, b) {
			t.Errorf("body %d changed", i)
		}
	}
}

func TestFileIsNamedByTheMinuteFetchedNotTheFetchTime(t *testing.T) {
	log := raw.Open(t.TempDir(), "its")
	// Minute 12:59 fetched at 13:41 lands in the 12h file.
	loc, err := log.Append(header("2026-10-12T12:59:00Z", 0, 0, 200, nil, nil), []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "its/v1/2026/10/12/12.raw.gz"; loc.Key != want {
		t.Errorf("key = %q, want %q", loc.Key, want)
	}
	if _, err := os.Stat(filepath.Join(log.Dir(), filepath.FromSlash(loc.Key))); err != nil {
		t.Error(err)
	}
}

func TestTruncatedLastMemberIsReportedAndEarlierOnesReturned(t *testing.T) {
	log := raw.Open(t.TempDir(), "its")
	var key string
	for i := range 3 {
		loc, err := log.Append(header("2026-10-12T13:38:00Z", i, 0, 200, nil, nil), samples.MustRead(t, samplesAll[i]))
		if err != nil {
			t.Fatal(err)
		}
		key = loc.Key
	}
	path := log.Path(key)
	full, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Cut the file at several points inside the last member, including inside its gzip trailer.
	var walkEnd []int64
	if _, err := raw.Walk(path, false, func(_ raw.Envelope, loc raw.Loc) { walkEnd = append(walkEnd, loc.Offset+loc.Len) }); err != nil {
		t.Fatal(err)
	}
	secondEnd := walkEnd[1]
	for _, cut := range []int64{secondEnd + 1, secondEnd + 10, (secondEnd + int64(len(full))) / 2, int64(len(full)) - 8, int64(len(full)) - 1} {
		if err := os.WriteFile(path, full[:cut], 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := raw.ReadHour(path)
		if !errors.Is(err, raw.ErrTruncated) {
			t.Fatalf("cut at %d: err = %v, want ErrTruncated", cut, err)
		}
		if len(got) != 2 {
			t.Fatalf("cut at %d: %d envelopes, want the 2 complete ones", cut, len(got))
		}
		if !bytes.Equal(got[1].Body, samples.MustRead(t, samplesAll[1])) {
			t.Errorf("cut at %d: second body changed", cut)
		}
	}
}

func TestScanRepairsATruncatedTailSoLaterAppendsAreReadable(t *testing.T) {
	log := raw.Open(t.TempDir(), "its")
	minute := "2026-10-12T13:38:00Z"
	var key string
	for i := range 2 {
		loc, _ := log.Append(header(minute, i, 0, 200, nil, nil), samples.MustRead(t, samplesAll[i]))
		key = loc.Key
	}
	full, _ := os.ReadFile(log.Path(key))
	if err := os.WriteFile(log.Path(key), full[:len(full)-20], 0o644); err != nil {
		t.Fatal(err)
	}

	from := time.Date(2026, 10, 12, 13, 0, 0, 0, time.UTC)
	// Without repair the file is reported and left alone.
	res, err := log.Scan(from, from, false)
	if err == nil || !errors.Is(err, raw.ErrTruncated) {
		t.Fatalf("Scan without repair: err = %v, want ErrTruncated", err)
	}
	if after, _ := os.ReadFile(log.Path(key)); len(after) != len(full)-20 {
		t.Fatal("Scan without repair modified the file")
	}

	res, err = log.Scan(from, from, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Repaired) != 1 || res.Repaired[0] != key {
		t.Fatalf("Repaired = %v, want [%s]", res.Repaired, key)
	}
	if got := len(res.Index.Attempts(minute)); got != 1 {
		t.Fatalf("index has %d attempts, want 1", got)
	}

	// A new append after the repair is found by the next scan.
	if _, err := log.Append(header(minute, 1, 1, 200, nil, nil), []byte(`{"again":true}`)); err != nil {
		t.Fatal(err)
	}
	res, err = log.Scan(from, from, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(res.Index.Attempts(minute)); got != 2 {
		t.Fatalf("after repair + append: %d attempts, want 2", got)
	}
}

func TestCorruptionInTheMiddleIsAnErrorAndNeverRepaired(t *testing.T) {
	log := raw.Open(t.TempDir(), "its")
	var key string
	for i := range 3 {
		loc, _ := log.Append(header("2026-10-12T13:38:00Z", i, 0, 200, nil, nil), samples.MustRead(t, samplesAll[i]))
		key = loc.Key
	}
	path := log.Path(key)
	data, _ := os.ReadFile(path)
	var firstEnd int64
	_, _ = raw.Walk(path, false, func(_ raw.Envelope, loc raw.Loc) {
		if firstEnd == 0 {
			firstEnd = loc.Offset + loc.Len
		}
	})
	data[firstEnd+30] ^= 0xff // flip a byte inside the second member
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 10, 12, 13, 0, 0, 0, time.UTC)
	_, err := log.Scan(from, from, true)
	if !errors.Is(err, raw.ErrCorrupt) {
		t.Fatalf("err = %v, want ErrCorrupt", err)
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(after, data) {
		t.Fatal("a corrupt file was modified")
	}
}

func TestChainCompleteness(t *testing.T) {
	const minute = "2026-10-12T13:38:00Z"
	c1, c2 := sp("cursor-1"), sp("cursor-2")
	entry := func(page, attempt, status int, cursor, next *string, errText string) raw.Entry {
		h := header(minute, page, attempt, status, cursor, next)
		h.Error = errText
		return raw.Entry{Header: h}
	}
	build := func(es ...raw.Entry) *raw.Index {
		ix := raw.NewIndex()
		for _, e := range es {
			ix.Add(e)
		}
		return ix
	}

	t.Run("full chain", func(t *testing.T) {
		pages, ok := build(entry(0, 0, 200, nil, c1, ""), entry(1, 0, 200, c1, c2, ""), entry(2, 0, 200, c2, nil, "")).Chain(minute)
		if !ok || len(pages) != 3 {
			t.Errorf("complete = %v, pages = %d; want true, 3", ok, len(pages))
		}
	})
	t.Run("a 5xx then success counts once", func(t *testing.T) {
		pages, ok := build(entry(0, 0, 503, nil, nil, ""), entry(0, 1, 200, nil, nil, "")).Chain(minute)
		if !ok || len(pages) != 1 || pages[0].Header.Attempt != 1 {
			t.Errorf("complete = %v, pages = %+v", ok, pages)
		}
	})
	t.Run("last page missing", func(t *testing.T) {
		pages, ok := build(entry(0, 0, 200, nil, c1, ""), entry(1, 0, 200, c1, c2, "")).Chain(minute)
		if ok || len(pages) != 2 {
			t.Errorf("complete = %v, pages = %d; want false, 2", ok, len(pages))
		}
	})
	t.Run("a page that failed is not a page", func(t *testing.T) {
		if _, ok := build(entry(0, 0, 200, nil, c1, ""), entry(1, 0, 0, c1, nil, "connection reset")).Chain(minute); ok {
			t.Error("a failed attempt completed the chain")
		}
	})
	t.Run("a page fetched with the wrong cursor does not chain", func(t *testing.T) {
		if _, ok := build(entry(0, 0, 200, nil, c1, ""), entry(1, 0, 200, c2, nil, "")).Chain(minute); ok {
			t.Error("a page with a foreign cursor completed the chain")
		}
	})
	t.Run("nothing", func(t *testing.T) {
		if _, ok := build().Chain(minute); ok {
			t.Error("an empty minute is complete")
		}
	})
}
