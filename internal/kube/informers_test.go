package kube

import (
	"context"
	"errors"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
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

func TestRunBuildsSnapshotWhenAuxiliaryInformerCannotList(t *testing.T) {
	core := kubernetesfake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}})
	core.PrependReactor("list", "ingresses", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(
			schema.GroupResource{Group: "networking.k8s.io", Resource: "ingresses"},
			"", errors.New("denied by test RBAC"),
		)
	})
	store := &Store{
		clusterID:  "restricted-auxiliary",
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
			t.Fatal("core snapshot was blocked by an auxiliary informer")
		case <-ticker.C:
		}
	}

	if got := store.Context(); got.Cluster.ID != "restricted-auxiliary" || got.Snapshot.State != "complete" {
		t.Fatalf("context=%#v", got)
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
