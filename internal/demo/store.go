package demo

import (
	"strings"

	"github.com/gatelens/gatelens/internal/domain"
)

type Store struct {
	context   domain.Context
	topology  domain.Topology
	findings  []domain.Finding
	resources []domain.Resource
}

func NewStore() *Store {
	snapshot := domain.Snapshot{ID: "snapshot-prod-20260724-104218", ObservedAt: "2026-07-24T10:42:18+08:00", State: "complete"}
	return &Store{
		context:  domain.Context{Cluster: domain.Cluster{ID: "edge-prod", Name: "prod-cn-shanghai", Version: "Kubernetes 1.31"}, Namespaces: []string{"all", "higress-system", "ai-platform", "inference"}, Snapshot: snapshot, Capabilities: []string{"gateway-api", "higress", "inference-extension", "transit-hop"}},
		topology: domain.Topology{SnapshotID: snapshot.ID, ObservedAt: snapshot.ObservedAt, Nodes: demoNodes(), Edges: demoEdges()},
		findings: []domain.Finding{
			{ID: "no-ready-endpoints", Severity: domain.StatusError, Title: "后端没有可用 Endpoint", Resource: "Service / inference/embedding-backend", Basis: "ReadyEndpoints=0", TargetID: "service-embedding"},
			{ID: "excluded-endpoint", Severity: domain.StatusWarning, Title: "Endpoint 被推理池排除", Resource: "Endpoint / inference/qwen-72b-c", Basis: "readiness=false", TargetID: "endpoint-qwen-c"},
			{ID: "unknown-filter", Severity: domain.StatusWarning, Title: "存在未支持的扩展过滤器", Resource: "HTTPRoute / inference/embeddings-v1", Basis: "higress.io/ai-cache", TargetID: "route-embeddings"},
		},
		resources: []domain.Resource{
			{ID: "gateway-public", Kind: "Gateway", Name: "ai-public-gateway", Namespace: "ai-platform", Status: domain.StatusHealthy, StatusText: "已接受", UpdatedAt: "2 分钟前"},
			{ID: "route-chat", Kind: "HTTPRoute", Name: "chat-completions", Namespace: "inference", Status: domain.StatusHealthy, StatusText: "已解析", UpdatedAt: "1 分钟前"},
			{ID: "route-embeddings", Kind: "HTTPRoute", Name: "embeddings-v1", Namespace: "inference", Status: domain.StatusWarning, StatusText: "已解析", UpdatedAt: "1 分钟前", Findings: 1},
			{ID: "pool-qwen", Kind: "InferencePool", Name: "qwen-production", Namespace: "inference", Status: domain.StatusHealthy, StatusText: "可用", UpdatedAt: "1 分钟前"},
			{ID: "service-embedding", Kind: "Service", Name: "embedding-backend", Namespace: "inference", Status: domain.StatusError, StatusText: "无 Endpoint", UpdatedAt: "2 分钟前", Findings: 1},
		},
	}
}

func (s *Store) Context() domain.Context    { return s.context }
func (s *Store) Topology() domain.Topology  { return s.topology }
func (s *Store) Findings() []domain.Finding { return s.findings }
func (s *Store) Resources(query string) []domain.Resource {
	if query == "" {
		return s.resources
	}
	needle := strings.ToLower(query)
	var result []domain.Resource
	for _, resource := range s.resources {
		if strings.Contains(strings.ToLower(resource.Kind+" "+resource.Name+" "+resource.Namespace), needle) {
			result = append(result, resource)
		}
	}
	return result
}

func (s *Store) Explain(request domain.RouteExplanationRequest) domain.RouteExplanation {
	base := domain.RouteExplanation{SnapshotID: s.context.Snapshot.ID, ObservedAt: s.context.Snapshot.ObservedAt, Confidence: "medium"}
	if request.Host != "api.ai.example.com" {
		base.Outcome, base.Summary = "Rejected", "没有匹配当前 Host 的 Listener。"
		base.Steps = []domain.ExplainStep{{Hop: 1, Title: "Listener 候选", Detail: "https-api 仅匹配 api.ai.example.com。", State: "rejected", TargetID: "listener-https"}}
		return base
	}
	if strings.Contains(request.Path, "embeddings") {
		base.Outcome, base.Summary = "NoHealthyBackend", "已命中 embeddings-v1，但后端没有 Ready Endpoint。"
		base.Steps = []domain.ExplainStep{{Hop: 1, Title: "Listener 候选", Detail: "命中 https-api。", State: "passed", TargetID: "listener-https"}, {Hop: 1, Title: "Route 规则匹配", Detail: "命中 inference/embeddings-v1。", State: "passed", TargetID: "route-embeddings"}, {Hop: 1, Title: "后端解析", Detail: "inference/embedding-backend 的 ReadyEndpoints=0。", State: "rejected", TargetID: "service-embedding"}}
		return base
	}
	base.Outcome, base.Summary = "Routed", "请求将进入 qwen-production 的健康候选集。"
	base.Steps = []domain.ExplainStep{{Hop: 1, Title: "Listener 候选", Detail: "命中 https-api：Host 与 Listener hostname 一致。", State: "passed", TargetID: "listener-https"}, {Hop: 1, Title: "Route 规则匹配", Detail: "命中 inference/chat-completions：POST /v1/chat/completions。", State: "passed", TargetID: "route-chat"}, {Hop: 1, Title: "推理后端解析", Detail: "qwen-production 有 2 个 Ready Endpoint；实时负载不可见。", State: "unknown", TargetID: "pool-qwen"}}
	return base
}

