package its

import (
	"encoding/json"
	"testing"

	"github.com/BenitoPedro13/transit-pulse/packages/contracts"
	"github.com/BenitoPedro13/transit-pulse/packages/event"
	"github.com/BenitoPedro13/transit-pulse/packages/raw"
)

// The conecta sample was captured with fornecedor=conecta, so its providers_status lists only
// conecta: the other three feeds have no provider status and count as provider_not_ok.
func TestMinuteStatusesOverARealSample(t *testing.T) {
	_, st, err := MinuteEvents([]raw.Envelope{sampleEnvelope(t, "conecta")})
	if err != nil {
		t.Fatal(err)
	}
	keys := []string{"its/v1/2026/10/12/13.raw.gz"}
	got := MinuteStatuses(testMinute, st, keys)
	if len(got) != 4 {
		t.Fatalf("got %d statuses, want one per mapped feed", len(got))
	}
	for _, s := range got {
		b, _ := json.Marshal(s)
		contracts.MustValidate(t, "feed-status.v1.schema.json", b)
		if s.MinuteUTC != testMinute || s.Operator != feedOperators[s.Feed] {
			t.Errorf("%+v: wrong minute or operator", s)
		}
		if s.Feed == "its:conecta" {
			if s.Status != event.StatusOK || s.ProviderStatus == nil || *s.ProviderStatus != "ok" ||
				s.Records != 40 || s.Events != 40 {
				t.Errorf("conecta = %+v", s)
			}
			continue
		}
		if s.Status != event.StatusProviderNotOK || s.ProviderStatus != nil || s.Records != 0 {
			t.Errorf("%s = %+v, want provider_not_ok with no provider status and zero counts", s.Feed, s)
		}
	}
}

func TestEmptyStatuses(t *testing.T) {
	got := EmptyStatuses(testMinute, event.StatusGap, nil)
	if len(got) != 4 {
		t.Fatalf("got %d statuses, want 4", len(got))
	}
	for _, s := range got {
		b, _ := json.Marshal(s)
		contracts.MustValidate(t, "feed-status.v1.schema.json", b)
		if s.Status != event.StatusGap || s.Records != 0 || s.Events != 0 || s.RawKeys == nil || len(s.RawKeys) != 0 {
			t.Errorf("%+v", s)
		}
	}
}
