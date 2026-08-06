package kube

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	"github.com/gatelens/gatelens/internal/domain"
)

var (
	gatewaysGVR       = schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gateways"}
	routesGVR         = schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "httproutes"}
	grantsGVR         = schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1beta1", Resource: "referencegrants"}
	gatewayClassesGVR = schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gatewayclasses"}
)

type Store struct {
	clusterID    string
	core         kubernetes.Interface
	dynamic      dynamic.Interface
	capabilities []string
	mutex        sync.RWMutex
	snapshot     snapshot
	restConfig   *rest.Config
	envoyCache   map[string]cachedEnvoyConfig
}
type snapshot struct {
	context   domain.Context
	topology  domain.Topology
	findings  []domain.Finding
	resources []domain.Resource
	routes    []routeRule
	runtimes  map[string]gatewayRuntime
}
type routeRule struct {
	routeID, namespace     string
	hostnames              []string
	method, pathType, path string
	backendIDs             []string
}

func NewInCluster(clusterID string) (*Store, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("create in-cluster config: %w", err)
	}
	return New(clusterID, config)
}
func New(clusterID string, config *rest.Config) (*Store, error) {
	core, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create Kubernetes client: %w", err)
	}
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create dynamic Kubernetes client: %w", err)
	}
	return &Store{clusterID: clusterID, core: core, dynamic: dynamicClient, restConfig: rest.CopyConfig(config), envoyCache: map[string]cachedEnvoyConfig{}}, nil
}

func (s *Store) Run(ctx context.Context) error {
	coreFactory := informers.NewSharedInformerFactory(s.core, 0)
	dynamicFactory := newDynamicInformerFactory(s.dynamic)
	services := coreFactory.Core().V1().Services().Informer()
	endpoints := coreFactory.Discovery().V1().EndpointSlices().Informer()
	namespaces := coreFactory.Core().V1().Namespaces().Informer()
	ingresses := coreFactory.Networking().V1().Ingresses().Informer()
	ingressClasses := coreFactory.Networking().V1().IngressClasses().Informer()
	pods := coreFactory.Core().V1().Pods().Informer()
	deployments := coreFactory.Apps().V1().Deployments().Informer()

	optionalResources := []schema.GroupVersionResource{gatewaysGVR, gatewayClassesGVR, routesGVR, grantsGVR, mcpBridgesGVR, inferencePoolsGVR}
	available := discoverDynamicResources(s.core.Discovery(), optionalResources)
	emptyStore := func() cache.Store { return cache.NewStore(cache.MetaNamespaceKeyFunc) }
	stores := []cache.Store{
		services.GetStore(),
		endpoints.GetStore(),
		namespaces.GetStore(),
		emptyStore(),
		emptyStore(),
		emptyStore(),
		ingresses.GetStore(),
		emptyStore(),
		emptyStore(),
		deployments.GetStore(),
		pods.GetStore(),
		ingressClasses.GetStore(),
		emptyStore(),
	}

	allInformers := []cache.SharedIndexInformer{services, endpoints, namespaces, ingresses, ingressClasses, pods, deployments}
	coreSynced := []cache.InformerSynced{services.HasSynced, endpoints.HasSynced, namespaces.HasSynced}
	dynamicInformerCount := 0
	addDynamicInformer := func(resource schema.GroupVersionResource, storeIndex int) {
		if !available[resource] {
			return
		}
		informer := dynamicFactory.ForResource(resource).Informer()
		stores[storeIndex] = informer.GetStore()
		allInformers = append(allInformers, informer)
		dynamicInformerCount++
	}
	addDynamicInformer(gatewaysGVR, 3)
	addDynamicInformer(routesGVR, 4)
	addDynamicInformer(grantsGVR, 5)
	addDynamicInformer(mcpBridgesGVR, 7)
	addDynamicInformer(gatewayClassesGVR, 8)
	addDynamicInformer(inferencePoolsGVR, 12)

	s.capabilities = []string{"endpointslice", "higress-ingress", "envoy-auto-discovery"}
	if available[gatewaysGVR] || available[routesGVR] || available[gatewayClassesGVR] {
		s.capabilities = append(s.capabilities, "gateway-api")
	}
	if available[grantsGVR] {
		s.capabilities = append(s.capabilities, "reference-grant")
	}
	if available[mcpBridgesGVR] {
		s.capabilities = append(s.capabilities, "higress-mcpbridge")
	}
	if available[inferencePoolsGVR] {
		s.capabilities = append(s.capabilities, "inference-pool")
	}

	ready := make(chan struct{})
	refresh := func() {
		select {
		case <-ready:
			s.rebuild(stores...)
		default:
		}
	}
	for _, informer := range allInformers {
		_, _ = informer.AddEventHandler(cache.ResourceEventHandlerFuncs{AddFunc: func(any) { refresh() }, UpdateFunc: func(any, any) { refresh() }, DeleteFunc: func(any) { refresh() }})
	}
	coreFactory.Start(ctx.Done())
	if dynamicInformerCount > 0 {
		dynamicFactory.Start(ctx.Done())
	}
	if !cache.WaitForCacheSync(ctx.Done(), coreSynced...) {
		return fmt.Errorf("synchronize Kubernetes informers")
	}
	close(ready)
	refresh()
	<-ctx.Done()
	return nil
}
func (s *Store) Context() domain.Context {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.snapshot.context
}
func (s *Store) Topology() domain.Topology {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.snapshot.topology
}
func (s *Store) Findings() []domain.Finding {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return append([]domain.Finding(nil), s.snapshot.findings...)
}
func (s *Store) Resources(query string) []domain.Resource {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return filterResources(s.snapshot.resources, query)
}

