package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"runtime/debug"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The history behind the time machine (docs/tasks/TASK-time-machine.md §2.1–2.2). The API keeps the
// last historyHours UTC hours of fixes in memory, one sample per vehicle per minute (the newest
// real fix in that minute, never interpolated), and serves each hour as a precomputed
// history-hour.v1 file. A request never queries the database (invariant 5).

const (
	defaultHistoryHours = 3
	maxHistoryHours     = 72 // positions are kept 3 days; more hours than that would be empty

	historySampling = "newest_fix_per_vehicle_per_minute"
	hourLayout      = "2006-01-02T15"
	// backfillWindow is how late the adapter may still fill a minute (ITS_BACKFILL_WINDOW plus an
	// hour of margin). An hour older than this can no longer change, so its file is immutable.
	backfillWindow = 24 * time.Hour

	historyLoadTimeout  = 2 * time.Minute
	historyMergeTimeout = 30 * time.Second
	// maxMergeMinutes caps the incremental path: a backfill that changed more minutes than this
	// (the adapter fills up to 23 h) is cheaper to meet with one reload than with a query per minute.
	maxMergeMinutes = 20
)

// The three queries history adds. The load streams every fix of the window and is folded in Go
// (no ORDER BY: the fold does not depend on order). The merge reads only the fixes whose earliest
// raw minute is the minute that changed; event_time is bounded too so chunk exclusion applies.
const (
	historyStatusQuery = `
SELECT feed, minute_utc, status, events
FROM feed_status
WHERE feed = ANY($1) AND minute_utc >= $2 AND minute_utc < $3`

	historyLoadQuery = `
SELECT operator, vehicle, mode, line, feed, event_time, received_time, lat, lon, speed_kmh
FROM positions
WHERE event_time >= $1 AND event_time < $2`

	historyMergeQuery = `
SELECT operator, vehicle, mode, line, feed, event_time, received_time, lat, lon, speed_kmh
FROM positions
WHERE event_time >= $1 AND event_time < $2 AND raw_minute = $3`
)

// histEntry is one row of a file's vehicle index. A vehicle that changes line gets two entries.
type histEntry struct {
	operator string
	vehicle  string
	mode     string
	line     string
	hasLine  bool
}

type vidKey struct{ operator, vehicle string }

// fixCand is the candidate for one vehicle's sample in one minute. ev and recv are Unix
// microseconds (Postgres precision). The tie rule is the snapshot's: newest event_time, then newest
// received_time, then the lowest feed name bytewise.
type fixCand struct {
	entry         int32
	feed          uint16
	ev, recv      int64
	lat, lon, spd int32
}

type feedMinute struct {
	status string
	events int
}

type histState struct {
	winStart time.Time // first hour of the window
	end      time.Time // newest minute every feed has a row for
	feeds    []string  // every feed known to feed_status, sorted

	statuses map[int64]map[string]feedMinute // minute (Unix minutes) -> feed -> status
	minutes  map[int64]map[int32]fixCand     // minute -> vid -> sample

	vids     map[vidKey]int32
	entries  []histEntry
	entryIDs map[histEntry]int32
	entryVid []int32 // entry id -> vid
	feedIDs  map[string]uint16
	feedName []string

	files map[string]*hourFile
}

func newHistState() *histState {
	return &histState{
		statuses: map[int64]map[string]feedMinute{},
		minutes:  map[int64]map[int32]fixCand{},
		vids:     map[vidKey]int32{},
		entryIDs: map[histEntry]int32{},
		feedIDs:  map[string]uint16{},
		files:    map[string]*hourFile{},
	}
}

// hourFile is one hour, fully encoded. Requests only copy these bytes.
type hourFile struct {
	Hour     time.Time
	Complete bool
	ETag     string
	JSON     []byte
	Gzip     []byte
}

// historyView is what the handlers read: swapped in whole, never edited.
type historyView struct {
	Newest, Oldest time.Time
	Hours          []time.Time
	Files          map[string]*hourFile
	IndexJSON      []byte
	IndexETag      string
}

