package demo

import (
	"context"
	"strings"

	"github.com/gatelens/gatelens/internal/domain"
)

type Store struct {
	context   domain.Context
	topology  domain.Topology
	findings  []domain.Finding
	resources []domain.Resource
	envoy     domain.EnvoyConfig
}

func NewStore() *Store {
	snapshot := domain.Snapshot{ID: "snapshot-prod-20260724-104218", ObservedAt: "2026-07-24T10:42:18+08:00", State: "complete"}
	return &Store{
		context:  domain.Context{Cluster: domain.Cluster{ID: "edge-prod", Name: "prod-cn-shanghai", Version: "Kubernetes 1.31"}, Namespaces: []string{"all", "higress-system", "ai-platform", "inference"}, Snapshot: snapshot, Capabilities: []string{"gateway-api", "higress", "inference-extension", "transit-hop"}},
		topology: domain.Topology{SnapshotID: snapshot.ID, FederatedSnapshotID: "federated-prod-20260724-104218", ObservedAt: snapshot.ObservedAt, Consistency: "consistent-window", Clusters: demoClusters(snapshot), Nodes: demoNodes(), Edges: demoEdges()},
		findings: []domain.Finding{
			{ID: "no-ready-endpoints", Severity: domain.StatusError, Title: "后端没有可用 Endpoint", Resource: "Service / inference/embedding-backend", Basis: "ReadyEndpoints=0", TargetID: "service-embedding"},
			{ID: "excluded-endpoint", Severity: domain.StatusWarning, Title: "Endpoint 被推理池排除", Resource: "Endpoint / inference/qwen-72b-c", Basis: "readiness=false", TargetID: "endpoint-qwen-c"},
			{ID: "unknown-filter", Severity: domain.StatusWarning, Title: "存在未支持的扩展过滤器", Resource: "HTTPRoute / inference/embeddings-v1", Basis: "higress.io/ai-cache", TargetID: "route-embeddings"},
		},
		envoy: envoyConfig(snapshot),
		resources: []domain.Resource{
			{ID: "gateway-public", Kind: "Gateway", Name: "ai-public-gateway", Namespace: "ai-platform", Status: domain.StatusHealthy, StatusText: "已接受", UpdatedAt: "2 分钟前"},
			{ID: "route-chat", Kind: "HTTPRoute", Name: "chat-completions", Namespace: "inference", Status: domain.StatusHealthy, StatusText: "已解析", UpdatedAt: "1 分钟前"},
			{ID: "route-embeddings", Kind: "HTTPRoute", Name: "embeddings-v1", Namespace: "inference", Status: domain.StatusWarning, StatusText: "已解析", UpdatedAt: "1 分钟前", Findings: 1},
			{ID: "pool-qwen", Kind: "InferencePool", Name: "qwen-production", Namespace: "inference", Status: domain.StatusHealthy, StatusText: "可用", UpdatedAt: "1 分钟前"},
			{ID: "service-embedding", Kind: "Service", Name: "embedding-backend", Namespace: "inference", Status: domain.StatusError, StatusText: "无 Endpoint", UpdatedAt: "2 分钟前", Findings: 1},
		},
	}
}

