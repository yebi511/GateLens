package federation

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gatelens/gatelens/internal/domain"
)

type staticProbeAssociationRule struct {
	ruleName string
	results  []probeAssociationRuleResult
}

func (rule staticProbeAssociationRule) name() string { return rule.ruleName }

func (rule staticProbeAssociationRule) infer(probeAssociationContext) []probeAssociationRuleResult {
	return rule.results
}

func TestStoreDiscoversUniqueConfigurationLink(t *testing.T) {
	now := time.Date(2026, 7, 28, 8, 0, 0, 0, time.UTC)
	store := NewStore("edge-prod", 2*time.Minute)
	store.now = func() time.Time { return now }

	mustReceive(t, store, snapshotFor("edge-prod", "edge-1", now, []domain.TopologyNode{{
		ID: "transit/inference", Name: "inference-upstream", Kind: "TransitHop",
		Namespace: "gateway-system", Conditions: []string{"Destination=inference-gw.example", "Transport=HTTPS"},
		Source: "v1 Service.spec.externalName",
	}}))
	mustReceive(t, store, snapshotFor("gpu-prod", "gpu-1", now, []domain.TopologyNode{{
		ID: "gateway/inference/listener/https", Name: "https", Kind: "Listener",
		Namespace: "inference", Conditions: []string{"Hostname=inference-gw.example", "Protocol=HTTPS"},
		Source: "Gateway.spec.listeners",
	}}))

	topology := store.Topology()
	if len(topology.Clusters) != 2 {
		t.Fatalf("clusters=%d, want 2", len(topology.Clusters))
	}
	var link *domain.TopologyEdge
	for index := range topology.Edges {
		if topology.Edges[index].Relation == "cross-cluster" {
			link = &topology.Edges[index]
		}
	}
	if link == nil {
		t.Fatalf("missing cross-cluster link: %#v", topology.Edges)
	}
	if link.From != "edge-prod::transit/inference" || link.To != "gpu-prod::gateway/inference/listener/https" {
		t.Fatalf("link=%#v", link)
	}
	if link.State != "resolved" || link.Transport != "HTTPS" || !strings.Contains(link.Evidence, "Gateway.spec.listeners") {
		t.Fatalf("link evidence=%#v", link)
	}
	if topology.FederatedSnapshotID == "" || topology.Consistency != "consistent-window" {
		t.Fatalf("snapshot=%#v", topology)
	}
}

func TestDiscoverLinksDoesNotGuessAmbiguousEntry(t *testing.T) {
	nodes := []domain.TopologyNode{
		{ID: "edge::transit", ClusterID: "edge", Kind: "TransitHop", Name: "upstream", Conditions: []string{"Destination=shared.example"}},
		{ID: "gpu-a::listener", ClusterID: "gpu-a", Kind: "Listener", Name: "https", Conditions: []string{"Hostname=shared.example"}},
		{ID: "gpu-b::listener", ClusterID: "gpu-b", Kind: "Listener", Name: "https", Conditions: []string{"Hostname=shared.example"}},
	}
	links, findings := discoverLinks(nodes, nil)
	if len(links) != 0 {
		t.Fatalf("links=%#v, want none", links)
	}
	if len(findings) != 1 || findings[0].Severity != domain.StatusWarning {
		t.Fatalf("findings=%#v", findings)
	}
}

func TestDiscoverLinksMatchesSelectedHigressMCPBridgeRegistryToGateway(t *testing.T) {
	nodes := []domain.TopologyNode{
		{
			ID: "edge::ingress/higress-system/model-route", ClusterID: "edge", Kind: "Ingress", Name: "model-route", Namespace: "higress-system",
			Conditions: []string{"higress.io/destination=inference.dns", "higress.io/backend-protocol=HTTPS"},
			Source:     "networking.k8s.io/v1 Ingress",
		},
		{
			ID: "edge::mcpbridge/higress-system/default/registry/inference", ClusterID: "edge", Kind: "Registry", Name: "inference", Namespace: "higress-system",
			Conditions: []string{"Type=dns", "Domain=https://inference-gw.example:8443", "Port=8443"},
			Source:     "McpBridge.spec.registries",
		},
		{
			ID: "gpu::gateway/istio-system/inference-gateway", ClusterID: "gpu", Kind: "Gateway", Name: "inference-gateway", Namespace: "istio-system",
			Conditions: []string{"Address=inference-gw.example"}, Source: "gateway.networking.k8s.io/v1 Gateway.status.addresses",
		},
	}
	existing := []domain.TopologyEdge{{
		From: "edge::ingress/higress-system/model-route",
		To:   "edge::mcpbridge/higress-system/default/registry/inference", Relation: "selects",
	}}

	links, findings := discoverLinks(nodes, existing)
	if len(findings) != 0 || len(links) != 1 {
		t.Fatalf("links=%#v findings=%#v", links, findings)
	}
	link := links[0]
	if link.From != nodes[1].ID || link.To != nodes[2].ID || link.Transport != "HTTPS" {
		t.Fatalf("link=%#v", link)
	}
	if !strings.Contains(link.Evidence, "higress-mcpbridge") || !strings.Contains(link.Evidence, "edge/higress-system/model-route") {
		t.Fatalf("evidence=%q", link.Evidence)
	}
}

