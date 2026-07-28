package envoy

import (
	"encoding/json"
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
	if len(result.RawConfig) == 0 || !json.Valid(result.RawConfig) {
		t.Fatalf("rawConfig is not valid JSON: %s", result.RawConfig)
	}
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

func TestParseUnnamedListenersHaveDistinctIdentity(t *testing.T) {
	listener := func(port float64) map[string]any {
		return map[string]any{
			"address": map[string]any{
				"socket_address": map[string]any{"address": "0.0.0.0", "port_value": port},
			},
		}
	}
	dump := map[string]any{"configs": []any{
		map[string]any{"static_listeners": []any{
			map[string]any{"listener": listener(8080)},
			map[string]any{"listener": listener(8443)},
		}},
	}}

	result := Parse(dump, "snap-unnamed", "2026-07-27T00:00:00Z")
	if len(result.Listeners) != 2 {
		t.Fatalf("listeners=%d, want 2", len(result.Listeners))
	}
	first, second := result.Listeners[0], result.Listeners[1]
	if first.ID == second.ID {
		t.Fatalf("listener IDs must be unique: %q", first.ID)
	}
	if first.Name != "unnamed-listener · 0.0.0.0:8080" || second.Name != "unnamed-listener · 0.0.0.0:8443" {
		t.Fatalf("listener names=%q, %q", first.Name, second.Name)
	}
	if first.Port != 8080 || second.Port != 8443 {
		t.Fatalf("listener ports=%d, %d", first.Port, second.Port)
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

func TestParseRuntimeExtensionsAndRelationships(t *testing.T) {
	wasmConfig := map[string]any{
		"@type": "type.googleapis.com/envoy.extensions.filters.http.wasm.v3.Wasm",
		"config": map[string]any{
			"name": "ai-guard", "root_id": "guard-root",
			"vm_config": map[string]any{
				"runtime": "envoy.wasm.runtime.v8",
				"code":    map[string]any{"local": map[string]any{"filename": "/var/local/lib/ai-guard.wasm"}},
			},
		},
	}
	extProcConfig := map[string]any{
		"@type":           "type.googleapis.com/envoy.extensions.filters.http.ext_proc.v3.ExternalProcessor",
		"grpc_service":    map[string]any{"envoy_grpc": map[string]any{"cluster_name": "ext-proc-policy"}},
		"message_timeout": "200ms", "failure_mode_allow": false,
	}
	hcm := map[string]any{"http_filters": []any{
		map[string]any{
			"name": "ai-platform.model-router",
			"config_discovery": map[string]any{
				"config_source": map[string]any{"ads": map[string]any{}},
				"type_urls":     []any{"type.googleapis.com/envoy.extensions.filters.http.wasm.v3.Wasm"},
			},
		},
		map[string]any{"name": "envoy.filters.http.ext_proc", "typed_config": extProcConfig},
		map[string]any{"name": "envoy.filters.http.cors"},
		map[string]any{"name": "envoy.filters.http.router"},
	}}
	listener := map[string]any{
		"name": "https", "address": map[string]any{"socket_address": map[string]any{"address": "0.0.0.0", "port_value": float64(8443)}},
		"filter_chains": []any{map[string]any{"name": "api", "filters": []any{
			map[string]any{"name": "envoy.filters.network.http_connection_manager", "typed_config": hcm},
		}}},
	}
	dump := map[string]any{"configs": []any{
		map[string]any{"dynamic_listeners": []any{map[string]any{"active_state": map[string]any{"listener": listener}}}},
		map[string]any{"ecds_filters": []any{map[string]any{"ecds_filter": map[string]any{"name": "ai-platform.model-router", "typed_config": wasmConfig}}}},
		map[string]any{"dynamic_active_clusters": []any{map[string]any{"cluster": map[string]any{"name": "ext-proc-policy", "type": "EDS"}}}},
	}}

	result := Parse(dump, "snap-extensions", "2026-07-27T00:00:00Z")
	if len(result.Extensions) != 3 {
		t.Fatalf("extensions=%d, want 3: %#v", len(result.Extensions), result.Extensions)
	}
	byKind := map[string]domain.EnvoyExtension{}
	for _, extension := range result.Extensions {
		byKind[extension.Kind] = extension
	}
	wasm := byKind["Wasm"]
	if wasm.Name != "ai-platform.model-router" || wasm.ConfigSource != "ECDS" || len(wasm.Attachments) != 1 {
		t.Fatalf("wasm=%#v", wasm)
	}
	if len(wasm.Dependencies) != 2 || !wasm.Dependencies[0].Resolved || !wasm.Dependencies[1].Resolved {
		t.Fatalf("wasm dependencies=%#v", wasm.Dependencies)
	}
	extProc := byKind["ext_proc"]
	if extProc.Status != domain.StatusHealthy || len(extProc.Dependencies) != 1 || extProc.Dependencies[0].Name != "ext-proc-policy" || !extProc.Dependencies[0].Resolved {
		t.Fatalf("ext_proc=%#v", extProc)
	}
	if extProc.ConfigSummary != "gRPC: ext-proc-policy · timeout 200ms · failure closed" {
		t.Fatalf("ext_proc summary=%q", extProc.ConfigSummary)
	}
	if _, found := byKind["router"]; found {
		t.Fatal("terminal router must not be included in extension inventory")
	}
}

func TestMissingECDSExtensionIsWarning(t *testing.T) {
	hcm := map[string]any{"http_filters": []any{
		map[string]any{
			"name": "ai-platform.missing-wasm",
			"config_discovery": map[string]any{
				"config_source": map[string]any{"ads": map[string]any{}},
				"type_urls":     []any{"type.googleapis.com/envoy.extensions.filters.http.wasm.v3.Wasm"},
			},
		},
		map[string]any{"name": "envoy.filters.http.router"},
	}}
	listener := map[string]any{"name": "http", "filter_chains": []any{map[string]any{"filters": []any{
		map[string]any{"name": "envoy.filters.network.http_connection_manager", "typed_config": hcm},
	}}}}
	result := Parse(map[string]any{"configs": []any{
		map[string]any{"dynamic_listeners": []any{map[string]any{"active_state": map[string]any{"listener": listener}}}},
	}}, "snap-missing-ecds", "2026-07-27T00:00:00Z")

	if len(result.Extensions) != 1 {
		t.Fatalf("extensions=%d, want 1", len(result.Extensions))
	}
	extension := result.Extensions[0]
	if extension.Kind != "Wasm" || extension.Status != domain.StatusWarning {
		t.Fatalf("extension=%#v", extension)
	}
	if len(extension.Dependencies) != 1 || extension.Dependencies[0].Kind != "ECDS" || extension.Dependencies[0].Resolved {
		t.Fatalf("dependencies=%#v", extension.Dependencies)
	}
}
