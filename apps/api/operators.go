package main

import "fmt"

// Coverage tiers (docs/architecture.md §2). Only live exists today; the others are named so a
// later phase adds entries, not a field.
const tierLive = "live"

type operatorInfo struct {
	Label string
	Mode  string
	Tier  string
	// rank orders the coverage list on screen: the order of the table below, not alphabetical.
	rank int
}

// operators is the only place that turns an operator id into text a reader sees. It mirrors the
// mapping table of packages/its: a new operator is added to both, after looking at its data.
var operators = map[string]operatorInfo{
	"rio-sppo":        {Label: "Ônibus municipais (SPPO)", Mode: "bus", Tier: tierLive, rank: 0},
	"rio-sistema-rio": {Label: "Sistema RIO", Mode: "bus", Tier: tierLive, rank: 1},
	"rio-brt":         {Label: "BRT", Mode: "brt", Tier: tierLive, rank: 2},
}

// operatorOf fails for an operator with no entry. There is no fallback label or mode: a refresh
// that meets an unknown operator keeps serving the last snapshot and logs why, instead of showing
// a guess.
func operatorOf(id string) (operatorInfo, error) {
	info, ok := operators[id]
	if !ok {
		return operatorInfo{}, fmt.Errorf("operator %q has no entry in the operators table (apps/api/operators.go)", id)
	}
	return info, nil
}
