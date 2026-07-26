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
	Address   string
	Mode      string
	ClusterID string
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
	return Config{Address: address, Mode: mode, ClusterID: clusterID}
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
	server := &http.Server{Addr: config.Address, Handler: api.NewHandler(reader), ReadHeaderTimeout: 5 * time.Second}
	errorsCh := make(chan error, 1)
	go func() { errorsCh <- server.ListenAndServe() }()
	fmt.Printf("GateLens is available at http://localhost%s (mode: %s)\n", config.Address, config.Mode)
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
