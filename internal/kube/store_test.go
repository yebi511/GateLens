package kube

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/tools/cache"

	"github.com/gatelens/gatelens/internal/domain"
)

func TestRebuildAcrossNamespaces(t *testing.T) {
	stores := make([]cache.Store, 6)
	for i := range stores {
		stores[i] = cache.NewStore(cache.MetaNamespaceKeyFunc)
	}
	ready := true
	mustAdd(t, stores[0], &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "qwen", Namespace: "inference"}})
	mustAdd(t, stores[1], &discoveryv1.EndpointSlice{ObjectMeta: metav1.ObjectMeta{Name: "qwen-a", Namespace: "inference", Labels: map[string]string{discoveryv1.LabelServiceName: "qwen"}}, Endpoints: []discoveryv1.Endpoint{{Addresses: []string{"10.0.0.8"}, Conditions: discoveryv1.EndpointConditions{Ready: &ready}}}})
	mustAdd(t, stores[2], &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "higress-system"}})
	mustAdd(t, stores[2], &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "inference"}})
	mustAdd(t, stores[3], object("Gateway", "higress-system", "ai-gateway", map[string]any{"listeners": []any{map[string]any{"name": "https", "protocol": "HTTPS", "port": int64(443), "hostname": "api.example.com"}}}))
	mustAdd(t, stores[4], object("HTTPRoute", "inference", "chat", map[string]any{"hostnames": []any{"api.example.com"}, "parentRefs": []any{map[string]any{"name": "ai-gateway", "namespace": "higress-system", "sectionName": "https"}}, "rules": []any{map[string]any{"matches": []any{map[string]any{"method": "POST", "path": map[string]any{"type": "PathPrefix", "value": "/v1/chat"}}}, "backendRefs": []any{map[string]any{"name": "qwen"}}}}}))
	mustAdd(t, stores[5], object("ReferenceGrant", "inference", "allow-route", map[string]any{"from": []any{map[string]any{"group": "gateway.networking.k8s.io", "kind": "HTTPRoute", "namespace": "inference"}}, "to": []any{map[string]any{"group": "", "kind": "Service", "name": "qwen"}}}))

	store := &Store{clusterID: "test"}
	store.rebuild(stores...)
	if got := len(store.Findings()); got != 0 {
		t.Fatalf("findings=%d, want 0: %#v", got, store.Findings())
	}
	kinds := map[string]int{}
	for _, node := range store.Topology().Nodes {
		kinds[node.Kind]++
	}
	if kinds["Listener"] != 1 || kinds["Endpoint"] != 1 {
		t.Fatalf("topology kinds=%v", kinds)
	}
	result := store.Explain(domain.RouteExplanationRequest{Host: "api.example.com", Path: "/v1/chat/completions", Method: "POST", Namespace: "inference"})
	if result.Outcome != "Routed" {
		t.Fatalf("outcome=%s, want Routed: %#v", result.Outcome, result)
	}
}

func mustAdd(t *testing.T, store cache.Store, value any) {
	t.Helper()
	if err := store.Add(value); err != nil {
		t.Fatal(err)
	}
}
func object(kind, namespace, name string, spec map[string]any) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{"apiVersion": "gateway.networking.k8s.io/v1", "kind": kind, "metadata": map[string]any{"namespace": namespace, "name": name}, "spec": spec}}
}
