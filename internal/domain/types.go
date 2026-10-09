package domain

import "encoding/json"

type Status string

const (
	StatusHealthy Status = "healthy"
	StatusWarning Status = "warning"
	StatusError   Status = "error"
)

// Context 描述当前集群及快照的概况，供 /api/v1/context 和 Agent 快照使用。
type Context struct {
	Cluster      Cluster  `json:"cluster"`
	Namespaces   []string `json:"namespaces"`
	Snapshot     Snapshot `json:"snapshot"`
	Capabilities []string `json:"adapterCapabilities"`
}

// Cluster 标识 Context 所属的集群，供概况接口展示集群名称和版本。
type Cluster struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Snapshot 记录一次采集的标识、时间和状态，供集群概况与拓扑标记数据时效。
type Snapshot struct {
	ID         string `json:"id"`
	ObservedAt string `json:"observedAt"`
	State      string `json:"state"`
}

// Topology 汇总集群、节点、连线和探测入口，供拓扑接口及跨集群聚合使用。
type Topology struct {
	SnapshotID          string              `json:"snapshotID"`
	FederatedSnapshotID string              `json:"federatedSnapshotID,omitempty"`
	ObservedAt          string              `json:"observedAt"`
	Consistency         SnapshotConsistency `json:"consistency,omitempty"`
	Clusters            []TopologyCluster   `json:"clusters,omitempty"`
	Nodes               []TopologyNode      `json:"nodes"`
	Edges               []TopologyEdge      `json:"edges"`
	ProbeEntries        []ProbeEntry        `json:"probeEntries,omitempty"`
	Truncated           bool                `json:"truncated"`
}

// ProbeEntry 描述可发起主动探测的网关服务入口，供拓扑展示和探测目标选择。
type ProbeEntry struct {
	ID          string      `json:"id"`
	GatewayID   string      `json:"gatewayID"`
	ClusterID   string      `json:"clusterID"`
	Namespace   string      `json:"namespace"`
	ServiceName string      `json:"serviceName"`
	DNSName     string      `json:"dnsName"`
	Port        int32       `json:"port"`
	Scheme      ProbeScheme `json:"scheme"`
	Protocol    string      `json:"protocol"`
	DisplayName string      `json:"displayName"`
	Addresses   []string    `json:"addresses,omitempty"`
}

// TopologyCluster 描述拓扑中的一个集群及其快照，供联邦拓扑标识数据来源。
type TopologyCluster struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Environment     string   `json:"environment,omitempty"`
	Version         string   `json:"version"`
	ConnectionState string   `json:"connectionState"`
	Namespaces      []string `json:"namespaces"`

	Snapshot Snapshot `json:"snapshot"`
}

// TopologyNodeKind 表示拓扑节点的实体类型。
type TopologyNodeKind string

const (
	TopologyNodeKindGateway        TopologyNodeKind = "Gateway"
	TopologyNodeKindListener       TopologyNodeKind = "Listener"
	TopologyNodeKindHTTPRoute      TopologyNodeKind = "HTTPRoute"
	TopologyNodeKindIngress        TopologyNodeKind = "Ingress"
	TopologyNodeKindService        TopologyNodeKind = "Service"
	TopologyNodeKindEndpoint       TopologyNodeKind = "Endpoint"
	TopologyNodeKindPod            TopologyNodeKind = "Pod"
	TopologyNodeKindInferencePool  TopologyNodeKind = "InferencePool"
	TopologyNodeKindEndpointPicker TopologyNodeKind = "EndpointPicker"
	TopologyNodeKindMcpBridge      TopologyNodeKind = "McpBridge"
	TopologyNodeKindRegistry       TopologyNodeKind = "Registry"
	TopologyNodeKindExternalTarget TopologyNodeKind = "ExternalTarget" // Mcpbridge 的 Registry 域名不是本地 service 的
	TopologyNodeKindTransitHop     TopologyNodeKind = "TransitHop"     // HttpRoute backend 是 ExternalName 类型的 service
)

