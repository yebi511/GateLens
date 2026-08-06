package kube

import (
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/gatelens/gatelens/internal/domain"
)

var inferencePoolsGVR = schema.GroupVersionResource{Group: "inference.networking.k8s.io", Version: "v1", Resource: "inferencepools"}

func (s *Store) addInferencePool(snap *snapshot, pool *unstructured.Unstructured, services map[string]*corev1.Service, endpoints map[string][]domain.TopologyNode, podItems []any) string {
	poolID := "inferencepool/" + pool.GetNamespace() + "/" + pool.GetName()
	selector, selectorErr := inferencePoolSelector(pool)
	matched := make([]*corev1.Pod, 0)
	ready := 0
	if selectorErr == nil {
		for _, item := range podItems {
			pod, ok := item.(*corev1.Pod)
			if !ok || pod.Namespace != pool.GetNamespace() || !selector.Matches(labels.Set(pod.Labels)) {
				continue
			}
			matched = append(matched, pod)
			if podReady(pod) {
				ready++
			}
		}
	}

	status, statusText := domain.StatusHealthy, fmt.Sprintf("%d/%d Ready", ready, len(matched))
	summary := fmt.Sprintf("InferencePool 选择了 %d 个模型服务 Pod，其中 %d 个 Ready。", len(matched), ready)
	conditions := []string{fmt.Sprintf("ReadyPods=%d/%d", ready, len(matched))}
	if selectorErr != nil {
		status, statusText = domain.StatusError, "Selector 无效"
		summary = "InferencePool selector 无法解析：" + selectorErr.Error()
		conditions = append(conditions, "Resolved=False")
	} else if ready == 0 {
		status, statusText = domain.StatusError, "无 Ready Pod"
	} else if ready < len(matched) {
		status = domain.StatusWarning
	}

	snap.topology.Nodes = appendUnique(snap.topology.Nodes, domain.TopologyNode{
		ID: poolID, Name: pool.GetName(), Kind: "InferencePool", Namespace: pool.GetNamespace(), ClusterID: s.clusterID,
		Status: status, StatusText: statusText, Summary: summary, Conditions: conditions,
		Source: pool.GetAPIVersion() + " InferencePool",
	})
	eppID := addEndpointPicker(snap, pool)
	if eppID != "" {
		snap.topology.Edges = append(snap.topology.Edges, domain.TopologyEdge{From: poolID, To: eppID, Relation: "discovers"})
	}
	matchedServices := make([]string, 0)
	if selectorErr == nil {
		for key, service := range services {
			if service.Namespace != pool.GetNamespace() || !serviceSelectorMatches(service, selector) {
				continue
			}
			matchedServices = append(matchedServices, key)
		}
		sort.Strings(matchedServices)
	}
	for _, key := range matchedServices {
		service := services[key]
		serviceID := "service/" + key
		serviceStatus := domain.StatusHealthy
		serviceText := fmt.Sprintf("%d Ready", len(endpoints[key]))
		serviceSummary := fmt.Sprintf("Service selector 与 InferencePool %s 匹配。", pool.GetName())
		if len(endpoints[key]) == 0 {
			serviceStatus, serviceText = domain.StatusError, "无 Endpoint"
			serviceSummary = "Service 与 InferencePool selector 匹配，但没有 EndpointSlice 地址。"
		}
		snap.topology.Nodes = appendUnique(snap.topology.Nodes, domain.TopologyNode{
			ID: serviceID, Name: service.Name, Kind: "Service", Namespace: service.Namespace, ClusterID: s.clusterID,
			Status: serviceStatus, StatusText: serviceText, Summary: serviceSummary,
			Conditions: []string{"InferencePool=" + pool.GetNamespace() + "/" + pool.GetName(), fmt.Sprintf("Endpoints=%d", len(endpoints[key]))}, Source: "v1 Service selector",
		})
		if eppID != "" {
			snap.topology.Edges = append(snap.topology.Edges, domain.TopologyEdge{From: eppID, To: serviceID, Relation: "selects", Evidence: "Service selector matches InferencePool.spec.selector"})
		} else {
			snap.topology.Edges = append(snap.topology.Edges, domain.TopologyEdge{From: poolID, To: serviceID, Relation: "selects", Evidence: "Service selector matches InferencePool.spec.selector"})
		}
		appendServiceEndpoints(snap, serviceID, endpoints[key])
	}
	if len(matchedServices) > 0 {
		return poolID
	}
	for _, pod := range matched {
		isReady := podReady(pod)
		podStatus, podStatusText := domain.StatusWarning, "NotReady"
		if isReady {
			podStatus, podStatusText = domain.StatusHealthy, "Ready"
		}
		podID := "pod/" + pod.Namespace + "/" + pod.Name
		snap.topology.Nodes = appendUnique(snap.topology.Nodes, domain.TopologyNode{
			ID: podID, Name: pod.Name, Kind: "Pod", Namespace: pod.Namespace, ClusterID: s.clusterID,
			Status: podStatus, StatusText: podStatusText,
			Summary:    "InferencePool " + pool.GetName() + " 选择的模型服务 Pod。",
			Conditions: []string{"Ready=" + fmt.Sprint(isReady), "Phase=" + string(pod.Status.Phase)}, Source: "v1 Pod",
		})
		from := poolID
		if eppID != "" {
			from = eppID
		}
		snap.topology.Edges = append(snap.topology.Edges, domain.TopologyEdge{From: from, To: podID, Relation: "selects", Evidence: "No matching Service; using InferencePool selector membership"})
	}
	return poolID
}