func TestDiscoverLinksMatchesDeclaredMCPBridgeRegistryToIngress(t *testing.T) {
	nodes := []domain.TopologyNode{
		{
			ID: "edge::mcpbridge/higress-system/default/registry/inference", ClusterID: "edge", Kind: "Registry", Name: "inference", Namespace: "higress-system",
			Conditions: []string{"Type=dns", "Domain=inference-gateway.infra-prd.sail-cloud.com", "Port=80", "Protocol=http"}, Source: "McpBridge.spec.registries",
		},
		{
			ID: "gpu::ingress/inference/inference-gateway-ingress", ClusterID: "gpu", Kind: "Ingress", Name: "inference-gateway-ingress", Namespace: "inference",
			Conditions: []string{"IngressClass=nginx", "Hostname=inference-gateway.infra-prd.sail-cloud.com"}, Source: "networking.k8s.io/v1 Ingress",
		},
	}

	links, findings := discoverLinks(nodes, nil)
	if len(links) != 1 || len(findings) != 0 {
		t.Fatalf("links=%#v findings=%#v", links, findings)
	}
	if links[0].From != nodes[0].ID || links[0].To != nodes[1].ID || links[0].Transport != "http" {
		t.Fatalf("link=%#v", links[0])
	}
	if !strings.Contains(links[0].Evidence, "declared upstream") || !strings.Contains(links[0].Evidence, "inference-gateway-ingress") {
		t.Fatalf("evidence=%q", links[0].Evidence)
	}
}

func TestStoreReportsTimeSkewAndStaleAgents(t *testing.T) {
	now := time.Date(2026, 7, 28, 8, 0, 0, 0, time.UTC)
	store := NewStore("edge", 2*time.Minute)
	store.now = func() time.Time { return now }
	mustReceive(t, store, snapshotFor("edge", "edge-1", now, nil))
	mustReceive(t, store, snapshotFor("gpu", "gpu-1", now.Add(-2*time.Minute), nil))

	if got := store.Topology().Consistency; got != "time-skew" {
		t.Fatalf("consistency=%q, want time-skew", got)
	}
	now = now.Add(3 * time.Minute)
	topology := store.Topology()
	if topology.Consistency != "remote-unavailable" {
		t.Fatalf("consistency=%q, want remote-unavailable", topology.Consistency)
	}
	for _, cluster := range topology.Clusters {
		if cluster.ConnectionState != "stale" {
			t.Fatalf("cluster=%#v, want stale", cluster)
		}
	}
}

func snapshotFor(clusterID, snapshotID string, observedAt time.Time, nodes []domain.TopologyNode) domain.AgentSnapshot {
	snapshot := domain.Snapshot{ID: snapshotID, ObservedAt: observedAt.Format(time.RFC3339), State: "complete"}
	return domain.AgentSnapshot{
		Cluster:  domain.TopologyCluster{ID: clusterID, Name: clusterID, Snapshot: snapshot},
		Context:  domain.Context{Cluster: domain.Cluster{ID: clusterID, Name: clusterID}, Snapshot: snapshot},
		Topology: domain.Topology{SnapshotID: snapshotID, ObservedAt: snapshot.ObservedAt, Nodes: nodes},
		SentAt:   observedAt.Format(time.RFC3339),
	}
}

func mustReceive(t *testing.T, store *Store, payload domain.AgentSnapshot) {
	t.Helper()
	if err := store.ReceiveSnapshot(context.Background(), payload); err != nil {
		t.Fatal(err)
	}
}
func TestDiscoverLinksRequiresExplicitConfigurationEvidence(t *testing.T) {
	nodes := []domain.TopologyNode{
		{ID: "edge::transit", ClusterID: "edge", Kind: "TransitHop", Name: "same-name"},
		{ID: "gpu::gateway", ClusterID: "gpu", Kind: "Gateway", Name: "same-name"},
	}
	links, findings := discoverLinks(nodes, nil)
	if len(links) != 0 || len(findings) != 0 {
		t.Fatalf("links=%#v findings=%#v", links, findings)
	}
}

