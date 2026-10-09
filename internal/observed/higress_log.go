package observed

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gatelens/gatelens/internal/domain"
)

type higressAccessLog struct {
	AILog                             string          `json:"ai_log"`
	Authority                         string          `json:"authority"`
	DownstreamRemoteAddress           string          `json:"downstream_remote_address"`
	DownstreamLocalAddress            string          `json:"downstream_local_address"`
	Duration                          accessLogScalar `json:"duration"`
	Method                            string          `json:"method"`
	Path                              string          `json:"path"`
	Protocol                          string          `json:"protocol"`
	GateLensProbeID                   string          `json:"gatelens_probe_id"`
	ResponseCode                      accessLogScalar `json:"response_code"`
	ResponseCodeDetails               string          `json:"response_code_details"`
	ResponseFlags                     string          `json:"response_flags"`
	RouteName                         string          `json:"route_name"`
	StartTime                         string          `json:"start_time"`
	TraceID                           string          `json:"trace_id"`
	UpstreamCluster                   string          `json:"upstream_cluster"`
	UpstreamHost                      string          `json:"upstream_host"`
	UpstreamLocalAddress              string          `json:"upstream_local_address"`
	UpstreamServiceTime               accessLogScalar `json:"upstream_service_time"`
	UpstreamTransportFailure          string          `json:"upstream_transport_failure_reason"`
	ExtProc                           json.RawMessage `json:"ext_proc"`
	ExtProcProcessor                  string          `json:"ext_proc_processor"`
	ExtProcRuleID                     string          `json:"ext_proc_rule_id"`
	ExtProcSelectedPool               string          `json:"ext_proc_selected_pool"`
	ExtProcSelectedEndpoint           string          `json:"ext_proc_selected_endpoint"`
	ExtProcReasonCode                 string          `json:"ext_proc_reason_code"`
	ExtProcHeaderCalls                accessLogScalar `json:"ext_proc_request_header_call_count"`
	ExtProcBodyCalls                  accessLogScalar `json:"ext_proc_request_body_call_count"`
	ExtProcResponseHeaderCalls        accessLogScalar `json:"ext_proc_response_header_call_count"`
	ExtProcResponseBodyCalls          accessLogScalar `json:"ext_proc_response_body_call_count"`
	ExtProcHeaderLatencyUS            accessLogScalar `json:"ext_proc_request_header_latency_us"`
	ExtProcBodyLatencyUS              accessLogScalar `json:"ext_proc_request_body_latency_us"`
	ExtProcHeaderCallStatus           accessLogScalar `json:"ext_proc_request_header_call_status"`
	ExtProcBodyTotalLatencyUS         accessLogScalar `json:"ext_proc_request_body_total_latency_us"`
	ExtProcBodyLastCallStatus         accessLogScalar `json:"ext_proc_request_body_last_call_status"`
	ExtProcResponseHeaderLatencyUS    accessLogScalar `json:"ext_proc_response_header_latency_us"`
	ExtProcResponseBodyLatencyUS      accessLogScalar `json:"ext_proc_response_body_latency_us"`
	ExtProcResponseHeaderCallStatus   accessLogScalar `json:"ext_proc_response_header_call_status"`
	ExtProcResponseBodyTotalLatencyUS accessLogScalar `json:"ext_proc_response_body_total_latency_us"`
	ExtProcResponseBodyLastCallStatus accessLogScalar `json:"ext_proc_response_body_last_call_status"`
	ExtProcGRPCStatus                 string          `json:"ext_proc_grpc_status"`
	ExtProcFailureModeAllowed         accessLogBool   `json:"ext_proc_failure_mode_allowed"`
	ExtProcFailedOpen                 accessLogBool   `json:"ext_proc_failed_open"`
	ExtProcMessageTimeout             accessLogBool   `json:"ext_proc_message_timeout"`
	ExtProcHTTPError                  accessLogBool   `json:"ext_proc_http_not_ok_resp_received"`
	ExtProcReceivedImmediateResponse  accessLogBool   `json:"ext_proc_received_immediate_response"`
	ExtProcGRPCStatusBeforeFirstCall  accessLogScalar `json:"ext_proc_grpc_status_before_first_call"`
}

