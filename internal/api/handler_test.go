package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gatelens/gatelens/internal/demo"
)

func TestDemoAPI(t *testing.T) {
	handler := NewHandler(demo.NewStore())

	tests := []struct {
		name   string
		method string
		path   string
		body   string
		want   int
	}{
		{name: "context", method: http.MethodGet, path: "/api/v1/context", want: http.StatusOK},
		{name: "topology", method: http.MethodGet, path: "/api/v1/topology", want: http.StatusOK},
		{name: "envoy config", method: http.MethodGet, path: "/api/v1/envoy/config?gatewayID=gateway/ai-platform/ai-public-gateway", want: http.StatusOK},
		{name: "envoy config requires gateway", method: http.MethodGet, path: "/api/v1/envoy/config", want: http.StatusBadRequest},
		{name: "resources", method: http.MethodGet, path: "/api/v1/resources?q=qwen", want: http.StatusOK},
		{name: "findings", method: http.MethodGet, path: "/api/v1/health/findings", want: http.StatusOK},
		{name: "routed explanation", method: http.MethodPost, path: "/api/v1/route-explanations", body: `{"host":"api.ai.example.com","path":"/v1/chat/completions"}`, want: http.StatusOK},
		{name: "invalid explanation", method: http.MethodPost, path: "/api/v1/route-explanations", body: `{}`, want: http.StatusBadRequest},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, bytes.NewBufferString(test.body))
			if test.body != "" {
				request.Header.Set("Content-Type", "application/json")
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, test.want, response.Body.String())
			}
		})
	}
}