// TopologyNode 表示网关、路由、服务等拓扑实体，供拓扑图和跨集群关联使用。
type TopologyNode struct {
	ID            string           `json:"id"`
	Name          string           `json:"name"`
	Kind          TopologyNodeKind `json:"kind"`
	Namespace     string           `json:"namespace"`
	ClusterID     string           `json:"clusterID"`
	Status        Status           `json:"status"`
	StatusText    string           `json:"statusText"`
	Summary       string           `json:"summary"`
	Conditions    []string         `json:"conditions"`
	Source        string           `json:"source"`
	WorkloadScope string           `json:"workloadScope,omitempty"`
}

// TopologyEdge 表示拓扑实体间的关系及其证据，供拓扑图展示流量路径。
type TopologyEdge struct {
	From        string `json:"from"`
	To          string `json:"to"`
	Relation    string `json:"relation"`
	Transport   string `json:"transport,omitempty"`
	Destination string `json:"destination,omitempty"`
	State       string `json:"state,omitempty"`
	Evidence    string `json:"evidence,omitempty"`
}

// EnvoyConfig 汇总网关的 Envoy 运行配置，供配置接口和 Agent 远程查询使用。
type EnvoyConfig struct {
	SnapshotID    string           `json:"snapshotID"`
	ObservedAt    string           `json:"observedAt"`
	State         string           `json:"state"`
	Source        string           `json:"source"`
	Proxy         string           `json:"proxy"`
	GatewayID     string           `json:"gatewayID,omitempty"`
	Controller    string           `json:"controller,omitempty"`
	Workload      string           `json:"workload,omitempty"`
	SampledPod    string           `json:"sampledPod,omitempty"`
	ReadyReplicas int              `json:"readyReplicas,omitempty"`
	Listeners     []EnvoyListener  `json:"listeners"`
	Clusters      []EnvoyCluster   `json:"clusters"`
	Extensions    []EnvoyExtension `json:"extensions"`
	RawConfig     json.RawMessage  `json:"rawConfig,omitempty"`
}

// EnvoyListener 描述 Envoy 监听地址及过滤器链，供网关配置详情展示。
type EnvoyListener struct {
	ID           string             `json:"id"`
	Name         string             `json:"name"`
	Address      string             `json:"address"`
	Port         int                `json:"port"`
	Protocol     string             `json:"protocol"`
	Status       Status             `json:"status"`
	FilterChains []EnvoyFilterChain `json:"filterChains"`
}

// EnvoyFilterChain 描述监听器中的匹配条件、HTTP 过滤器和路由。
type EnvoyFilterChain struct {
	Name        string            `json:"name"`
	Match       string            `json:"match"`
	Transport   string            `json:"transport"`
	HTTPFilters []EnvoyHTTPFilter `json:"httpFilters"`
	Routes      []EnvoyRoute      `json:"routes"`
}

// EnvoyHTTPFilter 描述过滤器的类型、执行阶段和配置摘要，供配置详情展示。
type EnvoyHTTPFilter struct {
	Name          string `json:"name"`
	Type          string `json:"type"`
	Stage         string `json:"stage"`
	ConfigSummary string `json:"configSummary"`
	Terminal      bool   `json:"terminal"`
}

// EnvoyExtension 汇总扩展过滤器的配置、挂载位置和依赖，供扩展详情展示。
type EnvoyExtension struct {
	ID            string                     `json:"id"`
	Name          string                     `json:"name"`
	Kind          string                     `json:"kind"`
	TypeURL       string                     `json:"typeURL,omitempty"`
	Status        Status                     `json:"status"`
	ConfigSource  string                     `json:"configSource"`
	ConfigSummary string                     `json:"configSummary"`
	Attachments   []EnvoyExtensionAttachment `json:"attachments"`
	Dependencies  []EnvoyExtensionDependency `json:"dependencies"`
}

