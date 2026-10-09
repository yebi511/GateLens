package domain

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func validLocalResult() (AgentCommand, ProbeAgentResult) {
	command := AgentCommand{Kind: AgentCommandProbeHTTP, ClusterID: "edge", GatewayID: "gateway", Probe: &ProbeCommand{ProbeID: "p", TraceID: "t", Method: ProbeHTTPMethodGET}}
	result := ProbeAgentResult{SchemaVersion: ProbeSchemaVersion, ProbeID: "p", TraceID: "t", ClusterID: "edge", GatewayID: "gateway", HTTP: &ProbeHTTPResult{Method: ProbeHTTPMethodGET, ResponseCode: 200}, Collection: &ProbeCollection{State: ProbeCollectionStateSettled}, Hops: []ObservedHop{}, Issues: []ProbeIssue{}}
	return command, result
}
func TestProbeProtocolShapeAndEnumRoundTrip(t *testing.T) {
	execution := ProbeExecution{SchemaVersion: ProbeSchemaVersion, State: ProbeExecutionStateCompleted, Method: ProbeHTTPMethodPOST, Issues: []ProbeIssue{}, Segments: []ProbeSegment{{GatewayID: "g", Hops: []ObservedHop{}, Collection: &ProbeCollection{State: ProbeCollectionStateUnknown}, RelationState: ProbeAttemptRelationStateUnconfirmed, Links: []ProbeAttemptLink{}, LocalTerminalHopIDs: []string{}, Issues: []ProbeIssue{}, SnapshotObservedAt: "snapshot-time"}}}
	encoded, err := json.Marshal(execution)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &raw); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"hops", "collection", "logSource", "evidenceComplete", "redirectSummary", "gaps"} {
		if _, ok := raw[field]; ok {
			t.Fatalf("removed top field %s", field)
		}
	}
	var segments []map[string]json.RawMessage
	_ = json.Unmarshal(raw["segments"], &segments)
	for _, field := range []string{"state", "evidence", "attemptGroups", "gaps", "observedAt"} {
		if _, ok := segments[0][field]; ok {
			t.Fatalf("removed segment field %s", field)
		}
	}
	for _, field := range []string{"hops", "links", "localTerminalHopIDs", "issues"} {
		if string(segments[0][field]) != "[]" {
			t.Fatalf("%s=%s", field, segments[0][field])
		}
	}
	var decoded ProbeExecution
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.SchemaVersion != ProbeSchemaVersion || decoded.State != ProbeExecutionStateCompleted || decoded.Segments[0].Collection.State != ProbeCollectionStateUnknown {
		t.Fatalf("decoded=%+v", decoded)
	}
	command, local := validLocalResult()
	if err := local.Validate(command); err != nil {
		t.Fatal(err)
	}
	command.Kind = AgentCommandProbeObserve
	local.HTTP = nil
	encoded, _ = json.Marshal(local)
	if strings.Contains(string(encoded), `"http"`) || strings.Contains(string(encoded), `"segments"`) {
		t.Fatalf("observe=%s", encoded)
	}
	if err := local.Validate(command); err != nil {
		t.Fatal(err)
	}
}
func TestRejectUnsupportedAndMalformedLocalProtocol(t *testing.T) {
	for _, version := range []int{0, 1, 3} {
		command, result := validLocalResult()
		result.SchemaVersion = version
		if !errors.Is(result.Validate(command), ErrProbeProtocolUnsupported) {
			t.Fatalf("version %d accepted", version)
		}
	}
	tests := map[string]func(*ProbeAgentResult){
		"probe": func(r *ProbeAgentResult) { r.ProbeID = "other" }, "trace": func(r *ProbeAgentResult) { r.TraceID = "other" },
		"cluster": func(r *ProbeAgentResult) { r.ClusterID = "other" }, "gateway": func(r *ProbeAgentResult) { r.GatewayID = "other" },
		"http absent": func(r *ProbeAgentResult) { r.HTTP = nil }, "method": func(r *ProbeAgentResult) { r.HTTP.Method = "CUSTOM" },
		"collection absent": func(r *ProbeAgentResult) { r.Collection = nil }, "state": func(r *ProbeAgentResult) { r.Collection.State = "ok" },
		"issue scope": func(r *ProbeAgentResult) { r.Issues = []ProbeIssue{{Scope: "wrong", Code: ProbeIssueCodeLogReadError}} },
		"issue code": func(r *ProbeAgentResult) {
			r.Issues = []ProbeIssue{{Scope: ProbeIssueScopeCollection, Code: "new-code"}}
		},
		"correlation": func(r *ProbeAgentResult) { r.Hops[0].Correlation = "exact" }, "confidence": func(r *ProbeAgentResult) { r.Hops[0].Confidence = "high" },
		"extproc":        func(r *ProbeAgentResult) { r.Hops[0].ExtProcs = []ExtProcObservation{{Outcome: "ok"}} },
		"record cluster": func(r *ProbeAgentResult) { r.Hops[0].ClusterID = "other" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			command, result := validLocalResult()
			result.Hops = []ObservedHop{{ID: "r1", ClusterID: "edge", Confidence: ObservationConfidenceObserved}}
			mutate(&result)
			if !errors.Is(result.Validate(command), ErrInvalidProbeResult) {
				t.Fatalf("invalid result accepted: %+v", result)
			}
		})
	}
	command, result := validLocalResult()
	command.Kind = AgentCommandProbeObserve
	if result.Validate(command) == nil {
		t.Fatal("observe accepted HTTP payload")
	}
	result.HTTP = nil
	result.Hops = []ObservedHop{{ID: "r1", ClusterID: "edge", Confidence: ObservationConfidenceObserved, Method: "CUSTOM", Protocol: "HTTP/99", ResponseFlags: "NEW", ResponseCodeDetails: "new_filter:detail"}}
	if err := result.Validate(command); err != nil {
		t.Fatalf("open values/missing metadata rejected: %v", err)
	}
}
func TestIssueIdentityAndOrderDoNotDependOnMessage(t *testing.T) {
	issue := ProbeIssue{Scope: ProbeIssueScopeCollection, Code: ProbeIssueCodeLogOrderUnconfirmed, Message: "one"}
	issues := AppendProbeIssue(nil, issue)
	issue.Message = "unrelated language"
	issues = AppendProbeIssue(issues, issue)
	if len(issues) != 1 || !issue.BreaksLogOrder() {
		t.Fatal(issues)
	}
	issue.ContextID = "other"
	issues = AppendProbeIssue(issues, issue)
	if len(issues) != 2 {
		t.Fatal(issues)
	}
}

