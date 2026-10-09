package domain

import (
	"errors"
	"fmt"
)

// ProbeSchemaVersion identifies the coordinated Agent/Server/UI probe contract.
const ProbeSchemaVersion = 2

// ProbeExecutionState defines the supported ProbeExecutionState wire values.
type ProbeExecutionState string

const (
	ProbeExecutionStateRunning   ProbeExecutionState = "running"   // 请求执行中
	ProbeExecutionStateCompleted ProbeExecutionState = "completed" // 请求已执行完成，HTTP 状态另行判断
	ProbeExecutionStateFailed    ProbeExecutionState = "failed"    // 请求或调度失败
)

func (value ProbeExecutionState) IsValid() bool {
	switch value {
	case ProbeExecutionStateRunning, ProbeExecutionStateCompleted, ProbeExecutionStateFailed:
		return true
	}
	return false
}

// ProbeIssueScope defines the supported ProbeIssueScope wire values.
type ProbeIssueScope string

const (
	ProbeIssueScopeExecution  ProbeIssueScope = "execution"  // 执行层问题
	ProbeIssueScopeCollection ProbeIssueScope = "collection" // 日志采集问题
	ProbeIssueScopeRelation   ProbeIssueScope = "relation"   // 记录关系问题
	ProbeIssueScopeInference  ProbeIssueScope = "inference"  // 候选推断问题
)

func (value ProbeIssueScope) IsValid() bool {
	switch value {
	case ProbeIssueScopeExecution, ProbeIssueScopeCollection, ProbeIssueScopeRelation, ProbeIssueScopeInference:
		return true
	}
	return false
}

// ProbeIssueCode defines the supported ProbeIssueCode wire values.
type ProbeIssueCode string

const (
	ProbeIssueCodeResponseLimitExceeded     ProbeIssueCode = "response-limit-exceeded"     // 响应读取超过上限
	ProbeIssueCodeLogSourceRotated          ProbeIssueCode = "log-source-rotated"          // 日志源轮转或截断
	ProbeIssueCodeLogWindowTruncated        ProbeIssueCode = "log-window-truncated"        // 读取窗口截断
	ProbeIssueCodeLogOrderUnconfirmed       ProbeIssueCode = "log-order-unconfirmed"       // 日志源顺序未确认
	ProbeIssueCodeCollectionWindowEnded     ProbeIssueCode = "collection-window-ended"     // 采集窗口结束
	ProbeIssueCodeCollectionCancelled       ProbeIssueCode = "collection-cancelled"        // 采集取消
	ProbeIssueCodeLogReadError              ProbeIssueCode = "log-read-error"              // 读取日志失败
	ProbeIssueCodeNoMatchingLog             ProbeIssueCode = "no-matching-log"             // 没有匹配日志
	ProbeIssueCodeAgentUnavailable          ProbeIssueCode = "agent-unavailable"           // Agent 命令不可用
	ProbeIssueCodeProbeProtocolUnsupported  ProbeIssueCode = "probe-protocol-unsupported"  // 探测协议不支持或结果无效
	ProbeIssueCodeRequestIdentityIncomplete ProbeIssueCode = "request-identity-incomplete" // 请求身份不完整
	ProbeIssueCodeAttemptOrderUnconfirmed   ProbeIssueCode = "attempt-order-unconfirmed"   // 尝试顺序未确认
	ProbeIssueCodeRedirectNextMissing       ProbeIssueCode = "redirect-next-missing"       // 重定向后继缺失
	ProbeIssueCodeTerminalAmbiguous         ProbeIssueCode = "terminal-ambiguous"          // 终止候选或重入有歧义
	ProbeIssueCodeGatewayEvidenceMissing    ProbeIssueCode = "gateway-evidence-missing"    // 候选网关缺少运行时证据
)

