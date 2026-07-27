package kube

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"

	"github.com/gatelens/gatelens/internal/domain"
	"github.com/gatelens/gatelens/internal/envoy"
)

const (
	istioEnvoyAdminPort   = 15000
	envoyGatewayAdminPort = 19000
)

type proxyPod struct {
	Name      string
	Namespace string
	UID       types.UID
	Container string
	AdminPort int
}

type gatewayRuntime struct {
	GatewayID    string
	Controller   string
	WorkloadID   string
	WorkloadName string
	Namespace    string
	Pods         []proxyPod
}

type cachedEnvoyConfig struct {
	PodUID    types.UID
	ExpiresAt time.Time
	Config    domain.EnvoyConfig
}

func collectGatewayClasses(items []any) map[string]string {
	result := map[string]string{}
	for _, item := range items {
		class, ok := item.(*unstructured.Unstructured)
		if !ok {
			continue
		}
		controller, _, _ := unstructured.NestedString(class.Object, "spec", "controllerName")
		result[class.GetName()] = controller
	}
	return result
}

func resolveGatewayRuntime(gateway *unstructured.Unstructured, classes map[string]string, deploymentItems, podItems []any) (gatewayRuntime, bool) {
	className, _, _ := unstructured.NestedString(gateway.Object, "spec", "gatewayClassName")
	controller := classes[className]
	kind := gatewayControllerKind(controller)
	if kind == "" {
		return gatewayRuntime{}, false
	}

	var candidates []*corev1.Pod
	bestScore := 0
	for _, item := range podItems {
		pod, ok := item.(*corev1.Pod)
		if !ok || !podReady(pod) || proxyContainer(pod, kind) == "" {
			continue
		}
		score := gatewayPodScore(gateway, pod, kind)
		if score == 0 || score < bestScore {
			continue
		}
		if score > bestScore {
			bestScore = score
			candidates = candidates[:0]
		}
		candidates = append(candidates, pod)
	}
	if len(candidates) == 0 {
		return gatewayRuntime{}, false
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Name < candidates[j].Name })

	deployment := matchingDeployment(candidates[0], deploymentItems)
	selected := candidates
	workloadName, workloadID := candidates[0].Name, "pod/"+candidates[0].Namespace+"/"+candidates[0].Name
	if deployment != nil {
		selector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
		if err == nil {
			selected = selected[:0]
			for _, pod := range candidates {
				if selector.Matches(labels.Set(pod.Labels)) {
					selected = append(selected, pod)
				}
			}
		}
		workloadName = deployment.Name
		workloadID = "gateway-workload/" + deployment.Namespace + "/" + deployment.Name
	}
	runtime := gatewayRuntime{GatewayID: "gateway/" + gateway.GetNamespace() + "/" + gateway.GetName(), Controller: controller, WorkloadID: workloadID, WorkloadName: workloadName, Namespace: candidates[0].Namespace}
	for _, pod := range selected {
		container := proxyContainer(pod, kind)
		runtime.Pods = append(runtime.Pods, proxyPod{Name: pod.Name, Namespace: pod.Namespace, UID: pod.UID, Container: container, AdminPort: proxyAdminPort(pod, container, kind)})
	}
	return runtime, len(runtime.Pods) > 0
}

func gatewayControllerKind(controller string) string {
	value := strings.ToLower(controller)
	switch {
	case strings.Contains(value, "higress"):
		return "higress"
	case strings.Contains(value, "istio"):
		return "istio"
	case strings.Contains(value, "envoy"):
		return "envoy"
	default:
		return ""
	}
}

func gatewayPodScore(gateway *unstructured.Unstructured, pod *corev1.Pod, kind string) int {
	if pod.Labels["gateway.networking.k8s.io/gateway-name"] == gateway.GetName() && pod.Namespace == gateway.GetNamespace() {
		return 100
	}
	identity := strings.ToLower(pod.Name + " " + pod.Labels["app"] + " " + pod.Labels["app.kubernetes.io/name"] + " " + pod.Labels["istio"])
	if strings.Contains(identity, strings.ToLower(gateway.GetName())) {
		return 70
	}
	switch kind {
	case "higress":
		if strings.Contains(identity, "higress") && strings.Contains(identity, "gateway") {
			return 40
		}
	case "istio":
		if strings.Contains(identity, "istio") && strings.Contains(identity, "gateway") {
			return 40
		}
	case "envoy":
		if strings.Contains(identity, "envoy") && strings.Contains(identity, "gateway") {
			return 40
		}
	}
	return 0
}

