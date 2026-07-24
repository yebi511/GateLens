package kube

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/informers"
)

type dynamicInformerFactory interface {
	ForResource(schema.GroupVersionResource) informers.GenericInformer
	Start(<-chan struct{})
}

func newDynamicInformerFactory(client dynamic.Interface) dynamicInformerFactory {
	return dynamicinformer.NewDynamicSharedInformerFactory(client, 0)
}
