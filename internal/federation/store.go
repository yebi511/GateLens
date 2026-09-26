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
	"github.com/gatelens/gatelens/internal/observed"
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
	mutex              sync.RWMutex
	preferredClusterID string
	staleAfter         time.Duration
	now                func() time.Time
	snapshots          map[string]receivedSnapshot
	commandQueues      map[string]chan domain.AgentCommand
	pendingCommands    map[string]pendingCommand
	probes             map[string]domain.ProbeExecution
}

func NewStore(preferredClusterID string, staleAfter time.Duration) *Store {
	if staleAfter <= 0 {
		staleAfter = 2 * time.Minute
	}
	return &Store{
		preferredClusterID: preferredClusterID,
		staleAfter:         staleAfter,
		now:                time.Now,
		snapshots:          map[string]receivedSnapshot{},
		commandQueues:      map[string]chan domain.AgentCommand{},
		pendingCommands:    map[string]pendingCommand{},
		probes:             map[string]domain.ProbeExecution{},
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
		return domain.Context{Cluster: domain.Cluster{ID: s.preferredClusterID, Name: s.preferredClusterID}, Snapshot: domain.Snapshot{State: "waiting-for-agents"}}
	}
	selected := snapshots[0].payload
	for _, snapshot := range snapshots {
		if snapshot.payload.Cluster.ID == s.preferredClusterID {
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

const (
	agentCommandPollWait         = 10 * time.Second
	envoyCommandQueueWait        = 45 * time.Second
	envoyCommandExecutionTimeout = 35 * time.Second
	envoyCommandWait             = envoyCommandQueueWait + envoyCommandExecutionTimeout + 5*time.Second
	agentCommandQueueSize        = 32
	probeCommandQueueWait        = 30 * time.Second
	probeCommandExecutionTimeout = 40 * time.Second
	probeObserveQueueWait        = 12 * time.Second
	probeObserveExecutionTimeout = 8 * time.Second
)

type probeGatewayTarget struct {
	ClusterID            string
	GatewayID            string
	GlobalID             string
	GatewayName          string
	SnapshotID           string
	ObservedAt           string
	Transport            string
	Destination          string
	Declared             bool
	PredecessorGatewayID string
}

type probeGatewayInference struct {
	Basis      string
	Confidence string
}

func (s *Store) CreateProbe(ctx context.Context, request domain.ProbeRequest) (domain.ProbeExecution, error) {
	clusterID, gatewayID, entry, err := s.resolveProbeEntry(request.SourceCluster, request.GatewayID, request.EntryID)
	if err != nil {
		return domain.ProbeExecution{}, err
	}
	method := strings.ToUpper(strings.TrimSpace(request.Method))
	if method == "" {
		method = "GET"
	}
	switch method {
	case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS":
	default:
		return domain.ProbeExecution{}, fmt.Errorf("unsupported probe method %q", method)
	}
	if len(request.Body) > 64<<10 {
		return domain.ProbeExecution{}, fmt.Errorf("probe body exceeds 65536 bytes")
	}
	path, err := url.ParseRequestURI(strings.TrimSpace(request.Path))
	if err != nil || path.IsAbs() || path.Host != "" || !strings.HasPrefix(path.Path, "/") || path.Fragment != "" {
		return domain.ProbeExecution{}, fmt.Errorf("probe path must be an absolute HTTP path beginning with /")
	}
	if strings.ContainsAny(request.Host, "\r\n") {
		return domain.ProbeExecution{}, fmt.Errorf("probe host must not contain line breaks")
	}
	if len(request.APIKey) > 8<<10 {
		return domain.ProbeExecution{}, fmt.Errorf("probe API key exceeds 8192 bytes")
	}
	if strings.ContainsAny(request.APIKey, "\r\n") {
		return domain.ProbeExecution{}, fmt.Errorf("probe API key must not contain line breaks")
	}
	timeout := request.TimeoutSeconds
	if timeout <= 0 {
		timeout = 15
	}
	if timeout > 30 {
		return domain.ProbeExecution{}, fmt.Errorf("probe timeout must not exceed 30 seconds")
	}
	probeID, err := newCommandID()
	if err != nil {
		return domain.ProbeExecution{}, err
	}
	traceID, err := newCommandID()
	if err != nil {
		return domain.ProbeExecution{}, err
	}
	commandID, err := newCommandID()
	if err != nil {
		return domain.ProbeExecution{}, err
	}
	started := s.now().UTC()
	topology := s.Topology()
	sourceGatewayID := globalID(clusterID, gatewayID)
	targets := discoverProbeGatewayTargets(topology, sourceGatewayID)
	target := (&url.URL{Scheme: entry.Scheme, Host: fmt.Sprintf("%s:%d", entry.DNSName, entry.Port), Path: path.Path, RawPath: path.RawPath}).String()
	execution := domain.ProbeExecution{
		ID: probeID, TraceID: traceID, SourceCluster: clusterID,
		GatewayID: sourceGatewayID, Method: method, Target: target, State: "running",
		StartedAt: started.Format(time.RFC3339Nano), FederatedSnapshotID: topology.FederatedSnapshotID,
		SnapshotConsistency: topology.Consistency,
	}
	command := domain.AgentCommand{
		ID: commandID, ClusterID: clusterID, Kind: domain.AgentCommandProbeHTTP, GatewayID: gatewayID,
		Deadline:                started.Add(probeCommandQueueWait).Format(time.RFC3339Nano),
		ExecutionTimeoutSeconds: timeout + 5,
		Probe: &domain.ProbeCommand{ProbeID: probeID, TraceID: traceID, GatewayID: gatewayID,
			EntryID: entry.ID, Method: method, Path: request.Path, Host: request.Host, APIKey: request.APIKey, ContentType: request.ContentType, Body: request.Body,
			StartedAt: started.Format(time.RFC3339Nano)},
	}
	pending := pendingCommand{clusterID: clusterID, result: make(chan domain.AgentCommandResult, 1)}
	s.mutex.Lock()
	queue := s.commandQueues[clusterID]
	if queue == nil {
		queue = make(chan domain.AgentCommand, agentCommandQueueSize)
		s.commandQueues[clusterID] = queue
	}
	s.pendingCommands[commandID] = pending
	s.probes[probeID] = execution
	s.mutex.Unlock()
	defer func() {
		s.mutex.Lock()
		delete(s.pendingCommands, commandID)
		s.mutex.Unlock()
	}()
	select {
	case queue <- command:
	case <-ctx.Done():
		return s.failProbe(probeID, ctx.Err().Error()), ctx.Err()
	default:
		err := fmt.Errorf("cluster agent %q command queue is full", clusterID)
		return s.failProbe(probeID, err.Error()), err
	}
	timer := time.NewTimer(probeCommandQueueWait + probeCommandExecutionTimeout)
	defer timer.Stop()
	select {
	case result := <-pending.result:
		if result.Error != "" {
			execution = s.failProbe(probeID, result.Error)
			return execution, nil
		}
		if result.Probe == nil {
			execution = s.failProbe(probeID, "cluster agent returned an empty probe result")
			return execution, nil
		}
		execution = *result.Probe
		execution.GatewayID = sourceGatewayID
		execution.Target = target
		execution.StartedAt = started.Format(time.RFC3339Nano)
		execution.FederatedSnapshotID = topology.FederatedSnapshotID
		execution.SnapshotConsistency = topology.Consistency
		execution = s.completeFederatedProbe(ctx, execution, topology, targets)
		s.mutex.Lock()
		s.probes[probeID] = execution
		s.mutex.Unlock()
		return execution, nil
	case <-ctx.Done():
		return s.failProbe(probeID, ctx.Err().Error()), ctx.Err()
	case <-timer.C:
		execution = s.failProbe(probeID, "timed out waiting for cluster agent")
		return execution, nil
	}
}

func (s *Store) completeFederatedProbe(ctx context.Context, execution domain.ProbeExecution, topology domain.Topology, targets []probeGatewayTarget) domain.ProbeExecution {
	source := targetForGateway(topology, execution.GatewayID)
	sourceSegment := domain.ProbeSegment{
		Index: 1, ClusterID: execution.SourceCluster, GatewayID: execution.GatewayID,
		GatewayName: source.GatewayName, SnapshotID: source.SnapshotID, ObservedAt: source.ObservedAt,
		State: segmentState(execution.Hops, execution.Gaps), Evidence: segmentEvidence(execution.Hops),
		LogSource: execution.LogSource, Hops: append([]domain.ObservedHop(nil), execution.Hops...),
		Collection: execution.Collection,
		Gaps:       append([]string(nil), execution.Gaps...),
	}
	execution.Segments = []domain.ProbeSegment{sourceSegment}
	if len(targets) == 0 {
		execution.EvidenceComplete = len(execution.Hops) > 0
		return observed.EnrichProbe(execution)
	}

	type observedSegment struct {
		index   int
		segment domain.ProbeSegment
	}
	results := make(chan observedSegment, len(targets))
	var wait sync.WaitGroup
	for index, target := range targets {
		wait.Add(1)
		go func(index int, target probeGatewayTarget) {
			defer wait.Done()
			segment := s.observeProbeGateway(ctx, execution, target)
			segment.Index = index + 2
			results <- observedSegment{index: index, segment: segment}
		}(index, target)
	}
	wait.Wait()
	close(results)
	segments := make([]domain.ProbeSegment, len(targets))
	for result := range results {
		segments[result.index] = result.segment
	}
	observedGatewayIDs := map[string]bool{execution.GatewayID: len(sourceSegment.Hops) > 0}
	observedHops := append([]domain.ObservedHop(nil), sourceSegment.Hops...)
	for index, segment := range segments {
		if len(segment.Hops) == 0 {
			continue
		}
		observedGatewayIDs[targets[index].GlobalID] = true
		observedHops = append(observedHops, segment.Hops...)
	}
	inferences := inferProbeGatewayCandidates(topology, targets, observedHops, observedGatewayIDs)
	observationComplete := true
	for index, segment := range segments {
		inference, inferred := inferences[targets[index].GlobalID]
		if len(segment.Hops) == 0 && !inferred {
			continue
		}
		if len(segment.Hops) == 0 {
			segment.InferenceBasis = inference.Basis
			segment.InferenceConfidence = inference.Confidence
		}
		execution.Segments = append(execution.Segments, segment)
		execution.Hops = append(execution.Hops, segment.Hops...)
		if segment.Evidence != "observed" {
			observationComplete = false
		}
		for _, gap := range segment.Gaps {
			execution.Gaps = append(execution.Gaps, fmt.Sprintf("%s/%s: %s", segment.ClusterID, segment.GatewayName, gap))
		}
	}
	if len(execution.Segments) > 2 {
		sort.SliceStable(execution.Segments[1:], func(i, j int) bool {
			left := execution.Segments[i+1]
			right := execution.Segments[j+1]
			if len(left.Hops) == 0 || len(right.Hops) == 0 {
				return len(left.Hops) > len(right.Hops)
			}
			return left.Hops[0].ObservedAt < right.Hops[0].ObservedAt
		})
	}
	for index := range execution.Segments {
		execution.Segments[index].Index = index + 1
	}
	sort.SliceStable(execution.Hops, func(i, j int) bool { return execution.Hops[i].ObservedAt < execution.Hops[j].ObservedAt })
	execution.EvidenceComplete = observationComplete
	for _, segment := range execution.Segments {
		if segment.Evidence != "observed" {
			execution.EvidenceComplete = false
			break
		}
	}
	return observed.EnrichProbe(execution)
}

func inferProbeGatewayCandidates(topology domain.Topology, targets []probeGatewayTarget, hops []domain.ObservedHop, observedGatewayIDs map[string]bool) map[string]probeGatewayInference {
	return inferProbeGatewayCandidatesWithRules(topology, targets, hops, observedGatewayIDs, defaultProbeAssociationRules())
}

func gatewayTargetServiceKeys(target probeGatewayTarget, entries []domain.ProbeEntry, gateway domain.TopologyNode) map[string]bool {
	result := map[string]bool{}
	for _, entry := range entries {
		clusterID := entry.ClusterID
		if clusterID == "" {
			clusterID = target.ClusterID
		}
		if entry.Namespace != "" && entry.ServiceName != "" {
			result[clusterID+"::"+entry.Namespace+"/"+entry.ServiceName] = true
		}
	}
	for _, condition := range gateway.Conditions {
		if service, ok := strings.CutPrefix(condition, "Service="); ok && strings.Contains(service, "/") {
			result[target.ClusterID+"::"+service] = true
		}
	}
	return result
}

func gatewayTargetMatchesAddress(entries []domain.ProbeEntry, gateway domain.TopologyNode, host string) bool {
	for _, entry := range entries {
		for _, address := range entry.Addresses {
			if normalizedUpstreamHost(address) == host {
				return true
			}
		}
	}
	for _, condition := range gateway.Conditions {
		address, ok := strings.CutPrefix(condition, "Address=")
		if ok && normalizedUpstreamHost(address) == host {
			return true
		}
	}
	return false
}

func gatewayTargetMatchesCluster(entries []domain.ProbeEntry, gateway domain.TopologyNode, owned []domain.TopologyNode, upstreamCluster string) bool {
	tokens := upstreamClusterTokens(upstreamCluster)
	var aliases []string
	for _, entry := range entries {
		aliases = append(aliases, strings.ToLower(strings.TrimSuffix(entry.DNSName, ".")))
		for _, address := range entry.Addresses {
			aliases = append(aliases, normalizedUpstreamHost(address))
		}
		if entry.ServiceName != "" && entry.Namespace != "" {
			aliases = append(aliases,
				strings.ToLower(entry.ServiceName+"."+entry.Namespace+".svc"),
				strings.ToLower(entry.ServiceName+"."+entry.Namespace),
			)
		}
	}
	for _, node := range append([]domain.TopologyNode{gateway}, owned...) {
		for _, condition := range node.Conditions {
			for _, prefix := range []string{"ServiceDNS=", "Address=", "Hostname="} {
				value, ok := strings.CutPrefix(condition, prefix)
				if !ok {
					continue
				}
				alias := normalizedUpstreamHost(value)
				aliases = append(aliases, alias)
				if strings.HasSuffix(alias, ".svc.cluster.local") {
					aliases = append(aliases, strings.TrimSuffix(alias, ".cluster.local"))
				}
			}
		}
	}
	for _, token := range tokens {
		for _, alias := range aliases {
			customClusterDomain := strings.HasSuffix(alias, ".svc") && strings.HasPrefix(token, alias+".")
			if alias != "" && (token == alias || customClusterDomain) {
				return true
			}
		}
	}
	return false
}

func upstreamClusterTokens(upstreamCluster string) []string {
	raw := strings.FieldsFunc(strings.ToLower(upstreamCluster), func(r rune) bool {
		switch r {
		case '|', ':', ';', ',', '/', '(', ')', '[', ']', ' ', '\t', '\r', '\n':
			return true
		default:
			return false
		}
	})
	var result []string
	for _, token := range raw {
		if token = normalizeTarget(token); token != "" {
			result = append(result, token)
		}
	}
	return result
}

func normalizedUpstreamHost(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if parsed, err := url.Parse(value); err == nil && parsed.Hostname() != "" {
		return strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		return strings.ToLower(strings.TrimSuffix(strings.Trim(host, "[]"), "."))
	}
	return strings.ToLower(strings.TrimSuffix(strings.Trim(value, "[]"), "."))
}

func (s *Store) observeProbeGateway(ctx context.Context, execution domain.ProbeExecution, target probeGatewayTarget) domain.ProbeSegment {
	segment := domain.ProbeSegment{
		ClusterID: target.ClusterID, GatewayID: target.GlobalID, GatewayName: target.GatewayName,
		SnapshotID: target.SnapshotID, ObservedAt: target.ObservedAt, State: "collecting", Evidence: "inferred",
		Transport: target.Transport, Destination: target.Destination,
	}
	commandID, err := newCommandID()
	if err != nil {
		segment.State, segment.Gaps = "unavailable", []string{err.Error()}
		return segment
	}
	command := domain.AgentCommand{
		ID: commandID, ClusterID: target.ClusterID, Kind: domain.AgentCommandProbeObserve, GatewayID: target.GatewayID,
		Deadline:                s.now().Add(probeObserveQueueWait).UTC().Format(time.RFC3339Nano),
		ExecutionTimeoutSeconds: int(probeObserveExecutionTimeout / time.Second),
		Probe: &domain.ProbeCommand{ProbeID: execution.ID, TraceID: execution.TraceID,
			GatewayID: target.GatewayID, StartedAt: execution.StartedAt},
	}
	result, err := s.executeProbeCommand(ctx, command, probeObserveQueueWait+probeObserveExecutionTimeout+time.Second)
	if err != nil {
		segment.State, segment.Gaps = "unavailable", []string{err.Error()}
		return segment
	}
	segment.Hops, segment.LogSource, segment.Gaps = result.Hops, result.LogSource, result.Gaps
	segment.Collection = result.Collection
	segment.State, segment.Evidence = segmentState(segment.Hops, segment.Gaps), segmentEvidence(segment.Hops)
	return segment
}

func (s *Store) executeProbeCommand(ctx context.Context, command domain.AgentCommand, waitFor time.Duration) (domain.ProbeExecution, error) {
	pending := pendingCommand{clusterID: command.ClusterID, result: make(chan domain.AgentCommandResult, 1)}
	s.mutex.Lock()
	queue := s.commandQueues[command.ClusterID]
	if queue == nil {
		queue = make(chan domain.AgentCommand, agentCommandQueueSize)
		s.commandQueues[command.ClusterID] = queue
	}
	s.pendingCommands[command.ID] = pending
	s.mutex.Unlock()
	defer func() {
		s.mutex.Lock()
		delete(s.pendingCommands, command.ID)
		s.mutex.Unlock()
	}()
	select {
	case queue <- command:
	case <-ctx.Done():
		return domain.ProbeExecution{}, ctx.Err()
	default:
		return domain.ProbeExecution{}, fmt.Errorf("cluster agent %q command queue is full", command.ClusterID)
	}
	timer := time.NewTimer(waitFor)
	defer timer.Stop()
	select {
	case result := <-pending.result:
		if result.Error != "" {
			return domain.ProbeExecution{}, fmt.Errorf("cluster agent: %s", result.Error)
		}
		if result.Probe == nil {
			return domain.ProbeExecution{}, fmt.Errorf("cluster agent returned an empty probe result")
		}
		return *result.Probe, nil
	case <-ctx.Done():
		return domain.ProbeExecution{}, ctx.Err()
	case <-timer.C:
		return domain.ProbeExecution{}, fmt.Errorf("timed out waiting for cluster agent")
	}
}

func segmentState(hops []domain.ObservedHop, gaps []string) string {
	if len(hops) > 0 {
		return "observed"
	}
	if len(gaps) > 0 {
		return "missing"
	}
	return "inferred"
}

func segmentEvidence(hops []domain.ObservedHop) string {
	if len(hops) > 0 {
		return "observed"
	}
	return "inferred"
}

func (s *Store) GetProbe(id string) (domain.ProbeExecution, bool) {
	s.mutex.RLock()
	probe, ok := s.probes[strings.TrimSpace(id)]
	s.mutex.RUnlock()
	return probe, ok
}

func (s *Store) failProbe(id, message string) domain.ProbeExecution {
	s.mutex.Lock()
	probe := s.probes[id]
	probe.State = "failed"
	probe.Error = message
	probe.CompletedAt = s.now().UTC().Format(time.RFC3339Nano)
	s.probes[id] = probe
	s.mutex.Unlock()
	return probe
}

func (s *Store) resolveProbeEntry(clusterID, gatewayID, entryID string) (string, string, domain.ProbeEntry, error) {
	clusterID, gatewayID, entryID = strings.TrimSpace(clusterID), strings.TrimSpace(gatewayID), strings.TrimSpace(entryID)
	if clusterID == "" || gatewayID == "" || entryID == "" {
		return "", "", domain.ProbeEntry{}, fmt.Errorf("sourceCluster, gatewayID and entryID are required")
	}
	if owner, local, found := strings.Cut(gatewayID, "::"); found {
		if owner != clusterID {
			return "", "", domain.ProbeEntry{}, fmt.Errorf("gateway belongs to cluster %q, not %q", owner, clusterID)
		}
		gatewayID = local
	}
	if owner, local, found := strings.Cut(entryID, "::"); found {
		if owner != clusterID {
			return "", "", domain.ProbeEntry{}, fmt.Errorf("probe entry belongs to cluster %q, not %q", owner, clusterID)
		}
		entryID = local
	}
	s.mutex.RLock()
	received, ok := s.snapshots[clusterID]
	s.mutex.RUnlock()
	if !ok {
		return "", "", domain.ProbeEntry{}, fmt.Errorf("source cluster %q is not registered", clusterID)
	}
	if !snapshotHasGateway(received.payload, gatewayID) {
		return "", "", domain.ProbeEntry{}, fmt.Errorf("Gateway %q was not found in cluster %q", gatewayID, clusterID)
	}
	for _, entry := range received.payload.Topology.ProbeEntries {
		if entry.ID == entryID && entry.GatewayID == gatewayID {
			return clusterID, gatewayID, entry, nil
		}
	}
	return "", "", domain.ProbeEntry{}, fmt.Errorf("probe entry %q was not found for Gateway %q in cluster %q", entryID, gatewayID, clusterID)
}

// discoverProbeGatewayTargets observes every known Gateway except the source.
// Runtime filters can rewrite routing inputs, so declared edges are useful for
// display metadata but must not decide which Gateways are checked for evidence.
func discoverProbeGatewayTargets(topology domain.Topology, sourceGatewayID string) []probeGatewayTarget {
	nodes := make(map[string]domain.TopologyNode, len(topology.Nodes))
	for _, node := range topology.Nodes {
		nodes[node.ID] = node
	}
	declared := discoverDeclaredProbeGatewayTargets(topology, sourceGatewayID)
	result := make([]probeGatewayTarget, 0, len(declared))
	seen := map[string]bool{sourceGatewayID: true}
	for _, target := range declared {
		if !gatewayHasObservableRuntime(nodes[target.GlobalID]) {
			continue
		}
		result = append(result, target)
		seen[target.GlobalID] = true
	}

	snapshots := make(map[string]domain.Snapshot, len(topology.Clusters))
	for _, cluster := range topology.Clusters {
		snapshots[cluster.ID] = cluster.Snapshot
	}
	var gateways []domain.TopologyNode
	for _, node := range topology.Nodes {
		if gatewayHasObservableRuntime(node) && !seen[node.ID] {
			gateways = append(gateways, node)
		}
	}
	sort.SliceStable(gateways, func(i, j int) bool { return gateways[i].ID < gateways[j].ID })
	for _, gateway := range gateways {
		snapshot := snapshots[gateway.ClusterID]
		result = append(result, probeGatewayTarget{
			ClusterID: gateway.ClusterID, GatewayID: localID(gateway.ClusterID, gateway.ID), GlobalID: gateway.ID,
			GatewayName: gateway.Name, SnapshotID: snapshot.ID, ObservedAt: snapshot.ObservedAt,
		})
	}
	return result
}

func gatewayHasObservableRuntime(node domain.TopologyNode) bool {
	if node.Kind != "Gateway" {
		return false
	}
	for _, condition := range node.Conditions {
		if condition == "EnvoyConfig=available" {
			return true
		}
	}
	return false
}

func discoverDeclaredProbeGatewayTargets(topology domain.Topology, sourceGatewayID string) []probeGatewayTarget {
	nodes := make(map[string]domain.TopologyNode, len(topology.Nodes))
	forward := make(map[string][]domain.TopologyEdge)
	reverse := make(map[string][]domain.TopologyEdge)
	for _, node := range topology.Nodes {
		nodes[node.ID] = node
	}
	for _, edge := range topology.Edges {
		forward[edge.From] = append(forward[edge.From], edge)
		reverse[edge.To] = append(reverse[edge.To], edge)
	}
	for id := range forward {
		sort.SliceStable(forward[id], func(i, j int) bool {
			if forward[id][i].Relation != forward[id][j].Relation {
				return forward[id][i].Relation < forward[id][j].Relation
			}
			return forward[id][i].To < forward[id][j].To
		})
	}
	snapshots := make(map[string]domain.Snapshot, len(topology.Clusters))
	for _, cluster := range topology.Clusters {
		snapshots[cluster.ID] = cluster.Snapshot
	}

	queue := []string{sourceGatewayID}
	visited := map[string]bool{sourceGatewayID: true}
	seenGateways := map[string]bool{sourceGatewayID: true}
	var result []probeGatewayTarget
	for len(queue) > 0 && len(visited) <= 4096 {
		current := queue[0]
		queue = queue[1:]
		for _, edge := range forward[current] {
			if edge.Relation == "cross-cluster" {
				gatewayID := owningGateway(edge.To, nodes, reverse)
				predecessorGatewayID := owningGateway(edge.From, nodes, reverse)
				gateway := nodes[gatewayID]
				if gatewayID != "" && !seenGateways[gatewayID] {
					seenGateways[gatewayID] = true
					snapshot := snapshots[gateway.ClusterID]
					result = append(result, probeGatewayTarget{
						ClusterID: gateway.ClusterID, GatewayID: localID(gateway.ClusterID, gatewayID), GlobalID: gatewayID,
						GatewayName: gateway.Name, SnapshotID: snapshot.ID, ObservedAt: snapshot.ObservedAt,
						Transport: edge.Transport, Destination: edge.Destination,
						Declared: true, PredecessorGatewayID: predecessorGatewayID,
					})
					if !visited[gatewayID] {
						visited[gatewayID] = true
						queue = append(queue, gatewayID)
					}
				}
			}
			if !visited[edge.To] {
				visited[edge.To] = true
				queue = append(queue, edge.To)
			}
		}
	}
	return result
}

func owningGateway(nodeID string, nodes map[string]domain.TopologyNode, reverse map[string][]domain.TopologyEdge) string {
	if nodes[nodeID].Kind == "Gateway" {
		return nodeID
	}
	queue := []string{nodeID}
	visited := map[string]bool{nodeID: true}
	for len(queue) > 0 && len(visited) <= 128 {
		current := queue[0]
		queue = queue[1:]
		for _, edge := range reverse[current] {
			candidate := nodes[edge.From]
			if candidate.Kind == "Gateway" && candidate.ClusterID == nodes[nodeID].ClusterID {
				return candidate.ID
			}
			if candidate.ClusterID == nodes[nodeID].ClusterID && !visited[candidate.ID] {
				visited[candidate.ID] = true
				queue = append(queue, candidate.ID)
			}
		}
	}
	return ""
}

func targetForGateway(topology domain.Topology, gatewayID string) probeGatewayTarget {
	for _, node := range topology.Nodes {
		if node.ID != gatewayID {
			continue
		}
		target := probeGatewayTarget{ClusterID: node.ClusterID, GatewayID: localID(node.ClusterID, node.ID), GlobalID: node.ID, GatewayName: node.Name}
		for _, cluster := range topology.Clusters {
			if cluster.ID == node.ClusterID {
				target.SnapshotID, target.ObservedAt = cluster.Snapshot.ID, cluster.Snapshot.ObservedAt
				break
			}
		}
		return target
	}
	return probeGatewayTarget{GlobalID: gatewayID}
}

func localID(clusterID, id string) string {
	return strings.TrimPrefix(id, clusterID+"::")
}

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
		ID:                      commandID,
		ClusterID:               clusterID,
		Kind:                    domain.AgentCommandEnvoyConfig,
		GatewayID:               localGatewayID,
		Deadline:                s.now().Add(envoyCommandQueueWait).UTC().Format(time.RFC3339Nano),
		ExecutionTimeoutSeconds: int(envoyCommandExecutionTimeout / time.Second),
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
			if deadline, err := time.Parse(time.RFC3339Nano, command.Deadline); err == nil && s.now().After(deadline) {
				continue
			}
			s.mutex.RLock()
			pending, exists := s.pendingCommands[command.ID]
			s.mutex.RUnlock()
			if !exists || pending.clusterID != clusterID {
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
	if result.Config == nil && result.Probe == nil && strings.TrimSpace(result.Error) == "" {
		return fmt.Errorf("command result must contain config, probe or error")
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
		for _, entry := range payload.Topology.ProbeEntries {
			entry.ID = globalID(cluster.ID, entry.ID)
			entry.GatewayID = globalID(cluster.ID, entry.GatewayID)
			entry.ClusterID = cluster.ID
			result.ProbeEntries = append(result.ProbeEntries, entry)
		}
		for _, finding := range payload.Findings {
			finding.ID = cluster.ID + ":" + finding.ID
			finding.TargetID = globalID(cluster.ID, finding.TargetID)
			findings = append(findings, finding)
		}
		if cluster.ID == s.preferredClusterID || result.SnapshotID == "" {
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
	return discoverLinksWithRules(nodes, existing, defaultLinkRules())
}

func outboundKeys(node domain.TopologyNode) []string {
	if node.Kind != "TransitHop" && node.Kind != "Registry" && node.Kind != "Service" {
		return nil
	}
	keys := valuesWithPrefixes(node.Conditions, "Destination=", "Domain=", "ExternalName=")
	return normalized(keys)
}

func entryKeys(node domain.TopologyNode) []string {
	if node.Kind != "Gateway" && node.Kind != "Listener" && node.Kind != "Ingress" {
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
