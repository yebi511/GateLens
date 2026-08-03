package federation

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gatelens/gatelens/internal/domain"
)

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
		Cluster:  domain.TopologyCluster{ID: clusterID, Name: clusterID, Role: "member", Snapshot: snapshot},
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

func TestStoreRejectsUnknownFederatedGateway(t *testing.T) {
	store := NewStore("federation", time.Minute)
	if _, err := store.EnvoyConfig(context.Background(), "missing::gateway/default/not-found"); err == nil {
		t.Fatal("expected unknown owning cluster error")
	}
}