// EnvoyExtensionAttachment 标识扩展在监听器过滤器链中的挂载位置。
type EnvoyExtensionAttachment struct {
	ListenerID   string `json:"listenerID"`
	ListenerName string `json:"listenerName"`
	FilterChain  string `json:"filterChain"`
	FilterName   string `json:"filterName"`
	FilterType   string `json:"filterType"`
	Position     int    `json:"position"`
}

// EnvoyExtensionDependency 记录扩展依赖的配置或集群及其解析证据。
type EnvoyExtensionDependency struct {
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Relation string `json:"relation"`
	Evidence string `json:"evidence"`
	Resolved bool   `json:"resolved"`
}

// EnvoyRoute 描述过滤器链中的路由匹配和上游目标，供配置详情追踪转发规则。
type EnvoyRoute struct {
	Name             string                    `json:"name"`
	Match            string                    `json:"match"`
	Cluster          string                    `json:"cluster"`
	WeightedClusters []EnvoyWeightedCluster    `json:"weightedClusters,omitempty"`
	ExtProcs         []EnvoyRouteExtProcTarget `json:"extProcs,omitempty"`
}

// EnvoyRouteExtProcTarget 记录路由关联的外部处理器及其 gRPC 集群。
type EnvoyRouteExtProcTarget struct {
	FilterName   string   `json:"filterName"`
	TypeURL      string   `json:"typeURL,omitempty"`
	GRPCClusters []string `json:"grpcClusters,omitempty"`
}

// EnvoyWeightedCluster 描述路由分流时的上游集群及权重。
type EnvoyWeightedCluster struct {
	Name   string `json:"name"`
	Weight int    `json:"weight"`
}

// EnvoyCluster 描述 Envoy 上游集群及其端点，供配置详情查看转发目标。
type EnvoyCluster struct {
	Name           string          `json:"name"`
	Type           string          `json:"type"`
	Discovery      string          `json:"discovery"`
	ConnectTimeout string          `json:"connectTimeout"`
	Endpoints      []EnvoyEndpoint `json:"endpoints"`
}

// EnvoyEndpoint 记录上游端点的地址、健康状态和权重。
type EnvoyEndpoint struct {
	Address string `json:"address"`
	Port    int    `json:"port"`
	Status  Status `json:"status"`
	Health  string `json:"health"`
	Weight  int    `json:"weight"`
}

// Finding 记录资源诊断发现及依据，供健康发现接口和 Agent 快照使用。
type Finding struct {
	ID       string `json:"id"`
	Severity Status `json:"severity"`
	Title    string `json:"title"`
	Resource string `json:"resource"`
	Basis    string `json:"basis"`
	TargetID string `json:"targetID"`
}

// Resource 概括可检索资源的状态和发现数量，供资源列表接口使用。
type Resource struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Namespace  string `json:"namespace"`
	Status     Status `json:"status"`
	StatusText string `json:"statusText"`
	UpdatedAt  string `json:"updatedAt"`
	Findings   int    `json:"findings"`
}

// AgentSnapshot 打包 Agent 采集的集群数据，上传后由联邦存储聚合。
type AgentSnapshot struct {
	Cluster   TopologyCluster `json:"cluster"`
	Context   Context         `json:"context"`
	Topology  Topology        `json:"topology"`
	Findings  []Finding       `json:"findings"`
	Resources []Resource      `json:"resources"`
	SentAt    string          `json:"sentAt"`
}

// ProbeRequest 是创建主动探测的 API 输入，指定入口和待发送的 HTTP 请求。
type ProbeRequest struct {
	SourceCluster  string          `json:"sourceCluster"`
	GatewayID      string          `json:"gatewayID"`
	EntryID        string          `json:"entryID"`
	Method         ProbeHTTPMethod `json:"method"`
	Path           string          `json:"path"`
	Host           string          `json:"host,omitempty"`
	APIKey         string          `json:"apiKey,omitempty"`
	ContentType    string          `json:"contentType,omitempty"`
	Body           string          `json:"body,omitempty"`
	TimeoutSeconds int             `json:"timeoutSeconds,omitempty"`
}

