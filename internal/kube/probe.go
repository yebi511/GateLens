package kube

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/gatelens/gatelens/internal/domain"
)

const (
	maxProbeBodyBytes     = 64 << 10
	maxProbeAPIKeyBytes   = 8 << 10
	maxProbeResponseBytes = 1 << 20
	maxProbeLogBytes      = 8 << 20
)

func (s *Store) ExecuteProbe(ctx context.Context, command domain.ProbeCommand) (domain.ProbeAgentResult, error) {
	started := time.Now().UTC()
	result := domain.ProbeAgentResult{
		SchemaVersion: domain.ProbeSchemaVersion,
		ProbeID:       command.ProbeID,
		TraceID:       command.TraceID,
		ClusterID:     s.clusterID,
		GatewayID:     command.GatewayID,
		StartedAt:     started.Format(time.RFC3339Nano),
		HTTP:          &domain.ProbeHTTPResult{Method: command.Method},
		Hops:          []domain.ObservedHop{},
		Issues:        []domain.ProbeIssue{},
	}
	if !command.Method.IsValid() {
		return result, fmt.Errorf("unsupported probe method %q", command.Method)
	}
	runtime, err := s.probeGatewayRuntime(command.GatewayID)
	if err != nil {
		return result, err
	}
	if len(command.Body) > maxProbeBodyBytes {
		return result, fmt.Errorf("probe body exceeds %d bytes", maxProbeBodyBytes)
	}
	if len(command.APIKey) > maxProbeAPIKeyBytes || strings.ContainsAny(command.APIKey, "\r\n") {
		return result, fmt.Errorf("probe API key is invalid")
	}
	_, target, err := s.resolveProbeEntry(command.GatewayID, command.EntryID, command.Path)
	if err != nil {
		return result, err
	}
	result.HTTP.Target = redactedProbeTarget(target)
	var fileOffset int64
	if s.probeLogFile != "" {
		if info, statErr := os.Stat(s.probeLogFile); statErr == nil {
			fileOffset = info.Size()
		}
	}
	request, err := http.NewRequestWithContext(ctx, string(command.Method), target, strings.NewReader(command.Body))
	if err != nil {
		return result, fmt.Errorf("create probe request: %w", err)
	}
	request.Header.Set("X-B3-TraceId", command.TraceID)
	request.Header.Set("X-GateLens-Probe-ID", command.ProbeID)
	request.Header.Set("User-Agent", "GateLens-Probe/1.0")
	if command.APIKey != "" {
		request.Header.Set("Authorization", "Bearer "+command.APIKey)
		command.APIKey = ""
	}
	if command.ContentType != "" {
		request.Header.Set("Content-Type", command.ContentType)
	}
	if command.Host != "" {
		request.Host = command.Host
	}

	responseStarted := time.Now()
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	response, requestErr := client.Do(request)
	if requestErr == nil {
		result.HTTP.ResponseCode = response.StatusCode
		read, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, maxProbeResponseBytes+1))
		_ = response.Body.Close()
		result.HTTP.ResponseBytes = read
		if readErr != nil {
			requestErr = fmt.Errorf("read probe response: %w", readErr)
		} else if read > maxProbeResponseBytes {
			result.Issues = domain.AppendProbeIssue(result.Issues, domain.ProbeIssue{Scope: domain.ProbeIssueScopeExecution, Code: domain.ProbeIssueCodeResponseLimitExceeded, Message: "响应正文超过采集上限；正文未保存"})
		}
	}
	result.HTTP.DurationMillis = time.Since(responseStarted).Milliseconds()

	// Access logs are written when the HTTP stream completes. Give the container
	// runtime a short bounded interval to make the record available.
	logs := s.observeProbeWindow(ctx, runtime, started.Add(-time.Second), fileOffset, command.ProbeID, command.TraceID, 2*time.Second)
	result.Hops, result.Collection = logs.hops, logs.collection
	result.Issues = append(result.Issues, logs.issues...)
	if len(result.Hops) == 0 {
		result.Issues = domain.AppendProbeIssue(result.Issues, domain.ProbeIssue{Scope: domain.ProbeIssueScopeCollection, Code: domain.ProbeIssueCodeNoMatchingLog, Message: "未找到匹配 gatelens_probe_id 或 trace_id 的 Higress JSON 访问日志"})
	}
	sort.SliceStable(result.Hops, func(i, j int) bool { return result.Hops[i].ObservedAt < result.Hops[j].ObservedAt })
	result.CompletedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if requestErr != nil {
		result.HTTP.Error = requestErr.Error()
	}
	return result, nil
}

