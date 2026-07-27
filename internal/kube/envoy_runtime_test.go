package kube

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"

	"github.com/gatelens/gatelens/internal/domain"
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
func TestRebuildMergesRuntimeIntoGatewayNode(t *testing.T) {
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
	var gatewayNode *domain.TopologyNode
	gatewayCount := 0
	for index := range store.Topology().Nodes {
		node := &store.snapshot.topology.Nodes[index]
		if node.Kind == "GatewayWorkload" {
			t.Fatalf("unexpected GatewayWorkload node: %#v", node)
		}
		if node.Kind == "Gateway" {
			gatewayCount++
		}
		if node.ID == gatewayID {
			gatewayNode = node
		}
	}
	if gatewayCount != 1 {
		t.Fatalf("Gateway nodes=%d, want 1: %#v", gatewayCount, store.Topology().Nodes)
	}
	if gatewayNode == nil {
		t.Fatal("Gateway node not found")
	}
	for _, want := range []string{"EnvoyConfig=available", "Controller=istio.io/gateway-controller", "Workload=istio-system/public-gateway-istio", "ReadyReplicas=1"} {
		if !hasCondition(gatewayNode.Conditions, want) {
			t.Fatalf("Gateway conditions=%v, missing %q", gatewayNode.Conditions, want)
		}
	}
	for _, edge := range store.Topology().Edges {
		if edge.Relation == "serves" {
			t.Fatalf("unexpected workload edge: %#v", edge)
		}
	}
}

func hasCondition(conditions []string, want string) bool {
	for _, condition := range conditions {
		if condition == want {
			return true
		}
	}
	return false
}
func TestRebuildDiscoversStandaloneHigressGatewayDeployment(t *testing.T) {
	stores := make([]cache.Store, 11)
	for i := range stores {
		stores[i] = cache.NewStore(cache.MetaNamespaceKeyFunc)
	}
	labels := map[string]string{"app": "higress-gateway"}
	mustAdd(t, stores[9], &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "higress-gateway", Namespace: "higress-system", Labels: labels},
		Spec:       appsv1.DeploymentSpec{Selector: &metav1.LabelSelector{MatchLabels: labels}},
	})
	mustAdd(t, stores[10], &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "higress-gateway-7f8d9", Namespace: "higress-system", UID: types.UID("higress-pod"), Labels: labels},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name: "higress-gateway", Image: "higress-registry.cn-hangzhou.cr.aliyuncs.com/higress/gateway:2.2.0",
		}}},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}},
	})

	store := &Store{clusterID: "test"}
	store.rebuild(stores...)
	gatewayID := "gateway-runtime/higress-system/higress-gateway"
	runtime, ok := store.snapshot.runtimes[gatewayID]
	if !ok || len(runtime.Pods) != 1 {
		t.Fatalf("runtime=%#v, found=%t", runtime, ok)
	}
	var gatewayNode *domain.TopologyNode
	for index := range store.snapshot.topology.Nodes {
		node := &store.snapshot.topology.Nodes[index]
		if node.ID == gatewayID {
			gatewayNode = node
		}
	}
	if gatewayNode == nil || gatewayNode.Kind != "Gateway" {
		t.Fatalf("standalone Gateway node=%#v", gatewayNode)
	}
	for _, want := range []string{"EnvoyConfig=available", "Controller=higress.io/gateway-controller", "Workload=higress-system/higress-gateway", "ReadyReplicas=1"} {
		if !hasCondition(gatewayNode.Conditions, want) {
			t.Fatalf("Gateway conditions=%v, missing %q", gatewayNode.Conditions, want)
		}
	}
}

func TestStandaloneHigressGatewayUsesExactNamesOrLabels(t *testing.T) {
	privateProxy := corev1.Container{Name: "higress-gateway", Image: "registry.example.com/platform/data-plane:v1"}
	tests := []struct {
		name       string
		deployment *appsv1.Deployment
		pod        *corev1.Pod
		want       string
	}{
		{
			name:       "stable app label",
			deployment: &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "edge-proxy", Labels: map[string]string{"app.kubernetes.io/name": "higress-gateway"}}},
			pod:        &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{privateProxy}}},
			want:       "higress",
		},
		{
			name:       "similar name is not enough",
			deployment: &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "my-higress-gateway-helper"}},
			pod:        &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{privateProxy}}},
			want:       "",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := standaloneGatewayKind(test.deployment, test.pod); got != test.want {
				t.Fatalf("standaloneGatewayKind()=%q, want %q", got, test.want)
			}
		})
	}
}

func TestEnvoyGatewayControllerIsNotProxyContainer(t *testing.T) {
	pod := &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "envoy-gateway", Image: "docker.io/envoyproxy/gateway:v1.5.0"}}}}
	if got := proxyContainer(pod, "envoy"); got != "" {
		t.Fatalf("controller container detected as Envoy proxy: %q", got)
	}
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
