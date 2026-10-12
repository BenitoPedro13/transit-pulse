package event_test

import (
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/BenitoPedro13/transit-pulse/packages/contracts"
	"github.com/BenitoPedro13/transit-pulse/packages/event"
)

func ptr[T any](v T) *T { return &v }

func samplePosition() event.Position {
	return event.Position{
		City: "rio", Mode: "bus", Operator: "rio-sppo", Authority: "city", SourceKind: "live",
		Feed: "its:conecta", Vehicle: "A48011", Line: ptr("426"), RouteID: ptr("O0426AAA0A"),
		Lat: -22.91397, Lon: -43.20468,
		EventTime:    time.Date(2026, 10, 12, 13, 37, 43, 0, time.UTC),
		ReceivedTime: time.Date(2026, 10, 12, 13, 41, 2, 0, time.UTC),
		SpeedKmh:     ptr(7.0), HeadingDeg: ptr(5.0),
		RawRef: event.RawRef{Key: "its/v1/2026/10/12/13.raw.gz", MinuteUTC: "2026-10-12T13:38:00Z", Page: 0},
	}
}

func TestIdentityAndKey(t *testing.T) {
	p := samplePosition()
	if got, want := p.Key(), "rio-sppo/its:conecta/A48011"; got != want {
		t.Errorf("Key() = %q, want %q", got, want)
	}
	if got, want := p.Identity(), "rio-sppo/its:conecta/A48011/2026-10-12T13:37:43Z"; got != want {
		t.Errorf("Identity() = %q, want %q", got, want)
	}
	// The same instant in another zone has the same identity.
	q := p
	q.EventTime = p.EventTime.In(time.FixedZone("brt", -3*3600))
	if q.Identity() != p.Identity() {
		t.Errorf("Identity() depends on the time zone: %q vs %q", q.Identity(), p.Identity())
	}
}

func TestPositionValidatesAgainstSchema(t *testing.T) {
	p := samplePosition()
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	contracts.MustValidate(t, "position.v1.schema.json", b)

	// Nullable fields marshal as null and still validate.
	p.Line, p.RouteID, p.SpeedKmh, p.HeadingDeg = nil, nil, nil, nil
	b, _ = json.Marshal(p)
	contracts.MustValidate(t, "position.v1.schema.json", b)
}

func TestSchemaRejectsBadPositions(t *testing.T) {
	good := samplePosition()
	cases := map[string]func(*event.Position){
		"lat out of range":    func(p *event.Position) { p.Lat = 91 },
		"unknown mode":        func(p *event.Position) { p.Mode = "hovercraft" },
		"feed without colon":  func(p *event.Position) { p.Feed = "conecta" },
		"empty vehicle":       func(p *event.Position) { p.Vehicle = "" },
		"negative speed":      func(p *event.Position) { p.SpeedKmh = ptr(-1.0) },
		"heading above 360":   func(p *event.Position) { p.HeadingDeg = ptr(906.0) },
		"minute not on the 0": func(p *event.Position) { p.RawRef.MinuteUTC = "2026-10-12T13:38:05Z" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			p := good
			mutate(&p)
			b, _ := json.Marshal(p)
			if err := contracts.Validate("position.v1.schema.json", b); err == nil {
				t.Errorf("schema accepted an invalid position: %s", b)
			}
		})
	}
}

func TestFeedStatusValidatesAgainstSchema(t *testing.T) {
	s := event.FeedStatus{
		Feed: "its:conecta", Operator: "rio-sppo", MinuteUTC: "2026-10-12T13:38:00Z", Status: event.StatusOK,
		Records: 40, Events: 40, ProviderStatus: ptr("ok"), RawKeys: []string{"its/v1/2026/10/12/13.raw.gz"},
	}
	b, _ := json.Marshal(s)
	contracts.MustValidate(t, "feed-status.v1.schema.json", b)

	s.Status, s.ProviderStatus, s.RawKeys = event.StatusFailed, nil, []string{}
	b, _ = json.Marshal(s)
	contracts.MustValidate(t, "feed-status.v1.schema.json", b)

	s.Status = "maybe"
	b, _ = json.Marshal(s)
	if err := contracts.Validate("feed-status.v1.schema.json", b); err == nil {
		t.Error("schema accepted an unknown status")
	}
}

// jsonFields returns the JSON field names of a struct type, from its tags.
func jsonFields(t reflect.Type) []string {
	var out []string
	for i := range t.NumField() {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}

func schemaProperties(t *testing.T, schema string, path ...string) []string {
	t.Helper()
	p, err := contracts.Schema(schema)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var node map[string]any
	if err := json.Unmarshal(raw, &node); err != nil {
		t.Fatal(err)
	}
	for _, step := range path {
		node = node["properties"].(map[string]any)[step].(map[string]any)
	}
	props := node["properties"].(map[string]any)
	var out []string
	for k := range props {
		out = append(out, k)
	}
	slices.Sort(out)
	// Every property must also be required, so a field can't silently become optional.
	var required []string
	for _, r := range node["required"].([]any) {
		required = append(required, r.(string))
	}
	slices.Sort(required)
	if !slices.Equal(out, required) {
		t.Errorf("%s: properties %v != required %v", schema, out, required)
	}
	return out
}

func TestGoTypesMatchSchemaFields(t *testing.T) {
	check := func(name string, got, want []string) {
		if !slices.Equal(got, want) {
			t.Errorf("%s: Go fields %v != schema properties %v", name, got, want)
		}
	}
	check("Position", jsonFields(reflect.TypeFor[event.Position]()), schemaProperties(t, "position.v1.schema.json"))
	check("RawRef", jsonFields(reflect.TypeFor[event.RawRef]()), schemaProperties(t, "position.v1.schema.json", "raw_ref"))
	check("FeedStatus", jsonFields(reflect.TypeFor[event.FeedStatus]()), schemaProperties(t, "feed-status.v1.schema.json"))
}
