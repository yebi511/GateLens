package observed

import (
	"encoding/json"
	"testing"
)

func TestRedirectMarkerBoundaries(t *testing.T) {
	for _, details := range []string{"internal_redirect", " internal_redirect ", "internal_redirect:ai_usage_via_upstream", "internal_redirect:other_filter_detail"} {
		if !IsInternalRedirect(details) {
			t.Errorf("missing marker %q", details)
		}
	}
	for _, details := range []string{"via_upstream", "ai_usage_via_upstream", "not_internal_redirect", "internal_redirect_failed", "internal_redirect:", "internal_redirect:  "} {
		if IsInternalRedirect(details) {
			t.Errorf("false marker %q", details)
		}
	}
}

func TestProbePlaceholderIdentity(t *testing.T) {
	for _, test := range []struct {
		probe, trace string
		match        bool
		basis        string
	}{
		{"-", "TRACE", true, "trace-id"}, {" ", "TRACE", true, "trace-id"},
		{"-", "-", false, ""}, {"other", "TRACE", false, ""}, {"probe", "-", true, "probe-id"},
	} {
		line, _ := json.Marshal(map[string]string{"gatelens_probe_id": test.probe, "trace_id": test.trace, "request_id": "probe"})
		hop, matched, err := ParseHigressLineForProbe(string(line), "probe", "trace", "edge", "pod", "source")
		if err != nil || matched != test.match || hop.Correlation != test.basis {
			t.Fatalf("test=%+v hop=%+v match=%v err=%v", test, hop, matched, err)
		}
	}
	if _, match, _ := ParseHigressLineForProbe(`{"gatelens_probe_id":"-","trace_id":"-"}`, "-", "-", "edge", "pod", "source"); match {
		t.Fatal("placeholder command matched")
	}
}

func TestAIRoutingSummaryDoesNotAffectObservation(t *testing.T) {
	for _, value := range []string{"", "model:qwen", "{", "[]", "null", `{"provider":5}`} {
		line, _ := json.Marshal(map[string]string{"gatelens_probe_id": "probe", "ai_log": value, "route_name": "chat", "response_code_details": "internal_redirect", "start_time": "-"})
		hop, matched, err := ParseHigressLineForProbe(string(line), "probe", "", "edge", "pod", "source")
		if err != nil || !matched || !hop.InternalRedirect || hop.RouteName != "chat" || hop.AIRouting != nil || hop.RequestStartTime != "" {
			t.Fatalf("value=%q hop=%+v", value, hop)
		}
	}
	line, _ := json.Marshal(map[string]string{"gatelens_probe_id": "probe", "ai_log": `{"provider":"sail","request_model":"Qwen3.6","upstream_model":"Qwen3.5","response_model":"Qwen3.5","question":"secret question","answer_no_stream":"secret answer","nested":{"key":"secret"}}`})
	hop, matched, _ := ParseHigressLineForProbe(string(line), "probe", "", "edge", "pod", "source")
	if !matched || hop.AIRouting == nil || hop.AIRouting.Provider != "sail" || hop.AIRouting.UpstreamModel != "Qwen3.5" {
		t.Fatalf("hop=%+v", hop)
	}
	encoded, _ := json.Marshal(hop.AIRouting)
	var fields map[string]any
	_ = json.Unmarshal(encoded, &fields)
	if len(fields) != 4 {
		t.Fatalf("unexpected summary %s", encoded)
	}
}

func TestUsageRecordsAreNotAccessAttempts(t *testing.T) {
	content := `{"gatelens_probe_id":"probe","ai_usage_record":{"outcome":"complete"}}
{"gatelens_probe_id":"probe","route_name":"first","response_code":400,"response_code_details":"internal_redirect:ai_usage_via_upstream"}
{"gatelens_probe_id":"probe","ai_usage_record":{"outcome":"complete","total_token":769}}
{"gatelens_probe_id":"probe","route_name":"second","response_code":200,"response_code_details":"ai_usage_via_upstream"}`
	hops, _ := ParseHigressLinesForProbe(content, "probe", "", "edge", "pod", "source")
	if len(hops) != 2 || !hops[0].InternalRedirect || hops[1].InternalRedirect {
		t.Fatalf("hops=%+v", hops)
	}
}

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

