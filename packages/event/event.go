// Package event holds the canonical event types that provider adapters emit. The JSON Schema
// files in packages/contracts are the contract; these structs mirror them by hand, and
// event_test.go fails if the two drift apart.
//
// Delivery is at-least-once: after a crash an adapter re-publishes from its checkpoint, so a
// topic can hold the same event twice. Consumers de-duplicate on Position.Identity().
package event

import "time"

// RawRef says where the source bytes of an event live (invariant 2). Key is the raw-log object;
// MinuteUTC and Page pick the response inside it.
type RawRef struct {
	Key       string `json:"key"`
	MinuteUTC string `json:"minute_utc"`
	Page      int    `json:"page"`
}

// Position is position.v1: one vehicle fix. Nothing downstream may branch on Feed.
type Position struct {
	City         string    `json:"city"`
	Mode         string    `json:"mode"`
	Operator     string    `json:"operator"`
	Authority    string    `json:"authority"`
	SourceKind   string    `json:"source_kind"`
	Feed         string    `json:"feed"`
	Vehicle      string    `json:"vehicle"`
	Line         *string   `json:"line"`
	RouteID      *string   `json:"route_id"`
	Lat          float64   `json:"lat"`
	Lon          float64   `json:"lon"`
	EventTime    time.Time `json:"event_time"`
	ReceivedTime time.Time `json:"received_time"`
	SpeedKmh     *float64  `json:"speed_kmh"`
	HeadingDeg   *float64  `json:"heading_deg"`
	RawRef       RawRef    `json:"raw_ref"`
}

// Key is the Kafka message key: all fixes of one device land in one partition, in order.
func (p Position) Key() string {
	return p.Operator + "/" + p.Feed + "/" + p.Vehicle
}

// Identity is the deterministic identity of a fix: (operator, feed, vehicle, event_time).
func (p Position) Identity() string {
	return p.Key() + "/" + p.EventTime.UTC().Format(time.RFC3339Nano)
}

// Feed-status values.
const (
	StatusOK            = "ok"
	StatusFailed        = "failed"
	StatusProviderNotOK = "provider_not_ok"
	StatusGap           = "gap"
)

// FeedStatus is feed-status.v1: what happened to one (feed, minute). It lets a reader tell "no
// vehicles reported" from "fetch failed" (invariant 6). The latest message per Key wins.
type FeedStatus struct {
	Feed                  string   `json:"feed"`
	Operator              string   `json:"operator"`
	MinuteUTC             string   `json:"minute_utc"`
	Status                string   `json:"status"`
	Records               int      `json:"records"`
	Events                int      `json:"events"`
	Invalid               int      `json:"invalid"`
	Unmapped              int      `json:"unmapped"`
	ConflictingDuplicates int      `json:"conflicting_duplicates"`
	ProviderStatus        *string  `json:"provider_status"`
	RawKeys               []string `json:"raw_keys"`
}

// Key is the Kafka message key (and compaction key) of a feed-status message.
func (s FeedStatus) Key() string {
	return s.Feed + "/" + s.MinuteUTC
}