func (value ProbeIssueCode) IsValid() bool {
	switch value {
	case ProbeIssueCodeResponseLimitExceeded, ProbeIssueCodeLogSourceRotated, ProbeIssueCodeLogWindowTruncated, ProbeIssueCodeLogOrderUnconfirmed, ProbeIssueCodeCollectionWindowEnded, ProbeIssueCodeCollectionCancelled, ProbeIssueCodeLogReadError, ProbeIssueCodeNoMatchingLog, ProbeIssueCodeAgentUnavailable, ProbeIssueCodeProbeProtocolUnsupported, ProbeIssueCodeRequestIdentityIncomplete, ProbeIssueCodeAttemptOrderUnconfirmed, ProbeIssueCodeRedirectNextMissing, ProbeIssueCodeTerminalAmbiguous, ProbeIssueCodeGatewayEvidenceMissing:
		return true
	}
	return false
}

// ProbeCorrelation defines the supported ProbeCorrelation wire values.
type ProbeCorrelation string

const (
	ProbeCorrelationProbeID ProbeCorrelation = "probe-id" // Probe ID 精确匹配
	ProbeCorrelationTraceID ProbeCorrelation = "trace-id" // Trace ID 兜底匹配
)

func (value ProbeCorrelation) IsValid() bool {
	switch value {
	case ProbeCorrelationProbeID, ProbeCorrelationTraceID:
		return true
	}
	return false
}

// ProbeInferenceConfidence defines the supported ProbeInferenceConfidence wire values.
type ProbeInferenceConfidence string

const (
	ProbeInferenceConfidenceHigh      ProbeInferenceConfidence = "high"      // 地址精确匹配
	ProbeInferenceConfidenceMedium    ProbeInferenceConfidence = "medium"    // 配置服务匹配
	ProbeInferenceConfidenceLow       ProbeInferenceConfidence = "low"       // 声明关系推断
	ProbeInferenceConfidenceAmbiguous ProbeInferenceConfidence = "ambiguous" // 存在多个匹配
)

func (value ProbeInferenceConfidence) IsValid() bool {
	switch value {
	case ProbeInferenceConfidenceHigh, ProbeInferenceConfidenceMedium, ProbeInferenceConfidenceLow, ProbeInferenceConfidenceAmbiguous:
		return true
	}
	return false
}

// ProbeOrderBasis defines the supported ProbeOrderBasis wire values.
type ProbeOrderBasis string

const (
	ProbeOrderBasisSourceSequence ProbeOrderBasis = "source-sequence" // 同一来源的记录顺序
	ProbeOrderBasisUnavailable    ProbeOrderBasis = "unavailable"     // 无可比顺序
)

func (value ProbeOrderBasis) IsValid() bool {
	switch value {
	case ProbeOrderBasisSourceSequence, ProbeOrderBasisUnavailable:
		return true
	}
	return false
}

// AgentCommandKind defines the supported AgentCommandKind wire values.
type AgentCommandKind string

const (
	AgentCommandEnvoyConfig  AgentCommandKind = "envoy-config"  // 读取 Envoy 配置
	AgentCommandProbeHTTP    AgentCommandKind = "probe-http"    // 发出真实探测请求
	AgentCommandProbeObserve AgentCommandKind = "probe-observe" // 只读关联日志
)

func (value AgentCommandKind) IsValid() bool {
	switch value {
	case AgentCommandEnvoyConfig, AgentCommandProbeHTTP, AgentCommandProbeObserve:
		return true
	}
	return false
}

// ExtProcOutcome defines the supported ExtProcOutcome wire values.
type ExtProcOutcome string

const (
	ExtProcOutcomeSuccess           ExtProcOutcome = "success"            // 处理成功
	ExtProcOutcomeTimeout           ExtProcOutcome = "timeout"            // 处理超时
	ExtProcOutcomeError             ExtProcOutcome = "error"              // 处理错误
	ExtProcOutcomeFailOpen          ExtProcOutcome = "fail-open"          // 失败后放行
	ExtProcOutcomeImmediateResponse ExtProcOutcome = "immediate-response" // 处理器直接返回响应
	ExtProcOutcomeUnknown           ExtProcOutcome = "unknown"            // 处理结果未知
)

