package kube

import (
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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
			Annotations: map[string]string{"higress.io/destination": "github.dns:443"},
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
	requireNode(t, store.Topology(), "ExternalTarget", "api.github.com")
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
	if !hasEdge(store.Topology(), "mcpbridge/higress-system/github-bridge/registry/github", "mcpbridge/higress-system/github-bridge/registry/github/target", "resolves") {
		t.Fatalf("expected Registry domain to resolve to an external target: %#v", store.Topology().Edges)
	}
	result := store.Explain(domain.RouteExplanationRequest{Host: "mcp.example.com", Path: "/v1/tools", Method: "GET", Namespace: "higress-system"})
	if result.Outcome != "Routed" {
		t.Fatalf("outcome=%s, want Routed: %#v", result.Outcome, result)
	}
}

func TestHigressIngressSelectsSoleMcpBridgeRegistryWithoutDestination(t *testing.T) {
	stores := emptyStores(8)
	class := "higress"
	apiGroup := "networking.higress.io"
	pathType := networkingv1.PathTypePrefix
	mustAdd(t, stores[2], &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "higress-system"}})
	mustAdd(t, stores[6], &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "mcp-default", Namespace: "higress-system"},
		Spec: networkingv1.IngressSpec{
			IngressClassName: &class,
			Rules: []networkingv1.IngressRule{{
				IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{Paths: []networkingv1.HTTPIngressPath{{
					Path: "/", PathType: &pathType,
					Backend: networkingv1.IngressBackend{Resource: &corev1.TypedLocalObjectReference{
						APIGroup: &apiGroup, Kind: "McpBridge", Name: "only-bridge",
					}},
				}}}},
			}},
		},
	})
	mustAdd(t, stores[7], object("McpBridge", "higress-system", "only-bridge", map[string]any{
		"registries": []any{map[string]any{"name": "only", "type": "dns", "domain": "remote.example.com", "port": int64(443)}},
	}))

	store := &Store{clusterID: "test"}
	store.rebuild(stores...)
	if !hasEdge(store.Topology(), "ingress/higress-system/mcp-default", "mcpbridge/higress-system/only-bridge/registry/only", "selects") {
		t.Fatalf("expected Ingress to select the sole McpBridge registry: %#v", store.Topology().Edges)
	}
	if _, ok := soleRegistryID(map[string]string{"a": "registry-a", "b": "registry-b"}); ok {
		t.Fatal("multiple registries must not be selected implicitly")
	}
}
func TestMcpBridgeRegistryResolvesServiceAndEndpoints(t *testing.T) {
	stores := emptyStores(8)
	ready := true
	mustAdd(t, stores[0], &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "model-server", Namespace: "inference"}})
	mustAdd(t, stores[1], &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name: "model-server-a", Namespace: "inference",
			Labels: map[string]string{discoveryv1.LabelServiceName: "model-server"},
		},
		Endpoints: []discoveryv1.Endpoint{{
			Addresses: []string{"10.0.0.8"}, Conditions: discoveryv1.EndpointConditions{Ready: &ready},
		}},
	})
	mustAdd(t, stores[2], &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "gateway"}})
	mustAdd(t, stores[2], &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "inference"}})
	mustAdd(t, stores[7], object("McpBridge", "gateway", "model-bridge", map[string]any{
		"registries": []any{map[string]any{
			"name": "model", "type": "dns", "domain": "model-server.inference.svc.cluster.local", "port": int64(8000),
		}},
	}))

	store := &Store{clusterID: "test"}
	store.rebuild(stores...)

	requireNode(t, store.Topology(), "Service", "model-server")
	requireNode(t, store.Topology(), "Endpoint", "10.0.0.8")
	if hasNode(store.Topology(), "ExternalTarget", "model-server.inference.svc.cluster.local") {
		t.Fatalf("cluster Service DNS was represented as an ExternalTarget: %#v", store.Topology().Nodes)
	}
	registryID := "mcpbridge/gateway/model-bridge/registry/model"
	if !hasEdge(store.Topology(), registryID, "service/inference/model-server", "resolves") {
		t.Fatalf("missing Registry -> Service edge: %#v", store.Topology().Edges)
	}
	if !hasEdge(store.Topology(), "service/inference/model-server", "endpoint/inference/model-server/10.0.0.8", "selects") {
		t.Fatalf("missing Service -> Endpoint edge: %#v", store.Topology().Edges)
	}
	if len(store.Findings()) != 0 {
		t.Fatalf("findings=%#v", store.Findings())
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

func TestHigressIngressAttachesToUniqueCompatibleGateway(t *testing.T) {
	for _, test := range []struct {
		name             string
		withIngressClass bool
		legacyAnnotation bool
	}{
		{name: "IngressClass controller", withIngressClass: true},
		{name: "Higress compatibility fallback", legacyAnnotation: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			stores := emptyStores(12)
			class := "higress"
			ingress := &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: "model-route", Namespace: "models"}}
			if test.legacyAnnotation {
				ingress.Annotations = map[string]string{"kubernetes.io/ingress.class": class}
			} else {
				ingress.Spec.IngressClassName = &class
			}
			mustAdd(t, stores[6], ingress)
			if test.withIngressClass {
				mustAdd(t, stores[11], &networkingv1.IngressClass{
					ObjectMeta: metav1.ObjectMeta{Name: class},
					Spec:       networkingv1.IngressClassSpec{Controller: "higress.io/ingress-controller"},
				})
			}
			addReadyHigressGateway(t, stores, "higress-system")

			store := &Store{clusterID: "edge"}
			store.rebuild(stores...)
			if !hasEdge(store.Topology(), "gateway-runtime/higress-system/higress-gateway", "ingress/models/model-route", "attaches") {
				t.Fatalf("missing Gateway -> Ingress edge: %#v", store.Topology().Edges)
			}
			for _, edge := range store.Topology().Edges {
				if edge.From == "gateway-runtime/higress-system/higress-gateway" && edge.To == "ingress/models/model-route" && !strings.Contains(edge.Evidence, "IngressClass higress") {
					t.Fatalf("edge evidence=%q", edge.Evidence)
				}
			}
		})
	}
}