func TestParseHigressLineAcceptsNumericEnvoyFields(t *testing.T) {
	line := `{"duration":1509,"gatelens_probe_id":"2365d3f8083415ad85b3faaf3467f5bf","response_code":200,"response_code_details":"via_upstream","response_flags":"-","route_name":"inference.deepseek-r1-distill-qwen-7b.0","start_time":"2026-08-20T09:37:17.379Z","trace_id":"d06ff273a4130d8c54f76497fa286742","upstream_cluster":"outbound|54321||deepseek-r1-distill-qwen-7b-ip-9ecf49d6.inference.svc.cluster.local","upstream_host":"10.233.105.125:8080","upstream_service_time":1501}`
	hop, matched, err := ParseHigressLineForProbe(line, "2365d3f8083415ad85b3faaf3467f5bf", "d06ff273a4130d8c54f76497fa286742", "inference", "istio-gateway", "kubernetes-pod-log")
	if err != nil || !matched {
		t.Fatalf("matched=%v err=%v", matched, err)
	}
	if hop.ResponseCode != 200 || hop.DurationMillis != 1509 || hop.UpstreamServiceTimeMillis != 1501 || hop.RouteName != "inference.deepseek-r1-distill-qwen-7b.0" || hop.Correlation != "probe-id" {
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

func TestParseHigressLineParsesExtProcFilterState(t *testing.T) {
	line := `{"gatelens_probe_id":"probe-1","route_name":"chat","upstream_cluster":"inference","upstream_host":"10.0.0.8:8080","ext_proc":{"processor":"bbr","rule_id":"body-model","request_header_call_count":1,"request_body_call_count":1,"request_header_latency_us":1200,"request_body_latency_us":8300,"grpc_status":"0","selected_pool":"qwen","selected_endpoint":"10.0.0.8:8080","reason_code":"model_match"}}`
	hop, matched, err := ParseHigressLineForProbe(line, "probe-1", "", "edge", "gateway", "log")
	if err != nil || !matched || len(hop.ExtProcs) != 1 {
		t.Fatalf("hop=%#v matched=%v err=%v", hop, matched, err)
	}
	extProc := hop.ExtProcs[0]
	if extProc.Processor != "bbr" || extProc.RequestBodyCalls != 1 || extProc.RequestBodyLatencyUS != 8300 || extProc.Outcome != "success" || extProc.SelectedEndpoint != "10.0.0.8:8080" {
		t.Fatalf("ext proc=%#v", extProc)
	}
}

func TestParseHigressLineParsesFlatExtProcFailure(t *testing.T) {
	line := `{"gatelens_probe_id":"probe-1","response_code":200,"ext_proc_request_header_call_count":1,"ext_proc_grpc_status":"DeadlineExceeded","ext_proc_message_timeout":"true","ext_proc_failure_mode_allowed":"true"}`
	hop, matched, err := ParseHigressLineForProbe(line, "probe-1", "", "edge", "gateway", "log")
	if err != nil || !matched || len(hop.ExtProcs) != 1 || hop.ExtProcs[0].Outcome != "fail-open" || !hop.ExtProcs[0].FailureModeAllowed {
		t.Fatalf("hop=%#v matched=%v err=%v", hop, matched, err)
	}
}

func TestParseHigressLineParsesNamedBBRAndEPPFilterStates(t *testing.T) {
	line := `{"gatelens_probe_id":"probe-1","ext_proc_bbr":{"request_header_call_count":1,"grpc_status":"0"},"ext_proc_epp":{"request_header_call_count":1,"grpc_status":"DeadlineExceeded"}}`
	hop, matched, err := ParseHigressLineForProbe(line, "probe-1", "", "edge", "gateway", "log")
	if err != nil || !matched || len(hop.ExtProcs) != 2 {
		t.Fatalf("hop=%#v matched=%v err=%v", hop, matched, err)
	}
	if hop.ExtProcs[0].Processor != "bbr" || hop.ExtProcs[0].Outcome != "success" || hop.ExtProcs[1].Processor != "epp" || hop.ExtProcs[1].Outcome != "error" {
		t.Fatalf("ext procs=%#v", hop.ExtProcs)
	}
}

func TestParseHigressLineParsesCurrentEnvoyExtProcLoggingFields(t *testing.T) {
	line := `{"gatelens_probe_id":"probe-1","ext_proc_bbr":{"request_header_latency_us":100,"request_header_call_status":0,"request_body_call_count":2,"request_body_total_latency_us":900,"request_body_last_call_status":0,"failed_open":false,"grpc_status_before_first_call":0},"ext_proc_epp":{"request_header_latency_us":1200,"request_header_call_status":4,"failed_open":true,"grpc_status_before_first_call":0}}`
	hop, matched, err := ParseHigressLineForProbe(line, "probe-1", "", "edge", "gateway", "log")
	if err != nil || !matched || len(hop.ExtProcs) != 2 {
		t.Fatalf("hop=%#v matched=%v err=%v", hop, matched, err)
	}
	bbr, epp := hop.ExtProcs[0], hop.ExtProcs[1]
	if bbr.Processor != "bbr" || bbr.RequestHeaderCalls != 1 || bbr.RequestBodyCalls != 2 || bbr.RequestBodyLatencyUS != 900 || bbr.Outcome != "success" {
		t.Fatalf("bbr=%#v", bbr)
	}
	if epp.Processor != "epp" || epp.GRPCStatus != "4" || !epp.MessageTimeout || !epp.FailedOpen || epp.Outcome != "fail-open" {
		t.Fatalf("epp=%#v", epp)
	}
}