func proxyContainer(pod *corev1.Pod, kind string) string {
	for _, container := range pod.Spec.Containers {
		identity := strings.ToLower(container.Name + " " + container.Image)
		if kind == "istio" && (container.Name == "istio-proxy" || strings.Contains(identity, "proxyv2")) {
			return container.Name
		}
		if kind == "higress" {
			name := strings.ToLower(container.Name)
			if name == "higress-gateway" || (strings.Contains(identity, "higress") && strings.Contains(identity, "gateway")) {
				return container.Name
			}
		}
		if kind == "envoy" && (container.Name == "envoy" || strings.Contains(strings.ToLower(container.Image), "envoyproxy/envoy")) {
			return container.Name
		}
	}
	return ""
}

func proxyAdminPort(pod *corev1.Pod, containerName, kind string) int {
	defaultPort := istioEnvoyAdminPort
	if kind == "envoy" {
		defaultPort = envoyGatewayAdminPort
	}
	for _, container := range pod.Spec.Containers {
		if container.Name != containerName {
			continue
		}
		for _, port := range container.Ports {
			if strings.Contains(strings.ToLower(port.Name), "admin") {
				return int(port.ContainerPort)
			}
		}
	}
	return defaultPort
}
func podReady(pod *corev1.Pod) bool {
	if pod.Status.Phase != corev1.PodRunning || pod.DeletionTimestamp != nil {
		return false
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

func matchingDeployment(pod *corev1.Pod, items []any) *appsv1.Deployment {
	var matches []*appsv1.Deployment
	for _, item := range items {
		deployment, ok := item.(*appsv1.Deployment)
		if !ok || deployment.Namespace != pod.Namespace {
			continue
		}
		selector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
		if err == nil && selector.Matches(labels.Set(pod.Labels)) {
			matches = append(matches, deployment)
		}
	}
	if len(matches) == 0 {
		return nil
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].Name < matches[j].Name })
	return matches[0]
}

func isStandaloneHigressGateway(deployment *appsv1.Deployment, pod *corev1.Pod) bool {
	for _, value := range []string{
		deployment.Name,
		deployment.Labels["app"],
		deployment.Labels["app.kubernetes.io/name"],
		deployment.Labels["app.kubernetes.io/component"],
		pod.Labels["app"],
		pod.Labels["app.kubernetes.io/name"],
		pod.Labels["app.kubernetes.io/component"],
	} {
		switch strings.ToLower(value) {
		case "higress-gateway":
			return true
		}
	}
	return false
}

func standaloneGatewayKind(deployment *appsv1.Deployment, pod *corev1.Pod) string {
	if isStandaloneHigressGateway(deployment, pod) && proxyContainer(pod, "higress") != "" {
		return "higress"
	}

	parts := []string{
		deployment.Name,
		deployment.Labels["app"],
		deployment.Labels["app.kubernetes.io/name"],
		deployment.Labels["app.kubernetes.io/component"],
		pod.Name,
		pod.Labels["app"],
		pod.Labels["app.kubernetes.io/name"],
		pod.Labels["app.kubernetes.io/component"],
		pod.Labels["gateway.networking.k8s.io/gateway-name"],
	}
	for _, container := range pod.Spec.Containers {
		parts = append(parts, container.Name, container.Image)
	}
	identity := strings.ToLower(strings.Join(parts, " "))
	for _, kind := range []string{"istio", "envoy"} {
		if proxyContainer(pod, kind) == "" {
			continue
		}
		if strings.Contains(identity, kind) && strings.Contains(identity, "gateway") {
			return kind
		}
	}
	return ""
}

func inferredController(kind string) string {
	switch kind {
	case "higress":
		return "higress.io/gateway-controller"
	case "istio":
		return "istio.io/gateway-controller"
	case "envoy":
		return "gateway.envoyproxy.io/gatewayclass-controller"
	default:
		return ""
	}
}