func (s *Store) Explain(request domain.RouteExplanationRequest) domain.RouteExplanation {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	result := domain.RouteExplanation{SnapshotID: s.snapshot.topology.SnapshotID, ObservedAt: s.snapshot.topology.ObservedAt, Confidence: "medium", Outcome: "Indeterminate", Summary: "没有找到匹配的 HTTPRoute。"}
	nodes := map[string]domain.TopologyNode{}
	for _, n := range s.snapshot.topology.Nodes {
		nodes[n.ID] = n
	}
	for _, rule := range s.snapshot.routes {
		if request.Namespace != "" && request.Namespace != rule.namespace {
			continue
		}
		if !hostMatches(request.Host, rule.hostnames) || !methodMatches(request.Method, rule.method) || !pathMatches(request.Path, rule.pathType, rule.path) {
			continue
		}
		result.Steps = []domain.ExplainStep{{Hop: 1, Title: "Route 规则匹配", Detail: "命中 " + rule.namespace + "/" + nodes[rule.routeID].Name + "。", State: "passed", TargetID: rule.routeID}}
		if len(rule.backendIDs) == 0 {
			result.Outcome = "Unresolved"
			result.Summary = "路由没有可解析的后端。"
			return result
		}
		for _, id := range rule.backendIDs {
			backend := nodes[id]
			state := "passed"
			if backend.Status == domain.StatusError {
				state = "rejected"
			}
			result.Steps = append(result.Steps, domain.ExplainStep{Hop: 1, Title: "后端解析", Detail: backend.Summary, State: state, TargetID: id})
			if backend.Status == domain.StatusHealthy {
				result.Outcome = "Routed"
				result.Summary = "已解析到健康后端候选。"
				return result
			}
		}
		result.Outcome = "NoHealthyBackend"
		result.Summary = "匹配路由的后端均不可用。"
		return result
	}
	return result
}

