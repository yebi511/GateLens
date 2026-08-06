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

func TestHTTPRouteResolvesInferencePoolAndSelectedPods(t *testing.T) {
	stores := emptyStores(13)
	mustAdd(t, stores[2], &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "inference"}})
	mustAdd(t, stores[4], object("HTTPRoute", "inference", "chat", map[string]any{
		"rules": []any{map[string]any{"backendRefs": []any{map[string]any{
			"group": "inference.networking.k8s.io", "kind": "InferencePool", "name": "qwen",
		}}}},
	}))
	pool := object("InferencePool", "inference", "qwen", map[string]any{
		"selector": map[string]any{"matchLabels": map[string]any{"app": "qwen"}},
	})
	pool.SetAPIVersion("inference.networking.k8s.io/v1")
	mustAdd(t, stores[12], pool)
	ready := corev1.ConditionTrue
	mustAdd(t, stores[10], &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "qwen-ready", Namespace: "inference", Labels: map[string]string{"app": "qwen"}},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{
			Type: corev1.PodReady, Status: ready,
		}}},
	})
	mustAdd(t, stores[10], &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "other-model", Namespace: "inference", Labels: map[string]string{"app": "other"}},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning},
	})

	store := &Store{clusterID: "test"}
	store.rebuild(stores...)

	requireNode(t, store.Topology(), "InferencePool", "qwen")
	requireNode(t, store.Topology(), "Pod", "qwen-ready")
	if hasNode(store.Topology(), "Pod", "other-model") {
		t.Fatalf("InferencePool included a Pod outside its selector: %#v", store.Topology().Nodes)
	}
	if !hasEdge(store.Topology(), "route/inference/chat", "inferencepool/inference/qwen", "routes") {
		t.Fatalf("missing HTTPRoute -> InferencePool edge: %#v", store.Topology().Edges)
	}
	if !hasEdge(store.Topology(), "inferencepool/inference/qwen", "pod/inference/qwen-ready", "selects") {
		t.Fatalf("missing InferencePool -> Pod edge: %#v", store.Topology().Edges)
	}
	if len(store.Findings()) != 0 {
		t.Fatalf("findings=%#v", store.Findings())
	}
}

func TestInferencePoolBuildsEndpointPickerServiceEndpointChain(t *testing.T) {
	stores := emptyStores(13)
	ready := true
	mustAdd(t, stores[0], &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "qwen-service", Namespace: "inference"},
		Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "qwen"}},
	})
	mustAdd(t, stores[1], &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name: "qwen-service-a", Namespace: "inference",
			Labels: map[string]string{discoveryv1.LabelServiceName: "qwen-service"},
		},
		Endpoints: []discoveryv1.Endpoint{{
			Addresses: []string{"10.0.0.9"}, Conditions: discoveryv1.EndpointConditions{Ready: &ready},
		}},
	})
	mustAdd(t, stores[4], object("HTTPRoute", "inference", "chat", map[string]any{
		"rules": []any{map[string]any{"backendRefs": []any{map[string]any{
			"group": "inference.networking.k8s.io", "kind": "InferencePool", "name": "qwen",
		}}}},
	}))
	pool := object("InferencePool", "inference", "qwen", map[string]any{
		"selector": map[string]any{"matchLabels": map[string]any{"app": "qwen"}},
		"endpointPickerRef": map[string]any{
			"name": "qwen-epp", "kind": "Service", "port": map[string]any{"number": int64(9002)},
		},
	})
	pool.SetAPIVersion("inference.networking.k8s.io/v1")
	mustAdd(t, stores[12], pool)

	store := &Store{clusterID: "test"}
	store.rebuild(stores...)

	requireNode(t, store.Topology(), "EndpointPicker", "qwen-epp")
	requireNode(t, store.Topology(), "Service", "qwen-service")
	requireNode(t, store.Topology(), "Endpoint", "10.0.0.9")
	poolID := "inferencepool/inference/qwen"
	eppID := "endpoint-picker/inference/qwen-epp"
	serviceID := "service/inference/qwen-service"
	if !hasEdge(store.Topology(), poolID, eppID, "discovers") || !hasEdge(store.Topology(), eppID, serviceID, "selects") || !hasEdge(store.Topology(), serviceID, "endpoint/inference/qwen-service/10.0.0.9", "selects") {
		t.Fatalf("missing InferencePool -> EPP -> Service -> Endpoint chain: %#v", store.Topology().Edges)
	}
}

func hasNode(topology domain.Topology, kind, name string) bool {
	for _, node := range topology.Nodes {
		if node.Kind == kind && node.Name == name {
			return true
		}
	}
	return false
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
