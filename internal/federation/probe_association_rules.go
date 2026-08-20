package federation

import (
	"fmt"
	"strings"

	"github.com/gatelens/gatelens/internal/domain"
)

const (
	probeAssociationStrengthDeclared = 100
	probeAssociationStrengthCluster  = 200
	probeAssociationStrengthHost     = 300
)

type probeAssociationMatch struct {
	gatewayID string
	basis     string
}

// A result groups all gateways matched by one piece of evidence. Ambiguity is
// resolved centrally so every integration follows the same candidate semantics.
type probeAssociationRuleResult struct {
	matches        []probeAssociationMatch
	confidence     string
	strength       int
	ambiguousBasis string
}

type probeAssociationContext struct {
	topology           domain.Topology
	targets            []probeGatewayTarget
	hops               []domain.ObservedHop
	observedGatewayIDs map[string]bool
	entriesByGateway   map[string][]domain.ProbeEntry
	nodesByID          map[string]domain.TopologyNode
	ownedNodes         map[string][]domain.TopologyNode
	forwardEdges       map[string][]domain.TopologyEdge
}

// probeAssociationRule translates runtime or configuration evidence into
// candidate Gateway associations. New relationship chains only need a rule and
// registration in defaultProbeAssociationRules.
type probeAssociationRule interface {
	name() string
	infer(probeAssociationContext) []probeAssociationRuleResult
}

func defaultProbeAssociationRules() []probeAssociationRule {
	return []probeAssociationRule{
		upstreamHostGatewayAssociationRule{},
		mcpBridgeRegistryGatewayAssociationRule{},
		upstreamClusterGatewayAssociationRule{},
		declaredGatewayAssociationRule{},
	}
}

func inferProbeGatewayCandidatesWithRules(topology domain.Topology, targets []probeGatewayTarget, hops []domain.ObservedHop, observedGatewayIDs map[string]bool, rules []probeAssociationRule) map[string]probeGatewayInference {
	ctx := newProbeAssociationContext(topology, targets, hops, observedGatewayIDs)
	type rankedInference struct {
		probeGatewayInference
		strength int
	}
	ranked := map[string]rankedInference{}
	for _, rule := range rules {
		for _, association := range rule.infer(ctx) {
			matches := uniqueProbeAssociationMatches(association.matches)
			if len(matches) == 0 {
				continue
			}
			for _, match := range matches {
				inference := probeGatewayInference{Basis: match.basis, Confidence: association.confidence}
				if len(matches) > 1 {
					if association.ambiguousBasis != "" {
						inference.Basis = fmt.Sprintf(association.ambiguousBasis, len(matches))
					} else {
						inference.Basis = fmt.Sprintf("关联规则 %s 同时匹配 %d 个网关，无法唯一归属", rule.name(), len(matches))
					}
					inference.Confidence = "ambiguous"
				}
				current, found := ranked[match.gatewayID]
				if !found || association.strength > current.strength {
					ranked[match.gatewayID] = rankedInference{probeGatewayInference: inference, strength: association.strength}
				}
			}
		}
	}
	result := make(map[string]probeGatewayInference, len(ranked))
	for gatewayID, inference := range ranked {
		result[gatewayID] = inference.probeGatewayInference
	}
	return result
}

func newProbeAssociationContext(topology domain.Topology, targets []probeGatewayTarget, hops []domain.ObservedHop, observedGatewayIDs map[string]bool) probeAssociationContext {
	ctx := probeAssociationContext{
		topology: topology, targets: targets, hops: hops, observedGatewayIDs: observedGatewayIDs,
		entriesByGateway: map[string][]domain.ProbeEntry{}, nodesByID: map[string]domain.TopologyNode{},
		ownedNodes: map[string][]domain.TopologyNode{}, forwardEdges: map[string][]domain.TopologyEdge{},
	}
	for _, entry := range topology.ProbeEntries {
		ctx.entriesByGateway[entry.GatewayID] = append(ctx.entriesByGateway[entry.GatewayID], entry)
	}
	for _, node := range topology.Nodes {
		ctx.nodesByID[node.ID] = node
	}
	for _, edge := range topology.Edges {
		ctx.forwardEdges[edge.From] = append(ctx.forwardEdges[edge.From], edge)
		if edge.Relation == "owns" && ctx.nodesByID[edge.From].Kind == "Gateway" {
			ctx.ownedNodes[edge.From] = append(ctx.ownedNodes[edge.From], ctx.nodesByID[edge.To])
		}
	}
	return ctx
}

func uniqueProbeAssociationMatches(matches []probeAssociationMatch) []probeAssociationMatch {
	seen := map[string]bool{}
	result := make([]probeAssociationMatch, 0, len(matches))
	for _, match := range matches {
		if match.gatewayID == "" || seen[match.gatewayID] {
			continue
		}
		seen[match.gatewayID] = true
		result = append(result, match)
	}
	return result
}

type upstreamHostGatewayAssociationRule struct{}

func (upstreamHostGatewayAssociationRule) name() string { return "upstream-host-entry-address" }