func (s *Store) rebuild(stores ...cache.Store) {
	serviceStore, endpointStore, namespaceStore, gatewayStore, routeStore, grantStore := stores[0], stores[1], stores[2], stores[3], stores[4], stores[5]
	var ingressStore, mcpBridgeStore, gatewayClassStore, deploymentStore, podStore, ingressClassStore, inferencePoolStore cache.Store
	if len(stores) > 6 {
		ingressStore = stores[6]
	}
	if len(stores) > 7 {
		mcpBridgeStore = stores[7]
	}
	if len(stores) > 8 {
		gatewayClassStore = stores[8]
	}
	if len(stores) > 9 {
		deploymentStore = stores[9]
	}
	if len(stores) > 10 {
		podStore = stores[10]
	}
	if len(stores) > 11 {
		ingressClassStore = stores[11]
	}
	if len(stores) > 12 {
		inferencePoolStore = stores[12]
	}
	now := time.Now().UTC().Format(time.RFC3339)
	snap := snapshot{runtimes: map[string]gatewayRuntime{}, context: domain.Context{Cluster: domain.Cluster{ID: s.clusterID, Name: s.clusterID, Version: "Kubernetes"}, Snapshot: domain.Snapshot{ID: "live-" + now, ObservedAt: now, State: "complete"}, Capabilities: append([]string(nil), s.capabilities...)}}
	for _, item := range namespaceStore.List() {
		if ns, ok := item.(*corev1.Namespace); ok {
			snap.context.Namespaces = append(snap.context.Namespaces, ns.Name)
		}
	}
	sort.Strings(snap.context.Namespaces)
	grants := collectGrants(grantStore.List())
	services := map[string]*corev1.Service{}
	for _, item := range serviceStore.List() {
		if svc, ok := item.(*corev1.Service); ok {
			services[svc.Namespace+"/"+svc.Name] = svc
		}
	}
	ready, endpointsByService := endpointReadiness(endpointStore.List(), s.clusterID)
	inferencePools := map[string]*unstructured.Unstructured{}
	if inferencePoolStore != nil {
		for _, item := range inferencePoolStore.List() {
			if pool, ok := item.(*unstructured.Unstructured); ok {
				inferencePools[pool.GetNamespace()+"/"+pool.GetName()] = pool
			}
		}
	}
	gatewayClasses := map[string]string{}
	if gatewayClassStore != nil {
		gatewayClasses = collectGatewayClasses(gatewayClassStore.List())
	}
	gateways := map[string]string{}
	for _, item := range gatewayStore.List() {
		obj, ok := item.(*unstructured.Unstructured)
		if !ok {
			continue
		}
		gatewayID := "gateway/" + obj.GetNamespace() + "/" + obj.GetName()
		gateways[obj.GetNamespace()+"/"+obj.GetName()] = gatewayID
		snap.topology.Nodes = append(snap.topology.Nodes, node(s.clusterID, gatewayID, obj, "Gateway", domain.StatusHealthy, "已发现", "Gateway API 配置对象。"))
		gatewayNodeIndex := len(snap.topology.Nodes) - 1
		snap.topology.Nodes[gatewayNodeIndex].Conditions = append(
			snap.topology.Nodes[gatewayNodeIndex].Conditions, gatewayAddressConditions(obj)...,
		)
		if deploymentStore != nil && podStore != nil {
			addGatewayRuntime(&snap, obj, gatewayClasses, deploymentStore, podStore)
		}
		listeners, _, _ := unstructured.NestedSlice(obj.Object, "spec", "listeners")
		for _, raw := range listeners {
			listener, _ := raw.(map[string]any)
			name := stringValue(listener, "name", "listener")
			id := gatewayID + "/listener/" + name
			protocol := stringValue(listener, "protocol", "unknown")
			port := fmt.Sprint(listener["port"])
			hostname := stringValue(listener, "hostname", "*")
			snap.topology.Nodes = append(snap.topology.Nodes, domain.TopologyNode{ID: id, Name: name, Kind: "Listener", Namespace: obj.GetNamespace(), ClusterID: s.clusterID, Status: domain.StatusHealthy, StatusText: "已配置", Summary: protocol + " " + hostname + ":" + port, Conditions: []string{"Protocol=" + protocol, "Hostname=" + hostname}, Source: "Gateway.spec.listeners"})
			snap.topology.Edges = append(snap.topology.Edges, domain.TopologyEdge{From: gatewayID, To: id, Relation: "owns"})
		}
	}
	if deploymentStore != nil && podStore != nil {
		addStandaloneGatewayRuntimes(&snap, deploymentStore, podStore)
	}
	for _, item := range routeStore.List() {
		obj, ok := item.(*unstructured.Unstructured)
		if !ok {
			continue
		}
		routeID := "route/" + obj.GetNamespace() + "/" + obj.GetName()
		hostnames, _, _ := unstructured.NestedStringSlice(obj.Object, "spec", "hostnames")
		routeNode := node(s.clusterID, routeID, obj, "HTTPRoute", domain.StatusHealthy, "已发现", "HTTP 路由；Host: "+strings.Join(hostnames, ", "))
		snap.topology.Nodes = append(snap.topology.Nodes, routeNode)
		parents, _, _ := unstructured.NestedSlice(obj.Object, "spec", "parentRefs")
		for _, raw := range parents {
			parent, _ := raw.(map[string]any)
			ns := stringValue(parent, "namespace", obj.GetNamespace())
			gatewayID := gateways[ns+"/"+stringValue(parent, "name", "")]
			if gatewayID == "" {
				addFinding(&snap, domain.StatusError, "父 Gateway 不存在", obj.GetNamespace()+"/"+obj.GetName(), "parentRef 无法解析", routeID)
				continue
			}
			section := stringValue(parent, "sectionName", "")
			from := gatewayID
			if section != "" {
				from = gatewayID + "/listener/" + section
			}
			snap.topology.Edges = append(snap.topology.Edges, domain.TopologyEdge{From: from, To: routeID, Relation: "attaches"})
		}
		rules, _, _ := unstructured.NestedSlice(obj.Object, "spec", "rules")
		for _, rawRule := range rules {
			rule, _ := rawRule.(map[string]any)
			var podItems []any
			if podStore != nil {
				podItems = podStore.List()
			}
			backendIDs := s.addBackends(&snap, obj, routeID, rule, services, inferencePools, ready, endpointsByService, podItems, grants)
			matches, _ := rule["matches"].([]any)
			if len(matches) == 0 {
				snap.routes = append(snap.routes, routeRule{routeID: routeID, namespace: obj.GetNamespace(), hostnames: hostnames, pathType: "PathPrefix", path: "/", backendIDs: backendIDs})
			}
			for _, rawMatch := range matches {
				match, _ := rawMatch.(map[string]any)
				normalized := routeRule{routeID: routeID, namespace: obj.GetNamespace(), hostnames: hostnames, pathType: "PathPrefix", path: "/", backendIDs: backendIDs}
				if method, ok := match["method"].(string); ok {
					normalized.method = method
				}
				if path, ok := match["path"].(map[string]any); ok {
					normalized.pathType = stringValue(path, "type", "PathPrefix")
					normalized.path = stringValue(path, "value", "/")
				}
				snap.routes = append(snap.routes, normalized)
			}
		}
	}
	if ingressStore != nil {
		s.addIngressEntryResources(&snap, ingressStore.List())
	}
	if ingressStore != nil && mcpBridgeStore != nil {
		serviceRefs := map[string]*networkingService{}
		for key, svc := range services {
			serviceRefs[key] = &networkingService{node: domain.TopologyNode{ID: "service/" + key, Name: svc.Name, Kind: "Service", Namespace: svc.Namespace, ClusterID: s.clusterID, Status: domain.StatusHealthy, StatusText: "已发现", Summary: fmt.Sprintf("Service 有 %d 个 Ready Endpoint。", ready[key]), Conditions: []string{fmt.Sprintf("ReadyEndpoints=%d", ready[key])}, Source: "v1 Service"}, readyEndpoints: ready[key]}
			if svc.Spec.Type == corev1.ServiceTypeExternalName && svc.Spec.ExternalName != "" {
				serviceRefs[key].external = true
				serviceRefs[key].node.StatusText = "Configured"
				serviceRefs[key].node.Summary = "ExternalName Service forwards to " + svc.Spec.ExternalName + "."
				serviceRefs[key].node.Conditions = append(serviceRefs[key].node.Conditions, "ExternalName="+svc.Spec.ExternalName)
			}
		}
		s.addHigressResources(&snap, ingressStore.List(), mcpBridgeStore.List(), serviceRefs, endpointsByService)
	}
	if ingressStore != nil {
		var ingressClasses []any
		if ingressClassStore != nil {
			ingressClasses = ingressClassStore.List()
		}
		linkIngressesToGateways(&snap, ingressStore.List(), ingressClasses)
	}
	snap.topology.SnapshotID = snap.context.Snapshot.ID
	snap.topology.ObservedAt = now
	snap.resources = resourcesFromNodes(snap.topology.Nodes, snap.findings)
	snap.topology.Consistency = "single-cluster"
	snap.topology.Clusters = []domain.TopologyCluster{{
		ID: s.clusterID, Name: s.clusterID, Version: "Kubernetes",
		ConnectionState: "connected", Namespaces: append([]string(nil), snap.context.Namespaces...), Snapshot: snap.context.Snapshot,
	}}
	s.mutex.Lock()
	s.snapshot = snap
	s.mutex.Unlock()
}