// ObserveProbe only reads logs from a gateway. It never emits another HTTP
// request, so one probe can be correlated across gateways without duplicating
// traffic or side effects.
func (s *Store) ObserveProbe(ctx context.Context, command domain.ProbeCommand) (domain.ProbeAgentResult, error) {
	started := time.Now().UTC()
	result := domain.ProbeAgentResult{
		SchemaVersion: domain.ProbeSchemaVersion, ProbeID: command.ProbeID, TraceID: command.TraceID,
		ClusterID: s.clusterID, GatewayID: command.GatewayID, StartedAt: started.Format(time.RFC3339Nano),
		Hops: []domain.ObservedHop{}, Issues: []domain.ProbeIssue{},
	}
	runtime, err := s.probeGatewayRuntime(command.GatewayID)
	if err != nil {
		return result, err
	}
	since := started.Add(-time.Minute)
	if parsed, parseErr := time.Parse(time.RFC3339Nano, command.StartedAt); parseErr == nil {
		since = parsed.Add(-time.Second)
	}
	logs := s.observeProbeWindow(ctx, runtime, since, 0, command.ProbeID, command.TraceID, 4*time.Second)
	result.Hops, result.Collection = logs.hops, logs.collection
	result.Issues = append(result.Issues, logs.issues...)
	if len(result.Hops) == 0 {
		result.Issues = domain.AppendProbeIssue(result.Issues, domain.ProbeIssue{Scope: domain.ProbeIssueScopeCollection, Code: domain.ProbeIssueCodeNoMatchingLog, Message: "未找到匹配 gatelens_probe_id 或 trace_id 的网关访问日志"})
	}
	sort.SliceStable(result.Hops, func(i, j int) bool { return result.Hops[i].ObservedAt < result.Hops[j].ObservedAt })
	result.CompletedAt = time.Now().UTC().Format(time.RFC3339Nano)
	return result, nil
}

func (s *Store) resolveProbeEntry(gatewayID, entryID, path string) (domain.ProbeEntry, string, error) {
	s.mutex.RLock()
	entry, ok := s.snapshot.probeEntries[entryID]
	s.mutex.RUnlock()
	if !ok {
		return domain.ProbeEntry{}, "", fmt.Errorf("probe entry %q is not present in the current Kubernetes snapshot", entryID)
	}
	if !entry.Scheme.IsValid() {
		return domain.ProbeEntry{}, "", fmt.Errorf("unsupported probe scheme %q", entry.Scheme)
	}
	if entry.GatewayID != gatewayID {
		return domain.ProbeEntry{}, "", fmt.Errorf("probe entry %q does not belong to Gateway %q", entryID, gatewayID)
	}
	parsedPath, err := url.ParseRequestURI(path)
	if err != nil || parsedPath.IsAbs() || parsedPath.Host != "" || !strings.HasPrefix(parsedPath.Path, "/") || parsedPath.Fragment != "" {
		return domain.ProbeEntry{}, "", fmt.Errorf("probe path must be an absolute HTTP path beginning with /")
	}
	target := (&url.URL{Scheme: string(entry.Scheme), Host: fmt.Sprintf("%s:%d", entry.DNSName, entry.Port), Path: parsedPath.Path, RawPath: parsedPath.RawPath, RawQuery: parsedPath.RawQuery}).String()
	return entry, target, nil
}

func redactedProbeTarget(target string) string {
	parsed, err := url.Parse(target)
	if err != nil {
		return target
	}
	parsed.RawQuery, parsed.Fragment, parsed.User = "", "", nil
	return parsed.String()
}

func (s *Store) probeGatewayRuntime(gatewayID string) (gatewayRuntime, error) {
	s.mutex.RLock()
	runtime, ok := s.snapshot.runtimes[gatewayID]
	s.mutex.RUnlock()
	if !ok {
		return gatewayRuntime{}, fmt.Errorf("未找到 Gateway %q 对应的运行时工作负载", gatewayID)
	}
	if len(runtime.Pods) == 0 {
		return gatewayRuntime{}, fmt.Errorf("Gateway %q 没有 Ready Pod", gatewayID)
	}
	return runtime, nil
}
