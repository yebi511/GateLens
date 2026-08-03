package kube

import (
	"fmt"
	"strings"

	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/gatelens/gatelens/internal/domain"
)

var mcpBridgesGVR = schema.GroupVersionResource{Group: "networking.higress.io", Version: "v1", Resource: "mcpbridges"}

func (s *Store) addHigressResources(snap *snapshot, ingressItems, bridgeItems []any, services map[string]*networkingService) {
	bridgeIDs := map[string]string{}
	registryIDs := map[string]map[string]string{}
	for _, item := range bridgeItems {
		bridge, ok := item.(*unstructured.Unstructured)
		if !ok {
			continue
		}
		id := "mcpbridge/" + bridge.GetNamespace() + "/" + bridge.GetName()
		bridgeKey := bridge.GetNamespace() + "/" + bridge.GetName()
		bridgeIDs[bridgeKey] = id
		registryIDs[bridgeKey] = map[string]string{}
		registries, _, _ := unstructured.NestedSlice(bridge.Object, "spec", "registries")
		status, statusText := domain.StatusHealthy, "已发现"
		if len(registries) == 0 {
			status, statusText = domain.StatusWarning, "无注册中心"
		}
		snap.topology.Nodes = appendUnique(snap.topology.Nodes, domain.TopologyNode{ID: id, Name: bridge.GetName(), Kind: "McpBridge", Namespace: bridge.GetNamespace(), ClusterID: s.clusterID, Status: status, StatusText: statusText, Summary: fmt.Sprintf("Higress McpBridge，包含 %d 个注册中心。", len(registries)), Conditions: []string{fmt.Sprintf("Registries=%d", len(registries))}, Source: "networking.higress.io/v1 McpBridge"})
		if len(registries) == 0 {
			addFinding(snap, domain.StatusWarning, "McpBridge 未配置注册中心", bridgeKey, "spec.registries is empty", id)
		}
		for _, raw := range registries {
			registry, _ := raw.(map[string]any)
			name := stringValue(registry, "name", "unnamed")
			registryType := stringValue(registry, "type", "unknown")
			domainName := stringValue(registry, "domain", "")
			port := fmt.Sprint(registry["port"])
			protocol := stringValue(registry, "protocol", "")
			registryID := id + "/registry/" + name
			registryIDs[bridgeKey][name+"."+registryType] = registryID
			conditions := []string{"Type=" + registryType, "Domain=" + domainName, "Port=" + port}
			if protocol != "" {
				conditions = append(conditions, "Protocol="+protocol)
			}
			snap.topology.Nodes = appendUnique(snap.topology.Nodes, domain.TopologyNode{ID: registryID, Name: name, Kind: "Registry", Namespace: bridge.GetNamespace(), ClusterID: s.clusterID, Status: domain.StatusHealthy, StatusText: "已配置", Summary: registryType + "://" + domainName + ":" + port, Conditions: conditions, Source: "McpBridge.spec.registries"})
			snap.topology.Edges = append(snap.topology.Edges, domain.TopologyEdge{From: id, To: registryID, Relation: "discovers"})
		}
	}

	for _, item := range ingressItems {
		ingress, ok := item.(*networkingv1.Ingress)
		if !ok || !isHigressIngress(ingress) {
			continue
		}
		ingressID := "ingress/" + ingress.Namespace + "/" + ingress.Name
		conditions := append(annotationConditions(ingress.Annotations), ingressEntryConditions(ingress)...)
		snap.topology.Nodes = appendUnique(snap.topology.Nodes, domain.TopologyNode{ID: ingressID, Name: ingress.Name, Kind: "Ingress", Namespace: ingress.Namespace, ClusterID: s.clusterID, Status: domain.StatusHealthy, StatusText: "已发现", Summary: "Higress Ingress 路由。", Conditions: conditions, Source: "networking.k8s.io/v1 Ingress"})
		for _, rule := range ingress.Spec.Rules {
			if rule.HTTP == nil {
				continue
			}
			for _, path := range rule.HTTP.Paths {
				backendIDs := []string{}
				if resource := path.Backend.Resource; resource != nil && resource.APIGroup != nil && *resource.APIGroup == "networking.higress.io" && resource.Kind == "McpBridge" {
					bridgeKey := ingress.Namespace + "/" + resource.Name
					bridgeID := bridgeIDs[bridgeKey]
					if bridgeID == "" {
						addFinding(snap, domain.StatusError, "McpBridge 不存在", ingress.Namespace+"/"+ingress.Name, "backend.resource="+resource.Name, ingressID)
					} else {
						backendIDs = append(backendIDs, bridgeID)
						snap.topology.Edges = append(snap.topology.Edges, domain.TopologyEdge{From: ingressID, To: bridgeID, Relation: "routes"})
						if destination := ingress.Annotations["higress.io/destination"]; destination != "" {
							if registryID := registryIDs[bridgeKey][destination]; registryID != "" {
								snap.topology.Edges = append(snap.topology.Edges, domain.TopologyEdge{From: ingressID, To: registryID, Relation: "selects"})
							} else {
								addFinding(snap, domain.StatusWarning, "Higress destination 未解析", ingress.Namespace+"/"+ingress.Name, "higress.io/destination="+destination, ingressID)
							}
						}
					}
				} else if service := path.Backend.Service; service != nil {
					key := ingress.Namespace + "/" + service.Name
					if backend, ok := services[key]; ok {
						serviceNode := backend.node
						if backend.readyEndpoints == 0 && !backend.external {
							serviceNode.Status = domain.StatusError
							serviceNode.StatusText = "无 Ready Endpoint"
							serviceNode.Summary = "Service 没有可用 Endpoint。"
							addFinding(snap, domain.StatusError, "Ingress 后端没有可用 Endpoint", key, "ReadyEndpoints=0", ingressID)
						}
						backendIDs = append(backendIDs, serviceNode.ID)
						snap.topology.Nodes = appendUnique(snap.topology.Nodes, serviceNode)
						snap.topology.Edges = append(snap.topology.Edges, domain.TopologyEdge{From: ingressID, To: serviceNode.ID, Relation: "routes"})
					} else {
						addFinding(snap, domain.StatusError, "Ingress 后端 Service 不存在", ingress.Namespace+"/"+ingress.Name, "backend.service="+key, ingressID)
					}
				}
				pathType := "PathPrefix"
				if path.PathType != nil && *path.PathType == networkingv1.PathTypeExact {
					pathType = "Exact"
				}
				snap.routes = append(snap.routes, routeRule{routeID: ingressID, namespace: ingress.Namespace, hostnames: nonEmpty(rule.Host), pathType: pathType, path: defaultString(path.Path, "/"), backendIDs: backendIDs})
			}
		}
	}
}