type historyIndexJSON struct {
	NewestMinute time.Time `json:"newest_minute"`
	OldestMinute time.Time `json:"oldest_minute"`
	Hours        []string  `json:"hours"`
	WindowHours  int       `json:"window_hours"`
}

// history-hour.v1, as the contract describes it.
type hourJSON struct {
	Schema   string        `json:"schema"`
	HourUTC  time.Time     `json:"hour_utc"`
	Complete bool          `json:"complete"`
	Sampling string        `json:"sampling"`
	Vehicles []hourVehicle `json:"vehicles"`
	Fixes    hourFixes     `json:"fixes"`
	Minutes  []hourMinute  `json:"minutes"`
}

type hourVehicle struct {
	Operator string  `json:"operator"`
	Vehicle  string  `json:"vehicle"`
	Mode     string  `json:"mode"`
	Line     *string `json:"line"`
}

type hourFixes struct {
	V   []int32 `json:"v"`
	T   []int32 `json:"t"`
	Lat []int32 `json:"lat"`
	Lon []int32 `json:"lon"`
	Spd []int32 `json:"spd"`
}

type hourMinute struct {
	Vehicles int       `json:"vehicles"`
	Feeds    hourFeeds `json:"feeds"`
}

type hourFeeds struct {
	OK            int `json:"ok"`
	Failed        int `json:"failed"`
	ProviderNotOK int `json:"provider_not_ok"`
	Gap           int `json:"gap"`
	Missing       int `json:"missing"`
}

func hourStart(t time.Time) time.Time { return t.UTC().Truncate(time.Hour) }

func minuteKey(t time.Time) int64 { return t.Unix() / 60 }

func keyTime(k int64) time.Time { return time.Unix(k*60, 0).UTC() }

// microsMinute is floor(micros / 1 min); fixes before 1970 do not exist.
func microsMinute(us int64) int64 { return us / 60_000_000 }

// historyEnd is the newest minute that every known feed has a feed_status row for. The feeds report
// a few seconds apart, so a newer minute that only some feeds have is not yet comparable with the
// ones before it.
func historyEnd(state []feedRow) (time.Time, error) {
	if len(state) == 0 {
		return time.Time{}, errors.New("no feed_status rows yet")
	}
	end := state[0].Minute
	for _, r := range state[1:] {
		if r.Minute.Before(end) {
			end = r.Minute
		}
	}
	return end.UTC(), nil
}

// isComplete reports whether an hour can no longer change: it is closed and older than the longest
// backfill.
func isComplete(hour, end time.Time) bool {
	return !hour.Add(time.Hour + backfillWindow).After(end)
}

// ---- reading ----

func readStatuses(ctx context.Context, db querier, feeds []string, from, to time.Time) (map[int64]map[string]feedMinute, error) {
	rows, err := db.Query(ctx, historyStatusQuery, feeds, from, to)
	if err != nil {
		return nil, fmt.Errorf("read history status: %w", err)
	}
	defer rows.Close()
	out := map[int64]map[string]feedMinute{}
	for rows.Next() {
		var feed, status string
		var minute time.Time
		var events int
		if err := rows.Scan(&feed, &minute, &status, &events); err != nil {
			return nil, fmt.Errorf("read history status: %w", err)
		}
		k := minuteKey(minute)
		if out[k] == nil {
			out[k] = map[string]feedMinute{}
		}
		out[k][feed] = feedMinute{status: status, events: events}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read history status: %w", err)
	}
	return out, nil
}

