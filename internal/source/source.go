package source

import (
	"context"

	"github.com/gatelens/gatelens/internal/domain"
)

// Reader is the only data dependency exposed to the HTTP layer.
// Demo and Kubernetes-backed implementations intentionally share this contract.
type Reader interface {
	Context() domain.Context
	Topology() domain.Topology
	EnvoyConfig(context.Context, string) (domain.EnvoyConfig, error)
	Findings() []domain.Finding
	Resources(query string) []domain.Resource
}

type SnapshotReceiver interface {
	ReceiveSnapshot(context.Context, domain.AgentSnapshot) error
}

type AgentCommandBroker interface {
	NextAgentCommand(context.Context, string) (domain.AgentCommand, bool, error)
	CompleteAgentCommand(context.Context, domain.AgentCommandResult) error
}

type ProbeExecutor interface {
	ExecuteProbe(context.Context, domain.ProbeCommand) (domain.ProbeAgentResult, error)
	ObserveProbe(context.Context, domain.ProbeCommand) (domain.ProbeAgentResult, error)
}

type ProbeStore interface {
	CreateProbe(context.Context, domain.ProbeRequest) (domain.ProbeExecution, error)
	GetProbe(string) (domain.ProbeExecution, bool)
}