func (value ExtProcOutcome) IsValid() bool {
	switch value {
	case ExtProcOutcomeSuccess, ExtProcOutcomeTimeout, ExtProcOutcomeError, ExtProcOutcomeFailOpen, ExtProcOutcomeImmediateResponse, ExtProcOutcomeUnknown:
		return true
	}
	return false
}

// SnapshotConsistency defines the supported SnapshotConsistency wire values.
type SnapshotConsistency string

const (
	SnapshotConsistencySingleCluster     SnapshotConsistency = "single-cluster"     // 单集群快照
	SnapshotConsistencyWaitingForAgents  SnapshotConsistency = "waiting-for-agents" // 等待 Agent
	SnapshotConsistencyConsistentWindow  SnapshotConsistency = "consistent-window"  // 快照在一致性时间窗内
	SnapshotConsistencyRemoteUnavailable SnapshotConsistency = "remote-unavailable" // 远端不可用
	SnapshotConsistencyTimeSkew          SnapshotConsistency = "time-skew"          // 快照时间偏差
)

func (value SnapshotConsistency) IsValid() bool {
	switch value {
	case SnapshotConsistencySingleCluster, SnapshotConsistencyWaitingForAgents, SnapshotConsistencyConsistentWindow, SnapshotConsistencyRemoteUnavailable, SnapshotConsistencyTimeSkew:
		return true
	}
	return false
}

// ObservationConfidence defines the supported ObservationConfidence wire values.
type ObservationConfidence string

const (
	ObservationConfidenceObserved ObservationConfidence = "observed" // 直接日志观测
)

func (value ObservationConfidence) IsValid() bool {
	switch value {
	case ObservationConfidenceObserved:
		return true
	}
	return false
}

// ProbeHTTPMethod defines the supported ProbeHTTPMethod wire values.
type ProbeHTTPMethod string

const (
	ProbeHTTPMethodGET     ProbeHTTPMethod = "GET"     // 探测允许的 HTTP 方法
	ProbeHTTPMethodHEAD    ProbeHTTPMethod = "HEAD"    // 探测允许的 HTTP 方法
	ProbeHTTPMethodPOST    ProbeHTTPMethod = "POST"    // 探测允许的 HTTP 方法
	ProbeHTTPMethodPUT     ProbeHTTPMethod = "PUT"     // 探测允许的 HTTP 方法
	ProbeHTTPMethodPATCH   ProbeHTTPMethod = "PATCH"   // 探测允许的 HTTP 方法
	ProbeHTTPMethodDELETE  ProbeHTTPMethod = "DELETE"  // 探测允许的 HTTP 方法
	ProbeHTTPMethodOPTIONS ProbeHTTPMethod = "OPTIONS" // 探测允许的 HTTP 方法
)

func (value ProbeHTTPMethod) IsValid() bool {
	switch value {
	case ProbeHTTPMethodGET, ProbeHTTPMethodHEAD, ProbeHTTPMethodPOST, ProbeHTTPMethodPUT, ProbeHTTPMethodPATCH, ProbeHTTPMethodDELETE, ProbeHTTPMethodOPTIONS:
		return true
	}
	return false
}

// ProbeScheme defines the supported ProbeScheme wire values.
type ProbeScheme string

const (
	ProbeSchemeHTTP  ProbeScheme = "http"  // HTTP 入口
	ProbeSchemeHTTPS ProbeScheme = "https" // HTTPS 入口
)

func (value ProbeScheme) IsValid() bool {
	switch value {
	case ProbeSchemeHTTP, ProbeSchemeHTTPS:
		return true
	}
	return false
}

func (value ProbeCollectionState) IsValid() bool {
	switch value {
	case ProbeCollectionStateSettled, ProbeCollectionStateWindowEnded, ProbeCollectionStateCancelled, ProbeCollectionStateReadError, ProbeCollectionStateUnknown:
		return true
	}
	return false
}

