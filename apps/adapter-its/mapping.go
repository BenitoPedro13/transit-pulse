package main

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/BenitoPedro13/transit-pulse/packages/event"
	"github.com/BenitoPedro13/transit-pulse/packages/raw"
)

// triple is the exact (fornecedor, sistema, modo) a record carries. sistema is null for sonda, so
// null and "" are kept apart.
type triple struct {
	fornecedor string
	sistema    string
	hasSistema bool
	modo       string
}

type target struct {
	operator  string
	mode      string
	authority string
}

// mappingTable is verified against the four captured samples (docs/research/README.md §1.1).
// Any other triple is not emitted. A new supplier is added here after looking at its data, never
// through a fallback.
var mappingTable = map[triple]target{
	{"conecta", "sppo", true, "onibus"}: {"rio-sppo", "bus", "city"},
	{"zirix", "sppo", true, "onibus"}:   {"rio-sppo", "bus", "city"},
	{"maxtrack", "rio", true, "onibus"}: {"rio-sistema-rio", "bus", "city"},
	{"sonda", "", false, "brt"}:         {"rio-brt", "brt", "city"},
}

// feedOperators maps each mapped feed to its operator, for feed-status messages. It is derived
// from mappingTable; init panics if a supplier maps to two operators, because a feed-status
// message would then be ambiguous.
var feedOperators = func() map[string]string {
	m := map[string]string{}
	for t, tg := range mappingTable {
		feed := feedOf(t.fornecedor)
		if prev, ok := m[feed]; ok && prev != tg.operator {
			panic(fmt.Sprintf("feed %s maps to both %s and %s", feed, prev, tg.operator))
		}
		m[feed] = tg.operator
	}
	return m
}()

// mappedFeeds lists the feeds of mappingTable, sorted.
func mappedFeeds() []string {
	out := make([]string, 0, len(feedOperators))
	for f := range feedOperators {
		out = append(out, f)
	}
	slices.Sort(out)
	return out
}

func feedOf(fornecedor string) string { return "its:" + fornecedor }

// record is one element of the response's data array. Pointers keep null apart from zero.
type record struct {
	IDVeiculo  *string  `json:"id_veiculo"`
	Fornecedor *string  `json:"fornecedor"`
	Sistema    *string  `json:"sistema"`
	Modo       *string  `json:"modo"`
	Servico    *string  `json:"servico"`
	Latitude   *float64 `json:"latitude"`
	Longitude  *float64 `json:"longitude"`
	Velocidade *float64 `json:"velocidade"`
	Direcao    *float64 `json:"direcao"`
	RouteID    *string  `json:"route_id"`
	Datetime   *string  `json:"datetime"`
}

// page is the part of a response body the mapping reads.
type page struct {
	MinutoUTC      string            `json:"minuto_utc"`
	Data           []json.RawMessage `json:"data"`
	NextCursor     *string           `json:"next_cursor"`
	ProvidersStats map[string]string `json:"providers_status"`
}

// FeedStats counts what happened to the records of one supplier.
type FeedStats struct {
	Records               int
	Events                int
	Invalid               int
	Unmapped              int
	Dups                  int // exact duplicates, dropped
	ConflictingDuplicates int // same identity, different position; the first is kept
	NullSpeed             int // velocidade absent or negative
	NullHeading           int // direcao absent or outside [0, 360]
}

// Stats is the result of mapping one envelope, or one whole minute.
type Stats struct {
	Records int
	// Feeds is keyed by "its:<fornecedor>" for every supplier seen, mapped or not. Records with no
	// fornecedor are counted under the key "its:".
	Feeds map[string]*FeedStats
	// ProviderStatus is providers_status[fornecedor] as reported, per feed.
	ProviderStatus map[string]string
	// Unmapped lists each distinct unmapped triple once, for logging.
	UnmappedTriples []string
	// Conflicts holds the first few conflicting duplicates, for logging.
	Conflicts []string
}

