package observed

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gatelens/gatelens/internal/domain"
)

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

// AttemptGroups links only comparable occurrences within one gateway runtime.
func AttemptGroups(hops []domain.ObservedHop, collection *domain.ProbeCollection) []domain.ProbeAttemptGroup {
	var buckets [][]domain.ObservedHop
	indices := map[string]int{}
	for _, hop := range hops {
		source := hop.RuntimeSource
		if source == "" {
			source = "unknown/" + hop.LogSourceID + "/" + hop.Pod
		}
		key := source + "\x00" + hop.RequestStartTime + "\x00" + validLogValue(hop.DownstreamRemoteAddress) + "\x00" + validLogValue(hop.DownstreamLocalAddress)
		index, exists := indices[key]
		if !exists {
			index = len(buckets)
			indices[key] = index
			buckets = append(buckets, nil)
		}
		buckets[index] = append(buckets[index], hop)
	}
	groups := make([]domain.ProbeAttemptGroup, 0, len(buckets))
	for index, records := range buckets {
		group := domain.ProbeAttemptGroup{ID: fmt.Sprintf("group-%d", index+1), RuntimeSource: records[0].RuntimeSource, RelationState: "unconfirmed", OrderBasis: "unavailable", Gaps: []string{}}
		ordered := true
		sequences := map[int]bool{}
		for _, hop := range records {
			if hop.RuntimeSource == "" || hop.LogSourceID == "" || hop.LogSequence <= 0 || hop.RequestStartTime == "" || validLogValue(hop.DownstreamRemoteAddress) == "" || hop.LogSourceID != records[0].LogSourceID || sequences[hop.LogSequence] {
				ordered = false
			}
			sequences[hop.LogSequence] = true
		}
		if collection != nil {
			for _, reason := range collection.Reasons {
				if strings.Contains(reason, "连续性") || strings.Contains(reason, "顺序") {
					ordered = false
				}
			}
		}
		if ordered {
			sort.SliceStable(records, func(i, j int) bool { return records[i].LogSequence < records[j].LogSequence })
			group.OrderBasis = "source-sequence"
		} else if len(records) > 1 || records[0].InternalRedirect || IsInternalRedirect(records[0].ResponseCodeDetails) {
			group.Gaps = appendUnique(group.Gaps, "运行时身份或日志来源顺序不足，尝试关系未确认")
		}
		redirects := 0
		for _, hop := range records {
			group.HopIDs = append(group.HopIDs, hop.ID)
			if hop.InternalRedirect || IsInternalRedirect(hop.ResponseCodeDetails) {
				redirects++
			} else {
				group.TerminalCandidateIDs = append(group.TerminalCandidateIDs, hop.ID)
			}
		}
		terminalCount := len(group.TerminalCandidateIDs)
		invalidOrder := false
		if ordered {
			for i, hop := range records {
				if i < len(records)-1 && !hop.InternalRedirect && !IsInternalRedirect(hop.ResponseCodeDetails) {
					invalidOrder = true
				}
			}
		}
		if terminalCount > 1 || invalidOrder {
			group.RelationState = "ambiguous"
			group.Gaps = appendUnique(group.Gaps, "存在多个终止候选或重入记录，最终尝试归属未确认")
		} else {
			if ordered && redirects > 0 {
				for i, hop := range records {
					if i+1 < len(records) && (hop.InternalRedirect || IsInternalRedirect(hop.ResponseCodeDetails)) {
						group.Links = append(group.Links, domain.ProbeAttemptLink{From: hop.ID, To: records[i+1].ID})
					}
				}
			}
			if redirects > 0 && (terminalCount == 0 || (ordered && (records[len(records)-1].InternalRedirect || IsInternalRedirect(records[len(records)-1].ResponseCodeDetails)))) {
				group.RelationState = "missing-next"
				group.Gaps = appendUnique(group.Gaps, "已发生内部重定向，后续尝试记录缺失")
			} else if ordered && (redirects > 0 || len(records) == 1) {
				group.RelationState = "linked"
			} else if len(records) > 1 {
				group.Gaps = appendUnique(group.Gaps, "同请求多条记录，关系未确认")
			}
		}
		if group.RelationState == "linked" && terminalCount == 1 && collection != nil && collection.State == "settled" && len(collection.Reasons) == 0 {
			group.LocalTerminalHopID = group.TerminalCandidateIDs[0]
		}
		groups = append(groups, group)
	}
	return groups
}

