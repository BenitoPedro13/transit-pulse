package its

import "github.com/BenitoPedro13/transit-pulse/packages/event"

// MinuteStatuses builds the feed-status message of every mapped feed for a minute whose page
// chain is complete. The status is ok, or provider_not_ok when the provider's status for the feed
// is absent or not "ok". The adapter and the rebuild both call it, so a minute gets the same
// status either way.
func MinuteStatuses(minute string, st *Stats, rawKeys []string) []event.FeedStatus {
	var out []event.FeedStatus
	for _, f := range MappedFeeds() {
		fs := st.Feeds[f]
		if fs == nil {
			fs = &FeedStats{}
		}
		s := event.FeedStatus{
			Feed: f, Operator: feedOperators[f], MinuteUTC: minute, Status: event.StatusOK,
			Records: fs.Records, Events: fs.Events, Invalid: fs.Invalid, Unmapped: fs.Unmapped,
			ConflictingDuplicates: fs.ConflictingDuplicates, RawKeys: rawKeys,
		}
		if ps, ok := st.ProviderStatus[f]; ok {
			s.ProviderStatus = &ps
		}
		if s.ProviderStatus == nil || *s.ProviderStatus != "ok" {
			s.Status = event.StatusProviderNotOK
		}
		out = append(out, s)
	}
	return out
}

// EmptyStatuses builds one message per mapped feed with zero counts, for a minute with no complete
// chain: failed (our fetch failed) or gap (never captured, and too old to fetch). When a fetch
// fails the supplier list is not known, so every mapped feed gets a message (invariant 6).
func EmptyStatuses(minute, status string, rawKeys []string) []event.FeedStatus {
	if rawKeys == nil {
		rawKeys = []string{}
	}
	var out []event.FeedStatus
	for _, f := range MappedFeeds() {
		out = append(out, event.FeedStatus{
			Feed: f, Operator: feedOperators[f], MinuteUTC: minute, Status: status, RawKeys: rawKeys,
		})
	}
	return out
}
