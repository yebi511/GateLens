package kube

import (
	"fmt"
	"net"
	"sort"
	"strings"

	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/gatelens/gatelens/internal/domain"
)

var mcpBridgesGVR = schema.GroupVersionResource{Group: "networking.higress.io", Version: "v1", Resource: "mcpbridges"}

func (s *Store) addHigressResources(snap *snapshot, ingressItems, bridgeItems []any, services map[string]*networkingService, endpoints map[string][]domain.TopologyNode) {
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
			if domainName != "" {
				if serviceKey, ok := serviceForRegistryDomain(domainName, bridge.GetNamespace(), services); ok {
					backend := services[serviceKey]
					serviceNode := backend.node
					if backend.readyEndpoints == 0 && !backend.external {
						serviceNode.Status = domain.StatusError
						serviceNode.StatusText = "无 Ready Endpoint"
						serviceNode.Summary = "Registry 已解析到 Service，但 Service 没有可用 Endpoint。"
						addFinding(snap, domain.StatusError, "Registry Service 没有可用 Endpoint", serviceKey, "ReadyEndpoints=0", registryID)
					}
					snap.topology.Nodes = appendUnique(snap.topology.Nodes, serviceNode)
					snap.topology.Edges = append(snap.topology.Edges, domain.TopologyEdge{From: registryID, To: serviceNode.ID, Relation: "resolves", Evidence: "McpBridge registry domain=" + domainName})
					appendServiceEndpoints(snap, serviceNode.ID, endpoints[serviceKey])
				} else {
					targetID := registryID + "/target"
					targetConditions := []string{"Address=" + domainName, "Port=" + port}
					if protocol != "" {
						targetConditions = append(targetConditions, "Protocol="+protocol)
					}
					snap.topology.Nodes = appendUnique(snap.topology.Nodes, domain.TopologyNode{
						ID: targetID, Name: domainName, Kind: "ExternalTarget", Namespace: bridge.GetNamespace(), ClusterID: s.clusterID,
						Status: domain.StatusHealthy, StatusText: "Configured", Summary: "Configured target " + domainName + ":" + port,
						Conditions: targetConditions, Source: "McpBridge.spec.registries[].domain",
					})
					snap.topology.Edges = append(snap.topology.Edges, domain.TopologyEdge{From: registryID, To: targetID, Relation: "resolves"})
				}
			}
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
							if registryID := registryIDForDestination(registryIDs[bridgeKey], destination); registryID != "" {
								snap.topology.Edges = append(snap.topology.Edges, domain.TopologyEdge{From: ingressID, To: registryID, Relation: "selects"})
							} else {
								addFinding(snap, domain.StatusWarning, "Higress destination 未解析", ingress.Namespace+"/"+ingress.Name, "higress.io/destination="+destination, ingressID)
							}
						} else if registryID, ok := soleRegistryID(registryIDs[bridgeKey]); ok {
							snap.topology.Edges = append(snap.topology.Edges, domain.TopologyEdge{
								From: ingressID, To: registryID, Relation: "selects",
								Evidence: "McpBridge has a single registry",
							})
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
						appendServiceEndpoints(snap, serviceNode.ID, endpoints[key])
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

func soleRegistryID(registries map[string]string) (string, bool) {
	if len(registries) != 1 {
		return "", false
	}
	for _, id := range registries {
		return id, true
	}
	return "", false
}
func registryIDForDestination(registries map[string]string, destination string) string {
	destination = strings.TrimSpace(destination)
	if id := registries[destination]; id != "" {
		return id
	}
	host, _, err := net.SplitHostPort(destination)
	if err != nil {
		return ""
	}
	return registries[host]
}
func serviceForRegistryDomain(domain, defaultNamespace string, services map[string]*networkingService) (string, bool) {
	host := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
	parts := strings.Split(host, ".")
	var key string
	switch {
	case len(parts) == 1:
		key = defaultNamespace + "/" + parts[0]
	case len(parts) == 2:
		key = parts[1] + "/" + parts[0]
	case len(parts) >= 3 && parts[2] == "svc":
		key = parts[1] + "/" + parts[0]
	default:
		return "", false
	}
	_, ok := services[key]
	return key, ok
}

func appendServiceEndpoints(snap *snapshot, serviceID string, endpoints []domain.TopologyNode) {
	for _, endpoint := range endpoints {
		snap.topology.Nodes = appendUnique(snap.topology.Nodes, endpoint)
		snap.topology.Edges = append(snap.topology.Edges, domain.TopologyEdge{From: serviceID, To: endpoint.ID, Relation: "selects"})
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

func linkIngressesToGateways(snap *snapshot, ingressItems, ingressClassItems []any) {
	ingressClassControllers := map[string]string{}
	for _, item := range ingressClassItems {
		ingressClass, ok := item.(*networkingv1.IngressClass)
		if !ok {
			continue
		}
		ingressClassControllers[ingressClass.Name] = string(ingressClass.Spec.Controller)
	}

	type gatewayCandidate struct {
		id         string
		name       string
		controller string
	}
	gateways := []gatewayCandidate{}
	for _, node := range snap.topology.Nodes {
		if node.Kind != "Gateway" || node.ClusterID != snap.context.Cluster.ID {
			continue
		}
		controller := conditionWithPrefix(node.Conditions, "Controller=")
		if controller == "" {
			continue
		}
		gateways = append(gateways, gatewayCandidate{id: node.ID, name: node.Namespace + "/" + node.Name, controller: controller})
	}
	sort.Slice(gateways, func(i, j int) bool { return gateways[i].id < gateways[j].id })

	for _, item := range ingressItems {
		ingress, ok := item.(*networkingv1.Ingress)
		if !ok {
			continue
		}
		className := ingressClassName(ingress)
		if className == "" {
			continue
		}
		controller := ingressClassControllers[className]
		fallbackFamily := ""
		if controller == "" && strings.EqualFold(className, "higress") {
			fallbackFamily = "higress"
		}

		matches := []gatewayCandidate{}
		for _, gateway := range gateways {
			if controllersCompatible(controller, gateway.controller) || (fallbackFamily != "" && controllerFamily(gateway.controller) == fallbackFamily) {
				matches = append(matches, gateway)
			}
		}
		ingressID := "ingress/" + ingress.Namespace + "/" + ingress.Name
		switch len(matches) {
		case 1:
			evidence := "IngressClass " + className
			if controller != "" {
				evidence += " controller " + controller
			} else {
				evidence += " matched by Higress compatibility fallback"
			}
			evidence += "; Gateway controller " + matches[0].controller
			snap.topology.Edges = append(snap.topology.Edges, domain.TopologyEdge{From: matches[0].id, To: ingressID, Relation: "attaches", Evidence: evidence})
		case 0:
			// An Ingress controller may run outside the visible workload set; absence is not an error.
		default:
			names := make([]string, 0, len(matches))
			for _, match := range matches {
				names = append(names, match.name)
			}
			addFinding(snap, domain.StatusWarning, "Ingress 对应多个网关", ingress.Namespace+"/"+ingress.Name, "IngressClass="+className+"; Gateways="+strings.Join(names, ","), ingressID)
		}
	}
}

func ingressClassName(ingress *networkingv1.Ingress) string {
	if ingress.Spec.IngressClassName != nil && strings.TrimSpace(*ingress.Spec.IngressClassName) != "" {
		return strings.TrimSpace(*ingress.Spec.IngressClassName)
	}
	return strings.TrimSpace(ingress.Annotations["kubernetes.io/ingress.class"])
}

func controllersCompatible(left, right string) bool {
	left = strings.TrimSpace(strings.ToLower(left))
	right = strings.TrimSpace(strings.ToLower(right))
	if left == "" || right == "" {
		return false
	}
	if left == right {
		return true
	}
	leftFamily, rightFamily := controllerFamily(left), controllerFamily(right)
	return leftFamily != "" && leftFamily == rightFamily
}

func controllerFamily(controller string) string {
	controller = strings.ToLower(controller)
	for _, family := range []string{"higress", "istio", "envoy"} {
		if strings.Contains(controller, family) {
			return family
		}
	}
	return ""
}

func conditionWithPrefix(conditions []string, prefix string) string {
	for _, condition := range conditions {
		if strings.HasPrefix(condition, prefix) {
			return strings.TrimPrefix(condition, prefix)
		}
	}
	return ""
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
