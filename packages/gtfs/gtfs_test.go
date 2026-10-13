package gtfs

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/BenitoPedro13/transit-pulse/packages/contracts"
	"github.com/BenitoPedro13/transit-pulse/packages/samples"
)

func fixture(t *testing.T) []byte {
	t.Helper()
	return samples.MustRead(t, "smtr-20261012-gtfs-trimmed.zip")
}
func testFeed() Feed { return Feed{FetchedAt: "2026-10-12T13:22:07Z"} }
func sourceRows(t *testing.T, body []byte, name string) [][]string {
	t.Helper()
	z, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range z.File {
		if f.Name == name {
			r, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = r.Close() }()
			rows, err := csv.NewReader(r).ReadAll()
			if err != nil {
				t.Fatal(err)
			}
			return rows
		}
	}
	t.Fatalf("missing %s", name)
	return nil
}

// Independent CSV oracle: indexed directly, without using the production CSV reader.
func oracle(t *testing.T, body []byte, name string) []map[string]string {
	t.Helper()
	rows := sourceRows(t, body, name)
	result := make([]map[string]string, 0, len(rows)-1)
	for _, row := range rows[1:] {
		r := map[string]string{}
		for i, h := range rows[0] {
			r[h] = row[i]
		}
		result = append(result, r)
	}
	return result
}
func mutate(t *testing.T, body []byte, name string, change func([][]string) [][]string) []byte {
	t.Helper()
	z, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, f := range z.File {
		out, err := w.Create(f.Name)
		if err != nil {
			t.Fatal(err)
		}
		if f.Name == name {
			c := csv.NewWriter(out)
			if err := c.WriteAll(change(sourceRows(t, body, name))); err != nil {
				t.Fatal(err)
			}
		} else {
			in, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			_, err = io.Copy(out, in)
			_ = in.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
func column(rows [][]string, name string) int { return slices.Index(rows[0], name) }

func TestParseAndPublish(t *testing.T) {
	body := fixture(t)
	p, err := Parse(body, testFeed())
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Lines) != 3 {
		t.Fatalf("lines %d", len(p.Lines))
	}
	points := map[string][]map[string]string{}
	for _, r := range oracle(t, body, "shapes.txt") {
		points[r["shape_id"]] = append(points[r["shape_id"]], r)
	}
	stopByID := map[string]map[string]string{}
	for _, r := range oracle(t, body, "stops.txt") {
		stopByID[r["stop_id"]] = r
	}
	tripByID := map[string]map[string]string{}
	for _, r := range oracle(t, body, "trips.txt") {
		tripByID[r["trip_id"]] = r
	}
	calls := map[string][]map[string]string{}
	for _, r := range oracle(t, body, "stop_times.txt") {
		calls[r["trip_id"]] = append(calls[r["trip_id"]], r)
	}
	integer := func(s string) int {
		n, e := strconv.Atoi(s)
		if e != nil {
			t.Fatal(e)
		}
		return n
	}
	number := func(s string) float64 {
		n, e := strconv.ParseFloat(s, 64)
		if e != nil {
			t.Fatal(e)
		}
		return n
	}
	for _, line := range p.Lines {
		b, err := json.Marshal(line)
		if err != nil {
			t.Fatal(err)
		}
		contracts.MustValidate(t, "line-routes.v1.schema.json", b)
		if line.Line == "22" && (len(line.Shapes) != 4 || len(line.Patterns) != 4) {
			t.Fatalf("BRT variants lost: %d shapes, %d patterns", len(line.Shapes), len(line.Patterns))
		}
		for _, s := range line.Shapes {
			rs := points[s.ID]
			slices.SortFunc(rs, func(a, b map[string]string) int {
				return integer(a["shape_pt_sequence"]) - integer(b["shape_pt_sequence"])
			})
			if len(rs) != len(s.Path) {
				t.Fatal("point count")
			}
			for i, r := range rs {
				if s.Path[i] != [2]float64{number(r["shape_pt_lon"]), number(r["shape_pt_lat"])} {
					t.Fatalf("coordinate differs %s/%d", s.ID, i)
				}
			}
		}
		for _, s := range line.Stops {
			r := stopByID[s.ID]
			if s.Name != r["stop_name"] || s.Position != [2]float64{number(r["stop_lon"]), number(r["stop_lat"])} {
				t.Fatalf("stop differs %s", s.ID)
			}
		}
		for _, p := range line.Patterns {
			count := 0
			for id, tr := range tripByID {
				if tr["route_id"] != p.Route || tr["direction_id"] != p.Direction || tr["shape_id"] != p.Shape {
					continue
				}
				rs := calls[id]
				slices.SortFunc(rs, func(a, b map[string]string) int { return integer(a["stop_sequence"]) - integer(b["stop_sequence"]) })
				ids := make([]string, len(rs))
				for i, r := range rs {
					ids[i] = r["stop_id"]
				}
				if reflect.DeepEqual(ids, p.Stops) {
					count++
				}
			}
			if count != p.Trips {
				t.Fatalf("stop sequence/trip count differs %s", p.ID)
			}
		}
	}
	dir := t.TempDir()
	if err := Publish(dir, p); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(filepath.Join(dir, "index.json"))
	if err != nil {
		t.Fatal(err)
	}
	contracts.MustValidate(t, "line-routes-index.v1.schema.json", first)
	if err := Publish(dir, p); err != nil {
		t.Fatal(err)
	}
	next, _ := os.ReadFile(filepath.Join(dir, "index.json"))
	if !bytes.Equal(first, next) {
		t.Fatal("non-deterministic manifest")
	}
	for _, entry := range p.Manifest.Lines {
		if _, err := os.Stat(filepath.Join(dir, strings.TrimPrefix(entry.File, "/gtfs/"))); err != nil {
			t.Fatal(err)
		}
	}
	// A damaged content-addressed directory cannot cause a new manifest to be advertised.
	file := filepath.Join(dir, strings.TrimPrefix(p.Manifest.Lines[0].File, "/gtfs/"))
	if err := os.WriteFile(file, []byte("broken"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := Publish(dir, p); err == nil {
		t.Fatal("accepted inconsistent existing export")
	}
	next, _ = os.ReadFile(filepath.Join(dir, "index.json"))
	if !bytes.Equal(first, next) {
		t.Fatal("manifest changed after failure")
	}
	bad := bytes.ReplaceAll(first, []byte(p.Manifest.Feed.SHA256), []byte("bad"))
	if contracts.Validate("line-routes-index.v1.schema.json", bad) == nil {
		t.Fatal("schema accepted malformed hash")
	}
}

func TestArchiveBoundaries(t *testing.T) {
	body := fixture(t)
	cases := []struct {
		name, file string
		change     func([][]string) [][]string
		want       string
	}{
		{"missing column", "shapes.txt", func(r [][]string) [][]string { r[0][column(r, "shape_pt_lat")] = "unknown"; return r }, "missing column"},
		{"invalid coordinate", "shapes.txt", func(r [][]string) [][]string { r[1][column(r, "shape_pt_lon")] = "NaN"; return r }, "invalid coordinate"},
		{"unknown stop", "stop_times.txt", func(r [][]string) [][]string { r[1][column(r, "stop_id")] = "absent"; return r }, "unknown or unpositioned stop"},
		{"unknown shape", "trips.txt", func(r [][]string) [][]string { r[1][column(r, "shape_id")] = "absent"; return r }, "unknown shape"},
		{"duplicate sequence", "shapes.txt", func(r [][]string) [][]string { r[2] = slices.Clone(r[1]); return r }, "duplicate sequence"},
		{"negative sequence", "stop_times.txt", func(r [][]string) [][]string { r[1][column(r, "stop_sequence")] = "-1"; return r }, "invalid sequence"},
		{"duplicate trip", "trips.txt", func(r [][]string) [][]string { r[2][column(r, "trip_id")] = r[1][column(r, "trip_id")]; return r }, "duplicate identifier"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(mutate(t, body, tc.file, tc.change), testFeed())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %v, want %s", err, tc.want)
			}
		})
	}
	t.Run("BOM reordered columns rows and unknown field", func(t *testing.T) {
		changed := mutate(t, body, "shapes.txt", func(r [][]string) [][]string {
			for i := range r {
				slices.Reverse(r[i])
				r[i] = append(r[i], "ignored")
			}
			r[0][0] = "\ufeff" + r[0][0]
			slices.Reverse(r[1:])
			return r
		})
		p, err := Parse(changed, testFeed())
		if err != nil {
			t.Fatal(err)
		}
		original, _ := Parse(body, testFeed())
		for i := range p.Lines {
			if !reflect.DeepEqual(p.Lines[i].Shapes, original.Lines[i].Shapes) {
				t.Fatal("reordering changed geometry")
			}
		}
	})
	t.Run("loop repeats remain in sequence", func(t *testing.T) {
		changed := mutate(t, body, "stop_times.txt", func(r [][]string) [][]string {
			target := r[1][column(r, "trip_id")]
			last := -1
			for i, row := range r[1:] {
				if row[column(r, "trip_id")] == target {
					last = i + 1
				}
			}
			r[last][column(r, "stop_id")] = r[1][column(r, "stop_id")]
			return r
		})
		p, err := Parse(changed, testFeed())
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, l := range p.Lines {
			for _, p := range l.Patterns {
				if len(p.Stops) > 1 && p.Stops[0] == p.Stops[len(p.Stops)-1] {
					found = true
				}
			}
		}
		if !found {
			t.Fatal("loop repeat lost")
		}
	})
	if _, err := Parse([]byte("not a zip"), testFeed()); err == nil {
		t.Fatal("accepted non-ZIP")
	}
}
