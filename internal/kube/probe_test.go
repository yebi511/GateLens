package kube

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gatelens/gatelens/internal/domain"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestExecuteProbeReadsFileIncrementAndInjectsIdentifiers(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "access.log")
	if err := os.WriteFile(logPath, []byte("old log\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Request-ID") != "" || r.Header.Get("X-GateLens-Probe-ID") != "probe-1" || r.Header.Get("X-B3-TraceId") != "trace-1" || r.Header.Get("Authorization") != "Bearer secret-key" || r.Host != "api.example.com" {
			t.Errorf("headers=%v host=%q", r.Header, r.Host)
		}
		line := fmt.Sprintf(`{"gatelens_probe_id":"%s","request_id":"envoy-generated","trace_id":"%s","route_name":"chat","upstream_cluster":"qwen","upstream_host":"10.0.0.8:8000","response_code":"200","duration":"4","start_time":"2026-08-13T10:00:00Z"}`+"\n", r.Header.Get("X-GateLens-Probe-ID"), r.Header.Get("X-B3-TraceId"))
		file, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			t.Error(err)
			http.Error(w, "log", http.StatusInternalServerError)
			return
		}
		_, _ = file.WriteString(line)
		_ = file.Close()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)

	port := int32(80)
	if parsed.Port() != "" {
		port = int32(mustPort(t, parsed.Port()))
	}
	entry := domain.ProbeEntry{ID: "entry/higress", GatewayID: "gateway/higress-system/higress", DNSName: parsed.Hostname(), Port: port, Scheme: parsed.Scheme}
	store := &Store{clusterID: "edge", probeLogFile: logPath, snapshot: snapshot{runtimes: map[string]gatewayRuntime{
		"gateway/higress-system/higress": {GatewayID: "gateway/higress-system/higress", Pods: []proxyPod{{Name: "higress-1", Namespace: "higress-system"}}},
	}, probeEntries: map[string]domain.ProbeEntry{entry.ID: entry}}}
	result, err := store.ExecuteProbe(context.Background(), domain.ProbeCommand{
		ProbeID: "probe-1", TraceID: "trace-1", GatewayID: "gateway/higress-system/higress",
		EntryID: entry.ID, Method: http.MethodPost, Path: "/v1/chat", Host: "api.example.com", APIKey: "secret-key", Body: `{}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "completed" || result.ResponseCode != http.StatusOK || len(result.Hops) != 1 || result.Hops[0].RouteName != "chat" || !result.EvidenceComplete {
		t.Fatalf("result=%#v", result)
	}
}

func TestDiscoverGatewayProbeEntriesUsesServiceSelectorAndExplicitHTTPProtocols(t *testing.T) {
	https := "https"
	snap := snapshot{
		context:  domain.Context{Cluster: domain.Cluster{ID: "edge"}},
		topology: domain.Topology{Nodes: []domain.TopologyNode{{ID: "gateway/higress", Kind: "Gateway"}}},
		runtimes: map[string]gatewayRuntime{"gateway/higress": {
			GatewayID: "gateway/higress", Namespace: "higress-system",
			Pods: []proxyPod{{Name: "higress-1", Labels: map[string]string{"app": "higress-gateway"}}},
		}},
		probeEntries: map[string]domain.ProbeEntry{},
	}
	services := map[string]*corev1.Service{
		"higress-system/higress-gateway": {Spec: corev1.ServiceSpec{ClusterIP: "10.96.0.10", Selector: map[string]string{"app": "higress-gateway"}, Ports: []corev1.ServicePort{
			{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP},
			{Name: "secure", AppProtocol: &https, Port: 443, Protocol: corev1.ProtocolTCP},
			{Name: "metrics", Port: 15020, Protocol: corev1.ProtocolTCP},
		}}, ObjectMeta: metav1.ObjectMeta{Name: "higress-gateway", Namespace: "higress-system"}},
		"higress-system/other": {Spec: corev1.ServiceSpec{Selector: map[string]string{"app": "other"}, Ports: []corev1.ServicePort{{Name: "http", Port: 8080}}}, ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: "higress-system"}},
	}
	endpoints := map[string][]domain.TopologyNode{"higress-system/higress-gateway": {{Conditions: []string{"Address=10.233.1.7"}}}}
	discoverGatewayProbeEntries(&snap, services, endpoints)
	if len(snap.topology.ProbeEntries) != 2 {
		t.Fatalf("entries=%#v", snap.topology.ProbeEntries)
	}
	if snap.topology.ProbeEntries[0].Scheme != "http" || snap.topology.ProbeEntries[1].Scheme != "https" {
		t.Fatalf("entries=%#v", snap.topology.ProbeEntries)
	}
	if got := strings.Join(snap.topology.ProbeEntries[0].Addresses, ","); got != "10.233.1.7,10.96.0.10" {
		t.Fatalf("addresses=%q", got)
	}
	for _, condition := range []string{"Service=higress-system/higress-gateway", "ServiceDNS=higress-gateway.higress-system.svc.cluster.local", "Address=10.233.1.7", "Address=10.96.0.10"} {
		if !hasCondition(snap.topology.Nodes[0].Conditions, condition) {
			t.Fatalf("gateway conditions=%v, missing %q", snap.topology.Nodes[0].Conditions, condition)
		}
	}
}

func TestExecuteProbeRejectsUnknownEntry(t *testing.T) {
	store := &Store{clusterID: "edge", snapshot: snapshot{runtimes: map[string]gatewayRuntime{
		"gateway/test": {GatewayID: "gateway/test", Pods: []proxyPod{{Name: "gateway"}}},
	}}}
	_, err := store.ExecuteProbe(context.Background(), domain.ProbeCommand{ProbeID: "probe", GatewayID: "gateway/test", EntryID: "arbitrary", Method: http.MethodGet, Path: "/"})
	if err == nil {
		t.Fatal("expected unknown entry error")
	}
}

func TestExecuteProbeRejectsInvalidAPIKey(t *testing.T) {
	entry := domain.ProbeEntry{ID: "entry/test", GatewayID: "gateway/test", DNSName: "gateway.default.svc.cluster.local", Port: 80, Scheme: "http"}
	store := &Store{clusterID: "edge", snapshot: snapshot{
		runtimes:     map[string]gatewayRuntime{"gateway/test": {GatewayID: "gateway/test", Pods: []proxyPod{{Name: "gateway"}}}},
		probeEntries: map[string]domain.ProbeEntry{entry.ID: entry},
	}}
	_, err := store.ExecuteProbe(context.Background(), domain.ProbeCommand{ProbeID: "probe", GatewayID: "gateway/test", EntryID: entry.ID, Method: http.MethodGet, Path: "/", APIKey: "bad\r\nkey"})
	if err == nil {
		t.Fatal("expected invalid API key error")
	}
}

func mustPort(t *testing.T, value string) int {
	t.Helper()
	port, err := strconv.Atoi(value)
	if err != nil {
		t.Fatal(err)
	}
	return port
}

func TestObserveProbeReadsExistingLogWithoutTargetURL(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "access.log")
	line := `{"gatelens_probe_id":"probe-remote","request_id":"remote-envoy-id","route_name":"remote-route","response_code":"200","start_time":"2026-08-13T10:00:01Z"}` + "\n"
	if err := os.WriteFile(logPath, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	store := &Store{clusterID: "gpu", probeLogFile: logPath, snapshot: snapshot{runtimes: map[string]gatewayRuntime{
		"gateway/gpu": {GatewayID: "gateway/gpu", Pods: []proxyPod{{Name: "gpu-gateway"}}},
	}}}
	result, err := store.ObserveProbe(context.Background(), domain.ProbeCommand{
		ProbeID: "probe-remote", TraceID: "trace-remote", GatewayID: "gateway/gpu",
		StartedAt: "2026-08-13T10:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "completed" || len(result.Hops) != 1 || result.Hops[0].RouteName != "remote-route" || !result.EvidenceComplete {
		t.Fatalf("result=%#v", result)
	}
}
