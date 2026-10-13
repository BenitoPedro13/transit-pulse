// Package gtfs projects a retained GTFS ZIP into static route geometry and stop lists.
package gtfs

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"
)

const MaxZIPBytes = 64 << 20
const maxExpandedBytes = 256 << 20
const SourceURL = "https://dados.mobilidade.rio/gtfs/schedule"
const Attribution = "Secretaria Municipal de Transportes - SMTR, Prefeitura da Cidade do Rio de Janeiro - PCRJ"

type Feed struct {
	SHA256      string `json:"sha256"`
	Source      string `json:"source"`
	FetchedAt   string `json:"fetched_at"`
	ModifiedAt  string `json:"modified_at"`
	StartDate   string `json:"start_date"`
	EndDate     string `json:"end_date"`
	Attribution string `json:"attribution"`
}
type Route struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Agency string `json:"agency"`
	Type   int    `json:"type"`
}
type Shape struct {
	ID   string       `json:"id"`
	Path [][2]float64 `json:"path"`
}
type Stop struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	Position [2]float64 `json:"position"`
}
type Pattern struct {
	ID        string   `json:"id"`
	Route     string   `json:"route"`
	Direction string   `json:"direction"`
	Shape     string   `json:"shape"`
	Headsign  string   `json:"headsign"`
	Stops     []string `json:"stops"`
	Trips     int      `json:"trips"`
}
type Line struct {
	Schema   string    `json:"schema"`
	FeedHash string    `json:"feed_hash"`
	Line     string    `json:"line"`
	Routes   []Route   `json:"routes"`
	Patterns []Pattern `json:"patterns"`
	Shapes   []Shape   `json:"shapes"`
	Stops    []Stop    `json:"stops"`
}
type LineEntry struct {
	Line     string `json:"line"`
	File     string `json:"file"`
	Patterns int    `json:"patterns"`
}
type Manifest struct {
	Schema string      `json:"schema"`
	Feed   Feed        `json:"feed"`
	Lines  []LineEntry `json:"lines"`
}
type Projection struct {
	Manifest       Manifest
	Lines          []Line
	ExcludedRoutes int
}

type point struct {
	sequence int
	position [2]float64
}
type call struct {
	sequence int
	stop     string
}
type trip struct {
	route, direction, shape, headsign string
	calls                             []call
}
type route struct {
	Route
	short string
}