func TestStoreRoutesEnvoyConfigThroughOwningAgent(t *testing.T) {
	now := time.Now().UTC()
	store := NewStore("federation", 2*time.Minute)
	mustReceive(t, store, snapshotFor("gpu-prod", "gpu-1", now, []domain.TopologyNode{{
		ID: "gateway/inference/inference-gateway", Name: "inference-gateway", Kind: "Gateway",
		Namespace: "inference", Conditions: []string{"EnvoyConfig=available"},
	}}))

	type configResult struct {
		config domain.EnvoyConfig
		err    error
	}
	resultCh := make(chan configResult, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() {
		config, err := store.EnvoyConfig(ctx, "gpu-prod::gateway/inference/inference-gateway")
		resultCh <- configResult{config: config, err: err}
	}()

	command, ok, err := store.NextAgentCommand(ctx, "gpu-prod")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected Envoy command")
	}
	if command.Kind != domain.AgentCommandEnvoyConfig || command.GatewayID != "gateway/inference/inference-gateway" {
		t.Fatalf("command=%#v", command)
	}

	config := domain.EnvoyConfig{SnapshotID: "envoy-1", State: "complete", Source: "Envoy admin /config_dump"}
	if err := store.CompleteAgentCommand(ctx, domain.AgentCommandResult{
		CommandID: command.ID,
		ClusterID: "gpu-prod",
		Config:    &config,
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case result := <-resultCh:
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.config.GatewayID != "gpu-prod::gateway/inference/inference-gateway" || result.config.SnapshotID != "envoy-1" {
			t.Fatalf("config=%#v", result.config)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestNextAgentCommandSkipsCanceledQueuedCommand(t *testing.T) {
	now := time.Now().UTC()
	store := NewStore("federation", 2*time.Minute)
	store.now = func() time.Time { return now }
	mustReceive(t, store, snapshotFor("gpu-prod", "gpu-1", now, nil))

	queue := make(chan domain.AgentCommand, 2)
	deadline := now.Add(time.Minute).Format(time.RFC3339Nano)
	queue <- domain.AgentCommand{ID: "canceled", ClusterID: "gpu-prod", Deadline: deadline}
	queue <- domain.AgentCommand{ID: "live", ClusterID: "gpu-prod", Deadline: deadline}
	store.commandQueues["gpu-prod"] = queue
	store.pendingCommands["live"] = pendingCommand{clusterID: "gpu-prod", result: make(chan domain.AgentCommandResult, 1)}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	command, ok, err := store.NextAgentCommand(ctx, "gpu-prod")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || command.ID != "live" {
		t.Fatalf("command=%#v ok=%v", command, ok)
	}
}

func TestStoreRejectsUnknownFederatedGateway(t *testing.T) {
	store := NewStore("federation", time.Minute)
	if _, err := store.EnvoyConfig(context.Background(), "missing::gateway/default/not-found"); err == nil {
		t.Fatal("expected unknown owning cluster error")
	}
}

func TestDiscoverProbeGatewayTargetsFollowsMultipleGateways(t *testing.T) {
	topology := domain.Topology{
		Clusters: []domain.TopologyCluster{
			{ID: "edge", Snapshot: domain.Snapshot{ID: "edge-1", ObservedAt: "2026-08-13T10:00:00Z"}},
			{ID: "regional", Snapshot: domain.Snapshot{ID: "regional-1", ObservedAt: "2026-08-13T10:00:02Z"}},
			{ID: "gpu", Snapshot: domain.Snapshot{ID: "gpu-1", ObservedAt: "2026-08-13T10:00:03Z"}},
		},
		Nodes: []domain.TopologyNode{
			{ID: "edge::gateway/edge", ClusterID: "edge", Kind: "Gateway", Name: "edge-gateway", Conditions: []string{"EnvoyConfig=available"}},
			{ID: "edge::route/edge", ClusterID: "edge", Kind: "HTTPRoute"},
			{ID: "edge::transit/regional", ClusterID: "edge", Kind: "TransitHop"},
			{ID: "regional::gateway/regional", ClusterID: "regional", Kind: "Gateway", Name: "regional-gateway", Conditions: []string{"EnvoyConfig=available"}},
			{ID: "regional::listener/regional", ClusterID: "regional", Kind: "Listener"},
			{ID: "regional::transit/gpu", ClusterID: "regional", Kind: "TransitHop"},
			{ID: "gpu::gateway/gpu", ClusterID: "gpu", Kind: "Gateway", Name: "gpu-gateway", Conditions: []string{"EnvoyConfig=available"}},
			{ID: "gpu::listener/gpu", ClusterID: "gpu", Kind: "Listener"},
		},
		Edges: []domain.TopologyEdge{
			{From: "edge::gateway/edge", To: "edge::route/edge", Relation: "owns"},
			{From: "edge::route/edge", To: "edge::transit/regional", Relation: "routes"},
			{From: "edge::transit/regional", To: "regional::listener/regional", Relation: "cross-cluster", Transport: "mTLS"},
			{From: "regional::gateway/regional", To: "regional::listener/regional", Relation: "owns"},
			{From: "regional::gateway/regional", To: "regional::transit/gpu", Relation: "routes"},
			{From: "regional::transit/gpu", To: "gpu::listener/gpu", Relation: "cross-cluster", Transport: "HTTPS"},
			{From: "gpu::gateway/gpu", To: "gpu::listener/gpu", Relation: "owns"},
		},
	}

	targets := discoverProbeGatewayTargets(topology, "edge::gateway/edge")
	if len(targets) != 2 {
		t.Fatalf("targets=%#v", targets)
	}
	if targets[0].GlobalID != "regional::gateway/regional" || targets[0].GatewayID != "gateway/regional" || targets[0].Transport != "mTLS" {
		t.Fatalf("first target=%#v", targets[0])
	}
	if targets[1].GlobalID != "gpu::gateway/gpu" || targets[1].SnapshotID != "gpu-1" || targets[1].Transport != "HTTPS" {
		t.Fatalf("second target=%#v", targets[1])
	}
}

func TestDiscoverProbeGatewayTargetsIncludesUnlinkedGatewayInSameCluster(t *testing.T) {
	topology := domain.Topology{
		Clusters: []domain.TopologyCluster{{ID: "edge", Snapshot: domain.Snapshot{ID: "edge-1", ObservedAt: "2026-08-13T10:00:00Z"}}},
		Nodes: []domain.TopologyNode{
			{ID: "edge::gateway/ingress", ClusterID: "edge", Kind: "Gateway", Name: "ingress", Conditions: []string{"EnvoyConfig=available"}},
			{ID: "edge::gateway/model-router", ClusterID: "edge", Kind: "Gateway", Name: "model-router", Conditions: []string{"EnvoyConfig=available"}},
		},
	}

	targets := discoverProbeGatewayTargets(topology, "edge::gateway/ingress")
	if len(targets) != 1 || targets[0].GlobalID != "edge::gateway/model-router" || targets[0].GatewayID != "gateway/model-router" {
		t.Fatalf("targets=%#v", targets)
	}
}

func TestDiscoverProbeGatewayTargetsExcludesGatewayWithoutRuntime(t *testing.T) {
	topology := domain.Topology{Nodes: []domain.TopologyNode{
		{ID: "edge::gateway/ingress", ClusterID: "edge", Kind: "Gateway", Conditions: []string{"EnvoyConfig=available"}},
		{ID: "edge::gateway/config-only", ClusterID: "edge", Kind: "Gateway"},
	}}
	if targets := discoverProbeGatewayTargets(topology, "edge::gateway/ingress"); len(targets) != 0 {
		t.Fatalf("targets=%#v", targets)
	}
}

func TestInferProbeGatewayCandidatesUsesObservedUpstreamEvidence(t *testing.T) {
	topology := domain.Topology{
		Nodes: []domain.TopologyNode{
			{ID: "gpu::gateway/gpu", Kind: "Gateway"},
			{ID: "regional::gateway/regional", Kind: "Gateway", Conditions: []string{"ServiceDNS=regional-gateway.gateway-system.svc.cluster.local"}},
			{ID: "other::gateway/other", Kind: "Gateway"},
		},
		ProbeEntries: []domain.ProbeEntry{
			{GatewayID: "gpu::gateway/gpu", Namespace: "higress-system", ServiceName: "gpu-gateway", DNSName: "gpu-gateway.higress-system.svc.cluster.local", Addresses: []string{"10.233.72.18"}},
			{GatewayID: "other::gateway/other", Namespace: "gateway-system", ServiceName: "other", DNSName: "other.gateway-system.svc.cluster.local", Addresses: []string{"10.233.90.30"}},
		},
	}
	targets := []probeGatewayTarget{
		{GlobalID: "gpu::gateway/gpu"},
		{GlobalID: "regional::gateway/regional"},
		{GlobalID: "other::gateway/other"},
	}
	hops := []domain.ObservedHop{
		{UpstreamHost: "10.233.72.18:8080"},
		{UpstreamCluster: "outbound|80||regional-gateway.gateway-system.svc.cluster.local"},
	}

	got := inferProbeGatewayCandidates(topology, targets, hops, map[string]bool{"edge::gateway/edge": true})
	if got["gpu::gateway/gpu"].Confidence != "high" || got["regional::gateway/regional"].Confidence != "medium" {
		t.Fatalf("inferences=%#v", got)
	}
	if _, found := got["other::gateway/other"]; found {
		t.Fatalf("unrelated gateway was inferred: %#v", got)
	}
}

func TestInferProbeGatewayCandidatesMatchesGatewayAddressAndListenerHostname(t *testing.T) {
	topology := domain.Topology{
		Nodes: []domain.TopologyNode{
			{ID: "edge::gateway/inference", ClusterID: "edge", Kind: "Gateway", Conditions: []string{"Address=llm-inference-providers.internal.dns"}},
			{ID: "edge::gateway/listener-only", ClusterID: "edge", Kind: "Gateway"},
			{ID: "edge::gateway/listener-only/listener/http", ClusterID: "edge", Kind: "Listener", Conditions: []string{"Hostname=listener.internal.dns"}},
		},
		Edges: []domain.TopologyEdge{{From: "edge::gateway/listener-only", To: "edge::gateway/listener-only/listener/http", Relation: "owns"}},
	}
	targets := []probeGatewayTarget{{GlobalID: "edge::gateway/inference"}, {GlobalID: "edge::gateway/listener-only"}}
	hops := []domain.ObservedHop{
		{UpstreamCluster: "outbound|80||llm-inference-providers.internal.dns"},
		{UpstreamCluster: "outbound|8080||listener.internal.dns"},
	}

	got := inferProbeGatewayCandidates(topology, targets, hops, nil)
	if got[targets[0].GlobalID].Confidence != "medium" || got[targets[1].GlobalID].Confidence != "medium" {
		t.Fatalf("inferences=%#v", got)
	}
}

func TestInferProbeGatewayCandidatesFollowsMcpBridgeRegistryToGatewayService(t *testing.T) {
	topology := domain.Topology{
		Nodes: []domain.TopologyNode{
			{
				ID: "edge::mcpbridge/higress-system/default/registry/llm", ClusterID: "edge",
				Kind: "Registry", Name: "llm-inference-providers.internal", Namespace: "higress-system",
				Conditions: []string{"Type=dns", "Domain=inference-gateway.istio-system.svc.cluster.local"},
			},
			{ID: "edge::service/istio-system/inference-gateway", ClusterID: "edge", Kind: "Service", Name: "inference-gateway", Namespace: "istio-system"},
			{
				ID: "edge::gateway/inference", ClusterID: "edge", Kind: "Gateway", Name: "inference-gateway",
				Conditions: []string{"Service=istio-system/inference-gateway", "EnvoyConfig=available"},
			},
		},
		Edges: []domain.TopologyEdge{{
			From: "edge::mcpbridge/higress-system/default/registry/llm", To: "edge::service/istio-system/inference-gateway", Relation: "resolves",
		}},
	}
	targets := []probeGatewayTarget{{ClusterID: "edge", GlobalID: "edge::gateway/inference"}}
	hops := []domain.ObservedHop{{UpstreamCluster: "outbound|80||llm-inference-providers.internal.dns"}}

	got := inferProbeGatewayCandidates(topology, targets, hops, nil)
	inference := got[targets[0].GlobalID]
	if inference.Confidence != "medium" || !strings.Contains(inference.Basis, "McpBridge registry") {
		t.Fatalf("inferences=%#v", got)
	}
}

func TestProbeAssociationRulesCanAddRelationshipWithoutChangingAggregator(t *testing.T) {
	target := probeGatewayTarget{GlobalID: "edge::gateway/future"}
	rules := []probeAssociationRule{staticProbeAssociationRule{
		ruleName: "future-backend-service-association",
		results: []probeAssociationRuleResult{{
			matches:    []probeAssociationMatch{{gatewayID: target.GlobalID, basis: "Backend resolved to the Gateway Service"}},
			confidence: "medium", strength: probeAssociationStrengthCluster,
		}},
	}}

	got := inferProbeGatewayCandidatesWithRules(domain.Topology{}, []probeGatewayTarget{target}, nil, nil, rules)
	if got[target.GlobalID].Confidence != "medium" || !strings.Contains(got[target.GlobalID].Basis, "Backend") {
		t.Fatalf("inferences=%#v", got)
	}
}

func TestProbeAssociationAggregatorCentralizesAmbiguityAndStrength(t *testing.T) {
	weak := staticProbeAssociationRule{
		ruleName: "shared-service",
		results: []probeAssociationRuleResult{{
			matches: []probeAssociationMatch{
				{gatewayID: "edge::gateway/a", basis: "weak a"},
				{gatewayID: "edge::gateway/b", basis: "weak b"},
			},
			confidence: "medium", strength: probeAssociationStrengthCluster,
			ambiguousBasis: "shared service matched %d gateways",
		}},
	}
	strong := staticProbeAssociationRule{
		ruleName: "endpoint-address",
		results: []probeAssociationRuleResult{{
			matches:    []probeAssociationMatch{{gatewayID: "edge::gateway/a", basis: "endpoint address matched gateway a"}},
			confidence: "high", strength: probeAssociationStrengthHost,
		}},
	}

	got := inferProbeGatewayCandidatesWithRules(domain.Topology{}, nil, nil, nil, []probeAssociationRule{weak, strong})
	if got["edge::gateway/a"].Confidence != "high" {
		t.Fatalf("strong evidence did not replace weak ambiguity: %#v", got)
	}
	if got["edge::gateway/b"].Confidence != "ambiguous" || !strings.Contains(got["edge::gateway/b"].Basis, "2 gateways") {
		t.Fatalf("ambiguity was not handled centrally: %#v", got)
	}
}

func TestInferProbeGatewayCandidatesMarksAddressAmbiguity(t *testing.T) {
	topology := domain.Topology{ProbeEntries: []domain.ProbeEntry{
		{GatewayID: "a::gateway/shared", Addresses: []string{"192.0.2.10"}},
		{GatewayID: "b::gateway/shared", Addresses: []string{"192.0.2.10"}},
	}}
	targets := []probeGatewayTarget{{GlobalID: "a::gateway/shared"}, {GlobalID: "b::gateway/shared"}}
	got := inferProbeGatewayCandidates(topology, targets, []domain.ObservedHop{{UpstreamHost: "192.0.2.10:443"}}, nil)
	if got[targets[0].GlobalID].Confidence != "ambiguous" || got[targets[1].GlobalID].Confidence != "ambiguous" {
		t.Fatalf("inferences=%#v", got)
	}
}

func TestInferProbeGatewayCandidatesDoesNotContinuePastMissingDeclaredHop(t *testing.T) {
	targets := []probeGatewayTarget{
		{GlobalID: "regional::gateway/regional", GatewayName: "regional", Declared: true, PredecessorGatewayID: "edge::gateway/edge"},
		{GlobalID: "gpu::gateway/gpu", GatewayName: "gpu", Declared: true, PredecessorGatewayID: "regional::gateway/regional"},
	}
	got := inferProbeGatewayCandidates(domain.Topology{}, targets, nil, map[string]bool{"edge::gateway/edge": true})
	if got[targets[0].GlobalID].Confidence != "low" {
		t.Fatalf("first configured candidate=%#v", got)
	}
	if _, found := got[targets[1].GlobalID]; found {
		t.Fatalf("inference continued past a missing gateway: %#v", got)
	}
}

func TestStoreProbeCollectsRemoteGatewayWithoutSendingAnotherRequest(t *testing.T) {
	now := time.Now().UTC()
	store := NewStore("federation", 2*time.Minute)
	mustReceive(t, store, domain.AgentSnapshot{
		Cluster: domain.TopologyCluster{ID: "edge", Name: "edge", Snapshot: domain.Snapshot{ID: "edge-1", ObservedAt: now.Format(time.RFC3339)}},
		Context: domain.Context{Cluster: domain.Cluster{ID: "edge"}},
		Topology: domain.Topology{Nodes: []domain.TopologyNode{
			{ID: "gateway/edge", Kind: "Gateway", Name: "edge-gateway"},
			{ID: "transit/gpu", Kind: "TransitHop", Conditions: []string{"Destination=gpu.example", "Transport=HTTPS"}},
		}, Edges: []domain.TopologyEdge{{From: "gateway/edge", To: "transit/gpu", Relation: "routes"}}, ProbeEntries: []domain.ProbeEntry{{ID: "gateway/edge/probe-entry/gateway/edge/80", GatewayID: "gateway/edge", ClusterID: "edge", Namespace: "gateway", ServiceName: "edge", DNSName: "edge.gateway.svc.cluster.local", Port: 80, Scheme: "http", Protocol: "http"}}},
	})
	mustReceive(t, store, domain.AgentSnapshot{
		Cluster: domain.TopologyCluster{ID: "gpu", Name: "gpu", Snapshot: domain.Snapshot{ID: "gpu-1", ObservedAt: now.Format(time.RFC3339)}},
		Context: domain.Context{Cluster: domain.Cluster{ID: "gpu"}},
		Topology: domain.Topology{Nodes: []domain.TopologyNode{
			{ID: "gateway/gpu", Kind: "Gateway", Name: "gpu-gateway", Conditions: []string{"EnvoyConfig=available"}},
			{ID: "listener/gpu", Kind: "Listener", Conditions: []string{"Hostname=gpu.example"}},
		}, Edges: []domain.TopologyEdge{{From: "gateway/gpu", To: "listener/gpu", Relation: "owns"}}},
	})

	type probeResult struct {
		probe domain.ProbeExecution
		err   error
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan probeResult, 1)
	go func() {
		probe, err := store.CreateProbe(ctx, domain.ProbeRequest{SourceCluster: "edge", GatewayID: "edge::gateway/edge", EntryID: "edge::gateway/edge/probe-entry/gateway/edge/80", Method: "POST", Path: "/v1/chat", APIKey: "secret-key", Body: `{}`})
		done <- probeResult{probe: probe, err: err}
	}()

	sourceCommand, ok, err := store.NextAgentCommand(ctx, "edge")
	if err != nil || !ok || sourceCommand.Kind != domain.AgentCommandProbeHTTP {
		t.Fatalf("source command=%#v ok=%v err=%v", sourceCommand, ok, err)
	}
	if sourceCommand.Probe == nil || sourceCommand.Probe.APIKey != "secret-key" {
		t.Fatalf("source command did not carry the transient API key: %#v", sourceCommand.Probe)
	}
	sourceResult := domain.ProbeExecution{
		ID: sourceCommand.Probe.ProbeID, TraceID: sourceCommand.Probe.TraceID,
		SourceCluster: "edge", State: "completed", LogSource: "edge-log",
		Hops: []domain.ObservedHop{{ObservedAt: now.Format(time.RFC3339Nano), ClusterID: "edge", RouteName: "to-gpu", Confidence: "observed"}}, EvidenceComplete: true,
	}
	if err := store.CompleteAgentCommand(ctx, domain.AgentCommandResult{CommandID: sourceCommand.ID, ClusterID: "edge", Probe: &sourceResult}); err != nil {
		t.Fatal(err)
	}

	remoteCommand, ok, err := store.NextAgentCommand(ctx, "gpu")
	if err != nil || !ok {
		t.Fatalf("remote command=%#v ok=%v err=%v", remoteCommand, ok, err)
	}
	if remoteCommand.Kind != domain.AgentCommandProbeObserve || remoteCommand.Probe == nil || remoteCommand.Probe.EntryID != "" || remoteCommand.Probe.Path != "" || remoteCommand.Probe.APIKey != "" || remoteCommand.Probe.Body != "" {
		t.Fatalf("remote observation could emit traffic: %#v", remoteCommand)
	}
	remoteResult := domain.ProbeExecution{
		ID: remoteCommand.Probe.ProbeID, TraceID: remoteCommand.Probe.TraceID,
		SourceCluster: "gpu", State: "completed", LogSource: "gpu-log",
		Hops: []domain.ObservedHop{{ObservedAt: now.Add(time.Millisecond).Format(time.RFC3339Nano), ClusterID: "gpu", RouteName: "model", Confidence: "observed"}}, EvidenceComplete: true,
	}
	if err := store.CompleteAgentCommand(ctx, domain.AgentCommandResult{CommandID: remoteCommand.ID, ClusterID: "gpu", Probe: &remoteResult}); err != nil {
		t.Fatal(err)
	}

	result := <-done
	if result.err != nil {
		t.Fatal(result.err)
	}
	if len(result.probe.Segments) != 2 || len(result.probe.Hops) != 2 || !result.probe.EvidenceComplete {
		t.Fatalf("probe=%#v", result.probe)
	}
	if result.probe.Segments[1].GatewayID != "gpu::gateway/gpu" || result.probe.Segments[1].Evidence != "observed" {
		t.Fatalf("remote segment=%#v", result.probe.Segments[1])
	}
}

func TestStoreProbeRejectsAPIKeyWithLineBreak(t *testing.T) {
	store := NewStore("federation", time.Minute)
	now := time.Now().UTC().Format(time.RFC3339)
	mustReceive(t, store, domain.AgentSnapshot{
		Cluster:  domain.TopologyCluster{ID: "edge", Snapshot: domain.Snapshot{ID: "edge-1", ObservedAt: now}},
		Context:  domain.Context{Cluster: domain.Cluster{ID: "edge"}},
		Topology: domain.Topology{Nodes: []domain.TopologyNode{{ID: "gateway/edge", Kind: "Gateway"}}, ProbeEntries: []domain.ProbeEntry{{ID: "entry/edge", GatewayID: "gateway/edge", DNSName: "edge.default.svc.cluster.local", Port: 80, Scheme: "http"}}},
	})
	_, err := store.CreateProbe(context.Background(), domain.ProbeRequest{SourceCluster: "edge", GatewayID: "gateway/edge", EntryID: "entry/edge", Method: "GET", Path: "/", APIKey: "bad\nkey"})
	if err == nil {
		t.Fatal("expected API key validation error")
	}
}

func TestCompleteFederatedProbeKeepsSourceEvidenceWhenRemoteUnavailable(t *testing.T) {
	store := NewStore("federation", time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	execution := domain.ProbeExecution{
		ID: "probe", TraceID: "trace", SourceCluster: "edge",
		GatewayID: "edge::gateway/edge", State: "completed", StartedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Hops: []domain.ObservedHop{{ClusterID: "edge", RouteName: "source", UpstreamHost: "10.0.0.8:80", Confidence: "observed"}},
	}
	topology := domain.Topology{
		Nodes:        []domain.TopologyNode{{ID: execution.GatewayID, ClusterID: "edge", Kind: "Gateway", Name: "edge"}},
		ProbeEntries: []domain.ProbeEntry{{GatewayID: "gpu::gateway/gpu", Addresses: []string{"10.0.0.8"}}},
	}
	result := store.completeFederatedProbe(ctx, execution, topology, []probeGatewayTarget{{
		ClusterID: "gpu", GatewayID: "gateway/gpu", GlobalID: "gpu::gateway/gpu", GatewayName: "gpu",
	}})
	if result.State != "completed" || len(result.Segments) != 2 || result.Segments[0].Evidence != "observed" {
		t.Fatalf("result=%#v", result)
	}
	if result.Segments[1].State != "unavailable" || result.EvidenceComplete || len(result.Gaps) != 1 {
		t.Fatalf("remote failure was not preserved as a gap: %#v", result)
	}
	if result.Segments[1].InferenceConfidence != "high" || result.Segments[1].InferenceBasis == "" {
		t.Fatalf("remote inference was not preserved: %#v", result.Segments[1])
	}
}

func TestCompleteFederatedProbeOmitsGatewaysWithoutMatchingLogs(t *testing.T) {
	store := NewStore("federation", time.Minute)
	now := time.Now().UTC()
	mustReceive(t, store, domain.AgentSnapshot{
		Cluster: domain.TopologyCluster{ID: "edge", Name: "edge", Snapshot: domain.Snapshot{ID: "edge-1", ObservedAt: now.Format(time.RFC3339)}},
		Context: domain.Context{Cluster: domain.Cluster{ID: "edge"}},
		Topology: domain.Topology{Nodes: []domain.TopologyNode{
			{ID: "gateway/edge", Kind: "Gateway", Name: "edge"},
			{ID: "gateway/unrelated", Kind: "Gateway", Name: "unrelated"},
		}},
	})
	execution := domain.ProbeExecution{
		ID: "probe", TraceID: "trace", SourceCluster: "edge",
		GatewayID: "edge::gateway/edge", State: "completed", StartedAt: now.Format(time.RFC3339Nano),
		Hops: []domain.ObservedHop{{ObservedAt: now.Format(time.RFC3339Nano), ClusterID: "edge", RouteName: "source", Confidence: "observed"}},
	}
	topology := domain.Topology{Nodes: []domain.TopologyNode{{ID: execution.GatewayID, ClusterID: "edge", Kind: "Gateway", Name: "edge"}}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	type result struct{ probe domain.ProbeExecution }
	done := make(chan result, 1)
	go func() {
		done <- result{probe: store.completeFederatedProbe(ctx, execution, topology, []probeGatewayTarget{{
			ClusterID: "edge", GatewayID: "gateway/unrelated", GlobalID: "edge::gateway/unrelated", GatewayName: "unrelated",
		}})}
	}()
	command, ok, err := store.NextAgentCommand(ctx, "edge")
	if err != nil || !ok {
		t.Fatalf("command=%#v ok=%v err=%v", command, ok, err)
	}
	empty := domain.ProbeExecution{ID: "probe", TraceID: "trace", SourceCluster: "edge", State: "completed", Gaps: []string{"未找到匹配 gatelens_probe_id 或 trace_id 的网关访问日志"}}
	if err := store.CompleteAgentCommand(ctx, domain.AgentCommandResult{CommandID: command.ID, ClusterID: "edge", Probe: &empty}); err != nil {
		t.Fatal(err)
	}
	probe := (<-done).probe
	if len(probe.Segments) != 1 || len(probe.Gaps) != 0 || !probe.EvidenceComplete {
		t.Fatalf("probe=%#v", probe)
	}
}

func TestCompleteFederatedProbeKeepsInferredGatewayWithoutMatchingLogs(t *testing.T) {
	store := NewStore("federation", time.Minute)
	now := time.Now().UTC()
	mustReceive(t, store, domain.AgentSnapshot{
		Cluster:  domain.TopologyCluster{ID: "gpu", Name: "gpu", Snapshot: domain.Snapshot{ID: "gpu-1", ObservedAt: now.Format(time.RFC3339)}},
		Context:  domain.Context{Cluster: domain.Cluster{ID: "gpu"}},
		Topology: domain.Topology{Nodes: []domain.TopologyNode{{ID: "gateway/gpu", Kind: "Gateway", Name: "gpu"}}},
	})
	execution := domain.ProbeExecution{
		ID: "probe", TraceID: "trace", SourceCluster: "edge",
		GatewayID: "edge::gateway/edge", State: "completed", StartedAt: now.Format(time.RFC3339Nano),
		Hops: []domain.ObservedHop{{ObservedAt: now.Format(time.RFC3339Nano), ClusterID: "edge", RouteName: "source", UpstreamHost: "10.233.72.18:8080", Confidence: "observed"}},
	}
	topology := domain.Topology{
		Nodes:        []domain.TopologyNode{{ID: execution.GatewayID, ClusterID: "edge", Kind: "Gateway", Name: "edge"}},
		ProbeEntries: []domain.ProbeEntry{{GatewayID: "gpu::gateway/gpu", Addresses: []string{"10.233.72.18"}}},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	type result struct{ probe domain.ProbeExecution }
	done := make(chan result, 1)
	go func() {
		done <- result{probe: store.completeFederatedProbe(ctx, execution, topology, []probeGatewayTarget{{
			ClusterID: "gpu", GatewayID: "gateway/gpu", GlobalID: "gpu::gateway/gpu", GatewayName: "gpu",
		}})}
	}()
	command, ok, err := store.NextAgentCommand(ctx, "gpu")
	if err != nil || !ok {
		t.Fatalf("command=%#v ok=%v err=%v", command, ok, err)
	}
	empty := domain.ProbeExecution{ID: "probe", TraceID: "trace", SourceCluster: "gpu", State: "completed", LogSource: "gpu-log", Gaps: []string{"未找到匹配 gatelens_probe_id 或 trace_id 的网关访问日志"}}
	if err := store.CompleteAgentCommand(ctx, domain.AgentCommandResult{CommandID: command.ID, ClusterID: "gpu", Probe: &empty}); err != nil {
		t.Fatal(err)
	}
	probe := (<-done).probe
	if len(probe.Segments) != 2 || len(probe.Gaps) != 1 || probe.EvidenceComplete {
		t.Fatalf("probe=%#v", probe)
	}
	candidate := probe.Segments[1]
	if candidate.State != "missing" || candidate.InferenceConfidence != "high" || !strings.Contains(candidate.InferenceBasis, "upstream_host") {
		t.Fatalf("candidate=%#v", candidate)
	}
}
