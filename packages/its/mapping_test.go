package its

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/BenitoPedro13/transit-pulse/packages/contracts"
	"github.com/BenitoPedro13/transit-pulse/packages/event"
	"github.com/BenitoPedro13/transit-pulse/packages/raw"
	"github.com/BenitoPedro13/transit-pulse/packages/samples"
)

const testMinute = "2026-10-12T13:38:00Z"

var fetchedAt = time.Date(2026, 10, 12, 13, 41, 2, 0, time.UTC)

func envelopeOf(body []byte, page int) raw.Envelope {
	return raw.Envelope{
		Header: raw.Header{
			V: 1, Source: "its", MinuteUTC: testMinute, Page: page, Status: 200,
			FetchedAt: fetchedAt, BodyLen: len(body),
		},
		Body: body,
	}
}

func sampleEnvelope(t *testing.T, supplier string) raw.Envelope {
	t.Helper()
	return envelopeOf(samples.MustRead(t, "smtr-its-20261012-veiculos-"+supplier+".json"), 0)
}

// E1. The expected numbers were derived by reading the four samples (a script over the JSON, not
// this code): 40 records each, all mapped, valid and distinct. Only the sonda sample has a record
// without a heading (7003, direcao null), and zirix/maxtrack have empty servico on 5 records.
func TestEventsOverTheRealSamples(t *testing.T) {
	cases := []struct {
		supplier, feed, operator, mode   string
		nullHeading, nullLine, nullRoute int
	}{
		{"conecta", "its:conecta", "rio-sppo", "bus", 0, 0, 0},
		{"zirix", "its:zirix", "rio-sppo", "bus", 0, 5, 40},
		{"maxtrack", "its:maxtrack", "rio-sistema-rio", "bus", 0, 5, 5},
		{"sonda", "its:sonda", "rio-brt", "brt", 1, 0, 40},
	}
	for _, c := range cases {
		t.Run(c.supplier, func(t *testing.T) {
			evs, st, err := Events(sampleEnvelope(t, c.supplier))
			if err != nil {
				t.Fatal(err)
			}
			fs := st.Feeds[c.feed]
			if fs == nil || len(st.Feeds) != 1 {
				t.Fatalf("feeds = %v, want only %s", st.Feeds, c.feed)
			}
			want := FeedStats{Records: 40, Events: 40, NullHeading: c.nullHeading}
			if *fs != want {
				t.Errorf("stats = %+v, want %+v", *fs, want)
			}
			if st.ProviderStatus[c.feed] != "ok" {
				t.Errorf("provider status = %q", st.ProviderStatus[c.feed])
			}
			var nullLine, nullRoute int
			for _, e := range evs {
				if e.Operator != c.operator || e.Mode != c.mode || e.Feed != c.feed || e.City != "rio" || e.SourceKind != "live" || e.Authority != "city" {
					t.Fatalf("wrong constants on %+v", e)
				}
				if !e.ReceivedTime.Equal(fetchedAt) {
					t.Errorf("received_time = %v, want the envelope's fetched_at", e.ReceivedTime)
				}
				if e.Line == nil {
					nullLine++
				}
				if e.RouteID == nil {
					nullRoute++
				}
				b, _ := json.Marshal(e)
				contracts.MustValidate(t, "position.v1.schema.json", b) // E2
			}
			if nullLine != c.nullLine || nullRoute != c.nullRoute {
				t.Errorf("null line/route = %d/%d, want %d/%d", nullLine, nullRoute, c.nullLine, c.nullRoute)
			}
		})
	}
}