func gatewayRuntimeConditions(runtime gatewayRuntime) []string {
	return []string{
		"EnvoyConfig=available",
		"Controller=" + runtime.Controller,
		"Workload=" + runtime.Namespace + "/" + runtime.WorkloadName,
		fmt.Sprintf("ReadyReplicas=%d", len(runtime.Pods)),
	}
}

func addStandaloneGatewayRuntimes(snap *snapshot, deployments, pods cache.Store) {
	claimed := map[string]struct{}{}
	for _, runtime := range snap.runtimes {
		claimed[runtime.WorkloadID] = struct{}{}
	}

	deploymentItems := append([]any(nil), deployments.List()...)
	sort.Slice(deploymentItems, func(i, j int) bool {
		left, leftOK := deploymentItems[i].(*appsv1.Deployment)
		right, rightOK := deploymentItems[j].(*appsv1.Deployment)
		if !leftOK || !rightOK {
			return leftOK
		}
		return left.Namespace+"/"+left.Name < right.Namespace+"/"+right.Name
	})
	for _, item := range deploymentItems {
		deployment, ok := item.(*appsv1.Deployment)
		if !ok || deployment.Spec.Selector == nil {
			continue
		}
		workloadID := "gateway-workload/" + deployment.Namespace + "/" + deployment.Name
		if _, found := claimed[workloadID]; found {
			continue
		}
		selector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
		if err != nil {
			continue
		}
		kind := ""
		var selected []*corev1.Pod
		for _, podItem := range pods.List() {
			pod, ok := podItem.(*corev1.Pod)
			if !ok || pod.Namespace != deployment.Namespace || !podReady(pod) || !selector.Matches(labels.Set(pod.Labels)) {
				continue
			}
			podKind := standaloneGatewayKind(deployment, pod)
			if podKind == "" || (kind != "" && podKind != kind) {
				continue
			}
			kind = podKind
			selected = append(selected, pod)
		}
		if kind == "" || len(selected) == 0 {
			continue
		}
		sort.Slice(selected, func(i, j int) bool { return selected[i].Name < selected[j].Name })
		runtime := gatewayRuntime{
			GatewayID:    "gateway-runtime/" + deployment.Namespace + "/" + deployment.Name,
			Controller:   inferredController(kind),
			WorkloadID:   workloadID,
			WorkloadName: deployment.Name,
			Namespace:    deployment.Namespace,
		}
		for _, pod := range selected {
			container := proxyContainer(pod, kind)
			runtime.Pods = append(runtime.Pods, proxyPod{Name: pod.Name, Namespace: pod.Namespace, UID: pod.UID, Container: container, AdminPort: proxyAdminPort(pod, container, kind)})
		}
		snap.runtimes[runtime.GatewayID] = runtime
		snap.topology.Nodes = appendUnique(snap.topology.Nodes, domain.TopologyNode{
			ID: runtime.GatewayID, Name: runtime.WorkloadName, Kind: "Gateway", Namespace: runtime.Namespace,
			ClusterID: snap.context.Cluster.ID, Status: domain.StatusHealthy, StatusText: fmt.Sprintf("%d Ready", len(runtime.Pods)),
			Summary: "根据 Deployment 和 Ready Pod 自动识别的数据面网关。", Conditions: gatewayRuntimeConditions(runtime),
			Source: "apps/v1 Deployment / v1 Pod", WorkloadScope: runtime.Namespace,
		})
	}
}
func addGatewayRuntime(snap *snapshot, gateway *unstructured.Unstructured, classes map[string]string, deployments, pods cache.Store) {
	runtime, ok := resolveGatewayRuntime(gateway, classes, deployments.List(), pods.List())
	if !ok {
		return
	}
	snap.runtimes[runtime.GatewayID] = runtime
	for index := range snap.topology.Nodes {
		node := &snap.topology.Nodes[index]
		if node.ID != runtime.GatewayID {
			continue
		}
		node.Conditions = append(node.Conditions, gatewayRuntimeConditions(runtime)...)
		break
	}
}
func (s *Store) EnvoyConfig(ctx context.Context, gatewayID string) (domain.EnvoyConfig, error) {
	s.mutex.RLock()
	runtime, ok := s.snapshot.runtimes[gatewayID]
	snapshotID, observedAt := s.snapshot.topology.SnapshotID, s.snapshot.topology.ObservedAt
	if ok && len(runtime.Pods) > 0 {
		if cached, found := s.envoyCache[gatewayID]; found && time.Now().Before(cached.ExpiresAt) {
			for _, pod := range runtime.Pods {
				if cached.PodUID == pod.UID {
					s.mutex.RUnlock()
					return cached.Config, nil
				}
			}
		}
	}
	s.mutex.RUnlock()
	if !ok {
		return domain.EnvoyConfig{}, fmt.Errorf("未找到 Gateway 对应的 Envoy 工作负载")
	}
	if len(runtime.Pods) == 0 {
		return domain.EnvoyConfig{}, fmt.Errorf("Gateway 工作负载没有 Ready Envoy Pod")
	}
	if s.restConfig == nil {
		return domain.EnvoyConfig{}, fmt.Errorf("Kubernetes port-forward 尚未配置")
	}

	var lastErr error
	for _, pod := range runtime.Pods {
		config, err := s.fetchPodEnvoyConfig(ctx, pod, snapshotID, observedAt)
		if err != nil {
			lastErr = err
			continue
		}
		config.GatewayID, config.Controller = gatewayID, runtime.Controller
		config.Workload = runtime.Namespace + "/" + runtime.WorkloadName
		config.SampledPod = pod.Namespace + "/" + pod.Name
		config.ReadyReplicas = len(runtime.Pods)
		if config.Proxy == "" {
			config.Proxy = pod.Name
		}
		s.mutex.Lock()
		s.envoyCache[gatewayID] = cachedEnvoyConfig{PodUID: pod.UID, ExpiresAt: time.Now().Add(30 * time.Second), Config: config}
		s.mutex.Unlock()
		return config, nil
	}
	return domain.EnvoyConfig{}, fmt.Errorf("无法读取 Envoy config_dump: %w", lastErr)
}

