package kube

import (
	"testing"

	corev1 "k8s.io/api/core/v1"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	"github.com/gatelens/gatelens/internal/domain"
)

func TestHigressIngressMcpBridge(t *testing.T) {
	stores := make([]cache.Store, 8)
	for i := range stores {
		stores[i] = cache.NewStore(cache.MetaNamespaceKeyFunc)
	}
	mustAdd(t, stores[2], &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "higress-system"}})
	class := "higress"
	apiGroup := "networking.higress.io"
	pathType := networkingv1.PathTypePrefix
	mustAdd(t, stores[6], &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mcp-api", Namespace: "higress-system",
			Annotations: map[string]string{"higress.io/destination": "github.dns"},
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: &class,
			Rules: []networkingv1.IngressRule{{
				Host: "mcp.example.com",
				IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{Paths: []networkingv1.HTTPIngressPath{{
					Path: "/v1", PathType: &pathType,
					Backend: networkingv1.IngressBackend{Resource: &corev1.TypedLocalObjectReference{
						APIGroup: &apiGroup, Kind: "McpBridge", Name: "github-bridge",
					}},
				}}}},
			}},
		},
	})
	mustAdd(t, stores[7], object("McpBridge", "higress-system", "github-bridge", map[string]any{
		"registries": []any{map[string]any{"name": "github", "type": "dns", "domain": "api.github.com", "port": int64(443), "protocol": "https"}},
	}))

	store := &Store{clusterID: "test"}
	store.rebuild(stores...)

	requireNode(t, store.Topology(), "Ingress", "mcp-api")
	requireNode(t, store.Topology(), "McpBridge", "github-bridge")
	requireNode(t, store.Topology(), "Registry", "github")
	for _, node := range store.Topology().Nodes {
		if node.Kind == "Registry" && node.Name == "github" {
			if !hasCondition(node.Conditions, "Protocol=https") {
				t.Fatalf("registry conditions=%#v", node.Conditions)
			}
		}
	}
	for _, node := range store.Topology().Nodes {
		if node.Kind == "GatewayWorkload" {
			t.Fatalf("Ingress compatibility must not create a GatewayWorkload node: %#v", node)
		}
	}
	if !hasEdge(store.Topology(), "ingress/higress-system/mcp-api", "mcpbridge/higress-system/github-bridge/registry/github", "selects") {
		t.Fatalf("expected Ingress destination to select McpBridge registry: %#v", store.Topology().Edges)
	}
	result := store.Explain(domain.RouteExplanationRequest{Host: "mcp.example.com", Path: "/v1/tools", Method: "GET", Namespace: "higress-system"})
	if result.Outcome != "Routed" {
		t.Fatalf("outcome=%s, want Routed: %#v", result.Outcome, result)
	}
}

func TestNginxIngressProvidesRemoteEntryKeysWithoutHigressCRD(t *testing.T) {
	stores := make([]cache.Store, 7)
	for i := range stores {
		stores[i] = cache.NewStore(cache.MetaNamespaceKeyFunc)
	}
	mustAdd(t, stores[2], &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "inference"}})
	class := "nginx"
	mustAdd(t, stores[6], &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "inference-gateway-ingress", Namespace: "inference"},
		Spec: networkingv1.IngressSpec{
			IngressClassName: &class,
			Rules:            []networkingv1.IngressRule{{Host: "inference-gateway.infra-prd.sail-cloud.com"}},
		},
		Status: networkingv1.IngressStatus{LoadBalancer: networkingv1.IngressLoadBalancerStatus{
			Ingress: []networkingv1.IngressLoadBalancerIngress{{IP: "10.27.0.29"}},
		}},
	})

	store := &Store{clusterID: "gpu"}
	store.rebuild(stores...)

	for _, node := range store.Topology().Nodes {
		if node.Kind != "Ingress" || node.Name != "inference-gateway-ingress" {
			continue
		}
		for _, condition := range []string{
			"IngressClass=nginx",
			"Hostname=inference-gateway.infra-prd.sail-cloud.com",
			"Address=10.27.0.29",
		} {
			if !hasCondition(node.Conditions, condition) {
				t.Fatalf("missing %q in %#v", condition, node.Conditions)
			}
		}
		return
	}
	t.Fatalf("missing nginx Ingress node: %#v", store.Topology().Nodes)
}

func requireNode(t *testing.T, topology domain.Topology, kind, name string) {
	t.Helper()
	for _, node := range topology.Nodes {
		if node.Kind == kind && node.Name == name {
			return
		}
	}
	t.Fatalf("missing %s node %q: %#v", kind, name, topology.Nodes)
}

func hasEdge(topology domain.Topology, from, to, relation string) bool {
	for _, edge := range topology.Edges {
		if edge.From == from && edge.To == to && edge.Relation == relation {
			return true
		}
	}
	return false
}
