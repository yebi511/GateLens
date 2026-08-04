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
	Truncated           bool              `json:"truncated"`
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

type RouteExplanationRequest struct {
	SnapshotID string `json:"snapshotID"`
	Gateway    string `json:"gateway"`
	Listener   string `json:"listener"`
	Method     string `json:"method"`
	Host       string `json:"host"`
	Path       string `json:"path"`
	Namespace  string `json:"namespace"`
	Model      string `json:"model"`
}

type RouteExplanation struct {
	SnapshotID string        `json:"snapshotID"`
	ObservedAt string        `json:"observedAt"`
	Outcome    string        `json:"outcome"`
	Confidence string        `json:"confidence"`
	Summary    string        `json:"summary"`
	Steps      []ExplainStep `json:"steps"`
}

type ExplainStep struct {
	Hop      int    `json:"hop"`
	Title    string `json:"title"`
	Detail   string `json:"detail"`
	State    string `json:"state"`
	TargetID string `json:"targetID"`
}

type AgentSnapshot struct {
	Cluster   TopologyCluster `json:"cluster"`
	Context   Context         `json:"context"`
	Topology  Topology        `json:"topology"`
	Findings  []Finding       `json:"findings"`
	Resources []Resource      `json:"resources"`
	SentAt    string          `json:"sentAt"`
}

const AgentCommandEnvoyConfig = "envoy-config"

type AgentCommand struct {
	ID        string `json:"id"`
	ClusterID string `json:"clusterID"`
	Kind      string `json:"kind"`
	GatewayID string `json:"gatewayID"`
	Deadline  string `json:"deadline"`
}

type AgentCommandResult struct {
	CommandID string       `json:"commandID"`
	ClusterID string       `json:"clusterID"`
	Config    *EnvoyConfig `json:"config,omitempty"`
	Error     string       `json:"error,omitempty"`
}
