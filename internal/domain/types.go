package domain

import "encoding/json"

type Status string

const (
	StatusHealthy Status = "healthy"
	StatusWarning Status = "warning"
	StatusError   Status = "error"
)

type Context struct {
	Cluster      Cluster  `json:"cluster"`
	Namespaces   []string `json:"namespaces"`
	Snapshot     Snapshot `json:"snapshot"`
	Capabilities []string `json:"adapterCapabilities"`
}

type Cluster struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

type Snapshot struct {
	ID         string `json:"id"`
	ObservedAt string `json:"observedAt"`
	State      string `json:"state"`
}

type Topology struct {
	SnapshotID          string            `json:"snapshotID"`
	FederatedSnapshotID string            `json:"federatedSnapshotID,omitempty"`
	ObservedAt          string            `json:"observedAt"`
	Consistency         string            `json:"consistency,omitempty"`
	Clusters            []TopologyCluster `json:"clusters,omitempty"`
	Nodes               []TopologyNode    `json:"nodes"`
	Edges               []TopologyEdge    `json:"edges"`
	ProbeEntries        []ProbeEntry      `json:"probeEntries,omitempty"`
	Truncated           bool              `json:"truncated"`
}

type ProbeEntry struct {
	ID          string   `json:"id"`
	GatewayID   string   `json:"gatewayID"`
	ClusterID   string   `json:"clusterID"`
	Namespace   string   `json:"namespace"`
	ServiceName string   `json:"serviceName"`
	DNSName     string   `json:"dnsName"`
	Port        int32    `json:"port"`
	Scheme      string   `json:"scheme"`
	Protocol    string   `json:"protocol"`
	DisplayName string   `json:"displayName"`
	Addresses   []string `json:"addresses,omitempty"`
}
type TopologyCluster struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Environment     string   `json:"environment,omitempty"`
	Version         string   `json:"version"`
	ConnectionState string   `json:"connectionState"`
	Namespaces      []string `json:"namespaces"`

	Snapshot Snapshot `json:"snapshot"`
}
type TopologyNode struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Kind          string   `json:"kind"`
	Namespace     string   `json:"namespace"`
	ClusterID     string   `json:"clusterID"`
	Status        Status   `json:"status"`
	StatusText    string   `json:"statusText"`
	Summary       string   `json:"summary"`
	Conditions    []string `json:"conditions"`
	Source        string   `json:"source"`
	WorkloadScope string   `json:"workloadScope,omitempty"`
}

type TopologyEdge struct {
	From        string `json:"from"`
	To          string `json:"to"`
	Relation    string `json:"relation"`
	Transport   string `json:"transport,omitempty"`
	Destination string `json:"destination,omitempty"`
	State       string `json:"state,omitempty"`
	Evidence    string `json:"evidence,omitempty"`
}

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

type EnvoyListener struct {
	ID           string             `json:"id"`
	Name         string             `json:"name"`
	Address      string             `json:"address"`
	Port         int                `json:"port"`
	Protocol     string             `json:"protocol"`
	Status       Status             `json:"status"`
	FilterChains []EnvoyFilterChain `json:"filterChains"`
}

type EnvoyFilterChain struct {
	Name        string            `json:"name"`
	Match       string            `json:"match"`
	Transport   string            `json:"transport"`
	HTTPFilters []EnvoyHTTPFilter `json:"httpFilters"`
	Routes      []EnvoyRoute      `json:"routes"`
}

type EnvoyHTTPFilter struct {
	Name          string `json:"name"`
	Type          string `json:"type"`
	Stage         string `json:"stage"`
	ConfigSummary string `json:"configSummary"`
	Terminal      bool   `json:"terminal"`
}

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

type EnvoyExtensionAttachment struct {
	ListenerID   string `json:"listenerID"`
	ListenerName string `json:"listenerName"`
	FilterChain  string `json:"filterChain"`
	FilterName   string `json:"filterName"`
	FilterType   string `json:"filterType"`
	Position     int    `json:"position"`
}

type EnvoyExtensionDependency struct {
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Relation string `json:"relation"`
	Evidence string `json:"evidence"`
	Resolved bool   `json:"resolved"`
}

type EnvoyRoute struct {
	Name             string                 `json:"name"`
	Match            string                 `json:"match"`
	Cluster          string                 `json:"cluster"`
	WeightedClusters []EnvoyWeightedCluster `json:"weightedClusters,omitempty"`
}

type EnvoyWeightedCluster struct {
	Name   string `json:"name"`
	Weight int    `json:"weight"`
}

type EnvoyCluster struct {
	Name           string          `json:"name"`
	Type           string          `json:"type"`
	Discovery      string          `json:"discovery"`
	ConnectTimeout string          `json:"connectTimeout"`
	Endpoints      []EnvoyEndpoint `json:"endpoints"`
}

type EnvoyEndpoint struct {
	Address string `json:"address"`
	Port    int    `json:"port"`
	Status  Status `json:"status"`
	Health  string `json:"health"`
	Weight  int    `json:"weight"`
}
type Finding struct {
	ID       string `json:"id"`
	Severity Status `json:"severity"`
	Title    string `json:"title"`
	Resource string `json:"resource"`
	Basis    string `json:"basis"`
	TargetID string `json:"targetID"`
}

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

type AgentSnapshot struct {
	Cluster   TopologyCluster `json:"cluster"`
	Context   Context         `json:"context"`
	Topology  Topology        `json:"topology"`
	Findings  []Finding       `json:"findings"`
	Resources []Resource      `json:"resources"`
	SentAt    string          `json:"sentAt"`
}