// ProbeExecution 记录一次主动探测的响应和观测链路，供探测查询接口返回。
type ProbeExecution struct {
	SchemaVersion       int                 `json:"schemaVersion"`
	ID                  string              `json:"id"`
	TraceID             string              `json:"traceID"`
	SourceCluster       string              `json:"sourceCluster"`
	GatewayID           string              `json:"gatewayID"`
	Method              ProbeHTTPMethod     `json:"method"`
	Target              string              `json:"target"`
	State               ProbeExecutionState `json:"state"`
	StartedAt           string              `json:"startedAt"`
	CompletedAt         string              `json:"completedAt,omitempty"`
	ResponseCode        int                 `json:"responseCode,omitempty"`
	ResponseBytes       int64               `json:"responseBytes,omitempty"`
	DurationMillis      int64               `json:"durationMillis,omitempty"`
	Segments            []ProbeSegment      `json:"segments"`
	FederatedSnapshotID string              `json:"federatedSnapshotID,omitempty"`
	SnapshotConsistency SnapshotConsistency `json:"snapshotConsistency,omitempty"`
	Issues              []ProbeIssue        `json:"issues"`
	Error               string              `json:"error,omitempty"`
	FinalResponseHopID  string              `json:"finalResponseHopID,omitempty"`
	FinalUpstreamHopID  string              `json:"finalUpstreamHopID,omitempty"`
}

// ProbeAgentResult contains only local facts; HTTP is absent for log-only commands.
type ProbeAgentResult struct {
	SchemaVersion int              `json:"schemaVersion"`
	ProbeID       string           `json:"probeID"`
	TraceID       string           `json:"traceID"`
	ClusterID     string           `json:"clusterID"`
	GatewayID     string           `json:"gatewayID"`
	StartedAt     string           `json:"startedAt"`
	CompletedAt   string           `json:"completedAt"`
	HTTP          *ProbeHTTPResult `json:"http,omitempty"`
	Hops          []ObservedHop    `json:"hops"`
	Collection    *ProbeCollection `json:"collection"`
	Issues        []ProbeIssue     `json:"issues"`
}

// ProbeHTTPResult is the source Agent's measured response, never inferred from logs.
type ProbeHTTPResult struct {
	Method         ProbeHTTPMethod `json:"method"`
	Target         string          `json:"target"`
	ResponseCode   int             `json:"responseCode,omitempty"`
	ResponseBytes  int64           `json:"responseBytes,omitempty"`
	DurationMillis int64           `json:"durationMillis,omitempty"`
	Error          string          `json:"error,omitempty"`
}

// ProbeSegment owns one gateway's records, collection outcome and confirmed relations.
type ProbeSegment struct {
	Index               int                       `json:"index"`
	ClusterID           string                    `json:"clusterID"`
	GatewayID           string                    `json:"gatewayID"`
	GatewayName         string                    `json:"gatewayName,omitempty"`
	SnapshotID          string                    `json:"snapshotID,omitempty"`
	SnapshotObservedAt  string                    `json:"snapshotObservedAt,omitempty"`
	LogSource           string                    `json:"logSource,omitempty"`
	Transport           string                    `json:"transport,omitempty"`
	Destination         string                    `json:"destination,omitempty"`
	InferenceBasis      string                    `json:"inferenceBasis,omitempty"`
	InferenceConfidence ProbeInferenceConfidence  `json:"inferenceConfidence,omitempty"`
	Hops                []ObservedHop             `json:"hops"`
	Collection          *ProbeCollection          `json:"collection"`
	RelationState       ProbeAttemptRelationState `json:"relationState"`
	Links               []ProbeAttemptLink        `json:"links"`
	LocalTerminalHopIDs []string                  `json:"localTerminalHopIDs"`
	Issues              []ProbeIssue              `json:"issues"`
}

// ProbeCollectionState 表示日志采集窗口的结束状态。
type ProbeCollectionState string

