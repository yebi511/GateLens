package envoy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gatelens/gatelens/internal/domain"
)

// Fetch reads Envoy's admin config_dump endpoint. The URL is intentionally
// supplied by the deployment because Istio and Higress expose different
// service and pod boundaries.
func Fetch(ctx context.Context, client *http.Client, url, snapshotID, observedAt string) (domain.EnvoyConfig, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(url, "/")+"/config_dump", nil)
	if err != nil {
		return domain.EnvoyConfig{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return domain.EnvoyConfig{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return domain.EnvoyConfig{}, fmt.Errorf("envoy config_dump returned %s", resp.Status)
	}
	var dump map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(&dump); err != nil {
		return domain.EnvoyConfig{}, fmt.Errorf("decode Envoy config_dump: %w", err)
	}
	return Parse(dump, snapshotID, observedAt), nil
}

func Parse(dump map[string]any, snapshotID, observedAt string) domain.EnvoyConfig {
	rawConfig, _ := json.Marshal(dump)
	result := domain.EnvoyConfig{SnapshotID: snapshotID, ObservedAt: observedAt, State: "complete", Source: "Envoy admin /config_dump", RawConfig: rawConfig}
	routeConfigs := map[string]map[string]any{}
	endpointAssignments := map[string]map[string]any{}
	for _, raw := range slice(dump, "configs") {
		section := object(raw)
		parseRouteConfigs(section, routeConfigs)
		parseEndpointAssignments(section, endpointAssignments)
	}
	for _, raw := range slice(dump, "configs") {
		section := object(raw)
		parseListeners(section, &result, routeConfigs)
		parseClusters(section, &result, endpointAssignments)
	}
	return result
}

func parseRouteConfigs(section map[string]any, result map[string]map[string]any) {
	for _, key := range []string{"static_route_configs", "dynamic_route_configs"} {
		for _, raw := range slice(section, key) {
			entry := object(raw)
			routeConfig := object(entry, "route_config")
			if routeConfig == nil {
				routeConfig = entry
			}
			if name := stringValue(routeConfig, "name", ""); name != "" {
				result[name] = routeConfig
			}
		}
	}
}

func parseEndpointAssignments(section map[string]any, result map[string]map[string]any) {
	for _, key := range []string{"static_endpoint_configs", "dynamic_endpoint_configs"} {
		for _, raw := range slice(section, key) {
			entry := object(raw)
			assignment := object(entry, "endpoint_config")
			if assignment == nil {
				assignment = entry
			}
			if name := stringValue(assignment, "cluster_name", ""); name != "" {
				result[name] = assignment
			}
		}
	}
}
func parseListeners(section map[string]any, result *domain.EnvoyConfig, routeConfigs map[string]map[string]any) {
	for _, key := range []string{"static_listeners", "dynamic_listeners"} {
		for _, raw := range slice(section, key) {
			entry := object(raw)
			listener := object(entry, "listener")
			if listener == nil {
				listener = object(object(entry, "active_state"), "listener")
			}
			if listener == nil {
				continue
			}
			address := object(object(listener, "address"), "socket_address")
			listenerAddress := stringValue(address, "address", "0.0.0.0")
			listenerPort := intValue(address, "port_value")
			listenerName := stringValue(listener, "name", stringValue(entry, "name", ""))
			if listenerName == "" {
				listenerName = fmt.Sprintf("unnamed-listener · %s:%d", listenerAddress, listenerPort)
			}
			listenerID := fmt.Sprintf("%s|%s:%d|%d", listenerName, listenerAddress, listenerPort, len(result.Listeners))
			item := domain.EnvoyListener{ID: listenerID, Name: listenerName, Address: listenerAddress, Port: listenerPort, Protocol: "TCP", Status: domain.StatusHealthy}
			chains := slice(listener, "filter_chains")
			if defaultChain := object(listener, "default_filter_chain"); defaultChain != nil {
				chains = append(chains, defaultChain)
			}
			for _, rawChain := range chains {
				chain := object(rawChain)
				fc := domain.EnvoyFilterChain{Name: stringValue(chain, "name", "default"), Match: filterChainMatchSummary(chain), Transport: transportSummary(chain)}
				for _, rawFilter := range slice(chain, "filters") {
					filter := object(rawFilter)
					name := stringValue(filter, "name", "unknown")
					config := object(filter, "typed_config")
					if strings.Contains(name, "http_connection_manager") {
						item.Protocol = "HTTP"
						if fc.Transport == "TLS" {
							item.Protocol = "HTTPS"
						}
						parseHTTPFilters(config, &fc)
						parseRoutes(config, routeConfigs, &fc)
					} else {
						fc.HTTPFilters = append(fc.HTTPFilters, domain.EnvoyHTTPFilter{Name: name, Type: "network filter", Stage: "network", ConfigSummary: "configured"})
					}
				}
				item.FilterChains = append(item.FilterChains, fc)
			}
			result.Listeners = append(result.Listeners, item)
		}
	}
}

func parseHTTPFilters(config map[string]any, chain *domain.EnvoyFilterChain) {
	for _, raw := range slice(config, "http_filters") {
		filter := object(raw)
		name := stringValue(filter, "name", "unknown")
		chain.HTTPFilters = append(chain.HTTPFilters, domain.EnvoyHTTPFilter{Name: name, Type: "HTTP filter", Stage: "request", ConfigSummary: "configured", Terminal: strings.Contains(name, "router")})
	}
}

func parseRoutes(config map[string]any, routeConfigs map[string]map[string]any, chain *domain.EnvoyFilterChain) {
	routeConfig := object(config, "route_config")
	if routeConfig == nil {
		routeConfig = routeConfigs[stringValue(object(config, "rds"), "route_config_name", "")]
	}
	for _, rawHost := range slice(routeConfig, "virtual_hosts") {
		host := object(rawHost)
		for _, rawRoute := range slice(host, "routes") {
			route := object(rawRoute)
			match := object(route, "match")
			action := object(route, "route")
			item := domain.EnvoyRoute{
				Name:    stringValue(route, "name", stringValue(host, "name", "route")),
				Match:   routeMatchSummary(host, match),
				Cluster: stringValue(action, "cluster", ""),
			}
			for _, rawCluster := range slice(object(action, "weighted_clusters"), "clusters") {
				cluster := object(rawCluster)
				weight := intValue(cluster, "weight")
				if weight == 0 {
					weight = intValue(object(cluster, "weight"), "value")
				}
				item.WeightedClusters = append(item.WeightedClusters, domain.EnvoyWeightedCluster{Name: stringValue(cluster, "name", "unknown"), Weight: weight})
			}
			chain.Routes = append(chain.Routes, item)
		}
	}
}

func parseClusters(section map[string]any, result *domain.EnvoyConfig, endpointAssignments map[string]map[string]any) {
	for _, key := range []string{"static_clusters", "dynamic_active_clusters"} {
		for _, raw := range slice(section, key) {
			entry := object(raw)
			cluster := object(entry, "cluster")
			if cluster == nil {
				cluster = entry
			}
			clusterType := stringValue(cluster, "type", "UNKNOWN")
			item := domain.EnvoyCluster{Name: stringValue(cluster, "name", "unnamed-cluster"), Type: clusterType, Discovery: clusterDiscovery(cluster, clusterType), ConnectTimeout: stringValue(cluster, "connect_timeout", "")}
			assignment := object(cluster, "load_assignment")
			if assignment == nil {
				assignment = endpointAssignments[item.Name]
			}
			parseEndpoints(assignment, &item)
			result.Clusters = append(result.Clusters, item)
		}
	}
}

func parseEndpoints(assignment map[string]any, cluster *domain.EnvoyCluster) {
	for _, rawEndpointSet := range slice(assignment, "endpoints") {
		for _, rawLB := range slice(object(rawEndpointSet), "lb_endpoints") {
			lb := object(rawLB)
			endpoint := object(lb, "endpoint")
			address := object(object(endpoint, "address"), "socket_address")
			health := strings.ToLower(stringValue(lb, "health_status", "unknown"))
			status := domain.StatusWarning
			switch health {
			case "healthy":
				status = domain.StatusHealthy
			case "unhealthy", "draining", "timeout":
				status = domain.StatusError
			}
			weight := intValue(lb, "load_balancing_weight")
			if weight == 0 {
				weight = 1
			}
			cluster.Endpoints = append(cluster.Endpoints, domain.EnvoyEndpoint{Address: stringValue(address, "address", "unknown"), Port: intValue(address, "port_value"), Status: status, Health: health, Weight: weight})
		}
	}
}

func filterChainMatchSummary(chain map[string]any) string {
	match := object(chain, "filter_chain_match")
	parts := []string{}
	if names := stringValues(match, "server_names"); len(names) > 0 {
		parts = append(parts, "SNI: "+strings.Join(names, ", "))
	}
	if protocol := stringValue(match, "transport_protocol", ""); protocol != "" {
		parts = append(parts, "transport: "+protocol)
	}
	if protocols := stringValues(match, "application_protocols"); len(protocols) > 0 {
		parts = append(parts, "ALPN: "+strings.Join(protocols, ", "))
	}
	if port := intValue(match, "destination_port"); port > 0 {
		parts = append(parts, fmt.Sprintf("port: %d", port))
	}
	if len(parts) == 0 {
		return "all connections"
	}
	return strings.Join(parts, " · ")
}

func transportSummary(chain map[string]any) string {
	name := stringValue(object(chain, "transport_socket"), "name", "raw_buffer")
	if strings.Contains(strings.ToLower(name), "tls") {
		return "TLS"
	}
	return name
}

func routeMatchSummary(host, match map[string]any) string {
	parts := []string{}
	if domains := stringValues(host, "domains"); len(domains) > 0 {
		parts = append(parts, strings.Join(domains, ", "))
	}
	path := stringValue(match, "path", stringValue(match, "path_separated_prefix", stringValue(match, "prefix", "")))
	if path == "" {
		path = stringValue(object(match, "safe_regex"), "regex", "all paths")
	}
	parts = append(parts, path)
	for _, rawHeader := range slice(match, "headers") {
		header := object(rawHeader)
		if stringValue(header, "name", "") == ":method" {
			parts = append([]string{stringValue(header, "exact_match", "")}, parts...)
			break
		}
	}
	return strings.Join(parts, " ")
}

func clusterDiscovery(cluster map[string]any, clusterType string) string {
	if strings.EqualFold(clusterType, "EDS") {
		service := stringValue(object(cluster, "eds_cluster_config"), "service_name", "")
		if service != "" {
			return "xDS / EDS: " + service
		}
		return "xDS / EDS"
	}
	if object(cluster, "load_assignment") != nil {
		return "inline"
	}
	return strings.ToLower(clusterType)
}

func stringValues(item map[string]any, key string) []string {
	values := []string{}
	for _, raw := range slice(item, key) {
		if value, ok := raw.(string); ok && value != "" {
			values = append(values, value)
		}
	}
	return values
}
func object(value any, keys ...string) map[string]any {
	item, _ := value.(map[string]any)
	for _, key := range keys {
		item = object(item[key])
	}
	return item
}
func slice(value any, key string) []any {
	item := object(value)
	values, _ := item[key].([]any)
	return values
}
func stringValue(item map[string]any, key, fallback string) string {
	if item != nil {
		if value, ok := item[key].(string); ok && value != "" {
			return value
		}
	}
	return fallback
}
func intValue(item map[string]any, key string) int {
	if item != nil {
		if value, ok := item[key].(float64); ok {
			return int(value)
		}
	}
	return 0
}
