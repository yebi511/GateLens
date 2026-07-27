package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gatelens/gatelens/internal/api"
	"github.com/gatelens/gatelens/internal/demo"
	"github.com/gatelens/gatelens/internal/kube"
	"github.com/gatelens/gatelens/internal/source"
)

type Config struct {
	Address        string
	Mode           string
	ClusterID      string
	AllowedOrigins []string
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
	allowedOrigins := splitCommaSeparated(os.Getenv("GATELENS_ALLOWED_ORIGINS"))
	return Config{Address: address, Mode: mode, ClusterID: clusterID, AllowedOrigins: allowedOrigins}
}

func Run(ctx context.Context, config Config) error {
	var reader source.Reader
	if config.Mode == "demo" {
		reader = demo.NewStore()
	} else {
		store, err := kube.NewInCluster(config.ClusterID)
		if err != nil {
			return err
		}
		reader = store
		go func() { _ = store.Run(ctx) }()
	}
	server := &http.Server{
		Addr:              config.Address,
		Handler:           api.NewHandler(reader, api.WithAllowedOrigins(config.AllowedOrigins...)),
		ReadHeaderTimeout: 5 * time.Second,
	}
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