// Envoy access-log JSON formats may quote numeric substitutions or emit them
// as JSON numbers. Accept both forms so one numeric field cannot discard the
// entire otherwise valid access-log record.
type accessLogScalar string

func (value *accessLogScalar) UnmarshalJSON(data []byte) error {
	var text string
	if len(data) > 0 && data[0] == '"' {
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		*value = accessLogScalar(text)
		return nil
	}
	if string(data) == "null" {
		*value = ""
		return nil
	}
	var number json.Number
	if err := json.Unmarshal(data, &number); err != nil {
		return err
	}
	*value = accessLogScalar(number.String())
	return nil
}

type accessLogBool bool

func (value *accessLogBool) UnmarshalJSON(data []byte) error {
	var boolean bool
	if err := json.Unmarshal(data, &boolean); err == nil {
		*value = accessLogBool(boolean)
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return err
	}
	*value = accessLogBool(strings.EqualFold(strings.TrimSpace(text), "true") || strings.TrimSpace(text) == "1")
	return nil
}

// ParseHigressLineForProbe accepts raw JSON and CRI-prefixed access log lines.
// The GateLens-specific probe ID is authoritative; trace ID is a fallback for
// gateways that preserve trace context but do not log the custom header.
func ParseHigressLineForProbe(line, probeID, traceID, clusterID, pod, source string) (domain.ObservedHop, bool, error) {
	line = strings.TrimSpace(line)
	if index := strings.IndexByte(line, '{'); index > 0 {
		line = line[index:]
	}
	if line == "" || line[0] != '{' {
		return domain.ObservedHop{}, false, nil
	}
	var entry higressAccessLog
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		return domain.ObservedHop{}, false, nil
	}
	var rawFields map[string]json.RawMessage
	_ = json.Unmarshal([]byte(line), &rawFields)
	if _, usageRecord := rawFields["ai_usage_record"]; usageRecord {
		return domain.ObservedHop{}, false, nil
	}
	var correlation domain.ProbeCorrelation
	loggedProbeID := validLogValue(entry.GateLensProbeID)
	if loggedProbeID != "" {
		if validLogValue(probeID) != "" && loggedProbeID == probeID {
			correlation = domain.ProbeCorrelationProbeID
		}
	} else if validLogValue(traceID) != "" && validLogValue(entry.TraceID) != "" && strings.EqualFold(strings.TrimSpace(entry.TraceID), strings.TrimSpace(traceID)) {
		correlation = domain.ProbeCorrelationTraceID
	}
	if correlation == "" {
		return domain.ObservedHop{}, false, nil
	}
	hop := domain.ObservedHop{
		RequestStartTime:         validLogValue(entry.StartTime),
		InternalRedirect:         IsInternalRedirect(entry.ResponseCodeDetails),
		AIRouting:                parseAIRouting(entry.AILog),
		DownstreamLocalAddress:   entry.DownstreamLocalAddress,
		ObservedAt:               entry.StartTime,
		ClusterID:                clusterID,
		Pod:                      pod,
		Authority:                entry.Authority,
		Method:                   entry.Method,
		Path:                     redactQuery(entry.Path),
		Protocol:                 entry.Protocol,
		RouteName:                entry.RouteName,
		UpstreamCluster:          entry.UpstreamCluster,
		UpstreamHost:             entry.UpstreamHost,
		UpstreamLocalAddress:     entry.UpstreamLocalAddress,
		DownstreamRemoteAddress:  entry.DownstreamRemoteAddress,
		ResponseFlags:            entry.ResponseFlags,
		ResponseCodeDetails:      entry.ResponseCodeDetails,
		UpstreamTransportFailure: entry.UpstreamTransportFailure,
		AILog:                    entry.AILog,
		EvidenceSource:           source,
		Correlation:              correlation,
		Confidence:               domain.ObservationConfidenceObserved,
	}
	hop.ResponseCode = integer(string(entry.ResponseCode))
	hop.DurationMillis = integer64(string(entry.Duration))
	hop.UpstreamServiceTimeMillis = integer64(string(entry.UpstreamServiceTime))
	hop.ExtProcs = parseExtProcObservations(entry, rawFields)
	if hop.ObservedAt == "" {
		hop.ObservedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	return hop, true, nil
}

func validLogValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "-" {
		return ""
	}
	return value
}