func TestKnownRecords(t *testing.T) {
	evs, _, err := Events(sampleEnvelope(t, "conecta"))
	if err != nil {
		t.Fatal(err)
	}
	var a48011 *event.Position
	for i := range evs {
		if evs[i].Vehicle == "A48011" {
			a48011 = &evs[i]
		}
	}
	if a48011 == nil {
		t.Fatal("A48011 not found")
	}
	if got, want := a48011.EventTime, time.Date(2026, 10, 12, 13, 37, 43, 0, time.UTC); !got.Equal(want) {
		t.Errorf("event_time = %v, want %v", got, want)
	}
	if a48011.Operator != "rio-sppo" || a48011.Feed != "its:conecta" {
		t.Errorf("operator/feed = %s/%s", a48011.Operator, a48011.Feed)
	}
	if a48011.Line == nil || *a48011.Line != "426" || a48011.RouteID == nil || *a48011.RouteID != "O0426AAA0A" {
		t.Errorf("line/route = %v/%v", a48011.Line, a48011.RouteID)
	}
	if a48011.SpeedKmh == nil || *a48011.SpeedKmh != 7 || a48011.HeadingDeg == nil || *a48011.HeadingDeg != 5 {
		t.Errorf("speed/heading = %v/%v", a48011.SpeedKmh, a48011.HeadingDeg)
	}
	if a48011.RawRef != (event.RawRef{Key: "its/v1/2026/10/12/13.raw.gz", MinuteUTC: testMinute, Page: 0}) {
		t.Errorf("raw_ref = %+v", a48011.RawRef)
	}

	evs, _, _ = Events(sampleEnvelope(t, "sonda"))
	for _, e := range evs {
		if e.Mode != "brt" || e.RouteID != nil {
			t.Fatalf("sonda event %s: mode %s, route_id %v", e.Vehicle, e.Mode, e.RouteID)
		}
	}
}

