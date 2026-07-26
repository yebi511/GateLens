package api

import (
	"embed"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"strings"

	"github.com/gatelens/gatelens/internal/domain"
	"github.com/gatelens/gatelens/internal/source"
)

//go:embed all:web
var assets embed.FS

type Handler struct {
	store  source.Reader
	static http.Handler
}

func NewHandler(store source.Reader) http.Handler {
	staticFiles, err := fs.Sub(assets, "web")
	if err != nil {
		panic(err)
	}
	h := &Handler{store: store, static: http.FileServer(http.FS(staticFiles))}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/context", h.context)
	mux.HandleFunc("GET /api/v1/topology", h.topology)
	mux.HandleFunc("GET /api/v1/envoy/config", h.envoyConfig)
	mux.HandleFunc("GET /api/v1/resources", h.resources)
	mux.HandleFunc("GET /api/v1/health/findings", h.findings)
	mux.HandleFunc("POST /api/v1/route-explanations", h.explain)
	mux.Handle("/", h.static)
	return logging(mux)
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
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}