func newStats() *Stats {
	return &Stats{Feeds: map[string]*FeedStats{}, ProviderStatus: map[string]string{}}
}

func (s *Stats) feed(f string) *FeedStats {
	fs, ok := s.Feeds[f]
	if !ok {
		fs = &FeedStats{}
		s.Feeds[f] = fs
	}
	return fs
}

// Total adds up the per-feed counters.
func (s *Stats) Total() FeedStats {
	var t FeedStats
	for _, fs := range s.Feeds {
		t.Records += fs.Records
		t.Events += fs.Events
		t.Invalid += fs.Invalid
		t.Unmapped += fs.Unmapped
		t.Dups += fs.Dups
		t.ConflictingDuplicates += fs.ConflictingDuplicates
		t.NullSpeed += fs.NullSpeed
		t.NullHeading += fs.NullHeading
	}
	return t
}

// maxLoggedConflicts bounds the conflicting-duplicate details kept per minute.
const maxLoggedConflicts = 5

// decoder maps envelopes to events. One decoder spans one minute, so duplicates are found across
// its pages; Events uses a fresh one per call.
type decoder struct {
	seen   map[string]event.Position
	events []event.Position
	stats  *Stats
}

func newDecoder() *decoder {
	return &decoder{seen: map[string]event.Position{}, stats: newStats()}
}

// Events maps one stored raw envelope to events. It is a pure function of the stored bytes: live
// ingestion and replay both call it on envelopes read back from the raw log, never on a live HTTP
// response, so the two cannot drift apart.
func Events(env raw.Envelope) ([]event.Position, *Stats, error) {
	d := newDecoder()
	if err := d.add(env); err != nil {
		return nil, nil, err
	}
	return d.events, d.stats, nil
}

// MinuteEvents maps all pages of one minute, de-duplicating across pages.
func MinuteEvents(envs []raw.Envelope) ([]event.Position, *Stats, error) {
	d := newDecoder()
	for _, env := range envs {
		if err := d.add(env); err != nil {
			return nil, nil, err
		}
	}
	return d.events, d.stats, nil
}

func (d *decoder) add(env raw.Envelope) error {
	if env.Status != 200 || env.Error != "" {
		return fmt.Errorf("envelope for %s page %d is not a successful response (status %d, error %q)",
			env.MinuteUTC, env.Page, env.Status, env.Error)
	}
	var pg page
	if err := json.Unmarshal(env.Body, &pg); err != nil {
		return fmt.Errorf("envelope for %s page %d: body is not the expected JSON: %w", env.MinuteUTC, env.Page, err)
	}
	for f, st := range pg.ProvidersStats {
		feed := feedOf(f)
		// A supplier is "ok" only if no page says otherwise; the first non-ok value wins.
		if prev, seen := d.stats.ProviderStatus[feed]; !seen || (prev == "ok" && st != "ok") {
			d.stats.ProviderStatus[feed] = st
		}
	}

	ref := event.RawRef{Key: raw.HourKey(env.Source, mustMinute(env.MinuteUTC)), MinuteUTC: env.MinuteUTC, Page: env.Page}
	for _, msg := range pg.Data {
		d.stats.Records++
		var r record
		if err := json.Unmarshal(msg, &r); err != nil {
			d.stats.feed(feedOf("")).Records++
			d.stats.feed(feedOf("")).Invalid++
			continue
		}
		fornecedor := ""
		if r.Fornecedor != nil {
			fornecedor = *r.Fornecedor
		}
		fs := d.stats.feed(feedOf(fornecedor))
		fs.Records++

		tg, mapped := lookup(r)
		if !mapped {
			fs.Unmapped++
			label := fmt.Sprintf("fornecedor=%s sistema=%s modo=%s", fornecedor, strOrNull(r.Sistema), strOrNull(r.Modo))
			if !slices.Contains(d.stats.UnmappedTriples, label) {
				d.stats.UnmappedTriples = append(d.stats.UnmappedTriples, label)
			}
			continue
		}

		p, ok := position(r, tg, feedOf(fornecedor), env.FetchedAt, ref, fs)
		if !ok {
			fs.Invalid++
			continue
		}
		id := p.Identity()
		if first, dup := d.seen[id]; dup {
			if samePosition(first, p) {
				fs.Dups++
			} else {
				fs.ConflictingDuplicates++
				if len(d.stats.Conflicts) < maxLoggedConflicts {
					d.stats.Conflicts = append(d.stats.Conflicts, fmt.Sprintf("%s kept (%v,%v) dropped (%v,%v)", id, first.Lat, first.Lon, p.Lat, p.Lon))
				}
			}
			continue
		}
		d.seen[id] = p
		d.events = append(d.events, p)
		fs.Events++
	}
	return nil
}

