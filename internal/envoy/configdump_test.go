package envoy

import (
	"testing"

	"github.com/gatelens/gatelens/internal/domain"
)

func TestParseConfigDump(t *testing.T) {
	socket := map[string]any{"address": "10.0.0.8", "port_value": float64(8080)}
	endpoint := map[string]any{"address": map[string]any{"socket_address": socket}}
	lbEndpoint := map[string]any{"endpoint": endpoint, "health_status": "HEALTHY"}
	assignment := map[string]any{"endpoints": []any{map[string]any{"lb_endpoints": []any{lbEndpoint}}}}
	route := map[string]any{"name": "chat", "match": map[string]any{"prefix": "/v1"}, "route": map[string]any{"cluster": "api-cluster"}}
	virtualHost := map[string]any{"name": "api", "routes": []any{route}}
	hcm := map[string]any{"stat_prefix": "ingress", "http_filters": []any{map[string]any{"name": "envoy.filters.http.router"}}, "route_config": map[string]any{"virtual_hosts": []any{virtualHost}}}
	listener := map[string]any{"name": "listener_0", "address": map[string]any{"socket_address": map[string]any{"address": "0.0.0.0", "port_value": float64(8080)}}, "filter_chains": []any{map[string]any{"name": "https", "filters": []any{map[string]any{"name": "envoy.filters.network.http_connection_manager", "typed_config": hcm}}}}}
	cluster := map[string]any{"name": "api-cluster", "type": "EDS", "load_assignment": assignment}
	dump := map[string]any{"configs": []any{
		map[string]any{"static_listeners": []any{map[string]any{"listener": listener}}},
		map[string]any{"dynamic_active_clusters": []any{map[string]any{"cluster": cluster}}},
	}}
	result := Parse(dump, "snap-1", "2026-07-26T00:00:00Z")
	if len(result.Listeners) != 1 || len(result.Clusters) != 1 {
		t.Fatalf("listeners=%d clusters=%d", len(result.Listeners), len(result.Clusters))
	}
	chain := result.Listeners[0].FilterChains[0]
	if len(chain.HTTPFilters) != 1 || len(chain.Routes) != 1 || chain.Routes[0].Cluster != "api-cluster" {
		t.Fatalf("chain=%#v", chain)
	}
	if result.Clusters[0].Endpoints[0].Status != domain.StatusHealthy {
		t.Fatalf("endpoint=%#v", result.Clusters[0].Endpoints[0])
	}
}

func TestParseDynamicRDSRouteConfig(t *testing.T) {
	routeConfig := map[string]any{"name": "https-routes", "virtual_hosts": []any{map[string]any{"name": "api", "routes": []any{map[string]any{"name": "chat", "match": map[string]any{"prefix": "/v1"}, "route": map[string]any{"cluster": "api-cluster"}}}}}}
	hcm := map[string]any{"rds": map[string]any{"route_config_name": "https-routes"}, "http_filters": []any{map[string]any{"name": "envoy.filters.http.router"}}}
	listener := map[string]any{"name": "listener_0", "filter_chains": []any{map[string]any{"filters": []any{map[string]any{"name": "envoy.filters.network.http_connection_manager", "typed_config": hcm}}}}}
	dump := map[string]any{"configs": []any{
		map[string]any{"dynamic_listeners": []any{map[string]any{"active_state": map[string]any{"listener": listener}}}},
		map[string]any{"dynamic_route_configs": []any{map[string]any{"route_config": routeConfig}}},
	}}
	result := Parse(dump, "snap-rds", "2026-07-26T00:00:00Z")
	routes := result.Listeners[0].FilterChains[0].Routes
	if len(routes) != 1 || routes[0].Cluster != "api-cluster" {
		t.Fatalf("routes=%#v", routes)
	}
}
func TestParseEDSAssignmentsAndWeightedClusters(t *testing.T) {
	weighted := map[string]any{"clusters": []any{
		map[string]any{"name": "api-v1", "weight": float64(80)},
		map[string]any{"name": "api-v2", "weight": float64(20)},
	}}
	route := map[string]any{"name": "split", "match": map[string]any{"prefix": "/v1"}, "route": map[string]any{"weighted_clusters": weighted}}
	hcm := map[string]any{
		"route_config": map[string]any{"virtual_hosts": []any{map[string]any{"name": "api", "domains": []any{"api.example.com"}, "routes": []any{route}}}},
		"http_filters": []any{map[string]any{"name": "envoy.filters.http.router"}},
	}
	chain := map[string]any{
		"name":               "tls-api",
		"filter_chain_match": map[string]any{"server_names": []any{"api.example.com"}, "transport_protocol": "tls"},
		"transport_socket":   map[string]any{"name": "envoy.transport_sockets.tls"},
		"filters":            []any{map[string]any{"name": "envoy.filters.network.http_connection_manager", "typed_config": hcm}},
	}
	listener := map[string]any{"name": "https", "filter_chains": []any{chain}}
	cluster := func(name string) map[string]any {
		return map[string]any{"name": name, "type": "EDS", "eds_cluster_config": map[string]any{}}
	}
	lbEndpoint := map[string]any{
		"endpoint":      map[string]any{"address": map[string]any{"socket_address": map[string]any{"address": "10.0.0.9", "port_value": float64(8080)}}},
		"health_status": "UNHEALTHY",
	}
	assignment := map[string]any{"cluster_name": "api-v1", "endpoints": []any{map[string]any{"lb_endpoints": []any{lbEndpoint}}}}
	dump := map[string]any{"configs": []any{
		map[string]any{"dynamic_listeners": []any{map[string]any{"active_state": map[string]any{"listener": listener}}}},
		map[string]any{"dynamic_active_clusters": []any{map[string]any{"cluster": cluster("api-v1")}, map[string]any{"cluster": cluster("api-v2")}}},
		map[string]any{"dynamic_endpoint_configs": []any{map[string]any{"endpoint_config": assignment}}},
	}}

	result := Parse(dump, "snap-eds", "2026-07-26T00:00:00Z")
	listenerResult := result.Listeners[0]
	if listenerResult.Protocol != "HTTPS" || listenerResult.FilterChains[0].Match != "SNI: api.example.com · transport: tls" {
		t.Fatalf("listener=%#v", listenerResult)
	}
	routeResult := listenerResult.FilterChains[0].Routes[0]
	if len(routeResult.WeightedClusters) != 2 || routeResult.WeightedClusters[0].Weight != 80 {
		t.Fatalf("route=%#v", routeResult)
	}
	if len(result.Clusters[0].Endpoints) != 1 || result.Clusters[0].Endpoints[0].Status != domain.StatusError {
		t.Fatalf("cluster=%#v", result.Clusters[0])
	}
}