type networkingService struct {
	node           domain.TopologyNode
	readyEndpoints int
	external       bool
}

func (s *Store) addIngressEntryResources(snap *snapshot, items []any) {
	for _, item := range items {
		ingress, ok := item.(*networkingv1.Ingress)
		if !ok || isHigressIngress(ingress) {
			continue
		}
		conditions := ingressEntryConditions(ingress)
		if len(conditions) == 0 {
			continue
		}
		id := "ingress/" + ingress.Namespace + "/" + ingress.Name
		snap.topology.Nodes = appendUnique(snap.topology.Nodes, domain.TopologyNode{
			ID: id, Name: ingress.Name, Kind: "Ingress", Namespace: ingress.Namespace,
			ClusterID: s.clusterID, Status: domain.StatusHealthy, StatusText: "已发现",
			Summary: "Kubernetes Ingress 入口。", Conditions: conditions, Source: "networking.k8s.io/v1 Ingress",
		})
	}
}

func ingressEntryConditions(ingress *networkingv1.Ingress) []string {
	var result []string
	if ingress.Spec.IngressClassName != nil && *ingress.Spec.IngressClassName != "" {
		result = append(result, "IngressClass="+*ingress.Spec.IngressClassName)
	}
	for _, rule := range ingress.Spec.Rules {
		if host := strings.TrimSpace(rule.Host); host != "" {
			result = append(result, "Hostname="+host)
		}
	}
	for _, address := range ingress.Status.LoadBalancer.Ingress {
		if address.IP != "" {
			result = append(result, "Address="+address.IP)
		}
		if address.Hostname != "" {
			result = append(result, "Address="+address.Hostname)
		}
	}
	return result
}

func isHigressIngress(ingress *networkingv1.Ingress) bool {
	if ingress.Spec.IngressClassName != nil && *ingress.Spec.IngressClassName == "higress" {
		return true
	}
	return ingress.Annotations["kubernetes.io/ingress.class"] == "higress"
}

func annotationConditions(values map[string]string) []string {
	result := []string{}
	for _, key := range []string{"higress.io/destination", "higress.io/backend-protocol", "higress.io/upstream-vhost"} {
		if value := values[key]; value != "" {
			result = append(result, key+"="+value)
		}
	}
	return result
}

func nonEmpty(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return []string{value}
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
