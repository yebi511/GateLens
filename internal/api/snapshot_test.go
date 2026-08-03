package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gatelens/gatelens/internal/demo"
	"github.com/gatelens/gatelens/internal/domain"
	"github.com/gatelens/gatelens/internal/federation"
)

func TestSnapshotReceiverAuthentication(t *testing.T) {
	store := federation.NewStore("edge", time.Minute)
	handler := NewHandler(store, WithSnapshotReceiver(store, "shared-token"))
	payload := domain.AgentSnapshot{
		Cluster:  domain.TopologyCluster{ID: "gpu", Name: "gpu"},
		Context:  domain.Context{Cluster: domain.Cluster{ID: "gpu", Name: "gpu"}},
		Topology: domain.Topology{SnapshotID: "gpu-1"},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("rejects missing credentials", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/agent/snapshots", bytes.NewReader(body))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("status=%d, want %d", response.Code, http.StatusUnauthorized)
		}
	})

	t.Run("accepts valid credentials", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/agent/snapshots", bytes.NewReader(body))
		request.Header.Set("Authorization", "Bearer shared-token")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusAccepted {
			t.Fatalf("status=%d, want %d; body=%s", response.Code, http.StatusAccepted, response.Body.String())
		}
		if got := len(store.Topology().Clusters); got != 1 {
			t.Fatalf("clusters=%d, want 1", got)
		}
	})
}

func TestSnapshotReceiverIsDisabledByDefault(t *testing.T) {
	handler := NewHandler(demo.NewStore())
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agent/snapshots", bytes.NewBufferString("{}"))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want %d", response.Code, http.StatusNotFound)
	}
}

func TestAgentCommandEndpointsRequireAuthentication(t *testing.T) {
	store := federation.NewStore("edge", time.Minute)
	handler := NewHandler(
		store,
		WithSnapshotReceiver(store, "shared-token"),
		WithAgentCommandBroker(store, "shared-token"),
	)

	tests := []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodGet, path: "/api/v1/agent/commands/next?clusterID=edge"},
		{method: http.MethodPost, path: "/api/v1/agent/command-results", body: "{}"},
	}
	for _, test := range tests {
		request := httptest.NewRequest(test.method, test.path, bytes.NewBufferString(test.body))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s status=%d, want %d", test.method, test.path, response.Code, http.StatusUnauthorized)
		}
	}
}

func TestFederatedEnvoyQueryCompletesThroughAgentEndpoints(t *testing.T) {
	store := federation.NewStore("federation", time.Minute)
	snapshot := domain.AgentSnapshot{
		Cluster: domain.TopologyCluster{ID: "gpu", Name: "gpu"},
		Context: domain.Context{Cluster: domain.Cluster{ID: "gpu", Name: "gpu"}},
		Topology: domain.Topology{
			SnapshotID: "gpu-1",
			Nodes: []domain.TopologyNode{{
				ID: "gateway/inference/gpu-gateway", Kind: "Gateway", ClusterID: "gpu",
			}},
		},
	}
	if err := store.ReceiveSnapshot(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(
		store,
		WithSnapshotReceiver(store, "shared-token"),
		WithAgentCommandBroker(store, "shared-token"),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	browserDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/envoy/config?gatewayID=gpu::gateway/inference/gpu-gateway", nil).WithContext(ctx)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		browserDone <- response
	}()

	nextRequest := httptest.NewRequest(http.MethodGet, "/api/v1/agent/commands/next?clusterID=gpu", nil).WithContext(ctx)
	nextRequest.Header.Set("Authorization", "Bearer shared-token")
	nextResponse := httptest.NewRecorder()
	handler.ServeHTTP(nextResponse, nextRequest)
	if nextResponse.Code != http.StatusOK {
		t.Fatalf("next status=%d body=%s", nextResponse.Code, nextResponse.Body.String())
	}
	var command domain.AgentCommand
	if err := json.NewDecoder(nextResponse.Body).Decode(&command); err != nil {
		t.Fatal(err)
	}
	if command.GatewayID != "gateway/inference/gpu-gateway" {
		t.Fatalf("command=%#v", command)
	}

	config := domain.EnvoyConfig{SnapshotID: "envoy-1", State: "complete"}
	resultBody, err := json.Marshal(domain.AgentCommandResult{
		CommandID: command.ID,
		ClusterID: "gpu",
		Config:    &config,
	})
	if err != nil {
		t.Fatal(err)
	}
	resultRequest := httptest.NewRequest(http.MethodPost, "/api/v1/agent/command-results", bytes.NewReader(resultBody)).WithContext(ctx)
	resultRequest.Header.Set("Authorization", "Bearer shared-token")
	resultResponse := httptest.NewRecorder()
	handler.ServeHTTP(resultResponse, resultRequest)
	if resultResponse.Code != http.StatusAccepted {
		t.Fatalf("result status=%d body=%s", resultResponse.Code, resultResponse.Body.String())
	}

	select {
	case response := <-browserDone:
		if response.Code != http.StatusOK {
			t.Fatalf("Envoy status=%d body=%s", response.Code, response.Body.String())
		}
		var returned domain.EnvoyConfig
		if err := json.NewDecoder(response.Body).Decode(&returned); err != nil {
			t.Fatal(err)
		}
		if returned.GatewayID != "gpu::gateway/inference/gpu-gateway" || returned.SnapshotID != "envoy-1" {
			t.Fatalf("returned=%#v", returned)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