// fold reads positions rows into the per-minute samples and returns how many rows it read and
// the hours (Unix hours) of the minutes it touched.
func (h *histState) fold(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}, from, to time.Time) (read int, touched map[time.Time]bool, err error) {
	touched = map[time.Time]bool{}
	lo, hi := from.UnixMicro(), to.UnixMicro()
	for rows.Next() {
		var operator, vehicle, mode, feed string
		var line *string
		var ev, recv time.Time
		var lat, lon float64
		var speed *float64
		if err := rows.Scan(&operator, &vehicle, &mode, &line, &feed, &ev, &recv, &lat, &lon, &speed); err != nil {
			return read, touched, fmt.Errorf("read history positions: %w", err)
		}
		read++
		evUS := ev.UnixMicro()
		if evUS < lo || evUS >= hi {
			continue
		}
		spd := int32(-1) // null stays null: missing is not zero
		if speed != nil {
			spd = int32(math.Round(*speed * 10))
		}
		slot := microsMinute(evUS)
		if h.put(slot, operator, vehicle, mode, line, feed, fixCand{
			ev: evUS, recv: recv.UnixMicro(),
			lat: int32(math.Round(lat * 1e5)), lon: int32(math.Round(lon * 1e5)), spd: spd,
		}) {
			touched[hourStart(keyTime(slot))] = true
		}
	}
	if err := rows.Err(); err != nil {
		return read, touched, fmt.Errorf("read history positions: %w", err)
	}
	return read, touched, nil
}

// put offers a candidate for its (vehicle, minute) and reports whether the sample changed.
func (h *histState) put(slot int64, operator, vehicle, mode string, line *string, feed string, c fixCand) bool {
	vk := vidKey{operator, vehicle}
	vid, ok := h.vids[vk]
	if !ok {
		vid = int32(len(h.vids))
		h.vids[vk] = vid
	}
	e := histEntry{operator: operator, vehicle: vehicle, mode: mode}
	if line != nil {
		e.line, e.hasLine = *line, true
	}
	eid, ok := h.entryIDs[e]
	if !ok {
		eid = int32(len(h.entries))
		h.entries = append(h.entries, e)
		h.entryVid = append(h.entryVid, vid)
		h.entryIDs[e] = eid
	}
	fid, ok := h.feedIDs[feed]
	if !ok {
		fid = uint16(len(h.feedName))
		h.feedName = append(h.feedName, feed)
		h.feedIDs[feed] = fid
	}
	c.entry, c.feed = eid, fid

	m := h.minutes[slot]
	if m == nil {
		m = map[int32]fixCand{}
		h.minutes[slot] = m
	}
	cur, exists := m[vid]
	switch {
	case !exists:
	case cur.feed == c.feed && cur.ev == c.ev:
		// The same identity again (a merge meeting a row the load already folded, or a repeat that
		// replaced its later copy): the incoming copy is the one the database now holds.
		if cur == c {
			return false
		}
	case !h.better(c, cur):
		return false
	}
	m[vid] = c
	return true
}

// better is the snapshot's ordering: newest event_time, then newest received_time, then the feed
// name bytewise (COLLATE "C").
func (h *histState) better(a, b fixCand) bool {
	if a.ev != b.ev {
		return a.ev > b.ev
	}
	if a.recv != b.recv {
		return a.recv > b.recv
	}
	return h.feedName[a.feed] < h.feedName[b.feed]
}

// prune forgets everything before the window.
func (h *histState) prune(winStart time.Time) {
	lo := minuteKey(winStart)
	for k := range h.minutes {
		if k < lo {
			delete(h.minutes, k)
		}
	}
	for k := range h.statuses {
		if k < lo {
			delete(h.statuses, k)
		}
	}
}

// ---- refreshing ----

