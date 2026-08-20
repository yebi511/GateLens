package observed

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gatelens/gatelens/internal/domain"
)

type higressAccessLog struct {
	AILog                    string `json:"ai_log"`
	Authority                string `json:"authority"`
	DownstreamRemoteAddress  string `json:"downstream_remote_address"`
	Duration                 string `json:"duration"`
	Method                   string `json:"method"`
	Path                     string `json:"path"`
	Protocol                 string `json:"protocol"`
	GateLensProbeID          string `json:"gatelens_probe_id"`
	ResponseCode             string `json:"response_code"`
	ResponseCodeDetails      string `json:"response_code_details"`
	ResponseFlags            string `json:"response_flags"`
	RouteName                string `json:"route_name"`
	StartTime                string `json:"start_time"`
	TraceID                  string `json:"trace_id"`
	UpstreamCluster          string `json:"upstream_cluster"`
	UpstreamHost             string `json:"upstream_host"`
	UpstreamLocalAddress     string `json:"upstream_local_address"`
	UpstreamServiceTime      string `json:"upstream_service_time"`
	UpstreamTransportFailure string `json:"upstream_transport_failure_reason"`
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
	correlation := ""
	loggedProbeID := strings.TrimSpace(entry.GateLensProbeID)
	if loggedProbeID != "" {
		if loggedProbeID == probeID {
			correlation = "probe-id"
		}
	} else if traceID != "" && strings.EqualFold(strings.TrimSpace(entry.TraceID), strings.TrimSpace(traceID)) {
		correlation = "trace-id"
	}
	if correlation == "" {
		return domain.ObservedHop{}, false, nil
	}
	hop := domain.ObservedHop{
		ObservedAt: entry.StartTime, ClusterID: clusterID, Pod: pod,
		Authority: entry.Authority, Method: entry.Method, Path: redactQuery(entry.Path), Protocol: entry.Protocol,
		RouteName: entry.RouteName, UpstreamCluster: entry.UpstreamCluster, UpstreamHost: entry.UpstreamHost,
		UpstreamLocalAddress: entry.UpstreamLocalAddress, DownstreamRemoteAddress: entry.DownstreamRemoteAddress,
		ResponseFlags: entry.ResponseFlags, ResponseCodeDetails: entry.ResponseCodeDetails,
		UpstreamTransportFailure: entry.UpstreamTransportFailure, AILog: entry.AILog,
		EvidenceSource: source, Correlation: correlation, Confidence: "observed",
	}
	hop.ResponseCode = integer(entry.ResponseCode)
	hop.DurationMillis = integer64(entry.Duration)
	hop.UpstreamServiceTimeMillis = integer64(entry.UpstreamServiceTime)
	if hop.ObservedAt == "" {
		hop.ObservedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	return hop, true, nil
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