// EnrichProbe adds observation semantics without changing the Agent HTTP result
// or the legacy gateway-coverage EvidenceComplete flag.
func EnrichProbe(execution domain.ProbeExecution) domain.ProbeExecution {
	execution.FinalResponseHopID, execution.FinalUpstreamHopID = "", ""
	summary := &domain.ProbeRedirectSummary{ProcessState: "observed"}
	execution.RedirectSummary = summary
	execution.Hops = nil
	byID := map[string]domain.ObservedHop{}
	for segmentIndex := range execution.Segments {
		segment := &execution.Segments[segmentIndex]
		if segment.Collection == nil {
			segment.Collection = &domain.ProbeCollection{State: "unknown"}
		}
		for hopIndex := range segment.Hops {
			hop := &segment.Hops[hopIndex]
			// Agent numbering is scoped to its command; prefix it by gateway.
			if hop.ID == "" {
				hop.ID = fmt.Sprintf("legacy-%d", hopIndex+1)
			}
			prefix := segment.GatewayID + "/"
			if !strings.HasPrefix(hop.ID, prefix) {
				hop.ID = prefix + hop.ID
			}
			hop.InternalRedirect = IsInternalRedirect(hop.ResponseCodeDetails)
			if hop.AIRouting == nil {
				hop.AIRouting = parseAIRouting(hop.AILog)
			}
			if hop.InternalRedirect {
				summary.ObservedRedirects++
			}
			byID[hop.ID] = *hop
		}
		segment.AttemptGroups = AttemptGroups(segment.Hops, segment.Collection)
		for _, group := range segment.AttemptGroups {
			summary.LinkedRedirects += len(group.Links)
			for _, gap := range group.Gaps {
				segment.Gaps = appendUnique(segment.Gaps, gap)
			}
		}
		for _, reason := range segment.Collection.Reasons {
			segment.Gaps = appendUnique(segment.Gaps, reason)
		}
		if len(segment.Hops) == 0 || segment.Collection.State == "window-ended" || segment.Collection.State == "cancelled" || segment.Collection.State == "read-error" || len(segment.Gaps) > 0 {
			summary.ProcessState = "partial"
		}
		execution.Hops = append(execution.Hops, segment.Hops...)
		for _, gap := range segment.Gaps {
			execution.Gaps = appendUnique(execution.Gaps, segment.ClusterID+"/"+segment.GatewayName+": "+gap)
		}
	}
	unknown, ambiguous := false, false
	for _, segment := range execution.Segments {
		if segment.Collection.State == "unknown" {
			unknown = true
		}
		for _, group := range segment.AttemptGroups {
			if group.RelationState == "ambiguous" {
				ambiguous = true
			}
			if group.RelationState == "unconfirmed" {
				unknown = true
			}
		}
	}
	if unknown {
		summary.ProcessState = "unknown"
	}
	if ambiguous {
		summary.ProcessState = "ambiguous"
	}
	// A source response can be attributed only under a single unambiguous
	// runtime group with a settled exact-probe observation.
	if execution.State == "completed" && execution.Error == "" && execution.ResponseCode > 0 {
		for _, segment := range execution.Segments {
			if segment.GatewayID != execution.GatewayID || len(segment.AttemptGroups) != 1 || len(segment.Gaps) > 0 {
				continue
			}
			group := segment.AttemptGroups[0]
			hop, exists := byID[group.LocalTerminalHopID]
			exact := true
			for _, id := range group.HopIDs {
				if byID[id].Correlation != "probe-id" {
					exact = false
				}
			}
			if exists && exact && hop.ResponseCode == execution.ResponseCode {
				execution.FinalResponseHopID = hop.ID
				if len(execution.Segments) == 1 && len(execution.Gaps) == 0 {
					execution.FinalUpstreamHopID = hop.ID
				}
			}
		}
	}
	if execution.Collection == nil {
		execution.Collection = &domain.ProbeCollection{State: "unknown"}
	}
	return execution
}