func TestHigressIngressDoesNotGuessBetweenMultipleGateways(t *testing.T) {
	stores := emptyStores(12)
	class := "higress"
	mustAdd(t, stores[6], &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "model-route", Namespace: "models"},
		Spec:       networkingv1.IngressSpec{IngressClassName: &class},
	})
	mustAdd(t, stores[11], &networkingv1.IngressClass{
		ObjectMeta: metav1.ObjectMeta{Name: class},
		Spec:       networkingv1.IngressClassSpec{Controller: "higress.io/ingress-controller"},
	})
	addReadyHigressGateway(t, stores, "higress-a")
	addReadyHigressGateway(t, stores, "higress-b")

	store := &Store{clusterID: "edge"}
	store.rebuild(stores...)
	for _, edge := range store.Topology().Edges {
		if edge.To == "ingress/models/model-route" && edge.Relation == "attaches" {
			t.Fatalf("ambiguous Ingress must not attach to a guessed Gateway: %#v", edge)
		}
	}
	for _, finding := range store.Findings() {
		if finding.TargetID == "ingress/models/model-route" && finding.Title == "Ingress 对应多个网关" {
			return
		}
	}
	t.Fatalf("missing ambiguous Gateway finding: %#v", store.Findings())
}

func emptyStores(count int) []cache.Store {
	stores := make([]cache.Store, count)
	for index := range stores {
		stores[index] = cache.NewStore(cache.MetaNamespaceKeyFunc)
	}
	return stores
}

func addReadyHigressGateway(t *testing.T, stores []cache.Store, namespace string) {
	t.Helper()
	labels := map[string]string{"app": "higress-gateway"}
	mustAdd(t, stores[9], &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "higress-gateway", Namespace: namespace, Labels: labels},
		Spec:       appsv1.DeploymentSpec{Selector: &metav1.LabelSelector{MatchLabels: labels}},
	})
	mustAdd(t, stores[10], &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "higress-gateway-ready", Namespace: namespace, UID: types.UID(namespace + "-pod"), Labels: labels},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name: "higress-gateway", Image: "higress-registry.cn-hangzhou.cr.aliyuncs.com/higress/gateway:2.2.0",
		}}},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}},
	})
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