func TestClosedEnumValues(t *testing.T) {
	for _, value := range []ProbeExecutionState{ProbeExecutionStateRunning, ProbeExecutionStateCompleted, ProbeExecutionStateFailed} {
		encoded, _ := json.Marshal(value)
		var decoded ProbeExecutionState
		if err := json.Unmarshal(encoded, &decoded); err != nil || !decoded.IsValid() || decoded != value {
			t.Fatalf("ProbeExecutionState %s", encoded)
		}
	}
	if ProbeExecutionState("invalid").IsValid() || ProbeExecutionState("").IsValid() {
		t.Fatal("ProbeExecutionState accepted invalid value")
	}
	for _, value := range []ProbeIssueScope{ProbeIssueScopeExecution, ProbeIssueScopeCollection, ProbeIssueScopeRelation, ProbeIssueScopeInference} {
		encoded, _ := json.Marshal(value)
		var decoded ProbeIssueScope
		if err := json.Unmarshal(encoded, &decoded); err != nil || !decoded.IsValid() || decoded != value {
			t.Fatalf("ProbeIssueScope %s", encoded)
		}
	}
	if ProbeIssueScope("invalid").IsValid() || ProbeIssueScope("").IsValid() {
		t.Fatal("ProbeIssueScope accepted invalid value")
	}
	for _, value := range []ProbeIssueCode{ProbeIssueCodeResponseLimitExceeded, ProbeIssueCodeLogSourceRotated, ProbeIssueCodeLogWindowTruncated, ProbeIssueCodeLogOrderUnconfirmed, ProbeIssueCodeCollectionWindowEnded, ProbeIssueCodeCollectionCancelled, ProbeIssueCodeLogReadError, ProbeIssueCodeNoMatchingLog, ProbeIssueCodeAgentUnavailable, ProbeIssueCodeProbeProtocolUnsupported, ProbeIssueCodeRequestIdentityIncomplete, ProbeIssueCodeAttemptOrderUnconfirmed, ProbeIssueCodeRedirectNextMissing, ProbeIssueCodeTerminalAmbiguous, ProbeIssueCodeGatewayEvidenceMissing} {
		encoded, _ := json.Marshal(value)
		var decoded ProbeIssueCode
		if err := json.Unmarshal(encoded, &decoded); err != nil || !decoded.IsValid() || decoded != value {
			t.Fatalf("ProbeIssueCode %s", encoded)
		}
	}
	if ProbeIssueCode("invalid").IsValid() || ProbeIssueCode("").IsValid() {
		t.Fatal("ProbeIssueCode accepted invalid value")
	}
	for _, value := range []ProbeCorrelation{ProbeCorrelationProbeID, ProbeCorrelationTraceID} {
		encoded, _ := json.Marshal(value)
		var decoded ProbeCorrelation
		if err := json.Unmarshal(encoded, &decoded); err != nil || !decoded.IsValid() || decoded != value {
			t.Fatalf("ProbeCorrelation %s", encoded)
		}
	}
	if ProbeCorrelation("invalid").IsValid() || ProbeCorrelation("").IsValid() {
		t.Fatal("ProbeCorrelation accepted invalid value")
	}
	for _, value := range []ProbeInferenceConfidence{ProbeInferenceConfidenceHigh, ProbeInferenceConfidenceMedium, ProbeInferenceConfidenceLow, ProbeInferenceConfidenceAmbiguous} {
		encoded, _ := json.Marshal(value)
		var decoded ProbeInferenceConfidence
		if err := json.Unmarshal(encoded, &decoded); err != nil || !decoded.IsValid() || decoded != value {
			t.Fatalf("ProbeInferenceConfidence %s", encoded)
		}
	}
	if ProbeInferenceConfidence("invalid").IsValid() || ProbeInferenceConfidence("").IsValid() {
		t.Fatal("ProbeInferenceConfidence accepted invalid value")
	}
	for _, value := range []ProbeOrderBasis{ProbeOrderBasisSourceSequence, ProbeOrderBasisUnavailable} {
		encoded, _ := json.Marshal(value)
		var decoded ProbeOrderBasis
		if err := json.Unmarshal(encoded, &decoded); err != nil || !decoded.IsValid() || decoded != value {
			t.Fatalf("ProbeOrderBasis %s", encoded)
		}
	}
	if ProbeOrderBasis("invalid").IsValid() || ProbeOrderBasis("").IsValid() {
		t.Fatal("ProbeOrderBasis accepted invalid value")
	}
	for _, value := range []AgentCommandKind{AgentCommandEnvoyConfig, AgentCommandProbeHTTP, AgentCommandProbeObserve} {
		encoded, _ := json.Marshal(value)
		var decoded AgentCommandKind
		if err := json.Unmarshal(encoded, &decoded); err != nil || !decoded.IsValid() || decoded != value {
			t.Fatalf("AgentCommandKind %s", encoded)
		}
	}
	if AgentCommandKind("invalid").IsValid() || AgentCommandKind("").IsValid() {
		t.Fatal("AgentCommandKind accepted invalid value")
	}
	for _, value := range []ExtProcOutcome{ExtProcOutcomeSuccess, ExtProcOutcomeTimeout, ExtProcOutcomeError, ExtProcOutcomeFailOpen, ExtProcOutcomeImmediateResponse, ExtProcOutcomeUnknown} {
		encoded, _ := json.Marshal(value)
		var decoded ExtProcOutcome
		if err := json.Unmarshal(encoded, &decoded); err != nil || !decoded.IsValid() || decoded != value {
			t.Fatalf("ExtProcOutcome %s", encoded)
		}
	}
	if ExtProcOutcome("invalid").IsValid() || ExtProcOutcome("").IsValid() {
		t.Fatal("ExtProcOutcome accepted invalid value")
	}
	for _, value := range []SnapshotConsistency{SnapshotConsistencySingleCluster, SnapshotConsistencyWaitingForAgents, SnapshotConsistencyConsistentWindow, SnapshotConsistencyRemoteUnavailable, SnapshotConsistencyTimeSkew} {
		encoded, _ := json.Marshal(value)
		var decoded SnapshotConsistency
		if err := json.Unmarshal(encoded, &decoded); err != nil || !decoded.IsValid() || decoded != value {
			t.Fatalf("SnapshotConsistency %s", encoded)
		}
	}
	if SnapshotConsistency("invalid").IsValid() || SnapshotConsistency("").IsValid() {
		t.Fatal("SnapshotConsistency accepted invalid value")
	}
	for _, value := range []ObservationConfidence{ObservationConfidenceObserved} {
		encoded, _ := json.Marshal(value)
		var decoded ObservationConfidence
		if err := json.Unmarshal(encoded, &decoded); err != nil || !decoded.IsValid() || decoded != value {
			t.Fatalf("ObservationConfidence %s", encoded)
		}
	}
	if ObservationConfidence("invalid").IsValid() || ObservationConfidence("").IsValid() {
		t.Fatal("ObservationConfidence accepted invalid value")
	}
	for _, value := range []ProbeHTTPMethod{ProbeHTTPMethodGET, ProbeHTTPMethodHEAD, ProbeHTTPMethodPOST, ProbeHTTPMethodPUT, ProbeHTTPMethodPATCH, ProbeHTTPMethodDELETE, ProbeHTTPMethodOPTIONS} {
		encoded, _ := json.Marshal(value)
		var decoded ProbeHTTPMethod
		if err := json.Unmarshal(encoded, &decoded); err != nil || !decoded.IsValid() || decoded != value {
			t.Fatalf("ProbeHTTPMethod %s", encoded)
		}
	}
	if ProbeHTTPMethod("invalid").IsValid() || ProbeHTTPMethod("").IsValid() {
		t.Fatal("ProbeHTTPMethod accepted invalid value")
	}
	for _, value := range []ProbeScheme{ProbeSchemeHTTP, ProbeSchemeHTTPS} {
		encoded, _ := json.Marshal(value)
		var decoded ProbeScheme
		if err := json.Unmarshal(encoded, &decoded); err != nil || !decoded.IsValid() || decoded != value {
			t.Fatalf("ProbeScheme %s", encoded)
		}
	}
	if ProbeScheme("invalid").IsValid() || ProbeScheme("").IsValid() {
		t.Fatal("ProbeScheme accepted invalid value")
	}
	for _, value := range []ProbeCollectionState{ProbeCollectionStateSettled, ProbeCollectionStateWindowEnded, ProbeCollectionStateCancelled, ProbeCollectionStateReadError, ProbeCollectionStateUnknown} {
		encoded, _ := json.Marshal(value)
		var decoded ProbeCollectionState
		if err := json.Unmarshal(encoded, &decoded); err != nil || !decoded.IsValid() || decoded != value {
			t.Fatalf("ProbeCollectionState %s", encoded)
		}
	}
	if ProbeCollectionState("invalid").IsValid() || ProbeCollectionState("").IsValid() {
		t.Fatal("ProbeCollectionState accepted invalid value")
	}
	for _, value := range []ProbeAttemptRelationState{ProbeAttemptRelationStateLinked, ProbeAttemptRelationStateUnconfirmed, ProbeAttemptRelationStateMissingNext, ProbeAttemptRelationStateAmbiguous} {
		encoded, _ := json.Marshal(value)
		var decoded ProbeAttemptRelationState
		if err := json.Unmarshal(encoded, &decoded); err != nil || !decoded.IsValid() || decoded != value {
			t.Fatalf("ProbeAttemptRelationState %s", encoded)
		}
	}
	if ProbeAttemptRelationState("invalid").IsValid() || ProbeAttemptRelationState("").IsValid() {
		t.Fatal("ProbeAttemptRelationState accepted invalid value")
	}
}
