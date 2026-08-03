package kube

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
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
func discoverDynamicResources(client discovery.DiscoveryInterface, resources []schema.GroupVersionResource) map[schema.GroupVersionResource]bool {
	byGroupVersion := map[string][]schema.GroupVersionResource{}
	for _, resource := range resources {
		groupVersion := resource.GroupVersion().String()
		byGroupVersion[groupVersion] = append(byGroupVersion[groupVersion], resource)
	}

	available := map[schema.GroupVersionResource]bool{}
	for groupVersion, candidates := range byGroupVersion {
		resourceList, err := client.ServerResourcesForGroupVersion(groupVersion)
		if err != nil {
			fmt.Printf("GateLens optional Kubernetes API %s is unavailable: %v\n", groupVersion, err)
			continue
		}
		names := map[string]bool{}
		for _, resource := range resourceList.APIResources {
			names[resource.Name] = true
		}
		for _, candidate := range candidates {
			if names[candidate.Resource] {
				available[candidate] = true
			}
		}
	}
	return available
}