const (
	AgentCommandEnvoyConfig  = "envoy-config"
	AgentCommandProbeHTTP    = "probe-http"
	AgentCommandProbeObserve = "probe-observe"
)

type ProbeRequest struct {
	SourceCluster  string `json:"sourceCluster"`
	GatewayID      string `json:"gatewayID"`
	EntryID        string `json:"entryID"`
	Method         string `json:"method"`
	Path           string `json:"path"`
	Host           string `json:"host,omitempty"`
	APIKey         string `json:"apiKey,omitempty"`
	ContentType    string `json:"contentType,omitempty"`
	Body           string `json:"body,omitempty"`
	TimeoutSeconds int    `json:"timeoutSeconds,omitempty"`
}

type ProbeExecution struct {
	ID                  string         `json:"id"`
	TraceID             string         `json:"traceID"`
	SourceCluster       string         `json:"sourceCluster"`
	GatewayID           string         `json:"gatewayID"`
	Method              string         `json:"method"`
	Target              string         `json:"target"`
	State               string         `json:"state"`
	StartedAt           string         `json:"startedAt"`
	CompletedAt         string         `json:"completedAt,omitempty"`
	ResponseCode        int            `json:"responseCode,omitempty"`
	ResponseBytes       int64          `json:"responseBytes,omitempty"`
	DurationMillis      int64          `json:"durationMillis,omitempty"`
	LogSource           string         `json:"logSource"`
	Hops                []ObservedHop  `json:"hops"`
	Segments            []ProbeSegment `json:"segments,omitempty"`
	FederatedSnapshotID string         `json:"federatedSnapshotID,omitempty"`
	SnapshotConsistency string         `json:"snapshotConsistency,omitempty"`
	Gaps                []string       `json:"gaps"`
	Error               string         `json:"error,omitempty"`
	EvidenceComplete    bool           `json:"evidenceComplete"`
}

type ProbeSegment struct {
	Index               int           `json:"index"`
	ClusterID           string        `json:"clusterID"`
	GatewayID           string        `json:"gatewayID"`
	GatewayName         string        `json:"gatewayName,omitempty"`
	SnapshotID          string        `json:"snapshotID,omitempty"`
	ObservedAt          string        `json:"observedAt,omitempty"`
	State               string        `json:"state"`
	Evidence            string        `json:"evidence"`
	LogSource           string        `json:"logSource,omitempty"`
	Transport           string        `json:"transport,omitempty"`
	Destination         string        `json:"destination,omitempty"`
	InferenceBasis      string        `json:"inferenceBasis,omitempty"`
	InferenceConfidence string        `json:"inferenceConfidence,omitempty"`
	Hops                []ObservedHop `json:"hops"`
	Gaps                []string      `json:"gaps"`
}

type ObservedHop struct {
	ObservedAt                string `json:"observedAt"`
	ClusterID                 string `json:"clusterID"`
	Pod                       string `json:"pod"`
	Authority                 string `json:"authority,omitempty"`
	Method                    string `json:"method,omitempty"`
	Path                      string `json:"path,omitempty"`
	Protocol                  string `json:"protocol,omitempty"`
	RouteName                 string `json:"routeName,omitempty"`
	UpstreamCluster           string `json:"upstreamCluster,omitempty"`
	UpstreamHost              string `json:"upstreamHost,omitempty"`
	UpstreamLocalAddress      string `json:"upstreamLocalAddress,omitempty"`
	DownstreamRemoteAddress   string `json:"downstreamRemoteAddress,omitempty"`
	ResponseCode              int    `json:"responseCode,omitempty"`
	ResponseFlags             string `json:"responseFlags,omitempty"`
	ResponseCodeDetails       string `json:"responseCodeDetails,omitempty"`
	DurationMillis            int64  `json:"durationMillis,omitempty"`
	UpstreamServiceTimeMillis int64  `json:"upstreamServiceTimeMillis,omitempty"`
	UpstreamTransportFailure  string `json:"upstreamTransportFailureReason,omitempty"`
	AILog                     string `json:"aiLog,omitempty"`
	EvidenceSource            string `json:"evidenceSource"`
	Correlation               string `json:"correlation,omitempty"`
	Confidence                string `json:"confidence"`
}

type AgentCommand struct {
	ID                      string        `json:"id"`
	ClusterID               string        `json:"clusterID"`
	Kind                    string        `json:"kind"`
	GatewayID               string        `json:"gatewayID"`
	Deadline                string        `json:"deadline"`
	ExecutionTimeoutSeconds int           `json:"executionTimeoutSeconds,omitempty"`
	Probe                   *ProbeCommand `json:"probe,omitempty"`
}

type ProbeCommand struct {
	ProbeID     string `json:"probeID"`
	TraceID     string `json:"traceID"`
	GatewayID   string `json:"gatewayID"`
	EntryID     string `json:"entryID,omitempty"`
	Method      string `json:"method"`
	Path        string `json:"path,omitempty"`
	Host        string `json:"host,omitempty"`
	APIKey      string `json:"apiKey,omitempty"`
	ContentType string `json:"contentType,omitempty"`
	Body        string `json:"body,omitempty"`
	StartedAt   string `json:"startedAt,omitempty"`
}

type AgentCommandResult struct {
	CommandID string          `json:"commandID"`
	ClusterID string          `json:"clusterID"`
	Config    *EnvoyConfig    `json:"config,omitempty"`
	Probe     *ProbeExecution `json:"probe,omitempty"`
	Error     string          `json:"error,omitempty"`
}