// Parse never extracts ZIP paths. Files are bounded before their CSV contents are read.
func Parse(body []byte, feed Feed) (*Projection, error) {
	if len(body) > MaxZIPBytes {
		return nil, fmt.Errorf("gtfs: ZIP exceeds %d bytes", MaxZIPBytes)
	}
	z, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, fmt.Errorf("gtfs: ZIP: %w", err)
	}
	files := map[string]*zip.File{}
	var total uint64
	for _, f := range z.File {
		if _, exists := files[f.Name]; exists {
			return nil, fmt.Errorf("gtfs: duplicate ZIP member %q", f.Name)
		}
		total += f.UncompressedSize64
		if total > maxExpandedBytes || len(z.File) > 128 {
			return nil, fmt.Errorf("gtfs: expanded ZIP exceeds limits")
		}
		files[f.Name] = f
	}
	read := func(name string, required []string, consume func(map[string]string) error) error {
		f := files[name]
		if f == nil {
			return fmt.Errorf("gtfs: missing %s", name)
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer func() { _ = rc.Close() }()
		r := csv.NewReader(io.LimitReader(rc, int64(f.UncompressedSize64)+1))
		header, err := r.Read()
		if err != nil {
			return fmt.Errorf("%s header: %w", name, err)
		}
		header[0] = strings.TrimPrefix(header[0], "\ufeff")
		seen := map[string]bool{}
		for _, h := range header {
			if seen[h] {
				return fmt.Errorf("%s: duplicate column %q", name, h)
			}
			seen[h] = true
		}
		for _, h := range required {
			if !seen[h] {
				return fmt.Errorf("%s: missing column %s", name, h)
			}
		}
		for n := 2; ; n++ {
			values, err := r.Read()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return fmt.Errorf("%s row %d: %w", name, n, err)
			}
			row := make(map[string]string, len(header))
			for i, h := range header {
				row[h] = values[i]
			}
			if err := consume(row); err != nil {
				return fmt.Errorf("%s row %d: %w", name, n, err)
			}
		}
	}
	unique := func(id string, exists bool) error {
		if id == "" || exists {
			return fmt.Errorf("empty or duplicate identifier %q", id)
		}
		return nil
	}
	agencies := map[string]string{}
	if err := read("agency.txt", []string{"agency_name"}, func(r map[string]string) error {
		id := r["agency_id"]
		if _, ok := agencies[id]; ok {
			return fmt.Errorf("duplicate agency %q", id)
		}
		agencies[id] = r["agency_name"]
		return nil
	}); err != nil {
		return nil, err
	}
	routes := map[string]route{}
	if err := read("routes.txt", []string{"route_id", "route_short_name", "route_long_name", "route_type"}, func(r map[string]string) error {
		id := r["route_id"]
		_, exists := routes[id]
		if err := unique(id, exists); err != nil {
			return err
		}
		t, err := sequence(r["route_type"])
		if err != nil {
			return err
		}
		agency, ok := agencies[r["agency_id"]]
		if !ok && r["agency_id"] == "" && len(agencies) == 1 {
			for _, a := range agencies {
				agency, ok = a, true
			}
		}
		if !ok {
			return fmt.Errorf("unknown agency %q", r["agency_id"])
		}
		routes[id] = route{Route: Route{ID: id, Name: r["route_long_name"], Agency: agency, Type: t}, short: r["route_short_name"]}
		return nil
	}); err != nil {
		return nil, err
	}
	stops := map[string]Stop{}
	stopIDs := map[string]bool{}
	if err := read("stops.txt", []string{"stop_id", "stop_name", "stop_lat", "stop_lon"}, func(r map[string]string) error {
		id := r["stop_id"]
		if err := unique(id, stopIDs[id]); err != nil {
			return err
		}
		stopIDs[id] = true
		// Stations and non-boarding locations may have no position; referenced boarding stops must.
		if r["stop_lat"] == "" && r["stop_lon"] == "" {
			return nil
		}
		p, err := position(r["stop_lon"], r["stop_lat"])
		if err != nil {
			return err
		}
		stops[id] = Stop{ID: id, Name: r["stop_name"], Position: p}
		return nil
	}); err != nil {
		return nil, err
	}
	points := map[string][]point{}
	if err := read("shapes.txt", []string{"shape_id", "shape_pt_sequence", "shape_pt_lat", "shape_pt_lon"}, func(r map[string]string) error {
		id := r["shape_id"]
		if id == "" {
			return fmt.Errorf("empty shape_id")
		}
		seq, err := sequence(r["shape_pt_sequence"])
		if err != nil {
			return err
		}
		p, err := position(r["shape_pt_lon"], r["shape_pt_lat"])
		if err != nil {
			return err
		}
		points[id] = append(points[id], point{seq, p})
		return nil
	}); err != nil {
		return nil, err
	}
	shapes := map[string]Shape{}
	for id, ps := range points {
		slices.SortFunc(ps, func(a, b point) int { return a.sequence - b.sequence })
		path := make([][2]float64, len(ps))
		for i, p := range ps {
			if i > 0 && p.sequence == ps[i-1].sequence {
				return nil, fmt.Errorf("shape %s: duplicate sequence", id)
			}
			path[i] = p.position
		}
		if len(path) < 2 {
			return nil, fmt.Errorf("shape %s: fewer than two points", id)
		}
		shapes[id] = Shape{ID: id, Path: path}
	}
	trips := map[string]*trip{}
	if err := read("trips.txt", []string{"trip_id", "route_id"}, func(r map[string]string) error {
		id := r["trip_id"]
		_, exists := trips[id]
		if err := unique(id, exists); err != nil {
			return err
		}
		if _, ok := routes[r["route_id"]]; !ok {
			return fmt.Errorf("unknown route %q", r["route_id"])
		}
		if s := r["shape_id"]; s != "" {
			if _, ok := shapes[s]; !ok {
				return fmt.Errorf("unknown shape %q", s)
			}
		}
		if d := r["direction_id"]; d != "" && d != "0" && d != "1" {
			return fmt.Errorf("invalid direction %q", d)
		}
		trips[id] = &trip{route: r["route_id"], direction: r["direction_id"], shape: r["shape_id"], headsign: r["trip_headsign"]}
		return nil
	}); err != nil {
		return nil, err
	}
	if err := read("stop_times.txt", []string{"trip_id", "stop_id", "stop_sequence"}, func(r map[string]string) error {
		t, ok := trips[r["trip_id"]]
		if !ok {
			return fmt.Errorf("unknown trip %q", r["trip_id"])
		}
		if _, ok := stops[r["stop_id"]]; !ok {
			return fmt.Errorf("unknown or unpositioned stop %q", r["stop_id"])
		}
		seq, err := sequence(r["stop_sequence"])
		if err != nil {
			return err
		}
		t.calls = append(t.calls, call{seq, r["stop_id"]})
		return nil
	}); err != nil {
		return nil, err
	}
	if files["feed_info.txt"] != nil {
		if err := read("feed_info.txt", []string{"feed_publisher_name"}, func(r map[string]string) error {
			feed.StartDate = r["feed_start_date"]
			feed.EndDate = r["feed_end_date"]
			return nil
		}); err != nil {
			return nil, err
		}
	}
	hash := sha256.Sum256(body)
	feed.SHA256 = hex.EncodeToString(hash[:])
	feed.Source = SourceURL
	feed.Attribution = Attribution
	projection := &Projection{Manifest: Manifest{Schema: "line-routes-index.v1", Feed: feed, Lines: []LineEntry{}}}
	groups := map[string]map[string]*Pattern{}
	for _, t := range trips {
		r := routes[t.route]
		if r.short == "" || t.shape == "" {
			continue
		}
		slices.SortFunc(t.calls, func(a, b call) int { return a.sequence - b.sequence })
		ids := make([]string, len(t.calls))
		for i, c := range t.calls {
			if i > 0 && c.sequence == t.calls[i-1].sequence {
				return nil, fmt.Errorf("route %s: duplicate stop sequence", t.route)
			}
			ids[i] = c.stop
		}
		if len(ids) == 0 {
			return nil, fmt.Errorf("route %s: trip without stops", t.route)
		}
		keyBytes, _ := json.Marshal([]any{t.route, t.direction, t.shape, ids})
		key := sha256.Sum256(keyBytes)
		id := hex.EncodeToString(key[:])
		if groups[r.short] == nil {
			groups[r.short] = map[string]*Pattern{}
		}
		if p := groups[r.short][id]; p != nil {
			p.Trips++
			if t.headsign < p.Headsign {
				p.Headsign = t.headsign
			}
		} else {
			groups[r.short][id] = &Pattern{ID: id, Route: t.route, Direction: t.direction, Shape: t.shape, Headsign: t.headsign, Stops: ids, Trips: 1}
		}
	}
	for _, r := range routes {
		if r.short == "" {
			projection.ExcludedRoutes++
		}
	}
	lineNames := sortedKeys(groups)
	for _, name := range lineNames {
		line := Line{Schema: "line-routes.v1", FeedHash: feed.SHA256, Line: name, Routes: []Route{}, Patterns: []Pattern{}, Shapes: []Shape{}, Stops: []Stop{}}
		routeIDs, shapeIDs, stopIDs := map[string]bool{}, map[string]bool{}, map[string]bool{}
		for _, id := range sortedKeys(groups[name]) {
			p := groups[name][id]
			line.Patterns = append(line.Patterns, *p)
			routeIDs[p.Route] = true
			shapeIDs[p.Shape] = true
			for _, id := range p.Stops {
				stopIDs[id] = true
			}
		}
		for _, id := range sortedKeys(routeIDs) {
			line.Routes = append(line.Routes, routes[id].Route)
		}
		for _, id := range sortedKeys(shapeIDs) {
			line.Shapes = append(line.Shapes, shapes[id])
		}
		for _, id := range sortedKeys(stopIDs) {
			line.Stops = append(line.Stops, stops[id])
		}
		projection.Lines = append(projection.Lines, line)
		nameHash := sha256.Sum256([]byte(name))
		file := "/gtfs/" + feed.SHA256 + "/" + hex.EncodeToString(nameHash[:]) + ".json"
		projection.Manifest.Lines = append(projection.Manifest.Lines, LineEntry{Line: name, File: file, Patterns: len(line.Patterns)})
	}
	if len(projection.Lines) == 0 {
		return nil, fmt.Errorf("gtfs: no line geometry")
	}
	return projection, nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
func sequence(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 || n > math.MaxInt32 {
		return 0, fmt.Errorf("invalid sequence/type %q", s)
	}
	return n, nil
}
func position(lon, lat string) ([2]float64, error) {
	x, e1 := strconv.ParseFloat(lon, 64)
	y, e2 := strconv.ParseFloat(lat, 64)
	if e1 != nil || e2 != nil || math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) || x < -180 || x > 180 || y < -90 || y > 90 {
		return [2]float64{}, fmt.Errorf("invalid coordinate (%s,%s)", lon, lat)
	}
	return [2]float64{x, y}, nil
}