// refreshHistory brings the in-memory history up to date with what feed_status says. The first call
// (and a backfill that changed too many minutes) streams the whole window; after that only the
// minutes whose feed_status row changed are read again, by earliest raw minute.
func (s *Server) refreshHistory(ctx context.Context, state []feedRow) error {
	end, err := historyEnd(state)
	if err != nil {
		return err
	}
	feeds := make([]string, 0, len(state))
	for _, r := range state {
		feeds = append(feeds, r.Feed)
	}
	slices.Sort(feeds)
	winStart := hourStart(end).Add(-time.Duration(s.opts.historyHours-1) * time.Hour)
	winEnd := hourStart(end).Add(time.Hour) // fixes stamped past the current hour wait for their hour

	h := s.hist
	if h == nil {
		return s.reloadHistory(ctx, feeds, end, winStart, winEnd, "start")
	}

	sctx, cancel := context.WithTimeout(ctx, probeTimeout)
	statuses, err := readStatuses(sctx, s.db, feeds, winStart, end.Add(time.Minute))
	cancel()
	if err != nil {
		return err
	}
	var dirty []int64
	for k, now := range statuses {
		if !maps2Equal(now, h.statuses[k]) {
			dirty = append(dirty, k)
		}
	}
	slices.Sort(dirty)
	if len(dirty) > maxMergeMinutes {
		return s.reloadHistory(ctx, feeds, end, winStart, winEnd, "backfill")
	}

	touched := map[time.Time]bool{}
	if len(dirty) > 0 {
		mctx, cancel := context.WithTimeout(ctx, historyMergeTimeout)
		defer cancel()
		for _, k := range dirty {
			touched[hourStart(keyTime(k))] = true
			rows, err := s.db.Query(mctx, historyMergeQuery, winStart, winEnd, keyTime(k))
			if err != nil {
				return fmt.Errorf("read history positions: %w", err)
			}
			_, hours, err := h.fold(rows, winStart, winEnd)
			rows.Close()
			if err != nil {
				return err
			}
			for hr := range hours {
				touched[hr] = true
			}
		}
	}
	if !end.Equal(h.end) {
		touched[hourStart(end)] = true
		touched[hourStart(h.end)] = true
	}
	if !slices.Equal(feeds, h.feeds) {
		// A new feed changes every hour's "missing" counts.
		for hr := range h.files {
			touched[hr2time(hr)] = true
		}
	}
	h.statuses, h.feeds = statuses, feeds
	h.prune(winStart)
	prevEnd := h.end
	h.end, h.winStart = end, winStart
	return s.publishHistory(h, touched, !end.Equal(prevEnd) || len(dirty) > 0)
}

func hr2time(key string) time.Time {
	t, _ := time.Parse(hourLayout, key)
	return t.UTC()
}

func maps2Equal(a, b map[string]feedMinute) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || v != w {
			return false
		}
	}
	return true
}

// reloadHistory streams the window from scratch into a new state and swaps it in.
func (s *Server) reloadHistory(ctx context.Context, feeds []string, end, winStart, winEnd time.Time, why string) error {
	began := time.Now()
	lctx, cancel := context.WithTimeout(ctx, historyLoadTimeout)
	defer cancel()
	statuses, err := readStatuses(lctx, s.db, feeds, winStart, end.Add(time.Minute))
	if err != nil {
		return err
	}
	// The stream makes a few strings per row, about a million rows. A tight GC target keeps the
	// peak near the steady state instead of double it.
	defer debug.SetGCPercent(debug.SetGCPercent(20))
	h := newHistState()
	rows, err := s.db.Query(lctx, historyLoadQuery, winStart, winEnd)
	if err != nil {
		return fmt.Errorf("read history positions: %w", err)
	}
	read, _, err := h.fold(rows, winStart, winEnd)
	rows.Close()
	if err != nil {
		return err
	}
	h.statuses, h.feeds, h.end, h.winStart = statuses, feeds, end, winStart
	touched := map[time.Time]bool{}
	if err := s.publishHistory(h, touched, true); err != nil {
		return err
	}
	s.hist = h // only a fully built state replaces the old one
	// The stream leaves a megabytes-deep trail of per-row strings; hand it back now, not whenever
	// the next collection happens to run (the container's memory limit is 128 MB).
	debug.FreeOSMemory()
	s.lg.Info("history loaded", "why", why, "rows", read, "minutes", len(h.minutes), "hours", len(h.files),
		"newest_minute", end, "ms", time.Since(began).Milliseconds())
	return nil
}