func (s *Store) EnvoyConfig(_ context.Context, gatewayID string) (domain.EnvoyConfig, error) {
	config := s.envoy
	config.GatewayID = gatewayID
	config.Controller = "higress.io/gateway-controller"
	config.Workload = "higress-system/higress-gateway"
	config.SampledPod = "higress-system/higress-gateway-7d8f-abcde"
	config.ReadyReplicas = 3
	return config, nil
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

func demoClusters(edgeSnapshot domain.Snapshot) []domain.TopologyCluster {
	gpuSnapshot := domain.Snapshot{ID: "snapshot-gpu-prod-20260724-104246", ObservedAt: "2026-07-24T10:42:46+08:00", State: "complete"}
	return []domain.TopologyCluster{
		{ID: "edge-prod", Name: "prod-cn-shanghai", Role: "ingress-gateway", Environment: "production", Version: "Kubernetes 1.31", ConnectionState: "connected", Namespaces: []string{"higress-system", "ai-platform", "inference"}, Snapshot: edgeSnapshot},
		{ID: "gpu-prod", Name: "prod-cn-shanghai-gpu", Role: "gpu-inference", Environment: "production", Version: "Kubernetes 1.30", ConnectionState: "connected", Namespaces: []string{"istio-system", "inference"}, Snapshot: gpuSnapshot},
	}
}
func demoNodes() []domain.TopologyNode {
	return []domain.TopologyNode{
		{ID: "gateway-public", Name: "ai-public-gateway", Kind: "Gateway", Namespace: "ai-platform", ClusterID: "edge-prod", Status: domain.StatusHealthy, StatusText: "已接受", Summary: "面向公网 API 的 Gateway，由 Higress 工作负载处理。", Conditions: []string{"Accepted=True", "Programmed=True", "EnvoyConfig=available", "Controller=higress.io/gateway-controller", "Workload=higress-system/higress-gateway", "ReadyReplicas=3"}, Source: "gateway.networking.k8s.io/v1 Gateway", WorkloadScope: "higress-system"},
		{ID: "listener-https", Name: "https-api", Kind: "Listener", Namespace: "ai-platform", ClusterID: "edge-prod", Status: domain.StatusHealthy, StatusText: "已接受", Summary: "匹配 api.ai.example.com 的 HTTPS 入口。", Conditions: []string{"Accepted=True", "ResolvedRefs=True"}, Source: "Gateway.spec.listeners[0]"},
		{ID: "route-chat", Name: "chat-completions", Kind: "HTTPRoute", Namespace: "inference", ClusterID: "edge-prod", Status: domain.StatusHealthy, StatusText: "已解析", Summary: "跨命名空间附着到入口 Listener 的聊天路由。", Conditions: []string{"Accepted=True", "ResolvedRefs=True"}, Source: "gateway.networking.k8s.io/v1 HTTPRoute"},
		{ID: "route-embeddings", Name: "embeddings-v1", Kind: "HTTPRoute", Namespace: "inference", ClusterID: "edge-prod", Status: domain.StatusWarning, StatusText: "1 个警告", Summary: "匹配 embeddings 请求，但目标 Service 没有 Ready Endpoint。", Conditions: []string{"Accepted=True", "BackendNotReady=True"}, Source: "gateway.networking.k8s.io/v1 HTTPRoute"},
		{ID: "pool-qwen", Name: "qwen-production", Kind: "InferencePool", Namespace: "inference", ClusterID: "edge-prod", Status: domain.StatusHealthy, StatusText: "2/3 Ready", Summary: "模型候选池；一个端点被排除。", Conditions: []string{"Resolved=True", "AvailableEndpoints=2/3"}, Source: "inference.networking.k8s.io/v1 InferencePool"},
		{ID: "service-embedding", Name: "embedding-backend", Kind: "Service", Namespace: "inference", ClusterID: "edge-prod", Status: domain.StatusError, StatusText: "端点不可用", Summary: "Service 已解析，但没有 Ready Endpoint。", Conditions: []string{"ReadyEndpoints=0"}, Source: "v1 Service"},
		{ID: "endpoint-qwen-a", Name: "qwen-72b-a", Kind: "Endpoint", Namespace: "inference", ClusterID: "edge-prod", Status: domain.StatusHealthy, StatusText: "Ready", Summary: "qwen2.5-72b-instruct 副本 A。", Conditions: []string{"Ready=True", "Weight=50"}, Source: "discovery.k8s.io/v1 EndpointSlice"},
		{ID: "endpoint-qwen-b", Name: "qwen-72b-b", Kind: "Endpoint", Namespace: "inference", ClusterID: "edge-prod", Status: domain.StatusHealthy, StatusText: "Ready", Summary: "qwen2.5-72b-instruct 副本 B。", Conditions: []string{"Ready=True", "Weight=50"}, Source: "discovery.k8s.io/v1 EndpointSlice"},
		{ID: "endpoint-qwen-c", Name: "qwen-72b-c", Kind: "Endpoint", Namespace: "inference", ClusterID: "edge-prod", Status: domain.StatusWarning, StatusText: "NotReady", Summary: "readiness=false，已从候选集中排除。", Conditions: []string{"Ready=False", "Excluded=True"}, Source: "discovery.k8s.io/v1 EndpointSlice"},
		{ID: "transit-inference", Name: "inference-gw.example", Kind: "TransitHop", Namespace: "", ClusterID: "edge-prod", Status: domain.StatusWarning, StatusText: "远端未接入", Summary: "独立推理集群的 Istio IngressGateway 边界。只知道 HTTPS+mTLS 目标，尚未接入远端配置。", Conditions: []string{"Transport=HTTPS+mTLS", "RemoteCluster=unknown"}, Source: "Higress upstream configuration"},
		{ID: "gpu-ingress", Name: "inference-ingress", Kind: "Gateway", Namespace: "istio-system", ClusterID: "gpu-prod", Status: domain.StatusHealthy, StatusText: "Connected", Summary: "Istio ingress gateway for traffic from the edge cluster.", Conditions: []string{"Programmed=True", "mTLS=STRICT", "ReadyReplicas=3"}, Source: "networking.istio.io/v1 Gateway", WorkloadScope: "gpu-prod/istio-system"},
		{ID: "gpu-listener", Name: "https-inference", Kind: "Listener", Namespace: "istio-system", ClusterID: "gpu-prod", Status: domain.StatusHealthy, StatusText: "Accepted", Summary: "TLS listener for the registered inference ingress endpoint.", Conditions: []string{"Protocol=HTTPS", "TLS=PASSTHROUGH"}, Source: "Istio Gateway server"},
		{ID: "gpu-route-chat", Name: "qwen-chat", Kind: "HTTPRoute", Namespace: "inference", ClusterID: "gpu-prod", Status: domain.StatusHealthy, StatusText: "Resolved", Summary: "Maps chat API traffic to the Qwen inference service.", Conditions: []string{"ResolvedRefs=True", "Evidence=configuration"}, Source: "networking.istio.io/v1 VirtualService"},
		{ID: "gpu-service-qwen", Name: "qwen-72b-server", Kind: "Service", Namespace: "inference", ClusterID: "gpu-prod", Status: domain.StatusHealthy, StatusText: "2 Ready", Summary: "GPU inference service for qwen2.5-72b-instruct.", Conditions: []string{"ReadyEndpoints=2"}, Source: "v1 Service"},
		{ID: "gpu-pool-qwen", Name: "qwen-production", Kind: "InferencePool", Namespace: "inference", ClusterID: "gpu-prod", Status: domain.StatusHealthy, StatusText: "2/3 Ready", Summary: "Model candidate pool; one endpoint is excluded by readiness.", Conditions: []string{"Resolved=True", "AvailableEndpoints=2/3"}, Source: "inference.networking.k8s.io/v1 InferencePool"},
		{ID: "gpu-endpoint-qwen-a", Name: "qwen-72b-a", Kind: "Endpoint", Namespace: "inference", ClusterID: "gpu-prod", Status: domain.StatusHealthy, StatusText: "Ready", Summary: "qwen2.5-72b-instruct replica on gpu-a100-01.", Conditions: []string{"Ready=True", "GPU=A100-80G", "Weight=50"}, Source: "discovery.k8s.io/v1 EndpointSlice"},
		{ID: "gpu-endpoint-qwen-b", Name: "qwen-72b-b", Kind: "Endpoint", Namespace: "inference", ClusterID: "gpu-prod", Status: domain.StatusHealthy, StatusText: "Ready", Summary: "qwen2.5-72b-instruct replica on gpu-a100-02.", Conditions: []string{"Ready=True", "GPU=A100-80G", "Weight=50"}, Source: "discovery.k8s.io/v1 EndpointSlice"},
	}
}
func demoEdges() []domain.TopologyEdge {
	return []domain.TopologyEdge{
		{From: "gateway-public", To: "listener-https", Relation: "owns"}, {From: "listener-https", To: "route-chat", Relation: "attaches"}, {From: "listener-https", To: "route-embeddings", Relation: "attaches"},
		{From: "route-chat", To: "transit-inference", Relation: "transit", Transport: "HTTPS + mTLS", Destination: "inference-gw.example", State: "verified", Evidence: "Higress upstream and TLS policy"},
		{From: "transit-inference", To: "gpu-ingress", Relation: "cross-cluster", Transport: "HTTPS + mTLS", Destination: "gpu-prod/istio-system/inference-ingress", State: "resolved", Evidence: "configuration target inference-gw.example matched remote Gateway address"},
		{From: "gpu-ingress", To: "gpu-listener", Relation: "owns"}, {From: "gpu-listener", To: "gpu-route-chat", Relation: "attaches"}, {From: "gpu-route-chat", To: "gpu-service-qwen", Relation: "routes"}, {From: "gpu-service-qwen", To: "gpu-pool-qwen", Relation: "selects"},
		{From: "gpu-pool-qwen", To: "gpu-endpoint-qwen-a", Relation: "selects"}, {From: "gpu-pool-qwen", To: "gpu-endpoint-qwen-b", Relation: "selects"},
		{From: "route-embeddings", To: "service-embedding", Relation: "routes"}, {From: "pool-qwen", To: "endpoint-qwen-a", Relation: "selects"}, {From: "pool-qwen", To: "endpoint-qwen-b", Relation: "selects"}, {From: "pool-qwen", To: "endpoint-qwen-c", Relation: "excludes"},
	}
}

func envoyConfig(snapshot domain.Snapshot) domain.EnvoyConfig {
	config := domain.EnvoyConfig{
		SnapshotID: snapshot.ID,
		ObservedAt: snapshot.ObservedAt,
		State:      "complete",
		Source:     "Envoy admin /config_dump",
		Proxy:      "higress-gateway · Envoy 1.31",
		Listeners: []domain.EnvoyListener{
			{
				ID: "0.0.0.0_8443|0.0.0.0:8443|0", Name: "0.0.0.0_8443", Address: "0.0.0.0", Port: 8443, Protocol: "HTTPS", Status: domain.StatusHealthy,
				FilterChains: []domain.EnvoyFilterChain{
					{
						Name: "https-api", Match: "SNI: api.ai.example.com", Transport: "TLS",
						HTTPFilters: []domain.EnvoyHTTPFilter{
							{Name: "envoy.filters.http.cors", Type: "HTTP filter", Stage: "request", ConfigSummary: "allow_origin: https://console.ai.example.com"},
							{Name: "ai-platform.ai-guard", Type: "HTTP filter", Stage: "request", ConfigSummary: "ai-guard · root guard-root · envoy.wasm.runtime.v8"},
							{Name: "envoy.filters.http.ext_proc", Type: "HTTP filter", Stage: "request", ConfigSummary: "gRPC: ext-proc-ai-policy · timeout 200ms · failure closed"},
							{Name: "envoy.filters.http.router", Type: "HTTP filter", Stage: "terminal", ConfigSummary: "routes the request to the selected cluster", Terminal: true},
						},
						Routes: []domain.EnvoyRoute{
							{Name: "chat-completions", Match: "POST api.ai.example.com /v1/chat/completions", Cluster: "inference|qwen-production"},
							{Name: "embeddings", Match: "POST api.ai.example.com /v1/embeddings", Cluster: "inference|embedding-backend"},
						},
					},
					{
						Name: "http-fallback", Match: "all non-TLS traffic", Transport: "raw_buffer",
						HTTPFilters: []domain.EnvoyHTTPFilter{{Name: "envoy.filters.http.router", Type: "HTTP filter", Stage: "terminal", ConfigSummary: "direct response: 301 HTTPS redirect", Terminal: true}},
						Routes:      []domain.EnvoyRoute{{Name: "redirect", Match: "GET * /", Cluster: "redirect-to-https"}},
					},
				},
			},
		},
		Extensions: []domain.EnvoyExtension{
			{
				ID: "Wasm|ai-platform.ai-guard|demo", Name: "ai-platform.ai-guard", Kind: "Wasm",
				TypeURL: "type.googleapis.com/envoy.extensions.filters.http.wasm.v3.Wasm", Status: domain.StatusHealthy,
				ConfigSource: "ECDS", ConfigSummary: "ai-guard · root guard-root · envoy.wasm.runtime.v8",
				Attachments:  []domain.EnvoyExtensionAttachment{{ListenerID: "0.0.0.0_8443|0.0.0.0:8443|0", ListenerName: "0.0.0.0_8443", FilterChain: "https-api", FilterName: "ai-platform.ai-guard", FilterType: "HTTP filter", Position: 2}},
				Dependencies: []domain.EnvoyExtensionDependency{{Kind: "ECDS", Name: "ai-platform.ai-guard", Relation: "resolves through", Evidence: "http_filters[].config_discovery + EcdsConfigDump", Resolved: true}, {Kind: "Wasm module", Name: "/var/local/lib/ai-guard.wasm", Relation: "loads", Evidence: "typed_config.config.vm_config.code", Resolved: true}},
			},
			{
				ID: "ext_proc|envoy.filters.http.ext_proc|demo", Name: "envoy.filters.http.ext_proc", Kind: "ext_proc",
				TypeURL: "type.googleapis.com/envoy.extensions.filters.http.ext_proc.v3.ExternalProcessor", Status: domain.StatusHealthy,
				ConfigSource: "inline typed_config", ConfigSummary: "gRPC: ext-proc-ai-policy · timeout 200ms · failure closed",
				Attachments:  []domain.EnvoyExtensionAttachment{{ListenerID: "0.0.0.0_8443|0.0.0.0:8443|0", ListenerName: "0.0.0.0_8443", FilterChain: "https-api", FilterName: "envoy.filters.http.ext_proc", FilterType: "HTTP filter", Position: 3}},
				Dependencies: []domain.EnvoyExtensionDependency{{Kind: "Cluster", Name: "ext-proc-ai-policy", Relation: "calls", Evidence: "typed_config grpc_service/envoy_grpc.cluster_name", Resolved: true}},
			},
			{
				ID: "Envoy Filter|envoy.filters.http.cors|demo", Name: "envoy.filters.http.cors", Kind: "Envoy Filter",
				Status: domain.StatusHealthy, ConfigSource: "filter name", ConfigSummary: "configured",
				Attachments: []domain.EnvoyExtensionAttachment{{ListenerID: "0.0.0.0_8443|0.0.0.0:8443|0", ListenerName: "0.0.0.0_8443", FilterChain: "https-api", FilterName: "envoy.filters.http.cors", FilterType: "HTTP filter", Position: 1}}, Dependencies: []domain.EnvoyExtensionDependency{},
			},
		},
		Clusters: []domain.EnvoyCluster{
			{Name: "inference|qwen-production", Type: "EDS", Discovery: "xDS / istiod", ConnectTimeout: "2s", Endpoints: []domain.EnvoyEndpoint{{Address: "10.42.3.18", Port: 8080, Status: domain.StatusHealthy, Health: "healthy", Weight: 50}, {Address: "10.42.4.22", Port: 8080, Status: domain.StatusHealthy, Health: "healthy", Weight: 50}, {Address: "10.42.7.9", Port: 8080, Status: domain.StatusWarning, Health: "warming"}}},
			{Name: "inference|embedding-backend", Type: "EDS", Discovery: "xDS / istiod", ConnectTimeout: "2s", Endpoints: []domain.EnvoyEndpoint{{Address: "10.42.9.12", Port: 8080, Status: domain.StatusError, Health: "no healthy hosts"}}},
			{Name: "redirect-to-https", Type: "STATIC", Discovery: "static", ConnectTimeout: "1s"},
			{Name: "ext-proc-ai-policy", Type: "STATIC", Discovery: "inline", ConnectTimeout: "1s", Endpoints: []domain.EnvoyEndpoint{{Address: "10.42.12.7", Port: 9002, Status: domain.StatusHealthy, Health: "healthy", Weight: 1}}},
		},
	}
	config.RawConfig = demoRawEnvoyConfig()
	return config
}

func demoRawEnvoyConfig() []byte {
	return []byte(`{
  "configs": [
    {
      "@type": "type.googleapis.com/envoy.admin.v3.ListenersConfigDump",
      "dynamic_listeners": [
        {
          "name": "0.0.0.0_8443",
          "active_state": {
            "listener": {
              "name": "0.0.0.0_8443",
              "address": {"socket_address": {"address": "0.0.0.0", "port_value": 8443}},
              "filter_chains": [
                {
                  "name": "https-api",
                  "filter_chain_match": {"server_names": ["api.ai.example.com"], "transport_protocol": "tls"},
                  "filters": [
                    {
                      "name": "envoy.filters.network.http_connection_manager",
                      "typed_config": {
                        "@type": "type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager",
                        "stat_prefix": "https-api",
                        "http_filters": [
                          {"name": "envoy.filters.http.cors"},
                          {
                            "name": "ai-platform.ai-guard",
                            "config_discovery": {
                              "config_source": {"ads": {}},
                              "type_urls": ["type.googleapis.com/envoy.extensions.filters.http.wasm.v3.Wasm"]
                            }
                          },
                          {
                            "name": "envoy.filters.http.ext_proc",
                            "typed_config": {
                              "@type": "type.googleapis.com/envoy.extensions.filters.http.ext_proc.v3.ExternalProcessor",
                              "grpc_service": {"envoy_grpc": {"cluster_name": "ext-proc-ai-policy"}},
                              "message_timeout": "200ms",
                              "failure_mode_allow": false
                            }
                          },
                          {"name": "envoy.filters.http.router"}
                        ],
                        "route_config": {
                          "name": "https-api-routes",
                          "virtual_hosts": [
                            {
                              "name": "api.ai.example.com",
                              "domains": ["api.ai.example.com"],
                              "routes": [
                                {"name": "chat-completions", "match": {"prefix": "/v1/chat/completions"}, "route": {"cluster": "inference|qwen-production"}},
                                {"name": "embeddings", "match": {"prefix": "/v1/embeddings"}, "route": {"cluster": "inference|embedding-backend"}}
                              ]
                            }
                          ]
                        }
                      }
                    }
                  ]
                }
              ]
            }
          }
        }
      ]
    },
    {
      "@type": "type.googleapis.com/envoy.admin.v3.EcdsConfigDump",
      "ecds_filters": [
        {
          "ecds_filter": {
            "name": "ai-platform.ai-guard",
            "typed_config": {
              "@type": "type.googleapis.com/envoy.extensions.filters.http.wasm.v3.Wasm",
              "config": {
                "name": "ai-guard",
                "root_id": "guard-root",
                "vm_config": {
                  "vm_id": "ai-guard-vm",
                  "runtime": "envoy.wasm.runtime.v8",
                  "code": {"local": {"filename": "/var/local/lib/ai-guard.wasm"}}
                }
              }
            }
          }
        }
      ]
    },
    {
      "@type": "type.googleapis.com/envoy.admin.v3.ClustersConfigDump",
      "dynamic_active_clusters": [
        {"cluster": {"name": "inference|qwen-production", "type": "EDS", "connect_timeout": "2s"}},
        {"cluster": {"name": "inference|embedding-backend", "type": "EDS", "connect_timeout": "2s"}},
        {"cluster": {"name": "redirect-to-https", "type": "STATIC", "connect_timeout": "1s"}},
        {
          "cluster": {
            "name": "ext-proc-ai-policy",
            "type": "STATIC",
            "connect_timeout": "1s",
            "load_assignment": {
              "cluster_name": "ext-proc-ai-policy",
              "endpoints": [{"lb_endpoints": [{"health_status": "HEALTHY", "endpoint": {"address": {"socket_address": {"address": "10.42.12.7", "port_value": 9002}}}}]}]
            }
          }
        }
      ]
    }
  ]
}`)
}
