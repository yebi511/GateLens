package federation

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gatelens/gatelens/internal/domain"
)

type receivedSnapshot struct {
	payload    domain.AgentSnapshot
	receivedAt time.Time
}

type pendingCommand struct {
	clusterID string
	result    chan domain.AgentCommandResult
}

type Store struct {
	mutex           sync.RWMutex
	entryClusterID  string
	staleAfter      time.Duration
	now             func() time.Time
	snapshots       map[string]receivedSnapshot
	commandQueues   map[string]chan domain.AgentCommand
	pendingCommands map[string]pendingCommand
}

func NewStore(entryClusterID string, staleAfter time.Duration) *Store {
	if staleAfter <= 0 {
		staleAfter = 2 * time.Minute
	}
	return &Store{
		entryClusterID:  entryClusterID,
		staleAfter:      staleAfter,
		now:             time.Now,
		snapshots:       map[string]receivedSnapshot{},
		commandQueues:   map[string]chan domain.AgentCommand{},
		pendingCommands: map[string]pendingCommand{},
	}
}

func (s *Store) ReceiveSnapshot(_ context.Context, payload domain.AgentSnapshot) error {
	clusterID := strings.TrimSpace(payload.Cluster.ID)
	if clusterID == "" {
		clusterID = strings.TrimSpace(payload.Context.Cluster.ID)
	}
	if clusterID == "" {
		return fmt.Errorf("cluster.id is required")
	}
	payload.Cluster.ID = clusterID
	if payload.Cluster.Name == "" {
		payload.Cluster.Name = clusterID
	}
	if payload.Cluster.ConnectionState == "" {
		payload.Cluster.ConnectionState = "connected"
	}
	if payload.Context.Cluster.ID != "" && payload.Context.Cluster.ID != clusterID {
		return fmt.Errorf("context cluster %q does not match cluster %q", payload.Context.Cluster.ID, clusterID)
	}
	for index := range payload.Topology.Nodes {
		if payload.Topology.Nodes[index].ClusterID == "" {
			payload.Topology.Nodes[index].ClusterID = clusterID
		}
		if payload.Topology.Nodes[index].ClusterID != clusterID {
			return fmt.Errorf("node %q belongs to cluster %q", payload.Topology.Nodes[index].ID, payload.Topology.Nodes[index].ClusterID)
		}
	}
	if payload.Cluster.Snapshot.ID == "" {
		payload.Cluster.Snapshot = payload.Context.Snapshot
	}
	if payload.SentAt == "" {
		payload.SentAt = s.now().UTC().Format(time.RFC3339)
	}
	s.mutex.Lock()
	s.snapshots[clusterID] = receivedSnapshot{payload: payload, receivedAt: s.now()}
	s.mutex.Unlock()
	return nil
}

func (s *Store) Context() domain.Context {
	snapshots := s.snapshotList()
	if len(snapshots) == 0 {
		return domain.Context{Cluster: domain.Cluster{ID: s.entryClusterID, Name: s.entryClusterID}, Snapshot: domain.Snapshot{State: "waiting-for-agents"}}
	}
	selected := snapshots[0].payload
	for _, snapshot := range snapshots {
		if snapshot.payload.Cluster.ID == s.entryClusterID {
			selected = snapshot.payload
			break
		}
	}
	result := selected.Context
	result.Cluster = domain.Cluster{ID: selected.Cluster.ID, Name: selected.Cluster.Name, Version: selected.Cluster.Version}
	result.Snapshot = selected.Cluster.Snapshot
	return result
}

func (s *Store) Topology() domain.Topology {
	topology, _ := s.federated()
	return topology
}

func (s *Store) Findings() []domain.Finding {
	_, findings := s.federated()
	return findings
}

func (s *Store) Resources(query string) []domain.Resource {
	query = strings.ToLower(strings.TrimSpace(query))
	var result []domain.Resource
	for _, snapshot := range s.snapshotList() {
		clusterID := snapshot.payload.Cluster.ID
		for _, resource := range snapshot.payload.Resources {
			resource.ID = globalID(clusterID, resource.ID)
			if query == "" || strings.Contains(strings.ToLower(resource.Kind+" "+resource.Name+" "+resource.Namespace+" "+clusterID), query) {
				result = append(result, resource)
			}
		}
	}
	return result
}

func (s *Store) Explain(request domain.RouteExplanationRequest) domain.RouteExplanation {
	topology := s.Topology()
	return domain.RouteExplanation{SnapshotID: topology.FederatedSnapshotID, ObservedAt: topology.ObservedAt, Outcome: "Indeterminate", Confidence: "low", Summary: "Federated route simulation requires agent-side route rules; the topology remains available for configuration inspection."}
}