const (
	ProbeCollectionStateSettled     ProbeCollectionState = "settled"      // 已观察到稳定终止记录。
	ProbeCollectionStateWindowEnded ProbeCollectionState = "window-ended" // 采集窗口到期，未确认稳定终止记录。
	ProbeCollectionStateCancelled   ProbeCollectionState = "cancelled"    // 日志采集已取消。
	ProbeCollectionStateReadError   ProbeCollectionState = "read-error"   // 日志读取失败。
	ProbeCollectionStateUnknown     ProbeCollectionState = "unknown"      // 采集状态未确认。
)

// ProbeCollection 描述日志采集窗口的结束状态；该窗口不代表全部内部流量。
type ProbeCollection struct {
	State       ProbeCollectionState `json:"state"`
	CompletedAt string               `json:"completedAt,omitempty"`
}

// ProbeAttemptRelationState 表示同一请求尝试中观测跳点的关联状态。
type ProbeAttemptRelationState string

const (
	ProbeAttemptRelationStateLinked      ProbeAttemptRelationState = "linked"       // 已确认尝试关系，包括单条有序访问记录。
	ProbeAttemptRelationStateUnconfirmed ProbeAttemptRelationState = "unconfirmed"  // 运行时身份或日志顺序不足，尝试关系未确认。
	ProbeAttemptRelationStateMissingNext ProbeAttemptRelationState = "missing-next" // 已发生内部重定向，后续尝试记录缺失。
	ProbeAttemptRelationStateAmbiguous   ProbeAttemptRelationState = "ambiguous"    // 存在多个终止候选或重入记录，尝试归属有歧义。
)

// ProbeAttemptLink 表示同一请求尝试中两个观测跳点之间的关联。
type ProbeAttemptLink struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// AIRoutingSummary 提取访问日志中的模型路由信息，附在对应的观测跳点上。
type AIRoutingSummary struct {
	Provider      string `json:"provider,omitempty"`
	RequestModel  string `json:"requestModel,omitempty"`
	UpstreamModel string `json:"upstreamModel,omitempty"`
	ResponseModel string `json:"responseModel,omitempty"`
}

// ObservedHop 表示从网关访问日志观察到的一跳，供探测链路和证据关联使用。
type ObservedHop struct {
	ContextID                 string                `json:"contextID"`
	ID                        string                `json:"id,omitempty"`
	RuntimeSource             string                `json:"runtimeSource,omitempty"`
	LogSourceID               string                `json:"logSourceID,omitempty"`
	LogSequence               int                   `json:"logSequence,omitempty"`
	RequestStartTime          string                `json:"requestStartTime,omitempty"`
	InternalRedirect          bool                  `json:"internalRedirect,omitempty"`
	AIRouting                 *AIRoutingSummary     `json:"aiRouting,omitempty"`
	ObservedAt                string                `json:"observedAt"`
	ClusterID                 string                `json:"clusterID"`
	Pod                       string                `json:"pod"`
	Authority                 string                `json:"authority,omitempty"`
	Method                    string                `json:"method,omitempty"`
	Path                      string                `json:"path,omitempty"`
	Protocol                  string                `json:"protocol,omitempty"`
	RouteName                 string                `json:"routeName,omitempty"`
	UpstreamCluster           string                `json:"upstreamCluster,omitempty"`
	UpstreamHost              string                `json:"upstreamHost,omitempty"`
	UpstreamLocalAddress      string                `json:"upstreamLocalAddress,omitempty"`
	DownstreamRemoteAddress   string                `json:"downstreamRemoteAddress,omitempty"`
	DownstreamLocalAddress    string                `json:"downstreamLocalAddress,omitempty"`
	ResponseCode              int                   `json:"responseCode,omitempty"`
	ResponseFlags             string                `json:"responseFlags,omitempty"`
	ResponseCodeDetails       string                `json:"responseCodeDetails,omitempty"`
	DurationMillis            int64                 `json:"durationMillis,omitempty"`
	UpstreamServiceTimeMillis int64                 `json:"upstreamServiceTimeMillis,omitempty"`
	UpstreamTransportFailure  string                `json:"upstreamTransportFailureReason,omitempty"`
	AILog                     string                `json:"aiLog,omitempty"`
	ExtProcs                  []ExtProcObservation  `json:"extProcs,omitempty"`
	EvidenceSource            string                `json:"evidenceSource"`
	Correlation               ProbeCorrelation      `json:"correlation,omitempty"`
	Confidence                ObservationConfidence `json:"confidence"`
}