// IsInternalRedirect recognizes the core marker and bounded extension details.
func IsInternalRedirect(details string) bool {
	details = strings.TrimSpace(details)
	return details == "internal_redirect" || (strings.HasPrefix(details, "internal_redirect:") && strings.TrimSpace(strings.TrimPrefix(details, "internal_redirect:")) != "")
}

func parseAIRouting(value string) *domain.AIRoutingSummary {
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(value), &fields) != nil || fields == nil {
		return nil
	}
	read := func(key string) string {
		var value string
		if json.Unmarshal(fields[key], &value) != nil {
			return ""
		}
		runes := []rune(value)
		if len(runes) > 1024 {
			value = string(runes[:1024])
		}
		return value
	}
	summary := &domain.AIRoutingSummary{Provider: read("provider"), RequestModel: read("request_model"), UpstreamModel: read("upstream_model"), ResponseModel: read("response_model")}
	if *summary == (domain.AIRoutingSummary{}) {
		return nil
	}
	return summary
}

// parseExtProcObservations accepts the default ext_proc object, named objects
// such as ext_proc_bbr/ext_proc_epp, and allowlisted flat fields.
func parseExtProcObservations(entry higressAccessLog, rawFields map[string]json.RawMessage) []domain.ExtProcObservation {
	var observations []domain.ExtProcObservation
	if observation := parseExtProcObservation(entry, entry.ExtProc, ""); observation != nil {
		observations = append(observations, *observation)
	}
	keys := make([]string, 0, len(rawFields))
	for key := range rawFields {
		lower := strings.ToLower(key)
		if key != "ext_proc" && (strings.HasPrefix(lower, "ext_proc_") || strings.HasSuffix(lower, "_ext_proc")) && !isFlatExtProcField(lower) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		hint := strings.TrimSuffix(strings.TrimPrefix(strings.ToLower(key), "ext_proc_"), "_ext_proc")
		if observation := parseExtProcObservation(higressAccessLog{}, rawFields[key], hint); observation != nil {
			observations = append(observations, *observation)
		}
	}
	return observations
}

func isFlatExtProcField(key string) bool {
	for _, suffix := range []string{
		"processor", "rule_id", "selected_pool", "selected_endpoint", "reason_code", "grpc_status",
		"request_header_call_count", "request_body_call_count", "response_header_call_count", "response_body_call_count",
		"request_header_latency_us", "request_header_call_status", "request_body_latency_us", "request_body_total_latency_us", "request_body_last_call_status",
		"response_header_latency_us", "response_header_call_status", "response_body_latency_us", "response_body_total_latency_us", "response_body_last_call_status",
		"failure_mode_allowed", "failed_open", "message_timeout", "http_not_ok_resp_received", "received_immediate_response", "grpc_status_before_first_call",
	} {
		if key == "ext_proc_"+suffix {
			return true
		}
	}
	return false
}

