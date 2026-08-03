package kube

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
)

func TestRunBuildsSnapshotWithoutOptionalCRDs(t *testing.T) {
	core := kubernetesfake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}})
	store := &Store{
		clusterID:  "core-only",
		core:       core,
		dynamic:    dynamicfake.NewSimpleDynamicClient(runtime.NewScheme()),
		envoyCache: map[string]cachedEnvoyConfig{},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- store.Run(ctx) }()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for store.Context().Snapshot.ID == "" {
		select {
		case <-ctx.Done():
			t.Fatal("core-only snapshot was not ready before timeout")
		case <-ticker.C:
		}
	}

	got := store.Context()
	if got.Cluster.ID != "core-only" || got.Snapshot.State != "complete" {
		t.Fatalf("context=%#v", got)
	}
	for _, capability := range got.Capabilities {
		if capability == "gateway-api" || capability == "reference-grant" || capability == "higress-mcpbridge" {
			t.Fatalf("unexpected optional capability %q", capability)
		}
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("store did not stop")
	}
}
