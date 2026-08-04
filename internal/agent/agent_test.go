package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gatelens/gatelens/internal/demo"
	"github.com/gatelens/gatelens/internal/domain"
	"github.com/gatelens/gatelens/internal/source"
)

type notReadyReader struct{ source.Reader }

func (notReadyReader) Context() domain.Context   { return domain.Context{} }
func (notReadyReader) Topology() domain.Topology { return domain.Topology{} }

func TestSendOnceUploadsSnapshotWithCredentials(t *testing.T) {
	received := make(chan domain.AgentSnapshot, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/agent/snapshots" {
			t.Errorf("request=%s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization=%q", got)
		}
		var payload domain.AgentSnapshot
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		received <- payload
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	runner, err := New(demo.NewStore(), Config{
		ServerURL: server.URL, Token: "test-token", ClusterID: "gpu-prod",
		ClusterName: "GPU Production", Interval: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runner.SendOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	payload := <-received
	if payload.Cluster.ID != "gpu-prod" || payload.Cluster.Name != "GPU Production" {
		t.Fatalf("cluster=%#v", payload.Cluster)
	}
	if payload.Topology.SnapshotID == "" || payload.SentAt == "" {
		t.Fatalf("payload=%#v", payload)
	}
}

func TestNewRequiresServerURL(t *testing.T) {
	if _, err := New(demo.NewStore(), Config{ClusterID: "test"}); err == nil {
		t.Fatal("expected missing server URL error")
	}
}

func TestRunDoesNotPollCommandsBeforeFirstSnapshot(t *testing.T) {
	requests := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		select {
		case requests <- struct{}{}:
		default:
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	runner, err := New(notReadyReader{}, Config{ServerURL: server.URL, ClusterID: "gpu-prod", Interval: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := runner.Run(ctx); err != nil {
		t.Fatal(err)
	}

	select {
	case <-requests:
		t.Fatal("agent sent an HTTP request before its first local snapshot was ready")
	default:
	}
}

func TestAgentExecutesAndReturnsEnvoyCommand(t *testing.T) {
	results := make(chan domain.AgentCommandResult, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization=%q", got)
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/agent/commands/next":
			if got := r.URL.Query().Get("clusterID"); got != "gpu-prod" {
				t.Errorf("clusterID=%q", got)
			}
			_ = json.NewEncoder(w).Encode(domain.AgentCommand{
				ID:        "command-1",
				ClusterID: "gpu-prod",
				Kind:      domain.AgentCommandEnvoyConfig,
				GatewayID: "gateway/ai-platform/ai-public-gateway",
				Deadline:  time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano),
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/agent/command-results":
			var result domain.AgentCommandResult
			if err := json.NewDecoder(r.Body).Decode(&result); err != nil {
				t.Errorf("decode result: %v", err)
			}
			results <- result
			w.WriteHeader(http.StatusAccepted)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	runner, err := New(demo.NewStore(), Config{
		ServerURL: server.URL,
		Token:     "test-token",
		ClusterID: "gpu-prod",
	})
	if err != nil {
		t.Fatal(err)
	}
	command, ok, err := runner.nextCommand(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected command")
	}
	result := runner.executeCommand(context.Background(), command)
	if result.Error != "" || result.Config == nil {
		t.Fatalf("result=%#v", result)
	}
	if err := runner.sendCommandResult(context.Background(), result); err != nil {
		t.Fatal(err)
	}

	received := <-results
	if received.CommandID != "command-1" || received.ClusterID != "gpu-prod" || received.Config == nil {
		t.Fatalf("received=%#v", received)
	}
}