// publishHistory rebuilds the touched hours (and any hour with no file yet), drops hours that left
// the window and swaps in the new view. changed=false with nothing touched keeps the old view, so
// an idle refresh leaves every ETag alone.
func (s *Server) publishHistory(h *histState, touched map[time.Time]bool, changed bool) error {
	var hours []time.Time
	for hr := hourStart(h.winStart); !hr.After(hourStart(h.end)); hr = hr.Add(time.Hour) {
		hours = append(hours, hr)
	}
	keep := map[string]bool{}
	rebuilt := 0
	for _, hr := range hours {
		key := hr.Format(hourLayout)
		keep[key] = true
		if h.files[key] != nil && !touched[hr] {
			continue
		}
		f, err := h.buildHour(hr)
		if err != nil {
			return err
		}
		h.files[key] = f
		rebuilt++
	}
	dropped := 0
	for key := range h.files {
		if !keep[key] {
			delete(h.files, key)
			dropped++
		}
	}
	if rebuilt == 0 && dropped == 0 && !changed && s.view.Load() != nil {
		return nil
	}
	oldest := h.winStart
	lo := minuteKey(h.winStart)
	first := int64(math.MaxInt64)
	for k := range h.statuses {
		if k >= lo && k < first {
			first = k
		}
	}
	if first != math.MaxInt64 {
		oldest = keyTime(first)
	}
	files := make(map[string]*hourFile, len(h.files))
	for k, f := range h.files {
		files[k] = f
	}
	names := make([]string, len(hours))
	for i, hr := range hours {
		names[i] = hr.Format(time.RFC3339)
	}
	idx, err := json.Marshal(historyIndexJSON{NewestMinute: h.end, OldestMinute: oldest, Hours: names, WindowHours: s.opts.historyHours})
	if err != nil {
		return fmt.Errorf("encode history index: %w", err)
	}
	sum := sha256.Sum256(idx)
	s.view.Store(&historyView{
		Newest: h.end, Oldest: oldest, Hours: hours, Files: files,
		IndexJSON: idx, IndexETag: `W/"` + hex.EncodeToString(sum[:8]) + `"`,
	})
	if rebuilt > 0 {
		s.lg.Info("history hours rebuilt", "rebuilt", rebuilt, "dropped", dropped, "newest_minute", h.end)
	}
	return nil
}

// ---- encoding ----

