package federation

import (
	"fmt"

	"github.com/gatelens/gatelens/internal/domain"
)

type linkTargetMatch struct {
	target linkCandidate
	key    string
}

type linkRuleResult struct {
	matches   []linkTargetMatch
	transport string
	evidence  string
}

type linkRuleContext struct {
	nodesByID map[string]domain.TopologyNode
	edges     []domain.TopologyEdge
	entries   []linkCandidate
}

// A rule handles one outbound candidate. Specific integrations run before the
// generic rule while ambiguity handling and edge generation remain shared.
type linkRule interface {
	name() string
	match(linkCandidate, linkRuleContext) (linkRuleResult, bool)
}

func defaultLinkRules() []linkRule {
	return []linkRule{
		higressMCPBridgeLinkRule{},
		exactConfigurationLinkRule{},
	}
}

func discoverLinksWithRules(nodes []domain.TopologyNode, existing []domain.TopologyEdge, rules []linkRule) ([]domain.TopologyEdge, []domain.Finding) {
	ctx := linkRuleContext{
		nodesByID: make(map[string]domain.TopologyNode, len(nodes)),
		edges:     existing,
	}
	var outbound []linkCandidate
	for _, node := range nodes {
		ctx.nodesByID[node.ID] = node
		if keys := outboundKeys(node); len(keys) > 0 {
			outbound = append(outbound, linkCandidate{node: node, keys: keys})
		}
		if keys := entryKeys(node); len(keys) > 0 {
			rank := 2
			switch node.Kind {
			case "Listener":
				rank = 0
			case "Gateway":
				rank = 1
			}
			ctx.entries = append(ctx.entries, linkCandidate{node: node, keys: keys, rank: rank, evidence: entryEvidence(node)})
		}
	}

	var links []domain.TopologyEdge
	var findings []domain.Finding
	for _, from := range outbound {
		for _, rule := range rules {
			result, handled := rule.match(from, ctx)
			if !handled {
				continue
			}
			if len(result.matches) == 1 {
				match := result.matches[0]
				to := match.target.node
				links = append(links, domain.TopologyEdge{
					From: from.node.ID, To: to.ID, Relation: "cross-cluster",
					Transport: result.transport, Destination: to.ClusterID + "/" + to.Namespace + "/" + to.Name,
					State: "resolved", Evidence: result.evidence + ": target " + match.key + " matched " + match.target.evidence,
				})
			} else if len(result.matches) > 1 {
				findings = append(findings, domain.Finding{
					ID: "ambiguous-cluster-link:" + from.node.ID, Severity: domain.StatusWarning,
					Title: "Cross-cluster target is ambiguous", Resource: from.node.ClusterID + "/" + from.node.Name,
					Basis: fmt.Sprintf("%s matched %d remote entries", rule.name(), len(result.matches)), TargetID: from.node.ID,
				})
			}
			break
		}
	}
	return links, findings
}

type higressMCPBridgeLinkRule struct{}

func (higressMCPBridgeLinkRule) name() string { return "higress-mcpbridge" }

func (higressMCPBridgeLinkRule) match(from linkCandidate, ctx linkRuleContext) (linkRuleResult, bool) {
	if from.node.Kind != "Registry" || from.node.Source != "McpBridge.spec.registries" {
		return linkRuleResult{}, false
	}

	result := linkRuleResult{
		matches: bestEntryMatches(from, ctx), transport: transportOf(from.node),
		evidence: "auto-discovered by higress-mcpbridge rule: McpBridge registry " + nodeLocation(from.node) + " declared upstream",
	}
	selectingIngress, ok := ingressSelectingRegistry(from.node, ctx)
	if ok {
		if values := valuesWithPrefixes(selectingIngress.Conditions, "higress.io/backend-protocol="); len(values) > 0 {
			result.transport = values[0]
		}
		result.evidence += "; Ingress " + nodeLocation(selectingIngress) + " selected registry " + from.node.Name
	}
	return result, true
}

func ingressSelectingRegistry(registry domain.TopologyNode, ctx linkRuleContext) (domain.TopologyNode, bool) {
	for _, edge := range ctx.edges {
		if edge.To != registry.ID || edge.Relation != "selects" {
			continue
		}
		node, ok := ctx.nodesByID[edge.From]
		if ok && node.Kind == "Ingress" && node.ClusterID == registry.ClusterID {
			return node, true
		}
	}
	return domain.TopologyNode{}, false
}

type exactConfigurationLinkRule struct{}

func (exactConfigurationLinkRule) name() string { return "exact-configuration-address" }

func (exactConfigurationLinkRule) match(from linkCandidate, ctx linkRuleContext) (linkRuleResult, bool) {
	return linkRuleResult{
		matches: bestEntryMatches(from, ctx), transport: transportOf(from.node),
		evidence: "auto-discovered from configuration",
	}, true
}

func bestEntryMatches(from linkCandidate, ctx linkRuleContext) []linkTargetMatch {
	bestRank := 99
	var matches []linkTargetMatch
	for _, to := range ctx.entries {
		if from.node.ClusterID == to.node.ClusterID || edgeExists(ctx.edges, from.node.ID, to.node.ID) {
			continue
		}
		if key, ok := intersect(from.keys, to.keys); ok {
			if to.rank < bestRank {
				bestRank = to.rank
				matches = []linkTargetMatch{{target: to, key: key}}
			} else if to.rank == bestRank {
				matches = append(matches, linkTargetMatch{target: to, key: key})
			}
		}
	}
	return matches
}

func nodeLocation(node domain.TopologyNode) string {
	location := node.ClusterID + "/"
	if node.Namespace != "" {
		location += node.Namespace + "/"
	}
	return location + node.Name
}