// ExtProcObservation 仅保留 Envoy ext_proc 过滤器状态中允许展示的逐请求字段，
// 供探测跳点展示处理器结果；不接收任意类型元数据，以免泄露请求或模型载荷。
type ExtProcObservation struct {
	Processor                 string         `json:"processor,omitempty"`
	RuleID                    string         `json:"ruleID,omitempty"`
	SelectedPool              string         `json:"selectedPool,omitempty"`
	SelectedEndpoint          string         `json:"selectedEndpoint,omitempty"`
	ReasonCode                string         `json:"reasonCode,omitempty"`
	RequestHeaderCalls        int            `json:"requestHeaderCalls,omitempty"`
	RequestBodyCalls          int            `json:"requestBodyCalls,omitempty"`
	ResponseHeaderCalls       int            `json:"responseHeaderCalls,omitempty"`
	ResponseBodyCalls         int            `json:"responseBodyCalls,omitempty"`
	RequestHeaderLatencyUS    int64          `json:"requestHeaderLatencyUs,omitempty"`
	RequestBodyLatencyUS      int64          `json:"requestBodyLatencyUs,omitempty"`
	ResponseHeaderLatencyUS   int64          `json:"responseHeaderLatencyUs,omitempty"`
	ResponseBodyLatencyUS     int64          `json:"responseBodyLatencyUs,omitempty"`
	GRPCStatus                string         `json:"grpcStatus,omitempty"`
	FailureModeAllowed        bool           `json:"failureModeAllowed,omitempty"`
	FailedOpen                bool           `json:"failedOpen,omitempty"`
	MessageTimeout            bool           `json:"messageTimeout,omitempty"`
	HTTPError                 bool           `json:"httpError,omitempty"`
	ReceivedImmediateResponse bool           `json:"receivedImmediateResponse,omitempty"`
	Invoked                   bool           `json:"invoked"`
	Outcome                   ExtProcOutcome `json:"outcome"`
}

// AgentCommand 是服务端下发给 Agent 的命令，用于查询配置或执行、观测探测。
type AgentCommand struct {
	ID                      string           `json:"id"`
	ClusterID               string           `json:"clusterID"`
	Kind                    AgentCommandKind `json:"kind"`
	GatewayID               string           `json:"gatewayID"`
	Deadline                string           `json:"deadline"`
	ExecutionTimeoutSeconds int              `json:"executionTimeoutSeconds,omitempty"`
	Probe                   *ProbeCommand    `json:"probe,omitempty"`
}

// ProbeCommand 携带 Agent 执行或观测主动探测所需的请求参数和关联标识。
type ProbeCommand struct {
	ProbeID     string          `json:"probeID"`
	TraceID     string          `json:"traceID"`
	GatewayID   string          `json:"gatewayID"`
	EntryID     string          `json:"entryID,omitempty"`
	Method      ProbeHTTPMethod `json:"method"`
	Path        string          `json:"path,omitempty"`
	Host        string          `json:"host,omitempty"`
	APIKey      string          `json:"apiKey,omitempty"`
	ContentType string          `json:"contentType,omitempty"`
	Body        string          `json:"body,omitempty"`
	StartedAt   string          `json:"startedAt,omitempty"`
}

// AgentCommandResult 携带 Agent 返回的配置、探测结果或错误，供服务端完成命令。
type AgentCommandResult struct {
	CommandID string            `json:"commandID"`
	ClusterID string            `json:"clusterID"`
	Config    *EnvoyConfig      `json:"config,omitempty"`
	Probe     *ProbeAgentResult `json:"probe,omitempty"`
	Error     string            `json:"error,omitempty"`
}