func (value ProbeAttemptRelationState) IsValid() bool {
	switch value {
	case ProbeAttemptRelationStateLinked, ProbeAttemptRelationStateUnconfirmed, ProbeAttemptRelationStateMissingNext, ProbeAttemptRelationStateAmbiguous:
		return true
	}
	return false
}

// ProbeIssue is stored once, at its execution or gateway owner.
type ProbeIssue struct {
	Scope     ProbeIssueScope `json:"scope"`
	Code      ProbeIssueCode  `json:"code"`
	Message   string          `json:"message"`
	ContextID string          `json:"contextID,omitempty"`
	HopID     string          `json:"hopID,omitempty"`
}

// AppendProbeIssue deduplicates an issue by identity, never by display wording.
func AppendProbeIssue(issues []ProbeIssue, issue ProbeIssue) []ProbeIssue {
	for _, current := range issues {
		if current.Scope == issue.Scope && current.Code == issue.Code && current.ContextID == issue.ContextID && current.HopID == issue.HopID {
			return issues
		}
	}
	return append(issues, issue)
}

func (issue ProbeIssue) BreaksLogOrder() bool {
	switch issue.Code {
	case ProbeIssueCodeLogSourceRotated, ProbeIssueCodeLogWindowTruncated, ProbeIssueCodeLogOrderUnconfirmed:
		return true
	}
	return false
}

var ErrProbeProtocolUnsupported = errors.New("probe protocol version is not supported")
var ErrInvalidProbeResult = errors.New("invalid probe result")

// Validate checks closed wire values and command ownership without requiring unavailable log metadata.
func (result ProbeAgentResult) Validate(command AgentCommand) error {
	if result.SchemaVersion != ProbeSchemaVersion {
		return fmt.Errorf("%w: %d", ErrProbeProtocolUnsupported, result.SchemaVersion)
	}
	if (command.Kind != AgentCommandProbeHTTP && command.Kind != AgentCommandProbeObserve) || command.Probe == nil || result.ProbeID != command.Probe.ProbeID || result.TraceID != command.Probe.TraceID || result.ClusterID != command.ClusterID || result.GatewayID != command.GatewayID {
		return fmt.Errorf("%w: command identity mismatch", ErrInvalidProbeResult)
	}
	if result.Collection == nil || !result.Collection.State.IsValid() {
		return fmt.Errorf("%w: collection state", ErrInvalidProbeResult)
	}
	if (command.Kind == AgentCommandProbeHTTP) != (result.HTTP != nil) {
		return fmt.Errorf("%w: HTTP payload does not match command", ErrInvalidProbeResult)
	}
	if result.HTTP != nil && (!result.HTTP.Method.IsValid() || result.HTTP.Method != command.Probe.Method) {
		return fmt.Errorf("%w: HTTP method", ErrInvalidProbeResult)
	}
	seen := map[string]bool{}
	for _, hop := range result.Hops {
		if hop.ID == "" || seen[hop.ID] || hop.ClusterID != result.ClusterID {
			return fmt.Errorf("%w: record identity", ErrInvalidProbeResult)
		}
		seen[hop.ID] = true
		if (hop.Correlation != "" && !hop.Correlation.IsValid()) || !hop.Confidence.IsValid() {
			return fmt.Errorf("%w: record correlation/confidence", ErrInvalidProbeResult)
		}
		for _, proc := range hop.ExtProcs {
			if !proc.Outcome.IsValid() {
				return fmt.Errorf("%w: ext_proc outcome", ErrInvalidProbeResult)
			}
		}
	}
	for _, issue := range result.Issues {
		if (issue.HopID != "" && !seen[issue.HopID]) || !issue.Scope.IsValid() || !issue.Code.IsValid() || issue.Scope == ProbeIssueScopeRelation || issue.Scope == ProbeIssueScopeInference || (issue.Scope == ProbeIssueScopeExecution && result.HTTP == nil) {
			return fmt.Errorf("%w: issue", ErrInvalidProbeResult)
		}
	}
	return nil
}