func addEndpointPicker(snap *snapshot, pool *unstructured.Unstructured) string {
	ref, found, err := unstructured.NestedMap(pool.Object, "spec", "endpointPickerRef")
	if err != nil || !found {
		return ""
	}
	name := stringValue(ref, "name", "")
	if name == "" {
		return ""
	}
	namespace := stringValue(ref, "namespace", pool.GetNamespace())
	kind := stringValue(ref, "kind", "Service")
	group := stringValue(ref, "group", "")
	port := 0
	if portRef, ok := ref["port"].(map[string]any); ok {
		if value, ok := portRef["number"].(int64); ok {
			port = int(value)
		} else if value, ok := portRef["number"].(int); ok {
			port = value
		}
	}
	id := "endpoint-picker/" + namespace + "/" + name
	conditions := []string{"Ref=" + group + "/" + kind + " " + namespace + "/" + name}
	if port > 0 {
		conditions = append(conditions, fmt.Sprintf("Port=%d", port))
	}
	snap.topology.Nodes = appendUnique(snap.topology.Nodes, domain.TopologyNode{
		ID: id, Name: name, Kind: "EndpointPicker", Namespace: namespace, ClusterID: snap.context.Cluster.ID,
		Status: domain.StatusHealthy, StatusText: "已配置", Summary: "InferencePool 使用的 Endpoint Picker 扩展。",
		Conditions: conditions, Source: pool.GetAPIVersion() + " InferencePool.spec.endpointPickerRef",
	})
	return id
}

func serviceSelectorMatches(service *corev1.Service, selector labels.Selector) bool {
	if len(service.Spec.Selector) == 0 {
		return false
	}
	return selector.Matches(labels.Set(service.Spec.Selector))
}

func inferencePoolSelector(pool *unstructured.Unstructured) (labels.Selector, error) {
	raw, found, err := unstructured.NestedMap(pool.Object, "spec", "selector")
	if err != nil {
		return nil, err
	}
	if !found || len(raw) == 0 {
		return nil, fmt.Errorf("spec.selector is empty")
	}
	selector := metav1.LabelSelector{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(raw, &selector); err != nil {
		return nil, fmt.Errorf("convert spec.selector: %w", err)
	}
	if len(selector.MatchLabels) == 0 && len(selector.MatchExpressions) == 0 {
		selector.MatchLabels = make(map[string]string, len(raw))
		for key, value := range raw {
			text, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("spec.selector.%s is not a string", key)
			}
			selector.MatchLabels[key] = text
		}
	}
	return metav1.LabelSelectorAsSelector(&selector)
}