// withExtras returns a real sample body with extra records appended. Each extra is a copy of the
// sample's first record with some fields changed, so the shape stays what the provider sends.
func withExtras(t *testing.T, supplier string, mutate ...func(map[string]any)) []byte {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(samples.MustRead(t, "smtr-its-20261012-veiculos-"+supplier+".json"), &body); err != nil {
		t.Fatal(err)
	}
	data := body["data"].([]any)
	for _, m := range mutate {
		cp := map[string]any{}
		for k, v := range data[0].(map[string]any) {
			cp[k] = v
		}
		m(cp)
		data = append(data, cp)
	}
	body["data"] = data
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestDuplicatesInvalidAndUnmapped(t *testing.T) {
	set := func(k string, v any) func(map[string]any) { return func(m map[string]any) { m[k] = v } }
	// Copies of record 0 of the conecta sample, with one change each.
	body := withExtras(t, "conecta",
		func(map[string]any) {},                          // exact duplicate
		func(m map[string]any) { m["latitude"] = -22.5 }, // same identity, other position
		set("id_veiculo", ""),                            // invalid: no vehicle
		set("datetime", "not a time"),                    // invalid: unparseable
		set("datetime", nil),                             // invalid: no time
		func(m map[string]any) { m["latitude"], m["longitude"] = 0.0, 0.0 }, // invalid: null island
		set("latitude", 91.0),      // invalid: out of range
		set("fornecedor", "newco"), // unmapped supplier
		set("modo", "metro"),       // unmapped triple, mapped supplier
		func(m map[string]any) { m["id_veiculo"] = "X1"; m["velocidade"] = -3.0; m["direcao"] = 906.0 },
		func(m map[string]any) { m["id_veiculo"] = "X2"; m["servico"] = "0"; m["route_id"] = "" },
	)
	evs, st, err := Events(envelopeOf(body, 0))
	if err != nil {
		t.Fatal(err)
	}
	conecta := *st.Feeds["its:conecta"]
	want := FeedStats{Records: 40 + 10, Events: 40 + 2, Invalid: 5, Unmapped: 1, Dups: 1, ConflictingDuplicates: 1, NullSpeed: 1, NullHeading: 1}
	// The mapped-supplier unmapped triple (modo=metro) lands under conecta too.
	want.Unmapped = 1
	if conecta != want {
		t.Errorf("conecta = %+v\nwant     %+v", conecta, want)
	}
	if got := st.Feeds["its:newco"]; got == nil || *got != (FeedStats{Records: 1, Unmapped: 1}) {
		t.Errorf("newco = %+v", got)
	}
	if len(st.UnmappedTriples) != 2 {
		t.Errorf("unmapped triples = %v, want 2 distinct", st.UnmappedTriples)
	}
	if len(st.Conflicts) != 1 {
		t.Errorf("conflicts logged = %v", st.Conflicts)
	}

	byVehicle := map[string]event.Position{}
	for _, e := range evs {
		byVehicle[e.Vehicle] = e
	}
	if x := byVehicle["X1"]; x.SpeedKmh != nil || x.HeadingDeg != nil {
		t.Errorf("out-of-range speed/heading not nulled: %v/%v", x.SpeedKmh, x.HeadingDeg)
	}
	// "0" is kept as given; "" becomes null.
	if x := byVehicle["X2"]; x.Line == nil || *x.Line != "0" || x.RouteID != nil {
		t.Errorf("line/route = %v/%v", x.Line, x.RouteID)
	}
	// The first of a conflicting pair is kept.
	if e := byVehicle["A48011"]; e.Lat != -22.91397 {
		t.Errorf("A48011 lat = %v, want the first record's -22.91397", e.Lat)
	}
}

func TestDuplicatesAcrossPagesAreDroppedByMinuteEvents(t *testing.T) {
	a := sampleEnvelope(t, "sonda")
	b := a
	b.Page = 1
	evs, st, err := MinuteEvents([]raw.Envelope{a, b})
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 40 || st.Feeds["its:sonda"].Dups != 40 || st.Records != 80 {
		t.Errorf("events = %d, dups = %d, records = %d; want 40, 40, 80", len(evs), st.Feeds["its:sonda"].Dups, st.Records)
	}
}

func TestEventsIsDeterministic(t *testing.T) {
	env := sampleEnvelope(t, "maxtrack")
	var runs [2][]byte
	for i := range runs {
		evs, _, err := Events(env)
		if err != nil {
			t.Fatal(err)
		}
		runs[i], _ = json.Marshal(evs)
	}
	if !bytes.Equal(runs[0], runs[1]) {
		t.Error("the same envelope produced different event sets")
	}
}

func TestEventsRejectsUnusableEnvelopes(t *testing.T) {
	failed := envelopeOf([]byte(`{}`), 0)
	failed.Status = 503
	if _, _, err := Events(failed); err == nil {
		t.Error("a 503 envelope produced events")
	}
	if _, _, err := Events(envelopeOf([]byte(`not json`), 0)); err == nil {
		t.Error("a non-JSON body produced events")
	}
}

func TestProviderStatusNotOK(t *testing.T) {
	var body map[string]any
	_ = json.Unmarshal(samples.MustRead(t, "smtr-its-20261012-veiculos-conecta.json"), &body)
	body["providers_status"] = map[string]any{"conecta": "degraded", "zirix": "ok"}
	b, _ := json.Marshal(body)
	_, st, err := Events(envelopeOf(b, 0))
	if err != nil {
		t.Fatal(err)
	}
	if st.ProviderStatus["its:conecta"] != "degraded" || st.ProviderStatus["its:zirix"] != "ok" {
		t.Errorf("provider status = %v", st.ProviderStatus)
	}
}

func TestMappingTable(t *testing.T) {
	if got := MappedFeeds(); len(got) != 4 || got[0] != "its:conecta" || got[3] != "its:zirix" {
		t.Errorf("mapped feeds = %v", got)
	}
	if feedOperators["its:conecta"] != "rio-sppo" || feedOperators["its:zirix"] != "rio-sppo" ||
		feedOperators["its:maxtrack"] != "rio-sistema-rio" || feedOperators["its:sonda"] != "rio-brt" {
		t.Errorf("feed operators = %v", feedOperators)
	}
}
