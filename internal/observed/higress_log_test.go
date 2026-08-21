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