func (s *Store) fetchPodEnvoyConfig(ctx context.Context, pod proxyPod, snapshotID, observedAt string) (domain.EnvoyConfig, error) {
	transport, upgrader, err := spdy.RoundTripperFor(s.restConfig)
	if err != nil {
		return domain.EnvoyConfig{}, err
	}
	host, err := url.Parse(s.restConfig.Host)
	if err != nil {
		return domain.EnvoyConfig{}, err
	}
	serverURL := &url.URL{Scheme: host.Scheme, Host: host.Host, Path: fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/portforward", pod.Namespace, pod.Name)}
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, serverURL)
	stopCh, readyCh, errCh := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	var closeOnce sync.Once
	stop := func() { closeOnce.Do(func() { close(stopCh) }) }
	defer stop()
	var output, errorOutput bytes.Buffer
	forwarder, err := portforward.New(dialer, []string{fmt.Sprintf("0:%d", pod.AdminPort)}, stopCh, readyCh, &output, &errorOutput)
	if err != nil {
		return domain.EnvoyConfig{}, err
	}
	go func() { errCh <- forwarder.ForwardPorts() }()
	select {
	case <-readyCh:
	case err := <-errCh:
		return domain.EnvoyConfig{}, fmt.Errorf("port-forward %s/%s: %w: %s", pod.Namespace, pod.Name, err, errorOutput.String())
	case <-ctx.Done():
		return domain.EnvoyConfig{}, ctx.Err()
	case <-time.After(10 * time.Second):
		return domain.EnvoyConfig{}, fmt.Errorf("port-forward %s/%s timed out", pod.Namespace, pod.Name)
	}
	ports, err := forwarder.GetPorts()
	if err != nil || len(ports) == 0 {
		return domain.EnvoyConfig{}, fmt.Errorf("resolve forwarded Envoy admin port: %w", err)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	return envoy.Fetch(ctx, client, fmt.Sprintf("http://127.0.0.1:%d", ports[0].Local), snapshotID, observedAt)
}