// parseExtProcObservation reads one structured FILTER_STATE object plus the
// legacy flat fields. Unknown metadata is deliberately ignored.
func parseExtProcObservation(entry higressAccessLog, raw json.RawMessage, processorHint string) *domain.ExtProcObservation {
	values := map[string]any{}
	if len(raw) > 0 && string(raw) != "null" {
		var structured map[string]any
		if json.Unmarshal(raw, &structured) == nil {
			values = structured
		} else {
			var encoded string
			if json.Unmarshal(raw, &encoded) == nil {
				_ = json.Unmarshal([]byte(encoded), &values)
			}
		}
	}
	getString := func(keys ...string) string {
		for _, key := range keys {
			if value, ok := values[key]; ok {
				if text := scalarText(value); text != "" {
					return text
				}
			}
		}
		return ""
	}
	getInt := func(keys ...string) int64 {
		for _, key := range keys {
			if value, ok := values[key]; ok {
				if number := scalarInt64(value); number != 0 {
					return number
				}
			}
		}
		return 0
	}
	getIntValue := func(keys ...string) (int64, bool) {
		for _, key := range keys {
			if value, ok := values[key]; ok {
				return scalarInt64(value), true
			}
		}
		return 0, false
	}
	getBool := func(keys ...string) bool {
		for _, key := range keys {
			if value, ok := values[key]; ok {
				if boolean, present := scalarBool(value); present {
					return boolean
				}
			}
		}
		return false
	}
	observation := &domain.ExtProcObservation{
		Processor:                 getString("processor", "ext_proc_processor"),
		RuleID:                    getString("rule_id", "ruleID", "ext_proc_rule_id"),
		SelectedPool:              getString("selected_pool", "selectedPool", "ext_proc_selected_pool"),
		SelectedEndpoint:          getString("selected_endpoint", "selectedEndpoint", "ext_proc_selected_endpoint"),
		ReasonCode:                getString("reason_code", "reasonCode", "ext_proc_reason_code"),
		RequestHeaderCalls:        int(getInt("request_header_call_count", "requestHeaderCallCount")),
		RequestBodyCalls:          int(getInt("request_body_call_count", "requestBodyCallCount")),
		ResponseHeaderCalls:       int(getInt("response_header_call_count", "responseHeaderCallCount")),
		ResponseBodyCalls:         int(getInt("response_body_call_count", "responseBodyCallCount")),
		RequestHeaderLatencyUS:    getInt("request_header_latency_us", "requestHeaderLatencyUs"),
		RequestBodyLatencyUS:      getInt("request_body_total_latency_us", "requestBodyTotalLatencyUs", "request_body_latency_us", "requestBodyLatencyUs"),
		ResponseHeaderLatencyUS:   getInt("response_header_latency_us", "responseHeaderLatencyUs"),
		ResponseBodyLatencyUS:     getInt("response_body_total_latency_us", "responseBodyTotalLatencyUs", "response_body_latency_us", "responseBodyLatencyUs"),
		GRPCStatus:                getString("grpc_status", "grpcStatus", "status"),
		FailureModeAllowed:        getBool("failure_mode_allowed", "failureModeAllowed"),
		FailedOpen:                getBool("failed_open", "failedOpen"),
		MessageTimeout:            getBool("message_timeout", "messageTimeout"),
		HTTPError:                 getBool("http_not_ok_resp_received", "httpNotOkRespReceived", "http_error"),
		ReceivedImmediateResponse: getBool("received_immediate_response", "receivedImmediateResponse"),
	}
	requestHeaderStatus, hasRequestHeaderStatus := getIntValue("request_header_call_status", "requestHeaderCallStatus")
	requestBodyStatus, hasRequestBodyStatus := getIntValue("request_body_last_call_status", "requestBodyLastCallStatus")
	responseHeaderStatus, hasResponseHeaderStatus := getIntValue("response_header_call_status", "responseHeaderCallStatus")
	responseBodyStatus, hasResponseBodyStatus := getIntValue("response_body_last_call_status", "responseBodyLastCallStatus")
	grpcStatusBeforeFirstCall, hasGRPCStatusBeforeFirstCall := getIntValue("grpc_status_before_first_call", "grpcStatusBeforeFirstCall")
	if observation.RequestHeaderCalls == 0 && (observation.RequestHeaderLatencyUS > 0 || hasRequestHeaderStatus) {
		observation.RequestHeaderCalls = 1
	}
	if observation.ResponseHeaderCalls == 0 && (observation.ResponseHeaderLatencyUS > 0 || hasResponseHeaderStatus) {
		observation.ResponseHeaderCalls = 1
	}
	if observation.Processor == "" {
		observation.Processor = strings.TrimSpace(entry.ExtProcProcessor)
	}
	if observation.Processor == "" {
		observation.Processor = processorHint
	}
	if observation.RuleID == "" {
		observation.RuleID = strings.TrimSpace(entry.ExtProcRuleID)
	}
	if observation.SelectedPool == "" {
		observation.SelectedPool = strings.TrimSpace(entry.ExtProcSelectedPool)
	}
	if observation.SelectedEndpoint == "" {
		observation.SelectedEndpoint = strings.TrimSpace(entry.ExtProcSelectedEndpoint)
	}
	if observation.ReasonCode == "" {
		observation.ReasonCode = strings.TrimSpace(entry.ExtProcReasonCode)
	}
	if observation.RequestHeaderCalls == 0 {
		observation.RequestHeaderCalls = integer(string(entry.ExtProcHeaderCalls))
	}
	if observation.RequestBodyCalls == 0 {
		observation.RequestBodyCalls = integer(string(entry.ExtProcBodyCalls))
	}
	if observation.ResponseHeaderCalls == 0 {
		observation.ResponseHeaderCalls = integer(string(entry.ExtProcResponseHeaderCalls))
	}
	if observation.ResponseBodyCalls == 0 {
		observation.ResponseBodyCalls = integer(string(entry.ExtProcResponseBodyCalls))
	}
	if observation.RequestHeaderLatencyUS == 0 {
		observation.RequestHeaderLatencyUS = integer64(string(entry.ExtProcHeaderLatencyUS))
	}
	if observation.RequestBodyLatencyUS == 0 {
		observation.RequestBodyLatencyUS = integer64(string(entry.ExtProcBodyTotalLatencyUS))
		if observation.RequestBodyLatencyUS == 0 {
			observation.RequestBodyLatencyUS = integer64(string(entry.ExtProcBodyLatencyUS))
		}
	}
	if observation.ResponseHeaderLatencyUS == 0 {
		observation.ResponseHeaderLatencyUS = integer64(string(entry.ExtProcResponseHeaderLatencyUS))
	}
	if observation.ResponseBodyLatencyUS == 0 {
		observation.ResponseBodyLatencyUS = integer64(string(entry.ExtProcResponseBodyTotalLatencyUS))
		if observation.ResponseBodyLatencyUS == 0 {
			observation.ResponseBodyLatencyUS = integer64(string(entry.ExtProcResponseBodyLatencyUS))
		}
	}
	if !hasRequestHeaderStatus && string(entry.ExtProcHeaderCallStatus) != "" {
		requestHeaderStatus, hasRequestHeaderStatus = integer64(string(entry.ExtProcHeaderCallStatus)), true
	}
	if !hasRequestBodyStatus && string(entry.ExtProcBodyLastCallStatus) != "" {
		requestBodyStatus, hasRequestBodyStatus = integer64(string(entry.ExtProcBodyLastCallStatus)), true
	}
	if !hasResponseHeaderStatus && string(entry.ExtProcResponseHeaderCallStatus) != "" {
		responseHeaderStatus, hasResponseHeaderStatus = integer64(string(entry.ExtProcResponseHeaderCallStatus)), true
	}
	if !hasResponseBodyStatus && string(entry.ExtProcResponseBodyLastCallStatus) != "" {
		responseBodyStatus, hasResponseBodyStatus = integer64(string(entry.ExtProcResponseBodyLastCallStatus)), true
	}
	if !hasGRPCStatusBeforeFirstCall && string(entry.ExtProcGRPCStatusBeforeFirstCall) != "" {
		grpcStatusBeforeFirstCall, hasGRPCStatusBeforeFirstCall = integer64(string(entry.ExtProcGRPCStatusBeforeFirstCall)), true
	}
	if observation.RequestHeaderCalls == 0 && (observation.RequestHeaderLatencyUS > 0 || hasRequestHeaderStatus) {
		observation.RequestHeaderCalls = 1
	}
	if observation.ResponseHeaderCalls == 0 && (observation.ResponseHeaderLatencyUS > 0 || hasResponseHeaderStatus) {
		observation.ResponseHeaderCalls = 1
	}
	if observation.GRPCStatus == "" {
		observation.GRPCStatus = strings.TrimSpace(entry.ExtProcGRPCStatus)
	}
	if observation.GRPCStatus == "" {
		statuses := []struct {
			value   int64
			present bool
		}{{requestHeaderStatus, hasRequestHeaderStatus}, {requestBodyStatus, hasRequestBodyStatus}, {responseHeaderStatus, hasResponseHeaderStatus}, {responseBodyStatus, hasResponseBodyStatus}, {grpcStatusBeforeFirstCall, hasGRPCStatusBeforeFirstCall && grpcStatusBeforeFirstCall != 0}}
		for _, status := range statuses {
			if status.present && (observation.GRPCStatus == "" || status.value != 0) {
				observation.GRPCStatus = strconv.FormatInt(status.value, 10)
			}
			if status.present && status.value == 4 {
				observation.MessageTimeout = true
			}
			if status.present && status.value != 0 {
				break
			}
		}
	}
	if !observation.FailureModeAllowed {
		observation.FailureModeAllowed = bool(entry.ExtProcFailureModeAllowed)
	}
	if !observation.FailedOpen {
		observation.FailedOpen = bool(entry.ExtProcFailedOpen)
	}
	if !observation.MessageTimeout {
		observation.MessageTimeout = bool(entry.ExtProcMessageTimeout)
	}
	if !observation.HTTPError {
		observation.HTTPError = bool(entry.ExtProcHTTPError)
	}
	if !observation.ReceivedImmediateResponse {
		observation.ReceivedImmediateResponse = bool(entry.ExtProcReceivedImmediateResponse)
	}

	hasTelemetry := len(raw) > 0 || observation.Processor != "" || observation.RuleID != "" ||
		observation.SelectedPool != "" || observation.SelectedEndpoint != "" || observation.ReasonCode != "" ||
		observation.RequestHeaderCalls > 0 || observation.RequestBodyCalls > 0 || observation.ResponseHeaderCalls > 0 ||
		observation.ResponseBodyCalls > 0 || observation.RequestHeaderLatencyUS > 0 || observation.RequestBodyLatencyUS > 0 ||
		observation.ResponseHeaderLatencyUS > 0 || observation.ResponseBodyLatencyUS > 0 || observation.GRPCStatus != "" ||
		observation.FailureModeAllowed || observation.FailedOpen || observation.MessageTimeout || observation.HTTPError || observation.ReceivedImmediateResponse
	if !hasTelemetry {
		return nil
	}
	observation.Invoked = observation.RequestHeaderCalls+observation.RequestBodyCalls+observation.ResponseHeaderCalls+observation.ResponseBodyCalls > 0 ||
		observation.GRPCStatus != "" || observation.HTTPError || observation.MessageTimeout || observation.ReceivedImmediateResponse
	switch {
	case observation.FailedOpen:
		observation.Outcome = domain.ExtProcOutcomeFailOpen
	case observation.MessageTimeout:
		if observation.FailureModeAllowed {
			observation.Outcome = domain.ExtProcOutcomeFailOpen
		} else {
			observation.Outcome = domain.ExtProcOutcomeTimeout
		}
	case observation.HTTPError:
		if observation.FailureModeAllowed {
			observation.Outcome = domain.ExtProcOutcomeFailOpen
		} else {
			observation.Outcome = domain.ExtProcOutcomeError
		}
	case observation.ReceivedImmediateResponse:
		observation.Outcome = domain.ExtProcOutcomeImmediateResponse
	case observation.GRPCStatus != "" && observation.GRPCStatus != "0" && !strings.EqualFold(observation.GRPCStatus, "ok"):
		if observation.FailureModeAllowed {
			observation.Outcome = domain.ExtProcOutcomeFailOpen
		} else {
			observation.Outcome = domain.ExtProcOutcomeError
		}
	case observation.Invoked:
		observation.Outcome = domain.ExtProcOutcomeSuccess
	default:
		observation.Outcome = domain.ExtProcOutcomeUnknown
	}
	return observation
}