const (
	agentCommandPollWait  = 10 * time.Second
	envoyCommandDeadline  = 35 * time.Second
	envoyCommandWait      = 45 * time.Second
	agentCommandQueueSize = 32
)

func (s *Store) EnvoyConfig(ctx context.Context, gatewayID string) (domain.EnvoyConfig, error) {
	clusterID, localGatewayID, err := s.resolveGateway(gatewayID)
	if err != nil {
		return domain.EnvoyConfig{}, err
	}
	commandID, err := newCommandID()
	if err != nil {
		return domain.EnvoyConfig{}, fmt.Errorf("create Envoy command: %w", err)
	}

	command := domain.AgentCommand{
		ID:        commandID,
		ClusterID: clusterID,
		Kind:      domain.AgentCommandEnvoyConfig,
		GatewayID: localGatewayID,
		Deadline:  time.Now().Add(envoyCommandDeadline).UTC().Format(time.RFC3339Nano),
	}
	pending := pendingCommand{clusterID: clusterID, result: make(chan domain.AgentCommandResult, 1)}

	s.mutex.Lock()
	queue := s.commandQueues[clusterID]
	if queue == nil {
		queue = make(chan domain.AgentCommand, agentCommandQueueSize)
		s.commandQueues[clusterID] = queue
	}
	s.pendingCommands[commandID] = pending
	s.mutex.Unlock()
	defer func() {
		s.mutex.Lock()
		delete(s.pendingCommands, commandID)
		s.mutex.Unlock()
	}()

	select {
	case queue <- command:
	case <-ctx.Done():
		return domain.EnvoyConfig{}, ctx.Err()
	default:
		return domain.EnvoyConfig{}, fmt.Errorf("cluster agent %q command queue is full", clusterID)
	}

	timer := time.NewTimer(envoyCommandWait)
	defer timer.Stop()
	select {
	case result := <-pending.result:
		if result.Error != "" {
			return domain.EnvoyConfig{}, fmt.Errorf("cluster agent %q: %s", clusterID, result.Error)
		}
		if result.Config == nil {
			return domain.EnvoyConfig{}, fmt.Errorf("cluster agent %q returned an empty Envoy config", clusterID)
		}
		config := *result.Config
		config.GatewayID = gatewayID
		return config, nil
	case <-ctx.Done():
		return domain.EnvoyConfig{}, ctx.Err()
	case <-timer.C:
		return domain.EnvoyConfig{}, fmt.Errorf("timed out waiting for cluster agent %q", clusterID)
	}
}

func (s *Store) NextAgentCommand(ctx context.Context, clusterID string) (domain.AgentCommand, bool, error) {
	clusterID = strings.TrimSpace(clusterID)
	if clusterID == "" {
		return domain.AgentCommand{}, false, fmt.Errorf("clusterID is required")
	}

	s.mutex.Lock()
	if _, exists := s.snapshots[clusterID]; !exists {
		s.mutex.Unlock()
		return domain.AgentCommand{}, false, fmt.Errorf("cluster %q has not registered a snapshot", clusterID)
	}
	queue := s.commandQueues[clusterID]
	if queue == nil {
		queue = make(chan domain.AgentCommand, agentCommandQueueSize)
		s.commandQueues[clusterID] = queue
	}
	s.mutex.Unlock()

	timer := time.NewTimer(agentCommandPollWait)
	defer timer.Stop()
	for {
		select {
		case command := <-queue:
			if deadline, err := time.Parse(time.RFC3339Nano, command.Deadline); err == nil && time.Now().After(deadline) {
				continue
			}
			return command, true, nil
		case <-ctx.Done():
			return domain.AgentCommand{}, false, ctx.Err()
		case <-timer.C:
			return domain.AgentCommand{}, false, nil
		}
	}
}

func (s *Store) CompleteAgentCommand(ctx context.Context, result domain.AgentCommandResult) error {
	result.CommandID = strings.TrimSpace(result.CommandID)
	result.ClusterID = strings.TrimSpace(result.ClusterID)
	if result.CommandID == "" || result.ClusterID == "" {
		return fmt.Errorf("commandID and clusterID are required")
	}
	if result.Config == nil && strings.TrimSpace(result.Error) == "" {
		return fmt.Errorf("command result must contain config or error")
	}

	s.mutex.RLock()
	pending, exists := s.pendingCommands[result.CommandID]
	s.mutex.RUnlock()
	if !exists {
		return fmt.Errorf("command %q is no longer pending", result.CommandID)
	}
	if pending.clusterID != result.ClusterID {
		return fmt.Errorf("command %q belongs to cluster %q", result.CommandID, pending.clusterID)
	}

	select {
	case pending.result <- result:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		return fmt.Errorf("command %q already has a result", result.CommandID)
	}
}

