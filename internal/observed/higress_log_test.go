package observed

import "testing"

func TestParseHigressLine(t *testing.T) {
	line := `2026-08-13T10:00:00Z stdout F {"ai_log":"model:qwen","authority":"api.example.com","duration":"17","method":"POST","path":"/v1/chat?token=secret","protocol":"HTTP/2","gatelens_probe_id":"probe-1","request_id":"envoy-generated","response_code":"200","response_flags":"-","route_name":"chat","start_time":"2026-08-13T10:00:00.000Z","trace_id":"trace","upstream_cluster":"outbound|8000||qwen.default.svc.cluster.local","upstream_host":"10.0.0.8:8000","upstream_service_time":"15","response_code_details":"via_upstream"}`
	hop, matched, err := ParseHigressLineForProbe(line, "probe-1", "trace", "edge", "higress-gateway-1", "kubernetes-pod-log")
	if err != nil || !matched {
		t.Fatalf("matched=%v err=%v", matched, err)
	}
	if hop.RouteName != "chat" || hop.UpstreamHost != "10.0.0.8:8000" || hop.ResponseCode != 200 || hop.DurationMillis != 17 || hop.AILog != "model:qwen" || hop.Path != "/v1/chat" || hop.Correlation != "probe-id" {
		t.Fatalf("hop=%#v", hop)
	}
}

func TestParseHigressLineSkipsOtherRequests(t *testing.T) {
	if _, matched, err := ParseHigressLineForProbe(`{"gatelens_probe_id":"other","request_id":"wanted"}`, "wanted", "", "edge", "pod", "source"); err != nil || matched {
		t.Fatalf("matched=%v err=%v", matched, err)
	}
}

func TestParseHigressLineMatchesTraceWhenProbeHeaderIsNotLogged(t *testing.T) {
	line := `{"request_id":"generated-by-remote-gateway","trace_id":"ABCDEF0123456789","route_name":"remote"}`
	hop, matched, err := ParseHigressLineForProbe(line, "probe-1", "abcdef0123456789", "gpu", "gateway", "log")
	if err != nil || !matched || hop.Correlation != "trace-id" || hop.RouteName != "remote" {
		t.Fatalf("hop=%#v matched=%v err=%v", hop, matched, err)
	}
}

func TestParseHigressLineRejectsMismatchedProbeIDEvenWhenTraceMatches(t *testing.T) {
	line := `{"gatelens_probe_id":"another-probe","trace_id":"trace-1"}`
	if _, matched, err := ParseHigressLineForProbe(line, "probe-1", "trace-1", "gpu", "gateway", "log"); err != nil || matched {
		t.Fatalf("matched=%v err=%v", matched, err)
	}
}
