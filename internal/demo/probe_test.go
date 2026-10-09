package demo

import (
	"context"
	"encoding/json"
	"github.com/gatelens/gatelens/internal/domain"
	"testing"
)

func TestDemoProbeReturnsVersionTwoAndSameStoredResult(t *testing.T) {
	store := NewStore()
	result, err := store.CreateProbe(context.Background(), domain.ProbeRequest{EntryID: "demo-entry", SourceCluster: "edge-prod", Path: "/"})
	if err != nil || result.SchemaVersion != domain.ProbeSchemaVersion || result.Method != domain.ProbeHTTPMethodGET || len(result.Segments) != 1 || len(result.Segments[0].Hops[0].ExtProcs) != 2 || result.FinalUpstreamHopID == "" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	saved, ok := store.GetProbe(result.ID)
	a, _ := json.Marshal(result)
	b, _ := json.Marshal(saved)
	if !ok || string(a) != string(b) {
		t.Fatal("query differs")
	}
}