func (s *Store) resolveGateway(gatewayID string) (string, string, error) {
	gatewayID = strings.TrimSpace(gatewayID)
	if gatewayID == "" {
		return "", "", fmt.Errorf("gatewayID is required")
	}

	s.mutex.RLock()
	defer s.mutex.RUnlock()
	if clusterID, localID, found := strings.Cut(gatewayID, "::"); found {
		received, exists := s.snapshots[clusterID]
		if !exists {
			return "", "", fmt.Errorf("owning cluster %q is not registered", clusterID)
		}
		if !snapshotHasGateway(received.payload, localID) {
			return "", "", fmt.Errorf("Gateway %q was not found in cluster %q", localID, clusterID)
		}
		return clusterID, localID, nil
	}

	var owner string
	for clusterID, received := range s.snapshots {
		if snapshotHasGateway(received.payload, gatewayID) {
			if owner != "" {
				return "", "", fmt.Errorf("Gateway %q exists in multiple clusters; use the federated gateway ID", gatewayID)
			}
			owner = clusterID
		}
	}
	if owner == "" {
		return "", "", fmt.Errorf("Gateway %q was not found", gatewayID)
	}
	return owner, gatewayID, nil
}

func snapshotHasGateway(snapshot domain.AgentSnapshot, gatewayID string) bool {
	for _, node := range snapshot.Topology.Nodes {
		if node.Kind == "Gateway" && node.ID == gatewayID {
			return true
		}
	}
	return false
}

func newCommandID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func (s *Store) snapshotList() []receivedSnapshot {
	s.mutex.RLock()
	result := make([]receivedSnapshot, 0, len(s.snapshots))
	for _, snapshot := range s.snapshots {
		result = append(result, snapshot)
	}
	s.mutex.RUnlock()
	sort.Slice(result, func(i, j int) bool { return result[i].payload.Cluster.ID < result[j].payload.Cluster.ID })
	return result
}

func (s *Store) federated() (domain.Topology, []domain.Finding) {
	snapshots := s.snapshotList()
	result := domain.Topology{}
	if len(snapshots) == 0 {
		result.Consistency = "waiting-for-agents"
		return result, nil
	}
	var findings []domain.Finding
	var snapshotKeys []string
	var earliest, latest time.Time
	stale := false
	for _, received := range snapshots {
		payload := received.payload
		cluster := payload.Cluster
		if s.now().Sub(received.receivedAt) > s.staleAfter {
			cluster.ConnectionState = "stale"
			stale = true
		}
		result.Clusters = append(result.Clusters, cluster)
		snapshotKeys = append(snapshotKeys, cluster.ID+":"+cluster.Snapshot.ID)
		observedAt, err := time.Parse(time.RFC3339, cluster.Snapshot.ObservedAt)
		if err == nil {
			if earliest.IsZero() || observedAt.Before(earliest) {
				earliest = observedAt
			}
			if latest.IsZero() || observedAt.After(latest) {
				latest = observedAt
			}
		}
		ids := map[string]string{}
		for _, node := range payload.Topology.Nodes {
			ids[node.ID] = globalID(cluster.ID, node.ID)
			node.ID = ids[node.ID]
			result.Nodes = append(result.Nodes, node)
		}
		for _, edge := range payload.Topology.Edges {
			edge.From = globalID(cluster.ID, edge.From)
			edge.To = globalID(cluster.ID, edge.To)
			result.Edges = append(result.Edges, edge)
		}
		for _, finding := range payload.Findings {
			finding.ID = cluster.ID + ":" + finding.ID
			finding.TargetID = globalID(cluster.ID, finding.TargetID)
			findings = append(findings, finding)
		}
		if cluster.ID == s.entryClusterID || result.SnapshotID == "" {
			result.SnapshotID = cluster.Snapshot.ID
		}
	}
	links, linkFindings := discoverLinks(result.Nodes, result.Edges)
	result.Edges = append(result.Edges, links...)
	findings = append(findings, linkFindings...)
	result.ObservedAt = latest.Format(time.RFC3339)
	result.Consistency = "consistent-window"
	if stale {
		result.Consistency = "remote-unavailable"
	} else if !earliest.IsZero() && latest.Sub(earliest) > time.Minute {
		result.Consistency = "time-skew"
	}
	hash := sha256.Sum256([]byte(strings.Join(snapshotKeys, "|")))
	result.FederatedSnapshotID = fmt.Sprintf("federated-%x", hash[:8])
	return result, findings
}

type linkCandidate struct {
	node     domain.TopologyNode
	keys     []string
	rank     int
	evidence string
}

