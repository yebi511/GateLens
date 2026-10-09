package observed

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/gatelens/gatelens/internal/domain"
)

func TestSyntheticAccessLogsThroughAggregation(t *testing.T) {
	content, err := os.ReadFile("testdata/probe_redirect.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"internal_redirect", "internal_redirect:ai_usage_via_upstream", "internal_redirect:other_filter_detail"} {
		t.Run(marker, func(t *testing.T) {
			var hops []domain.ObservedHop
			for _, line := range strings.Split(strings.ReplaceAll(string(content), "internal_redirect:ai_usage_via_upstream", marker), "\n") {
				hop, matched, err := ParseHigressLineForProbe(line, "synthetic-probe", "", "edge", "demo/higress-0", "kubernetes-pod-log")
				if err != nil {
					t.Fatal(err)
				}
				if !matched {
					continue
				}
				hop.ID, hop.LogSequence = fmt.Sprintf("record-%d", len(hops)+1), len(hops)+1
				hop.RuntimeSource, hop.LogSourceID = "edge/demo/higress-0/uid/proxy", "edge/demo/higress-0/uid/proxy"
				hops = append(hops, hop)
			}
			execution := attemptExecution()
			execution.Segments[0].Hops = hops
			result := EnrichProbe(execution)
			if len(result.Segments[0].Hops) != 2 || len(result.Segments[0].Links) != 1 || result.FinalUpstreamHopID != "gateway/record-2" || result.ResponseCode != 200 {
				t.Fatalf("result=%+v", result)
			}
			if result.Segments[0].Hops[0].DurationMillis != 440 || result.Segments[0].Hops[1].DurationMillis != 8736 || result.Segments[0].Hops[1].AIRouting.Provider != "provider-b" || result.Segments[0].Hops[1].UpstreamHost != "-" {
				t.Fatalf("hops=%+v", result.Segments[0].Hops)
			}
		})
	}
}

func attemptRecords(details ...string) []domain.ObservedHop {
	var hops []domain.ObservedHop
	for i, detail := range details {
		code := 200
		if IsInternalRedirect(detail) {
			code = 400
		}
		hops = append(hops, domain.ObservedHop{ID: fmt.Sprintf("record-%d", i+1), ClusterID: "edge", Pod: "ns/pod", RuntimeSource: "edge/ns/pod/proxy", LogSourceID: "pod-source", LogSequence: i + 1, RequestStartTime: "2026-09-17T07:34:01.306Z", DownstreamRemoteAddress: "10.0.0.1:1234", DownstreamLocalAddress: "10.0.0.2:80", Correlation: "probe-id", ResponseCode: code, ResponseCodeDetails: detail, InternalRedirect: IsInternalRedirect(detail), RouteName: fmt.Sprintf("route-%d", i+1), UpstreamCluster: fmt.Sprintf("cluster-%d", i+1)})
	}
	return hops
}

func TestAttemptGroupRelationships(t *testing.T) {
	for _, test := range []struct {
		name     string
		details  []string
		state    domain.ProbeAttemptRelationState
		links    int
		terminal bool
	}{
		{"single", []string{"via_upstream"}, domain.ProbeAttemptRelationStateLinked, 0, true},
		{"redirect", []string{"internal_redirect", "via_upstream"}, domain.ProbeAttemptRelationStateLinked, 1, true},
		{"two redirects", []string{"internal_redirect:custom", "internal_redirect", "via_upstream"}, domain.ProbeAttemptRelationStateLinked, 2, true},
		{"missing next", []string{"internal_redirect", "internal_redirect"}, domain.ProbeAttemptRelationStateMissingNext, 1, false},
		{"ordinary multiple", []string{"via_upstream", "via_upstream"}, domain.ProbeAttemptRelationStateAmbiguous, 0, false},
		{"reentry", []string{"via_upstream", "internal_redirect"}, domain.ProbeAttemptRelationStateAmbiguous, 0, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			groups := attemptGroups(attemptRecords(test.details...), &domain.ProbeCollection{State: domain.ProbeCollectionStateSettled}, nil)
			if len(groups) != 1 || groups[0].RelationState != test.state || len(groups[0].Links) != test.links || (groups[0].LocalTerminalHopID != "") != test.terminal {
				t.Fatalf("groups=%+v", groups)
			}
		})
	}
}

