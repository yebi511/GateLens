package domain

import (
	"encoding/json"
	"testing"
)

func TestProbeObservationProtocolCompatibility(t *testing.T) {
	var legacy ProbeExecution
	if err := json.Unmarshal([]byte(`{"id":"p","responseCode":200,"hops":[{"routeName":"chat"}],"segments":[]}`), &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.Collection != nil || legacy.RedirectSummary != nil || len(legacy.Hops) != 1 {
		t.Fatalf("legacy=%+v", legacy)
	}
	legacy.Collection = &ProbeCollection{State: "settled"}
	legacy.RedirectSummary = &ProbeRedirectSummary{ObservedRedirects: 1, ProcessState: "observed"}
	legacy.Hops[0].ID = "record-1"
	legacy.FinalResponseHopID = "record-1"
	encoded, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	var decoded ProbeExecution
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Collection.State != "settled" || decoded.RedirectSummary.ObservedRedirects != 1 || decoded.FinalResponseHopID != decoded.Hops[0].ID {
		t.Fatalf("decoded=%+v", decoded)
	}
}
