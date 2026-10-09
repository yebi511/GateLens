package observed

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gatelens/gatelens/internal/domain"
)

// attemptGroup is an internal request context, never a wire object.
type attemptGroup struct {
	ID                   string
	RuntimeSource        string
	HopIDs               []string
	RelationState        domain.ProbeAttemptRelationState
	OrderBasis           domain.ProbeOrderBasis
	Links                []domain.ProbeAttemptLink
	TerminalCandidateIDs []string
	LocalTerminalHopID   string
	Issues               []domain.ProbeIssue
}

// attemptGroups links only comparable occurrences within one gateway runtime.
func attemptGroups(hops []domain.ObservedHop, collection *domain.ProbeCollection, issues []domain.ProbeIssue) []attemptGroup {
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
	groups := make([]attemptGroup, 0, len(buckets))
	for index, records := range buckets {
		group := attemptGroup{ID: fmt.Sprintf("context-%d", index+1), RuntimeSource: records[0].RuntimeSource, RelationState: domain.ProbeAttemptRelationStateUnconfirmed, OrderBasis: domain.ProbeOrderBasisUnavailable, Issues: []domain.ProbeIssue{}}
		ordered := true
		identityIncomplete := false
		sequences := map[int]bool{}
		for _, hop := range records {
			if hop.RuntimeSource == "" || hop.RequestStartTime == "" || validLogValue(hop.DownstreamRemoteAddress) == "" {
				identityIncomplete = true
			}
			if hop.RuntimeSource == "" || hop.LogSourceID == "" || hop.LogSequence <= 0 || hop.RequestStartTime == "" || validLogValue(hop.DownstreamRemoteAddress) == "" || hop.LogSourceID != records[0].LogSourceID || sequences[hop.LogSequence] {
				ordered = false
			}
			sequences[hop.LogSequence] = true
		}
		for _, issue := range issues {
			if issue.BreaksLogOrder() {
				ordered = false
			}
		}
		if ordered {
			sort.SliceStable(records, func(i, j int) bool { return records[i].LogSequence < records[j].LogSequence })
			group.OrderBasis = domain.ProbeOrderBasisSourceSequence
		} else {
			if identityIncomplete {
				group.Issues = domain.AppendProbeIssue(group.Issues, domain.ProbeIssue{Scope: domain.ProbeIssueScopeRelation, Code: domain.ProbeIssueCodeRequestIdentityIncomplete, ContextID: group.ID, Message: "运行时或原始请求身份不完整"})
			}
			group.Issues = domain.AppendProbeIssue(group.Issues, domain.ProbeIssue{Scope: domain.ProbeIssueScopeRelation, Code: domain.ProbeIssueCodeAttemptOrderUnconfirmed, ContextID: group.ID, Message: "运行时身份或日志来源顺序不足，尝试关系未确认"})
		}
		redirects := 0
		for _, hop := range records {
			group.HopIDs = append(group.HopIDs, hop.ID)
			if hop.InternalRedirect {
				redirects++
			} else {
				group.TerminalCandidateIDs = append(group.TerminalCandidateIDs, hop.ID)
			}
		}
		terminalCount := len(group.TerminalCandidateIDs)
		invalidOrder := false
		if ordered {
			for i, hop := range records {
				if i < len(records)-1 && !hop.InternalRedirect {
					invalidOrder = true
				}
			}
		}
		if terminalCount > 1 || invalidOrder {
			group.RelationState = domain.ProbeAttemptRelationStateAmbiguous
			group.Issues = domain.AppendProbeIssue(group.Issues, domain.ProbeIssue{Scope: domain.ProbeIssueScopeRelation, Code: domain.ProbeIssueCodeTerminalAmbiguous, ContextID: group.ID, Message: "存在多个终止候选或重入记录，最终尝试归属未确认"})
		} else {
			if ordered && redirects > 0 {
				for i, hop := range records {
					if i+1 < len(records) && (hop.InternalRedirect) {
						group.Links = append(group.Links, domain.ProbeAttemptLink{From: hop.ID, To: records[i+1].ID})
					}
				}
			}
			if redirects > 0 && (terminalCount == 0 || (ordered && (records[len(records)-1].InternalRedirect))) {
				group.RelationState = domain.ProbeAttemptRelationStateMissingNext
				group.Issues = domain.AppendProbeIssue(group.Issues, domain.ProbeIssue{Scope: domain.ProbeIssueScopeRelation, Code: domain.ProbeIssueCodeRedirectNextMissing, ContextID: group.ID, Message: "已发生内部重定向，后续尝试记录缺失"})
			} else if ordered && (redirects > 0 || len(records) == 1) {
				group.RelationState = domain.ProbeAttemptRelationStateLinked
			} else if len(records) > 1 {
				group.Issues = domain.AppendProbeIssue(group.Issues, domain.ProbeIssue{Scope: domain.ProbeIssueScopeRelation, Code: domain.ProbeIssueCodeAttemptOrderUnconfirmed, ContextID: group.ID, Message: "同请求多条记录，关系未确认"})
			}
		}
		if group.RelationState == domain.ProbeAttemptRelationStateLinked && terminalCount == 1 && collection != nil && collection.State == domain.ProbeCollectionStateSettled && len(issues) == 0 {
			group.LocalTerminalHopID = group.TerminalCandidateIDs[0]
		}
		groups = append(groups, group)
	}
	return groups
}