func (s *Store) addBackends(snap *snapshot, obj *unstructured.Unstructured, routeID string, rule map[string]any, services map[string]*corev1.Service, inferencePools map[string]*unstructured.Unstructured, ready map[string]int, endpoints map[string][]domain.TopologyNode, pods []any, grants map[string]bool) []string {
	refs, _ := rule["backendRefs"].([]any)
	ids := []string{}
	for _, raw := range refs {
		ref, _ := raw.(map[string]any)
		backendNS := stringValue(ref, "namespace", obj.GetNamespace())
		backendName := stringValue(ref, "name", "")
		backendGroup := stringValue(ref, "group", "")
		backendKind := stringValue(ref, "kind", "Service")
		key := backendNS + "/" + backendName
		if backendGroup == "inference.networking.k8s.io" && backendKind == "InferencePool" {
			pool := inferencePools[key]
			if pool == nil {
				addFinding(snap, domain.StatusError, "后端 InferencePool 不存在", obj.GetNamespace()+"/"+obj.GetName(), "BackendRef="+key, routeID)
				continue
			}
			allowed := backendNS == obj.GetNamespace() || grantsAllow(grants, obj.GetNamespace(), backendNS, backendGroup, backendKind, backendName)
			poolID := s.addInferencePool(snap, pool, services, endpoints, pods)
			ids = append(ids, poolID)
			if !allowed {
				markNodeError(snap, poolID, "ReferenceGrant 缺失", "跨命名空间 InferencePool BackendRef 未被 ReferenceGrant 允许。")
				addFinding(snap, domain.StatusError, "跨命名空间引用未授权", obj.GetNamespace()+"/"+obj.GetName(), "BackendRef="+key, routeID)
			}
			snap.topology.Edges = append(snap.topology.Edges, domain.TopologyEdge{From: routeID, To: poolID, Relation: "routes"})
			continue
		}
		if (backendGroup != "" && backendGroup != "core") || backendKind != "Service" {
			addFinding(snap, domain.StatusWarning, "不支持的 HTTPRoute 后端类型", obj.GetNamespace()+"/"+obj.GetName(), "BackendRef="+backendGroup+"/"+backendKind+" "+key, routeID)
			continue
		}
		svc := services[key]
		if svc == nil {
			addFinding(snap, domain.StatusError, "后端 Service 不存在", obj.GetNamespace()+"/"+obj.GetName(), "BackendRef="+key, routeID)
			continue
		}
		allowed := backendNS == obj.GetNamespace() || grantsAllow(grants, obj.GetNamespace(), backendNS, "", "Service", backendName)
		status, text, summary := domain.StatusHealthy, "Ready", fmt.Sprintf("Service 有 %d 个 Ready Endpoint。", ready[key])
		if svc.Spec.Type == corev1.ServiceTypeExternalName && svc.Spec.ExternalName != "" {
			status, text := domain.StatusHealthy, "Configured"
			if !allowed {
				status, text = domain.StatusError, "ReferenceGrant missing"
				addFinding(snap, status, "Cross-namespace reference is not authorized", obj.GetNamespace()+"/"+obj.GetName(), "BackendRef="+key, routeID)
			}
			transitID := "transit/" + key
			ids = append(ids, transitID)
			snap.topology.Nodes = appendUnique(snap.topology.Nodes, domain.TopologyNode{
				ID: transitID, Name: svc.Name, Kind: "TransitHop", Namespace: svc.Namespace,
				ClusterID: s.clusterID, Status: status, StatusText: text,
				Summary:    "ExternalName Service forwards to " + svc.Spec.ExternalName + ".",
				Conditions: []string{"ExternalName=" + svc.Spec.ExternalName, "Destination=" + svc.Spec.ExternalName},
				Source:     "v1 Service.spec.externalName",
			})
			snap.topology.Edges = append(snap.topology.Edges, domain.TopologyEdge{From: routeID, To: transitID, Relation: "routes"})
			continue
		}
		if !allowed {
			status = domain.StatusError
			text = "ReferenceGrant 缺失"
			summary = "跨命名空间 BackendRef 未被 ReferenceGrant 允许。"
			addFinding(snap, status, "跨命名空间引用未授权", obj.GetNamespace()+"/"+obj.GetName(), summary, routeID)
		} else if ready[key] == 0 {
			status = domain.StatusError
			text = "无 Ready Endpoint"
			summary = "Service 没有可用 Endpoint。"
			addFinding(snap, status, "后端没有可用 Endpoint", key, "ReadyEndpoints=0", routeID)
		}
		backendID := "service/" + key
		ids = append(ids, backendID)
		snap.topology.Nodes = appendUnique(snap.topology.Nodes, domain.TopologyNode{ID: backendID, Name: svc.Name, Kind: "Service", Namespace: svc.Namespace, ClusterID: s.clusterID, Status: status, StatusText: text, Summary: summary, Conditions: []string{fmt.Sprintf("ReadyEndpoints=%d", ready[key])}, Source: "v1 Service"})
		snap.topology.Edges = append(snap.topology.Edges, domain.TopologyEdge{From: routeID, To: backendID, Relation: "routes"})
		for _, endpoint := range endpoints[key] {
			snap.topology.Nodes = appendUnique(snap.topology.Nodes, endpoint)
			snap.topology.Edges = append(snap.topology.Edges, domain.TopologyEdge{From: backendID, To: endpoint.ID, Relation: "selects"})
		}
	}
	return ids
}
func node(clusterID, id string, obj *unstructured.Unstructured, kind string, status domain.Status, text, summary string) domain.TopologyNode {
	return domain.TopologyNode{ID: id, Name: obj.GetName(), Kind: kind, Namespace: obj.GetNamespace(), ClusterID: clusterID, Status: status, StatusText: text, Summary: summary, Source: obj.GetAPIVersion() + " " + obj.GetKind()}
}
func stringValue(values map[string]any, key, fallback string) string {
	if v, ok := values[key].(string); ok && v != "" {
		return v
	}
	return fallback
}
func appendUnique(nodes []domain.TopologyNode, node domain.TopologyNode) []domain.TopologyNode {
	for _, existing := range nodes {
		if existing.ID == node.ID {
			return nodes
		}
	}
	return append(nodes, node)
}
func endpointReadiness(items []any, clusterID string) (map[string]int, map[string][]domain.TopologyNode) {
	ready := map[string]int{}
	nodes := map[string][]domain.TopologyNode{}
	for _, item := range items {
		slice, ok := item.(*discoveryv1.EndpointSlice)
		if !ok {
			continue
		}
		key := slice.Namespace + "/" + slice.Labels[discoveryv1.LabelServiceName]
		for _, endpoint := range slice.Endpoints {
			isReady := endpoint.Conditions.Ready == nil || *endpoint.Conditions.Ready
			status, text := domain.StatusWarning, "NotReady"
			if isReady {
				status = domain.StatusHealthy
				text = "Ready"
				ready[key]++
			}
			for _, address := range endpoint.Addresses {
				id := "endpoint/" + slice.Namespace + "/" + address
				nodes[key] = append(nodes[key], domain.TopologyNode{ID: id, Name: address, Kind: "Endpoint", Namespace: slice.Namespace, Status: status, StatusText: text, Summary: "EndpointSlice " + slice.Name + " 中的后端地址。", Conditions: []string{"Ready=" + fmt.Sprint(isReady)}, Source: "discovery.k8s.io/v1 EndpointSlice"})
				last := len(nodes[key]) - 1
				nodes[key][last].ClusterID = clusterID
			}
		}
	}
	return ready, nodes
}
func collectGrants(items []any) map[string]bool {
	result := map[string]bool{}
	for _, item := range items {

		obj, ok := item.(*unstructured.Unstructured)
		if !ok {
			continue
		}
		from, _, _ := unstructured.NestedSlice(obj.Object, "spec", "from")
		to, _, _ := unstructured.NestedSlice(obj.Object, "spec", "to")
		for _, rawFrom := range from {
			f, _ := rawFrom.(map[string]any)
			if stringValue(f, "group", "gateway.networking.k8s.io") != "gateway.networking.k8s.io" || stringValue(f, "kind", "") != "HTTPRoute" {
				continue
			}
			for _, rawTo := range to {
				t, _ := rawTo.(map[string]any)
				group := stringValue(t, "group", "")
				kind := stringValue(t, "kind", "")
				name := stringValue(t, "name", "*")
				result[grantKey(stringValue(f, "namespace", ""), obj.GetNamespace(), group, kind, name)] = true
			}
		}
	}
	return result
}