func mustMinute(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func strOrNull(s *string) string {
	if s == nil {
		return "null"
	}
	return *s
}

func lookup(r record) (target, bool) {
	t := triple{}
	if r.Fornecedor != nil {
		t.fornecedor = *r.Fornecedor
	}
	if r.Sistema != nil {
		t.sistema, t.hasSistema = *r.Sistema, true
	}
	if r.Modo != nil {
		t.modo = *r.Modo
	}
	tg, ok := mappingTable[t]
	return tg, ok
}

// position builds an event from a mapped record. ok is false for an invalid record: missing
// id_veiculo or datetime, an unparseable datetime, or a bad coordinate. Out-of-range speed and
// heading become null and are counted, never clamped.
func position(r record, tg target, feed string, fetchedAt time.Time, ref event.RawRef, fs *FeedStats) (event.Position, bool) {
	if r.IDVeiculo == nil || *r.IDVeiculo == "" || r.Datetime == nil || *r.Datetime == "" {
		return event.Position{}, false
	}
	at, err := time.Parse(time.RFC3339, *r.Datetime)
	if err != nil {
		return event.Position{}, false
	}
	if r.Latitude == nil || r.Longitude == nil {
		return event.Position{}, false
	}
	lat, lon := *r.Latitude, *r.Longitude
	if math.IsNaN(lat) || math.IsInf(lat, 0) || math.IsNaN(lon) || math.IsInf(lon, 0) ||
		lat < -90 || lat > 90 || lon < -180 || lon > 180 || (lat == 0 && lon == 0) {
		return event.Position{}, false
	}

	p := event.Position{
		City: "rio", Mode: tg.mode, Operator: tg.operator, Authority: tg.authority, SourceKind: "live",
		Feed: feed, Vehicle: *r.IDVeiculo,
		Line: nonEmpty(r.Servico), RouteID: nonEmpty(r.RouteID),
		Lat: lat, Lon: lon,
		EventTime: at.UTC(), ReceivedTime: fetchedAt.UTC(),
		RawRef: ref,
	}
	if v := r.Velocidade; v != nil && !math.IsNaN(*v) && !math.IsInf(*v, 0) && *v >= 0 {
		p.SpeedKmh = v
	} else {
		fs.NullSpeed++
	}
	if h := r.Direcao; h != nil && !math.IsNaN(*h) && *h >= 0 && *h <= 360 {
		p.HeadingDeg = h
	} else {
		fs.NullHeading++
	}
	return p, true
}

// nonEmpty turns "" into null and keeps anything else exactly as given.
func nonEmpty(s *string) *string {
	if s == nil || *s == "" {
		return nil
	}
	return s
}

func samePosition(a, b event.Position) bool {
	return a.Lat == b.Lat && a.Lon == b.Lon && eqPtr(a.SpeedKmh, b.SpeedKmh) && eqPtr(a.HeadingDeg, b.HeadingDeg)
}

func eqPtr(a, b *float64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