func TestAttemptGroupsDoNotInventOrderOrCrossRuntime(t *testing.T) {
	hops := attemptRecords("internal_redirect", "via_upstream")
	hops[1].RuntimeSource = "other-pod"
	groups := attemptGroups(hops, &domain.ProbeCollection{State: domain.ProbeCollectionStateSettled}, nil)
	if len(groups) != 2 || len(groups[0].Links) != 0 {
		t.Fatalf("groups=%+v", groups)
	}
	hops = attemptRecords("internal_redirect", "via_upstream")
	for i := range hops {
		hops[i].RuntimeSource = ""
	}
	groups = attemptGroups(hops, &domain.ProbeCollection{State: domain.ProbeCollectionStateSettled}, nil)
	if groups[0].RelationState != domain.ProbeAttemptRelationStateUnconfirmed || len(groups[0].Links) != 0 || groups[0].LocalTerminalHopID != "" {
		t.Fatalf("groups=%+v", groups)
	}
	hops = attemptRecords("internal_redirect", "via_upstream")
	groups = attemptGroups(hops, &domain.ProbeCollection{State: domain.ProbeCollectionStateWindowEnded}, []domain.ProbeIssue{{Scope: domain.ProbeIssueScopeCollection, Code: domain.ProbeIssueCodeLogOrderUnconfirmed, Message: "arbitrary wording"}})
	if groups[0].OrderBasis != "unavailable" || len(groups[0].Links) != 0 {
		t.Fatalf("groups=%+v", groups)
	}
}

func attemptExecution() domain.ProbeExecution {
	return domain.ProbeExecution{ID: "probe", GatewayID: "gateway", SourceCluster: "edge", State: "completed", ResponseCode: 200, DurationMillis: 8736, SchemaVersion: domain.ProbeSchemaVersion, Issues: []domain.ProbeIssue{}, Segments: []domain.ProbeSegment{{GatewayID: "gateway", ClusterID: "edge", GatewayName: "higress", Hops: attemptRecords("internal_redirect:ai_usage_via_upstream", "ai_usage_via_upstream"), Collection: &domain.ProbeCollection{State: domain.ProbeCollectionStateSettled}}}}
}

func TestEnrichProbeKeepsHTTPResultAndConfirmsSource(t *testing.T) {
	result := EnrichProbe(attemptExecution())
	if result.ResponseCode != 200 || result.DurationMillis != 8736 || len(result.Segments[0].Links) != 1 || result.FinalResponseHopID != "gateway/record-2" || result.FinalUpstreamHopID != result.FinalResponseHopID {
		t.Fatalf("result=%+v", result)
	}
	// Re-enriching must not prefix IDs twice or change link references.
	result = EnrichProbe(result)
	if result.FinalResponseHopID != "gateway/record-2" {
		t.Fatalf("result=%+v", result)
	}
}

func TestEnrichProbeLeavesConflictingOrPartialAttributionUnknown(t *testing.T) {
	for _, mode := range []string{"response conflict", "request failed", "trace fallback", "multiple terminal", "partial", "multi gateway", "candidate gateway"} {
		t.Run(mode, func(t *testing.T) {
			execution := attemptExecution()
			switch mode {
			case "response conflict":
				execution.ResponseCode = 503
			case "request failed":
				execution.State = "failed"
				execution.Error = "response read failed"
			case "trace fallback":
				execution.Segments[0].Hops[0].Correlation = "trace-id"
			case "multiple terminal":
				execution.Segments[0].Hops = attemptRecords("via_upstream", "via_upstream")
			case "partial":
				execution.Segments[0].Collection = &domain.ProbeCollection{State: domain.ProbeCollectionStateWindowEnded}
			case "multi gateway":
				execution.Segments = append(execution.Segments, domain.ProbeSegment{GatewayID: "remote", ClusterID: "gpu", Hops: attemptRecords("internal_redirect", "via_upstream"), Collection: &domain.ProbeCollection{State: domain.ProbeCollectionStateSettled}})
			case "candidate gateway":
				execution.Segments = append(execution.Segments, domain.ProbeSegment{GatewayID: "candidate", ClusterID: "gpu", Issues: []domain.ProbeIssue{{Scope: domain.ProbeIssueScopeInference, Code: domain.ProbeIssueCodeGatewayEvidenceMissing, Message: "missing log"}}})
			}
			result := EnrichProbe(execution)
			if result.FinalUpstreamHopID != "" {
				t.Fatalf("mode=%s result=%+v", mode, result)
			}
			if mode != "multi gateway" && mode != "candidate gateway" && result.FinalResponseHopID != "" {
				t.Fatalf("unexpected response attribution %+v", result)
			}
			if mode == "multi gateway" && (len(result.Segments[0].Links)+len(result.Segments[1].Links) != 2 || len(result.Segments) != 2 || result.Segments[0].Hops[0].ID == result.Segments[1].Hops[0].ID) {
				t.Fatalf("result=%+v", result)
			}
		})
	}
}

