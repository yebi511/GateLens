package kube

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

func TestRebuildCollectsCrossClusterConfigurationEvidence(t *testing.T) {
	stores := make([]cache.Store, 6)
	for index := range stores {
		stores[index] = cache.NewStore(cache.MetaNamespaceKeyFunc)
	}
	mustAdd(t, stores[0], &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "remote-inference", Namespace: "gateway"},
		Spec:       corev1.ServiceSpec{Type: corev1.ServiceTypeExternalName, ExternalName: "inference-gw.example"},
	})
	mustAdd(t, stores[2], &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "gateway"}})
	gateway := object("Gateway", "gateway", "inference-gateway", map[string]any{
		"listeners": []any{map[string]any{"name": "https", "protocol": "HTTPS", "port": int64(443)}},
	})
	gateway.Object["status"] = map[string]any{
		"addresses": []any{map[string]any{"type": "Hostname", "value": "inference-gw.example"}},
	}
	mustAdd(t, stores[3], gateway)
	mustAdd(t, stores[4], object("HTTPRoute", "gateway", "remote-chat", map[string]any{
		"parentRefs": []any{map[string]any{"name": "inference-gateway"}},
		"rules": []any{map[string]any{
			"backendRefs": []any{map[string]any{"name": "remote-inference"}},
		}},
	}))

	store := &Store{clusterID: "edge"}
	store.rebuild(stores...)

	var gatewayFound, transitFound bool
	for _, node := range store.Topology().Nodes {
		if node.Kind == "Gateway" && hasCondition(node.Conditions, "Address=inference-gw.example") {
			gatewayFound = true
		}
		if node.Kind == "TransitHop" && hasCondition(node.Conditions, "Destination=inference-gw.example") {
			transitFound = true
			if node.ClusterID != "edge" {
				t.Fatalf("TransitHop clusterID=%q", node.ClusterID)
			}
		}
	}
	if !gatewayFound || !transitFound {
		t.Fatalf("gatewayEvidence=%t transitEvidence=%t nodes=%#v", gatewayFound, transitFound, store.Topology().Nodes)
	}
	if len(store.Findings()) != 0 {
		t.Fatalf("findings=%#v", store.Findings())
	}
}

func TestEndpointReadinessSetsClusterID(t *testing.T) {
	ready := true
	slice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name: "model-a", Namespace: "inference",
			Labels: map[string]string{discoveryv1.LabelServiceName: "model"},
		},
		Endpoints: []discoveryv1.Endpoint{{
			Addresses:  []string{"10.0.0.8"},
			Conditions: discoveryv1.EndpointConditions{Ready: &ready},
		}},
	}
	_, nodes := endpointReadiness([]any{slice}, "gpu")
	if got := nodes["inference/model"][0].ClusterID; got != "gpu" {
		t.Fatalf("clusterID=%q, want gpu", got)
	}
}
func TestHigressExternalNameDoesNotRequireEndpoints(t *testing.T) {
	stores := make([]cache.Store, 8)
	for index := range stores {
		stores[index] = cache.NewStore(cache.MetaNamespaceKeyFunc)
	}
	pathType := networkingv1.PathTypePrefix
	class := "higress"
	mustAdd(t, stores[0], &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "remote-inference", Namespace: "gateway"},
		Spec:       corev1.ServiceSpec{Type: corev1.ServiceTypeExternalName, ExternalName: "inference-gw.example"},
	})
	mustAdd(t, stores[2], &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "gateway"}})
	mustAdd(t, stores[6], &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "chat", Namespace: "gateway"},
		Spec: networkingv1.IngressSpec{
			IngressClassName: &class,
			Rules: []networkingv1.IngressRule{{
				IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{
					Paths: []networkingv1.HTTPIngressPath{{
						Path: "/", PathType: &pathType,
						Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{
							Name: "remote-inference",
						}},
					}},
				}},
			}},
		},
	})

	store := &Store{clusterID: "edge"}
	store.rebuild(stores...)
	if len(store.Findings()) != 0 {
		t.Fatalf("findings=%#v", store.Findings())
	}
	for _, node := range store.Topology().Nodes {
		if node.Kind == "Service" && hasCondition(node.Conditions, "ExternalName=inference-gw.example") {
			return
		}
	}
	t.Fatalf("missing ExternalName service: %#v", store.Topology().Nodes)
}
