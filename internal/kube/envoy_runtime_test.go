package kube

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
)

func TestResolveGatewayRuntimeSelectsStableReadyPod(t *testing.T) {
	gateway := object("Gateway", "istio-system", "public-gateway", map[string]any{"gatewayClassName": "istio"})
	labels := map[string]string{"gateway.networking.k8s.io/gateway-name": "public-gateway"}
	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "public-gateway-istio", Namespace: "istio-system"}, Spec: appsv1.DeploymentSpec{Selector: &metav1.LabelSelector{MatchLabels: labels}}}
	pod := func(name string, ready bool) *corev1.Pod {
		status := corev1.ConditionFalse
		if ready {
			status = corev1.ConditionTrue
		}
		return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "istio-system", UID: types.UID("uid-" + name), Labels: labels}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "istio-proxy", Image: "docker.io/istio/proxyv2:latest"}}}, Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: status}}}}
	}
	runtime, ok := resolveGatewayRuntime(gateway, map[string]string{"istio": "istio.io/gateway-controller"}, []any{deployment}, []any{pod("public-gateway-z", true), pod("public-gateway-a", true), pod("public-gateway-not-ready", false)})
	if !ok {
		t.Fatal("expected Gateway runtime")
	}
	if runtime.WorkloadName != deployment.Name {
		t.Fatalf("workload=%s", runtime.WorkloadName)
	}
	if len(runtime.Pods) != 2 || runtime.Pods[0].Name != "public-gateway-a" {
		t.Fatalf("pods=%#v", runtime.Pods)
	}
}

func TestKubernetesIngressControllerIsNotGatewayRuntime(t *testing.T) {
	if got := gatewayControllerKind("k8s.io/ingress-nginx"); got != "" {
		t.Fatalf("kind=%q, want empty", got)
	}
}

func TestResolveGatewayRuntimeKeepsHighestScoringPods(t *testing.T) {
	gateway := object("Gateway", "istio-system", "public-gateway", map[string]any{"gatewayClassName": "istio"})
	pod := func(name, namespace string, labels map[string]string) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: types.UID("uid-" + name), Labels: labels},
			Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "istio-proxy", Image: "docker.io/istio/proxyv2:latest"}}},
			Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}},
		}
	}
	exactLabels := map[string]string{"gateway.networking.k8s.io/gateway-name": "public-gateway"}
	genericLabels := map[string]string{"app": "istio-gateway"}
	runtime, ok := resolveGatewayRuntime(gateway, map[string]string{"istio": "istio.io/gateway-controller"}, nil, []any{
		pod("aaa-generic", "other-system", genericLabels),
		pod("public-gateway-a", "istio-system", exactLabels),
		pod("public-gateway-b", "istio-system", exactLabels),
	})
	if !ok {
		t.Fatal("expected Gateway runtime")
	}
	if len(runtime.Pods) != 2 || runtime.Pods[0].Name != "public-gateway-a" || runtime.Pods[1].Name != "public-gateway-b" {
		t.Fatalf("pods=%#v", runtime.Pods)
	}
}
func TestRebuildConnectsGatewayRuntimeStores(t *testing.T) {
	stores := make([]cache.Store, 11)
	for i := range stores {
		stores[i] = cache.NewStore(cache.MetaNamespaceKeyFunc)
	}
	labels := map[string]string{"gateway.networking.k8s.io/gateway-name": "public-gateway"}
	mustAdd(t, stores[3], object("Gateway", "istio-system", "public-gateway", map[string]any{"gatewayClassName": "istio"}))
	mustAdd(t, stores[8], object("GatewayClass", "", "istio", map[string]any{"controllerName": "istio.io/gateway-controller"}))
	mustAdd(t, stores[9], &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "public-gateway-istio", Namespace: "istio-system"},
		Spec:       appsv1.DeploymentSpec{Selector: &metav1.LabelSelector{MatchLabels: labels}},
	})
	mustAdd(t, stores[10], &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "public-gateway-istio-a", Namespace: "istio-system", UID: types.UID("pod-a"), Labels: labels},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "istio-proxy", Image: "docker.io/istio/proxyv2:latest"}}},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}},
	})

	store := &Store{clusterID: "test"}
	store.rebuild(stores...)
	gatewayID := "gateway/istio-system/public-gateway"
	if runtime, ok := store.snapshot.runtimes[gatewayID]; !ok || len(runtime.Pods) != 1 {
		t.Fatalf("runtime=%#v, found=%t", runtime, ok)
	}
	for _, edge := range store.Topology().Edges {
		if edge.To == gatewayID && edge.Relation == "serves" {
			return
		}
	}
	t.Fatal("expected GatewayWorkload serves edge")
}
func TestProxyAdminPortUsesControllerDefaultAndNamedPort(t *testing.T) {
	pod := &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{
		Name:  "envoy",
		Ports: []corev1.ContainerPort{{Name: "envoy-admin", ContainerPort: 19901}},
	}}}}
	if got := proxyAdminPort(pod, "envoy", "envoy"); got != 19901 {
		t.Fatalf("named admin port=%d, want 19901", got)
	}
	pod.Spec.Containers[0].Ports = nil
	if got := proxyAdminPort(pod, "envoy", "envoy"); got != envoyGatewayAdminPort {
		t.Fatalf("Envoy Gateway admin port=%d, want %d", got, envoyGatewayAdminPort)
	}
	if got := proxyAdminPort(pod, "envoy", "istio"); got != istioEnvoyAdminPort {
		t.Fatalf("Istio admin port=%d, want %d", got, istioEnvoyAdminPort)
	}
}