// EnrichProbe computes relationships without copying records or overriding the HTTP result.
func EnrichProbe(execution domain.ProbeExecution) domain.ProbeExecution {
	execution.FinalResponseHopID, execution.FinalUpstreamHopID = "", ""
	if execution.Segments == nil {
		execution.Segments = []domain.ProbeSegment{}
	}
	if execution.Issues == nil {
		execution.Issues = []domain.ProbeIssue{}
	}
	for segmentIndex := range execution.Segments {
		segment := &execution.Segments[segmentIndex]
		if segment.Hops == nil {
			segment.Hops = []domain.ObservedHop{}
		}
		if segment.Collection == nil {
			segment.Collection = &domain.ProbeCollection{State: domain.ProbeCollectionStateUnknown}
		}
		// Analysis may be repeated: rebuild relation issues and references, retain collection facts.
		issues := []domain.ProbeIssue{}
		for _, issue := range segment.Issues {
			if issue.Scope != domain.ProbeIssueScopeRelation {
				if issue.HopID != "" && !strings.HasPrefix(issue.HopID, segment.GatewayID+"/") {
					issue.HopID = segment.GatewayID + "/" + issue.HopID
				}
				issues = domain.AppendProbeIssue(issues, issue)
			}
		}
		segment.Issues = issues
		segment.Links = []domain.ProbeAttemptLink{}
		segment.LocalTerminalHopIDs = []string{}
		segment.RelationState = domain.ProbeAttemptRelationStateLinked
		byID := map[string]*domain.ObservedHop{}
		for hopIndex := range segment.Hops {
			hop := &segment.Hops[hopIndex]
			if hop.ID == "" {
				hop.ID = fmt.Sprintf("record-%d", hopIndex+1)
			}
			prefix := segment.GatewayID + "/"
			if !strings.HasPrefix(hop.ID, prefix) {
				hop.ID = prefix + hop.ID
			}
			byID[hop.ID] = hop
		}
		groups := attemptGroups(segment.Hops, segment.Collection, segment.Issues)
		if len(groups) == 0 {
			segment.RelationState = domain.ProbeAttemptRelationStateUnconfirmed
		}
		for _, group := range groups {
			for _, id := range group.HopIDs {
				byID[id].ContextID = group.ID
			}
			segment.Links = append(segment.Links, group.Links...)
			if group.LocalTerminalHopID != "" {
				segment.LocalTerminalHopIDs = append(segment.LocalTerminalHopIDs, group.LocalTerminalHopID)
			}
			for _, issue := range group.Issues {
				segment.Issues = domain.AppendProbeIssue(segment.Issues, issue)
			}
			if relationRank(group.RelationState) > relationRank(segment.RelationState) {
				segment.RelationState = group.RelationState
			}
		}
		if execution.State != domain.ProbeExecutionStateCompleted || execution.Error != "" || execution.ResponseCode <= 0 || segment.GatewayID != execution.GatewayID || len(groups) != 1 || len(segment.Issues) > 0 {
			continue
		}
		group := groups[0]
		hop := byID[group.LocalTerminalHopID]
		exact := true
		for _, id := range group.HopIDs {
			if byID[id].Correlation != domain.ProbeCorrelationProbeID {
				exact = false
			}
		}
		if hop != nil && exact && hop.ResponseCode == execution.ResponseCode {
			execution.FinalResponseHopID = hop.ID
			if len(execution.Segments) == 1 && len(execution.Issues) == 0 {
				execution.FinalUpstreamHopID = hop.ID
			}
		}
	}
	return execution
}

func relationRank(state domain.ProbeAttemptRelationState) int {
	switch state {
	case domain.ProbeAttemptRelationStateAmbiguous:
		return 3
	case domain.ProbeAttemptRelationStateUnconfirmed:
		return 2
	case domain.ProbeAttemptRelationStateMissingNext:
		return 1
	default:
		return 0
	}
}
