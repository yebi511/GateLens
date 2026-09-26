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
			if len(result.Hops) != 2 || result.RedirectSummary.ObservedRedirects != 1 || result.RedirectSummary.LinkedRedirects != 1 || result.FinalUpstreamHopID != "gateway/record-2" || result.ResponseCode != 200 {
				t.Fatalf("result=%+v", result)
			}
			if result.Hops[0].DurationMillis != 440 || result.Hops[1].DurationMillis != 8736 || result.Hops[1].AIRouting.Provider != "provider-b" || result.Hops[1].UpstreamHost != "-" {
				t.Fatalf("hops=%+v", result.Hops)
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
		state    string
		links    int
		terminal bool
	}{
		{"single", []string{"via_upstream"}, "linked", 0, true},
		{"redirect", []string{"internal_redirect", "via_upstream"}, "linked", 1, true},
		{"two redirects", []string{"internal_redirect:custom", "internal_redirect", "via_upstream"}, "linked", 2, true},
		{"missing next", []string{"internal_redirect", "internal_redirect"}, "missing-next", 1, false},
		{"ordinary multiple", []string{"via_upstream", "via_upstream"}, "ambiguous", 0, false},
		{"reentry", []string{"via_upstream", "internal_redirect"}, "ambiguous", 0, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			groups := AttemptGroups(attemptRecords(test.details...), &domain.ProbeCollection{State: "settled"})
			if len(groups) != 1 || groups[0].RelationState != test.state || len(groups[0].Links) != test.links || (groups[0].LocalTerminalHopID != "") != test.terminal {
				t.Fatalf("groups=%+v", groups)
			}
		})
	}
}

func TestAttemptGroupsDoNotInventOrderOrCrossRuntime(t *testing.T) {
	hops := attemptRecords("internal_redirect", "via_upstream")
	hops[1].RuntimeSource = "other-pod"
	groups := AttemptGroups(hops, &domain.ProbeCollection{State: "settled"})
	if len(groups) != 2 || len(groups[0].Links) != 0 {
		t.Fatalf("groups=%+v", groups)
	}
	hops = attemptRecords("internal_redirect", "via_upstream")
	for i := range hops {
		hops[i].RuntimeSource = ""
	}
	groups = AttemptGroups(hops, &domain.ProbeCollection{State: "settled"})
	if groups[0].RelationState != "unconfirmed" || len(groups[0].Links) != 0 || groups[0].LocalTerminalHopID != "" {
		t.Fatalf("groups=%+v", groups)
	}
	hops = attemptRecords("internal_redirect", "via_upstream")
	groups = AttemptGroups(hops, &domain.ProbeCollection{State: "window-ended", Reasons: []string{"日志来源连续性未确认"}})
	if groups[0].OrderBasis != "unavailable" || len(groups[0].Links) != 0 {
		t.Fatalf("groups=%+v", groups)
	}
}

func attemptExecution() domain.ProbeExecution {
	return domain.ProbeExecution{ID: "probe", GatewayID: "gateway", SourceCluster: "edge", State: "completed", ResponseCode: 200, DurationMillis: 8736, EvidenceComplete: true, Collection: &domain.ProbeCollection{State: "settled"}, Segments: []domain.ProbeSegment{{GatewayID: "gateway", ClusterID: "edge", GatewayName: "higress", Evidence: "observed", Hops: attemptRecords("internal_redirect:ai_usage_via_upstream", "ai_usage_via_upstream"), Collection: &domain.ProbeCollection{State: "settled"}}}}
}

func TestEnrichProbeKeepsHTTPResultAndConfirmsSource(t *testing.T) {
	result := EnrichProbe(attemptExecution())
	if result.ResponseCode != 200 || result.DurationMillis != 8736 || !result.EvidenceComplete || result.RedirectSummary.ObservedRedirects != 1 || result.RedirectSummary.LinkedRedirects != 1 || result.FinalResponseHopID != "gateway/record-2" || result.FinalUpstreamHopID != result.FinalResponseHopID {
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
				execution.Segments[0].Collection = &domain.ProbeCollection{State: "window-ended"}
			case "multi gateway":
				execution.Segments = append(execution.Segments, domain.ProbeSegment{GatewayID: "remote", ClusterID: "gpu", Hops: attemptRecords("internal_redirect", "via_upstream"), Collection: &domain.ProbeCollection{State: "settled"}})
			case "candidate gateway":
				execution.Segments = append(execution.Segments, domain.ProbeSegment{GatewayID: "candidate", ClusterID: "gpu", Evidence: "inferred", Gaps: []string{"missing log"}})
			}
			result := EnrichProbe(execution)
			if result.FinalUpstreamHopID != "" {
				t.Fatalf("mode=%s result=%+v", mode, result)
			}
			if mode != "multi gateway" && mode != "candidate gateway" && result.FinalResponseHopID != "" {
				t.Fatalf("unexpected response attribution %+v", result)
			}
			if mode == "multi gateway" && (result.RedirectSummary.ObservedRedirects != 2 || result.RedirectSummary.LinkedRedirects != 2 || len(result.Segments) != 2 || result.Hops[0].ID == result.Hops[2].ID) {
				t.Fatalf("result=%+v", result)
			}
		})
	}
}

func TestLegacyProbeKeepsEvidenceButUnknownProcess(t *testing.T) {
	execution := attemptExecution()
	execution.Collection = nil
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
	if result.Collection.State != "unknown" || result.RedirectSummary.ProcessState != "unknown" || result.RedirectSummary.ObservedRedirects != 1 || len(result.Hops) != 2 || len(result.Hops[0].ExtProcs) != 1 || result.FinalUpstreamHopID != "" {
		t.Fatalf("result=%+v", result)
	}
}