func scalarText(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatInt(int64(typed), 10)
	case bool:
		return strconv.FormatBool(typed)
	default:
		return ""
	}
}

func scalarInt64(value any) int64 {
	parsed, _ := strconv.ParseInt(scalarText(value), 10, 64)
	return parsed
}

func scalarBool(value any) (bool, bool) {
	switch typed := value.(type) {
	case bool:
		return typed, true
	case string:
		text := strings.TrimSpace(typed)
		return strings.EqualFold(text, "true") || text == "1", true
	default:
		return false, false
	}
}

func redactQuery(path string) string {
	path, _, _ = strings.Cut(path, "?")
	return path
}

func ParseHigressLinesForProbe(content, probeID, traceID, clusterID, pod, source string) ([]domain.ObservedHop, []string) {
	var hops []domain.ObservedHop
	var warnings []string
	for number, line := range strings.Split(content, "\n") {
		hop, matched, err := ParseHigressLineForProbe(line, probeID, traceID, clusterID, pod, source)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("line %d: %v", number+1, err))
			continue
		}
		if matched {
			hops = append(hops, hop)
		}
	}
	return hops, warnings
}

func integer(value string) int {
	parsed, _ := strconv.Atoi(strings.TrimSpace(value))
	return parsed
}

func integer64(value string) int64 {
	parsed, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return parsed
}