func discoverLinks(nodes []domain.TopologyNode, existing []domain.TopologyEdge) ([]domain.TopologyEdge, []domain.Finding) {
	var outbound, entries []linkCandidate
	for _, node := range nodes {
		if keys := outboundKeys(node); len(keys) > 0 {
			outbound = append(outbound, linkCandidate{node: node, keys: keys})
		}
		if keys := entryKeys(node); len(keys) > 0 {
			rank := 1
			if node.Kind == "Listener" {
				rank = 0
			}
			entries = append(entries, linkCandidate{node: node, keys: keys, rank: rank, evidence: entryEvidence(node)})
		}
	}
	var links []domain.TopologyEdge
	var findings []domain.Finding
	for _, from := range outbound {
		bestRank := 99
		var matches []linkCandidate
		matchedKey := ""
		for _, to := range entries {
			if from.node.ClusterID == to.node.ClusterID || edgeExists(existing, from.node.ID, to.node.ID) {
				continue
			}
			if key, ok := intersect(from.keys, to.keys); ok {
				if to.rank < bestRank {
					bestRank, matches, matchedKey = to.rank, []linkCandidate{to}, key
				} else if to.rank == bestRank {
					matches = append(matches, to)
				}
			}
		}
		if len(matches) == 1 {
			to := matches[0].node
			links = append(links, domain.TopologyEdge{From: from.node.ID, To: to.ID, Relation: "cross-cluster", Transport: transportOf(from.node), Destination: to.ClusterID + "/" + to.Namespace + "/" + to.Name, State: "resolved", Evidence: "auto-discovered from configuration: target " + matchedKey + " matched " + matches[0].evidence})
		} else if len(matches) > 1 {
			findings = append(findings, domain.Finding{ID: "ambiguous-cluster-link:" + from.node.ID, Severity: domain.StatusWarning, Title: "Cross-cluster target is ambiguous", Resource: from.node.ClusterID + "/" + from.node.Name, Basis: fmt.Sprintf("configuration target matched %d remote entries", len(matches)), TargetID: from.node.ID})
		}
	}
	return links, findings
}

func outboundKeys(node domain.TopologyNode) []string {
	if node.Kind != "TransitHop" && node.Kind != "Registry" && node.Kind != "Service" {
		return nil
	}
	keys := valuesWithPrefixes(node.Conditions, "Destination=", "Domain=", "ExternalName=")
	return normalized(keys)
}

func entryKeys(node domain.TopologyNode) []string {
	if node.Kind != "Gateway" && node.Kind != "Listener" {
		return nil
	}
	keys := valuesWithPrefixes(node.Conditions, "Address=", "Hostname=")
	return normalized(keys)
}

func entryEvidence(node domain.TopologyNode) string {
	location := node.ClusterID + "/"
	if node.Namespace != "" {
		location += node.Namespace + "/"
	}
	location += node.Name
	if node.Source == "" {
		return node.Kind + " " + location
	}
	return node.Kind + " " + location + " (" + node.Source + ")"
}
func valuesWithPrefixes(values []string, prefixes ...string) []string {
	var result []string
	for _, value := range values {
		for _, prefix := range prefixes {
			if strings.HasPrefix(value, prefix) {
				result = append(result, strings.TrimPrefix(value, prefix))
			}
		}
	}
	return result
}

func normalized(values []string) []string {
	seen := map[string]bool{}
	var result []string
	for _, value := range values {
		value = normalizeTarget(value)
		if value != "" && value != "*" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func normalizeTarget(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if strings.Contains(value, "://") {
		if parsed, err := url.Parse(value); err == nil && parsed.Host != "" {
			value = parsed.Host
		}
	}
	value = strings.TrimSuffix(strings.Split(value, "/")[0], ".")
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	} else if strings.Count(value, ":") == 1 {
		value = strings.Split(value, ":")[0]
	}
	return value
}

func transportOf(node domain.TopologyNode) string {
	values := valuesWithPrefixes(node.Conditions, "Transport=", "Protocol=")
	if len(values) > 0 {
		return values[0]
	}
	return "unknown"
}

func intersect(left, right []string) (string, bool) {
	set := map[string]bool{}
	for _, value := range left {
		set[value] = true
	}
	for _, value := range right {
		if set[value] {
			return value, true
		}
	}
	return "", false
}

func edgeExists(edges []domain.TopologyEdge, from, to string) bool {
	for _, edge := range edges {
		if edge.From == from && edge.To == to {
			return true
		}
	}
	return false
}

func globalID(clusterID, id string) string {
	if id == "" || strings.HasPrefix(id, clusterID+"::") {
		return id
	}
	return clusterID + "::" + id
}
