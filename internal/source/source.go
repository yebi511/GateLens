package source

import "github.com/gatelens/gatelens/internal/domain"

// Reader is the only data dependency exposed to the HTTP layer.
// Demo and Kubernetes-backed implementations intentionally share this contract.
type Reader interface {
	Context() domain.Context
	Topology() domain.Topology
	Findings() []domain.Finding
	Resources(query string) []domain.Resource
	Explain(domain.RouteExplanationRequest) domain.RouteExplanation
}