func demoNodes() []domain.TopologyNode {
	return []domain.TopologyNode{
		{ID: "higress-workload", Name: "higress-gateway", Kind: "GatewayWorkload", Namespace: "higress-system", ClusterID: "edge-prod", Status: domain.StatusHealthy, StatusText: "Ready", Summary: "Higress 数据面工作负载。配置对象不必与其同命名空间。", Conditions: []string{"Ready=2/2"}, Source: "apps/v1 Deployment", WorkloadScope: "higress-system"},
		{ID: "gateway-public", Name: "ai-public-gateway", Kind: "Gateway", Namespace: "ai-platform", ClusterID: "edge-prod", Status: domain.StatusHealthy, StatusText: "已接受", Summary: "面向公网 API 的 Gateway，由 Higress 工作负载处理。", Conditions: []string{"Accepted=True", "Programmed=True"}, Source: "gateway.networking.k8s.io/v1 Gateway", WorkloadScope: "higress-system"},
		{ID: "listener-https", Name: "https-api", Kind: "Listener", Namespace: "ai-platform", ClusterID: "edge-prod", Status: domain.StatusHealthy, StatusText: "已接受", Summary: "匹配 api.ai.example.com 的 HTTPS 入口。", Conditions: []string{"Accepted=True", "ResolvedRefs=True"}, Source: "Gateway.spec.listeners[0]"},
		{ID: "route-chat", Name: "chat-completions", Kind: "HTTPRoute", Namespace: "inference", ClusterID: "edge-prod", Status: domain.StatusHealthy, StatusText: "已解析", Summary: "跨命名空间附着到入口 Listener 的聊天路由。", Conditions: []string{"Accepted=True", "ResolvedRefs=True"}, Source: "gateway.networking.k8s.io/v1 HTTPRoute"},
		{ID: "route-embeddings", Name: "embeddings-v1", Kind: "HTTPRoute", Namespace: "inference", ClusterID: "edge-prod", Status: domain.StatusWarning, StatusText: "1 个警告", Summary: "匹配 embeddings 请求，但目标 Service 没有 Ready Endpoint。", Conditions: []string{"Accepted=True", "BackendNotReady=True"}, Source: "gateway.networking.k8s.io/v1 HTTPRoute"},
		{ID: "pool-qwen", Name: "qwen-production", Kind: "InferencePool", Namespace: "inference", ClusterID: "edge-prod", Status: domain.StatusHealthy, StatusText: "2/3 Ready", Summary: "模型候选池；一个端点被排除。", Conditions: []string{"Resolved=True", "AvailableEndpoints=2/3"}, Source: "inference.networking.k8s.io/v1 InferencePool"},
		{ID: "service-embedding", Name: "embedding-backend", Kind: "Service", Namespace: "inference", ClusterID: "edge-prod", Status: domain.StatusError, StatusText: "端点不可用", Summary: "Service 已解析，但没有 Ready Endpoint。", Conditions: []string{"ReadyEndpoints=0"}, Source: "v1 Service"},
		{ID: "endpoint-qwen-a", Name: "qwen-72b-a", Kind: "Endpoint", Namespace: "inference", ClusterID: "edge-prod", Status: domain.StatusHealthy, StatusText: "Ready", Summary: "qwen2.5-72b-instruct 副本 A。", Conditions: []string{"Ready=True", "Weight=50"}, Source: "discovery.k8s.io/v1 EndpointSlice"},
		{ID: "endpoint-qwen-b", Name: "qwen-72b-b", Kind: "Endpoint", Namespace: "inference", ClusterID: "edge-prod", Status: domain.StatusHealthy, StatusText: "Ready", Summary: "qwen2.5-72b-instruct 副本 B。", Conditions: []string{"Ready=True", "Weight=50"}, Source: "discovery.k8s.io/v1 EndpointSlice"},
		{ID: "endpoint-qwen-c", Name: "qwen-72b-c", Kind: "Endpoint", Namespace: "inference", ClusterID: "edge-prod", Status: domain.StatusWarning, StatusText: "NotReady", Summary: "readiness=false，已从候选集中排除。", Conditions: []string{"Ready=False", "Excluded=True"}, Source: "discovery.k8s.io/v1 EndpointSlice"},
		{ID: "transit-inference", Name: "inference-gw.example", Kind: "TransitHop", Namespace: "", ClusterID: "edge-prod", Status: domain.StatusWarning, StatusText: "远端未接入", Summary: "独立推理集群的 Istio IngressGateway 边界。只知道 HTTPS+mTLS 目标，尚未接入远端配置。", Conditions: []string{"Transport=HTTPS+mTLS", "RemoteCluster=unknown"}, Source: "Higress upstream configuration"},
	}
}
func demoEdges() []domain.TopologyEdge {
	return []domain.TopologyEdge{{From: "higress-workload", To: "gateway-public", Relation: "serves"}, {From: "gateway-public", To: "listener-https", Relation: "owns"}, {From: "listener-https", To: "route-chat", Relation: "attaches"}, {From: "listener-https", To: "route-embeddings", Relation: "attaches"}, {From: "route-chat", To: "pool-qwen", Relation: "routes"}, {From: "route-embeddings", To: "service-embedding", Relation: "routes"}, {From: "pool-qwen", To: "endpoint-qwen-a", Relation: "selects"}, {From: "pool-qwen", To: "endpoint-qwen-b", Relation: "selects"}, {From: "pool-qwen", To: "endpoint-qwen-c", Relation: "excludes"}, {From: "route-chat", To: "transit-inference", Relation: "transit", Transport: "HTTPS+mTLS"}}
}