func TestMissingIdentityKeepsEvidenceButUnknownProcess(t *testing.T) {
	execution := attemptExecution()
	execution.Segments[0].Collection = nil
	for i := range execution.Segments[0].Hops {
		hop := &execution.Segments[0].Hops[i]
		hop.RuntimeSource = ""
		hop.LogSourceID = ""
		hop.LogSequence = 0
		hop.RequestStartTime = ""
		hop.ID = ""
		hop.ExtProcs = []domain.ExtProcObservation{{Processor: "bbr", Outcome: "success"}}
	}
	result := EnrichProbe(execution)
	if result.Segments[0].Collection.State != domain.ProbeCollectionStateUnknown || result.Segments[0].RelationState != domain.ProbeAttemptRelationStateUnconfirmed || len(result.Segments[0].Hops) != 2 || len(result.Segments[0].Hops[0].ExtProcs) != 1 || result.FinalUpstreamHopID != "" {
		t.Fatalf("result=%+v", result)
	}
}

func TestContextsIsolateIdentityAndKeepMixedEvidence(t *testing.T) {
	for _, field := range []string{"runtime", "start", "remote", "local"} {
		t.Run(field, func(t *testing.T) {
			hops := attemptRecords("internal_redirect", "via_upstream")
			switch field {
			case "runtime":
				hops[1].RuntimeSource = "other"
			case "start":
				hops[1].RequestStartTime = "other"
			case "remote":
				hops[1].DownstreamRemoteAddress = "other"
			case "local":
				hops[1].DownstreamLocalAddress = "other"
			}
			execution := attemptExecution()
			execution.Segments[0].Hops = hops
			result := EnrichProbe(execution)
			if result.Segments[0].Hops[0].ContextID == result.Segments[0].Hops[1].ContextID || len(result.Segments[0].Links) != 0 || result.FinalResponseHopID != "" {
				t.Fatalf("cross context relation: %+v", result)
			}
		})
	}
	execution := attemptExecution()
	unknown := attemptRecords("via_upstream")[0]
	unknown.ID = "unknown"
	unknown.RuntimeSource = ""
	unknown.LogSequence = 0
	execution.Segments[0].Hops = append(execution.Segments[0].Hops, unknown)
	result := EnrichProbe(execution)
	segment := result.Segments[0]
	if segment.RelationState != domain.ProbeAttemptRelationStateUnconfirmed || len(segment.Links) != 1 || len(segment.LocalTerminalHopIDs) != 1 || result.FinalResponseHopID != "" {
		t.Fatalf("mixed %+v", result)
	}
	if segment.Hops[2].ContextID == segment.Hops[0].ContextID || len(segment.Issues) == 0 {
		t.Fatalf("unknown context missing %+v", segment)
	}
	again := EnrichProbe(result)
	if again.Segments[0].Hops[2].ContextID != segment.Hops[2].ContextID || again.Segments[0].Links[0] != segment.Links[0] || len(again.Segments[0].Issues) != len(segment.Issues) {
		t.Fatalf("unstable %+v", again)
	}
}
func TestEnrichUsesParsedFactsAndPrefixesIssueReferences(t *testing.T) {
	execution := attemptExecution()
	execution.Segments[0].Hops[0].AIRouting = &domain.AIRoutingSummary{Provider: "parsed"}
	execution.Segments[0].Hops[0].AILog = `{"provider":"raw"}`
	execution.Segments[0].Issues = []domain.ProbeIssue{{Scope: domain.ProbeIssueScopeCollection, Code: domain.ProbeIssueCodeLogOrderUnconfirmed, HopID: "record-1", Message: "no keywords"}}
	result := EnrichProbe(execution)
	if result.Segments[0].Hops[0].AIRouting.Provider != "parsed" || len(result.Segments[0].Links) != 0 || result.Segments[0].Issues[0].HopID != "gateway/record-1" {
		t.Fatal(result)
	}
}