func (upstreamHostGatewayAssociationRule) infer(ctx probeAssociationContext) []probeAssociationRuleResult {
	var results []probeAssociationRuleResult
	for _, hop := range ctx.hops {
		host := normalizedUpstreamHost(hop.UpstreamHost)
		if host == "" {
			continue
		}
		var matches []probeAssociationMatch
		for _, target := range ctx.targets {
			if gatewayTargetMatchesAddress(ctx.entriesByGateway[target.GlobalID], ctx.nodesByID[target.GlobalID], host) {
				matches = append(matches, probeAssociationMatch{
					gatewayID: target.GlobalID,
					basis:     fmt.Sprintf("上一跳 upstream_host %s 命中该网关的入口地址", hop.UpstreamHost),
				})
			}
		}
		results = append(results, probeAssociationRuleResult{
			matches: matches, confidence: "high", strength: probeAssociationStrengthHost,
			ambiguousBasis: fmt.Sprintf("上一跳 upstream_host %s 同时匹配 %%d 个网关入口，无法唯一归属", hop.UpstreamHost),
		})
	}
	return results
}

type mcpBridgeRegistryGatewayAssociationRule struct{}

func (mcpBridgeRegistryGatewayAssociationRule) name() string {
	return "higress-mcpbridge-registry-service"
}

func (mcpBridgeRegistryGatewayAssociationRule) infer(ctx probeAssociationContext) []probeAssociationRuleResult {
	type resolvedService struct {
		key      string
		registry string
	}
	var results []probeAssociationRuleResult
	for _, hop := range ctx.hops {
		tokenSet := map[string]bool{}
		for _, token := range upstreamClusterTokens(hop.UpstreamCluster) {
			tokenSet[token] = true
		}
		if len(tokenSet) == 0 {
			continue
		}
		var resolved []resolvedService
		for _, node := range ctx.topology.Nodes {
			if node.Kind != "Registry" {
				continue
			}
			types := valuesWithPrefixes(node.Conditions, "Type=")
			if len(types) == 0 || !tokenSet[normalizeTarget(node.Name+"."+types[0])] {
				continue
			}
			for _, edge := range ctx.forwardEdges[node.ID] {
				service := ctx.nodesByID[edge.To]
				if edge.Relation == "resolves" && service.Kind == "Service" {
					resolved = append(resolved, resolvedService{
						key:      service.ClusterID + "::" + service.Namespace + "/" + service.Name,
						registry: node.Namespace + "/" + node.Name + "." + types[0],
					})
				}
			}
		}
		var matches []probeAssociationMatch
		for _, target := range ctx.targets {
			services := gatewayTargetServiceKeys(target, ctx.entriesByGateway[target.GlobalID], ctx.nodesByID[target.GlobalID])
			for _, service := range resolved {
				if services[service.key] {
					matches = append(matches, probeAssociationMatch{
						gatewayID: target.GlobalID,
						basis:     fmt.Sprintf("上一跳 upstream_cluster %s 命中 McpBridge registry %s，并解析到该网关的入口 Service", hop.UpstreamCluster, service.registry),
					})
					break
				}
			}
		}
		results = append(results, probeAssociationRuleResult{
			matches: matches, confidence: "medium", strength: probeAssociationStrengthCluster,
			ambiguousBasis: fmt.Sprintf("上一跳 upstream_cluster %s 经 McpBridge registry 同时匹配 %%d 个网关入口 Service，无法唯一归属", hop.UpstreamCluster),
		})
	}
	return results
}

type upstreamClusterGatewayAssociationRule struct{}

func (upstreamClusterGatewayAssociationRule) name() string { return "upstream-cluster-entry-service" }

func (upstreamClusterGatewayAssociationRule) infer(ctx probeAssociationContext) []probeAssociationRuleResult {
	var results []probeAssociationRuleResult
	for _, hop := range ctx.hops {
		if strings.TrimSpace(hop.UpstreamCluster) == "" {
			continue
		}
		var matches []probeAssociationMatch
		for _, target := range ctx.targets {
			if gatewayTargetMatchesCluster(ctx.entriesByGateway[target.GlobalID], ctx.nodesByID[target.GlobalID], ctx.ownedNodes[target.GlobalID], hop.UpstreamCluster) {
				matches = append(matches, probeAssociationMatch{
					gatewayID: target.GlobalID,
					basis:     fmt.Sprintf("上一跳 upstream_cluster %s 命中该网关的入口 Service", hop.UpstreamCluster),
				})
			}
		}
		results = append(results, probeAssociationRuleResult{
			matches: matches, confidence: "medium", strength: probeAssociationStrengthCluster,
			ambiguousBasis: fmt.Sprintf("上一跳 upstream_cluster %s 同时匹配 %%d 个网关入口，无法唯一归属", hop.UpstreamCluster),
		})
	}
	return results
}

type declaredGatewayAssociationRule struct{}

func (declaredGatewayAssociationRule) name() string { return "observed-predecessor-declared-edge" }

func (declaredGatewayAssociationRule) infer(ctx probeAssociationContext) []probeAssociationRuleResult {
	var results []probeAssociationRuleResult
	for _, target := range ctx.targets {
		if !target.Declared || !ctx.observedGatewayIDs[target.PredecessorGatewayID] {
			continue
		}
		destination := strings.TrimSpace(target.Destination)
		if destination == "" {
			destination = target.GatewayName
		}
		results = append(results, probeAssociationRuleResult{
			matches: []probeAssociationMatch{{
				gatewayID: target.GlobalID,
				basis:     fmt.Sprintf("配置拓扑从已观测网关指向 %s；未获得该网关的运行时日志", destination),
			}},
			confidence: "low", strength: probeAssociationStrengthDeclared,
		})
	}
	return results
}
