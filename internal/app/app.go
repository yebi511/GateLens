package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gatelens/gatelens/internal/agent"
	"github.com/gatelens/gatelens/internal/api"
	"github.com/gatelens/gatelens/internal/demo"
	"github.com/gatelens/gatelens/internal/federation"
	"github.com/gatelens/gatelens/internal/kube"
	"github.com/gatelens/gatelens/internal/source"
)

type Config struct {
	Address        string
	Mode           string
	ClusterID      string
	AllowedOrigins []string
	AgentServerURL string
	AgentToken     string
	ClusterName    string
	AgentInterval  time.Duration
	StaleAfter     time.Duration
}

func ConfigFromEnv() Config {
	address := os.Getenv("GATELENS_ADDR")
	if address == "" {
		address = ":8080"
	}
	mode := strings.ToLower(os.Getenv("GATELENS_MODE"))
	if mode == "" {
		mode = "kubernetes"
	}
	clusterID := os.Getenv("GATELENS_CLUSTER_ID")
	if clusterID == "" {
		clusterID = "in-cluster"
	}
	clusterName := os.Getenv("GATELENS_CLUSTER_NAME")
	agentServerURL := os.Getenv("GATELENS_SERVER_URL")
	agentToken := os.Getenv("GATELENS_AGENT_TOKEN")
	agentInterval := durationFromEnv("GATELENS_AGENT_INTERVAL", 30*time.Second)
	staleAfter := durationFromEnv("GATELENS_STALE_AFTER", 2*time.Minute)
	allowedOrigins := splitCommaSeparated(os.Getenv("GATELENS_ALLOWED_ORIGINS"))
	return Config{Address: address, Mode: mode, ClusterID: clusterID, ClusterName: clusterName, AgentServerURL: agentServerURL, AgentToken: agentToken, AgentInterval: agentInterval, StaleAfter: staleAfter, AllowedOrigins: allowedOrigins}
}

func Run(ctx context.Context, config Config) error {
	var reader source.Reader
	var receiver source.SnapshotReceiver
	var commandBroker source.AgentCommandBroker
	switch config.Mode {
	case "demo":
		reader = demo.NewStore()
	case "server":
		hub := federation.NewStore(config.ClusterID, config.StaleAfter)
		reader, receiver, commandBroker = hub, hub, hub
	case "agent":
		store, err := kube.NewInCluster(config.ClusterID)
		if err != nil {
			return err
		}
		runner, err := agent.New(store, agent.Config{ServerURL: config.AgentServerURL, Token: config.AgentToken, ClusterID: config.ClusterID, ClusterName: config.ClusterName, Interval: config.AgentInterval})
		if err != nil {
			return err
		}
		go func() { _ = store.Run(ctx) }()
		return runner.Run(ctx)
	case "kubernetes":
		store, err := kube.NewInCluster(config.ClusterID)
		if err != nil {
			return err
		}
		reader = store
		go func() { _ = store.Run(ctx) }()
	default:
		return fmt.Errorf("unsupported GATELENS_MODE %q", config.Mode)
	}
	handlerOptions := []api.Option{api.WithAllowedOrigins(config.AllowedOrigins...)}
	if receiver != nil {
		handlerOptions = append(handlerOptions, api.WithSnapshotReceiver(receiver, config.AgentToken))
	}
	if commandBroker != nil {
		handlerOptions = append(handlerOptions, api.WithAgentCommandBroker(commandBroker, config.AgentToken))
	}
	server := &http.Server{Addr: config.Address, Handler: api.NewHandler(reader, handlerOptions...), ReadHeaderTimeout: 5 * time.Second}
	errorsCh := make(chan error, 1)
	go func() { errorsCh <- server.ListenAndServe() }()
	fmt.Printf("GateLens API is available at %s (mode: %s)\n", displayAddress(config.Address), config.Mode)
	select {
	case err := <-errorsCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	}
}

func displayAddress(address string) string {
	if strings.HasPrefix(address, ":") {
		return "http://localhost" + address
	}
	return "http://" + address
}

func splitCommaSeparated(value string) []string {
	var values []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			values = append(values, item)
		}
	}
	return values
}

func durationFromEnv(name string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