func grantKey(fromNamespace, toNamespace, group, kind, name string) string {
	return fromNamespace + "->" + toNamespace + "/" + group + "/" + kind + "/" + name
}

func grantsAllow(grants map[string]bool, fromNamespace, toNamespace, group, kind, name string) bool {
	return grants[grantKey(fromNamespace, toNamespace, group, kind, name)] || grants[grantKey(fromNamespace, toNamespace, group, kind, "*")]
}

func markNodeError(snap *snapshot, id, statusText, summary string) {
	for index := range snap.topology.Nodes {
		if snap.topology.Nodes[index].ID != id {
			continue
		}
		snap.topology.Nodes[index].Status = domain.StatusError
		snap.topology.Nodes[index].StatusText = statusText
		snap.topology.Nodes[index].Summary = summary
		return
	}
}

func gatewayAddressConditions(obj *unstructured.Unstructured) []string {
	addresses, _, _ := unstructured.NestedSlice(obj.Object, "status", "addresses")
	conditions := make([]string, 0, len(addresses))
	for _, raw := range addresses {
		address, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		value := stringValue(address, "value", "")
		if value != "" {
			conditions = append(conditions, "Address="+value)
		}
	}
	return conditions
}
func addFinding(s *snapshot, severity domain.Status, title, resource, basis, target string) {
	s.findings = append(s.findings, domain.Finding{ID: fmt.Sprint(len(s.findings) + 1), Severity: severity, Title: title, Resource: resource, Basis: basis, TargetID: target})
}
func resourcesFromNodes(nodes []domain.TopologyNode, findings []domain.Finding) []domain.Resource {
	result := []domain.Resource{}
	for _, n := range nodes {
		count := 0
		for _, f := range findings {
			if f.TargetID == n.ID {
				count++
			}
		}
		result = append(result, domain.Resource{ID: n.ID, Kind: n.Kind, Name: n.Name, Namespace: n.Namespace, Status: n.Status, StatusText: n.StatusText, UpdatedAt: "当前快照", Findings: count})
	}
	return result
}
func filterResources(resources []domain.Resource, query string) []domain.Resource {
	if query == "" {
		return append([]domain.Resource(nil), resources...)
	}
	result := []domain.Resource{}
	for _, r := range resources {
		if strings.Contains(strings.ToLower(r.Kind+" "+r.Name+" "+r.Namespace), strings.ToLower(query)) {
			result = append(result, r)
		}
	}
	return result
}
func hostMatches(host string, patterns []string) bool {
	if len(patterns) == 0 {
		return true
	}
	host = strings.ToLower(strings.Split(host, ":")[0])
	for _, pattern := range patterns {
		p := strings.ToLower(pattern)
		if p == host {
			return true
		}
		if strings.HasPrefix(p, "*.") && strings.HasSuffix(host, p[1:]) && host != p[2:] {
			return true
		}
	}
	return false
}
func methodMatches(actual, expected string) bool {
	return expected == "" || strings.EqualFold(actual, expected)
}
func pathMatches(actual, pathType, expected string) bool {
	switch pathType {
	case "Exact":
		return actual == expected
	case "RegularExpression":
		return false
	default:
		return strings.HasPrefix(actual, expected)
	}
}