// buildHour encodes one UTC hour. Minutes after the newest one every feed reported are empty: not
// yet published.
func (h *histState) buildHour(hour time.Time) (*hourFile, error) {
	type outFix struct {
		entry         int32
		t             int32
		lat, lon, spd int32
	}
	first := minuteKey(hour)
	var fixes []outFix
	used := map[int32]bool{}
	minutes := make([]hourMinute, 60)
	endKey := minuteKey(h.end)
	for i := range minutes {
		k := first + int64(i)
		counts := &minutes[i].Feeds
		if k > endKey {
			counts.Missing = len(h.feeds)
			continue
		}
		seen := 0
		for _, fm := range h.statuses[k] {
			seen++
			switch fm.status {
			case "ok":
				counts.OK++
			case "failed":
				counts.Failed++
			case "provider_not_ok":
				counts.ProviderNotOK++
			case "gap":
				counts.Gap++
			default:
				return nil, fmt.Errorf("history: unknown feed status %q", fm.status)
			}
		}
		counts.Missing = len(h.feeds) - seen
		m := h.minutes[k]
		minutes[i].Vehicles = len(m)
		for _, c := range m {
			used[c.entry] = true
			fixes = append(fixes, outFix{c.entry, int32((c.ev/1_000_000 - hour.Unix())), c.lat, c.lon, c.spd})
		}
	}

	// The vehicle index is sorted so the same data always encodes to the same bytes (the ETag).
	ids := make([]int32, 0, len(used))
	for id := range used {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(a, b int) bool { return entryLess(h.entries[ids[a]], h.entries[ids[b]]) })
	index := make(map[int32]int32, len(ids))
	vehicles := make([]hourVehicle, len(ids))
	for i, id := range ids {
		index[id] = int32(i)
		e := h.entries[id]
		v := hourVehicle{Operator: e.operator, Vehicle: e.vehicle, Mode: e.mode}
		if e.hasLine {
			line := e.line
			v.Line = &line
		}
		vehicles[i] = v
	}
	slices.SortFunc(fixes, func(a, b outFix) int {
		if x, y := index[a.entry], index[b.entry]; x != y {
			return int(x - y)
		}
		return int(a.t - b.t)
	})
	cols := hourFixes{
		V: make([]int32, len(fixes)), T: make([]int32, len(fixes)), Lat: make([]int32, len(fixes)),
		Lon: make([]int32, len(fixes)), Spd: make([]int32, len(fixes)),
	}
	for i, f := range fixes {
		cols.V[i], cols.T[i], cols.Lat[i], cols.Lon[i], cols.Spd[i] = index[f.entry], f.t, f.lat, f.lon, f.spd
	}
	complete := isComplete(hour, h.end)
	body, err := json.Marshal(hourJSON{
		Schema: "history-hour.v1", HourUTC: hour, Complete: complete, Sampling: historySampling,
		Vehicles: vehicles, Fixes: cols, Minutes: minutes,
	})
	if err != nil {
		return nil, fmt.Errorf("encode history hour: %w", err)
	}
	var zipped bytes.Buffer
	zw, err := gzip.NewWriterLevel(&zipped, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	if _, err := zw.Write(body); err != nil {
		return nil, fmt.Errorf("gzip history hour: %w", err)
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("gzip history hour: %w", err)
	}
	sum := sha256.Sum256(body)
	return &hourFile{
		Hour: hour, Complete: complete,
		// Weak: the same file goes out plain and as gzip.
		ETag: `W/"` + hex.EncodeToString(sum[:12]) + `"`,
		JSON: body, Gzip: zipped.Bytes(),
	}, nil
}

func entryLess(a, b histEntry) bool {
	if a.operator != b.operator {
		return a.operator < b.operator
	}
	if a.vehicle != b.vehicle {
		return a.vehicle < b.vehicle
	}
	if a.hasLine != b.hasLine {
		return !a.hasLine
	}
	if a.line != b.line {
		return a.line < b.line
	}
	return a.mode < b.mode
}

// ---- handlers ----

func (s *Server) serveBytes(w http.ResponseWriter, r *http.Request, etag, cache string, plain, zipped []byte) {
	h := w.Header()
	h.Set("ETag", etag)
	h.Set("Cache-Control", cache)
	h.Set("Vary", "Accept-Encoding")
	if etagMatches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	body := plain
	if zipped != nil && acceptsGzip(r.Header.Get("Accept-Encoding")) {
		h.Set("Content-Encoding", "gzip")
		body = zipped
	}
	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Set("Content-Length", strconv.Itoa(len(body)))
	_, _ = w.Write(body)
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	v := s.view.Load()
	if v == nil {
		writeError(w, http.StatusServiceUnavailable, "no history yet: the database has not answered with a window of positions")
		return
	}
	s.serveBytes(w, r, v.IndexETag, "public, max-age=10, s-maxage=10", v.IndexJSON, nil)
}

func (s *Server) handleHistoryHour(w http.ResponseWriter, r *http.Request) {
	v := s.view.Load()
	if v == nil {
		writeError(w, http.StatusServiceUnavailable, "no history yet: the database has not answered with a window of positions")
		return
	}
	key := r.PathValue("hour")
	if _, err := time.Parse(hourLayout, key); err != nil || strings.TrimSpace(key) != key {
		writeError(w, http.StatusBadRequest, "hour must look like 2026-10-12T13")
		return
	}
	f := v.Files[key]
	if f == nil {
		writeError(w, http.StatusNotFound, "that hour is not in the window (see /v1/history)")
		return
	}
	cache := "public, max-age=30, s-maxage=30"
	if f.Complete {
		cache = "public, max-age=31536000, immutable"
	}
	s.serveBytes(w, r, f.ETag, cache, f.JSON, f.Gzip)
}
