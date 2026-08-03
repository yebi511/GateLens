package api

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gatelens/gatelens/internal/domain"
	"github.com/gatelens/gatelens/internal/source"
)

type Handler struct {
	store            source.Reader
	allowedOrigins   map[string]struct{}
	snapshotReceiver source.SnapshotReceiver
	agentCommands    source.AgentCommandBroker
	agentToken       string
}

type Option func(*Handler)

func WithAllowedOrigins(origins ...string) Option {
	return func(h *Handler) {
		for _, origin := range origins {
			origin = strings.TrimSpace(origin)
			if origin != "" {
				h.allowedOrigins[origin] = struct{}{}
			}
		}
	}
}

func WithSnapshotReceiver(receiver source.SnapshotReceiver, token string) Option {
	return func(h *Handler) {
		h.snapshotReceiver = receiver
		h.agentToken = token
	}
}

func WithAgentCommandBroker(broker source.AgentCommandBroker, token string) Option {
	return func(h *Handler) {
		h.agentCommands = broker
		h.agentToken = token
	}
}

func NewHandler(store source.Reader, options ...Option) http.Handler {
	h := &Handler{store: store, allowedOrigins: make(map[string]struct{})}
	for _, option := range options {
		option(h)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.health)
	mux.HandleFunc("OPTIONS /api/", h.preflight)
	mux.HandleFunc("GET /api/v1/context", h.context)
	mux.HandleFunc("GET /api/v1/topology", h.topology)
	mux.HandleFunc("GET /api/v1/envoy/config", h.envoyConfig)
	mux.HandleFunc("GET /api/v1/resources", h.resources)
	mux.HandleFunc("GET /api/v1/health/findings", h.findings)
	mux.HandleFunc("POST /api/v1/route-explanations", h.explain)
	mux.HandleFunc("/", h.notFound)
	mux.HandleFunc("POST /api/v1/agent/snapshots", h.receiveSnapshot)
	mux.HandleFunc("GET /api/v1/agent/commands/next", h.nextAgentCommand)
	mux.HandleFunc("POST /api/v1/agent/command-results", h.completeAgentCommand)
	return logging(cors(mux, h.allowedOrigins))
}

func (h *Handler) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
func (h *Handler) preflight(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}
func (h *Handler) notFound(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotFound, "not found")
}
func (h *Handler) context(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, h.store.Context())
}
func (h *Handler) topology(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, h.store.Topology())
}
func (h *Handler) envoyConfig(w http.ResponseWriter, r *http.Request) {
	gatewayID := r.URL.Query().Get("gatewayID")
	if gatewayID == "" {
		writeError(w, http.StatusBadRequest, "gatewayID is required")
		return
	}
	config, err := h.store.EnvoyConfig(r.Context(), gatewayID)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, config)
}
func (h *Handler) resources(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.store.Resources(r.URL.Query().Get("q")))
}
func (h *Handler) findings(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, h.store.Findings())
}
func (h *Handler) explain(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var request domain.RouteExplanationRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON request")
		return
	}
	if request.Path == "" || request.Host == "" {
		writeError(w, http.StatusBadRequest, "host and path are required")
		return
	}
	writeJSON(w, http.StatusOK, h.store.Explain(request))
}
func (h *Handler) receiveSnapshot(w http.ResponseWriter, r *http.Request) {
	if h.snapshotReceiver == nil {
		writeError(w, http.StatusNotFound, "snapshot receiver is not enabled")
		return
	}
	if !h.authorizeAgent(w, r) {
		return
	}
	defer r.Body.Close()
	var snapshot domain.AgentSnapshot
	decoder := json.NewDecoder(io.LimitReader(r.Body, 16<<20))
	if err := decoder.Decode(&snapshot); err != nil {
		writeError(w, http.StatusBadRequest, "invalid agent snapshot")
		return
	}
	if err := h.snapshotReceiver.ReceiveSnapshot(r.Context(), snapshot); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{
		"clusterID":  snapshot.Cluster.ID,
		"snapshotID": snapshot.Topology.SnapshotID,
		"status":     "accepted",
	})
}
func (h *Handler) nextAgentCommand(w http.ResponseWriter, r *http.Request) {
	if h.agentCommands == nil {
		writeError(w, http.StatusNotFound, "agent command broker is not enabled")
		return
	}
	if !h.authorizeAgent(w, r) {
		return
	}
	clusterID := strings.TrimSpace(r.URL.Query().Get("clusterID"))
	if clusterID == "" {
		writeError(w, http.StatusBadRequest, "clusterID is required")
		return
	}
	command, ok, err := h.agentCommands.NextAgentCommand(r.Context(), clusterID)
	if err != nil {
		if r.Context().Err() == nil {
			writeError(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, command)
}

func (h *Handler) completeAgentCommand(w http.ResponseWriter, r *http.Request) {
	if h.agentCommands == nil {
		writeError(w, http.StatusNotFound, "agent command broker is not enabled")
		return
	}
	if !h.authorizeAgent(w, r) {
		return
	}
	defer r.Body.Close()
	var result domain.AgentCommandResult
	if err := json.NewDecoder(io.LimitReader(r.Body, 32<<20)).Decode(&result); err != nil {
		writeError(w, http.StatusBadRequest, "invalid agent command result")
		return
	}
	if err := h.agentCommands.CompleteAgentCommand(r.Context(), result); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{
		"commandID": result.CommandID,
		"status":    "accepted",
	})
}

func (h *Handler) authorizeAgent(w http.ResponseWriter, r *http.Request) bool {
	if h.agentToken == "" {
		return true
	}
	provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if subtle.ConstantTimeCompare([]byte(provided), []byte(h.agentToken)) == 1 {
		return true
	}
	writeError(w, http.StatusUnauthorized, "invalid agent credentials")
	return false
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/healthz" {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}
func cors(next http.Handler, allowedOrigins map[string]struct{}) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			w.Header().Add("Vary", "Origin")
		}
		if _, allowed := allowedOrigins[origin]; origin != "" && allowed {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Vary", "Origin")
		}
		next.ServeHTTP(w, r)
	})
}
