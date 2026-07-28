package envoy

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
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
	extensionConfigs := map[string]map[string]any{}
	for _, raw := range slice(dump, "configs") {
		section := object(raw)
		parseRouteConfigs(section, routeConfigs)
		parseEndpointAssignments(section, endpointAssignments)
		parseExtensionConfigs(section, extensionConfigs)
	}
	extensionIndex := map[string]int{}
	for _, raw := range slice(dump, "configs") {
		section := object(raw)
		parseListeners(section, &result, routeConfigs, extensionConfigs, extensionIndex)
		parseClusters(section, &result, endpointAssignments)
	}
	resolveExtensionDependencies(&result)
	sort.Slice(result.Extensions, func(i, j int) bool {
		if result.Extensions[i].Kind == result.Extensions[j].Kind {
			return result.Extensions[i].Name < result.Extensions[j].Name
		}
		return result.Extensions[i].Kind < result.Extensions[j].Kind
	})
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

func parseExtensionConfigs(section map[string]any, result map[string]map[string]any) {
	for _, key := range []string{"ecds_filters", "dynamic_active_extension_configs", "static_extension_configs"} {
		for _, raw := range slice(section, key) {
			entry := object(raw)
			config := object(entry, "ecds_filter")
			if config == nil {
				config = object(entry, "extension_config")
			}
			if config == nil {
				config = object(entry, "typed_extension_config")
			}
			if config == nil && stringValue(entry, "name", "") != "" {
				config = entry
			}
			if name := stringValue(config, "name", ""); name != "" {
				result[name] = config
			}
		}
	}
}

func parseListeners(section map[string]any, result *domain.EnvoyConfig, routeConfigs, extensionConfigs map[string]map[string]any, extensionIndex map[string]int) {
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
						parseHTTPFilters(config, &fc, item, extensionConfigs, result, extensionIndex)
						parseRoutes(config, routeConfigs, &fc)
					} else {
						fc.HTTPFilters = append(fc.HTTPFilters, domain.EnvoyHTTPFilter{Name: name, Type: "network filter", Stage: "network", ConfigSummary: "configured"})
						addRuntimeExtension(filter, item, fc, len(fc.HTTPFilters), "network filter", extensionConfigs, result, extensionIndex)
					}
				}
				item.FilterChains = append(item.FilterChains, fc)
			}
			result.Listeners = append(result.Listeners, item)
		}
	}
}

func parseHTTPFilters(config map[string]any, chain *domain.EnvoyFilterChain, listener domain.EnvoyListener, extensionConfigs map[string]map[string]any, result *domain.EnvoyConfig, extensionIndex map[string]int) {
	for _, raw := range slice(config, "http_filters") {
		filter := object(raw)
		name := stringValue(filter, "name", "unknown")
		terminal := isTerminalHTTPFilter(filter, extensionConfigs)
		chain.HTTPFilters = append(chain.HTTPFilters, domain.EnvoyHTTPFilter{Name: name, Type: "HTTP filter", Stage: "request", ConfigSummary: httpFilterSummary(filter, extensionConfigs), Terminal: terminal})
		if !terminal {
			addRuntimeExtension(filter, listener, *chain, len(chain.HTTPFilters), "HTTP filter", extensionConfigs, result, extensionIndex)
		}
	}
}

func isTerminalHTTPFilter(filter map[string]any, extensionConfigs map[string]map[string]any) bool {
	name := strings.ToLower(stringValue(filter, "name", ""))
	if name == "envoy.filters.http.router" || name == "envoy.router" {
		return true
	}
	_, _, typeURL, _, _ := extensionTypedConfig(filter, extensionConfigs)
	return strings.Contains(strings.ToLower(typeURL), "filters.http.router.")
}

func httpFilterSummary(filter map[string]any, extensionConfigs map[string]map[string]any) string {
	config, _, typeURL, _, _ := extensionTypedConfig(filter, extensionConfigs)
	kind := extensionKind(stringValue(filter, "name", "unknown"), typeURL, filter)
	return extensionSummary(kind, config)
}

func addRuntimeExtension(filter map[string]any, listener domain.EnvoyListener, chain domain.EnvoyFilterChain, position int, filterType string, extensionConfigs map[string]map[string]any, result *domain.EnvoyConfig, extensionIndex map[string]int) {
	name := stringValue(filter, "name", "unknown")
	config, source, typeURL, ecdsReferenced, ecdsResolved := extensionTypedConfig(filter, extensionConfigs)
	kind := extensionKind(name, typeURL, filter)
	configBytes, _ := json.Marshal(config)
	fingerprint := sha256.Sum256(append([]byte(kind+"|"+name+"|"+source+"|"+typeURL+"|"), configBytes...))
	id := fmt.Sprintf("%s|%s|%x", kind, name, fingerprint[:6])
	attachment := domain.EnvoyExtensionAttachment{
		ListenerID: listener.ID, ListenerName: listener.Name, FilterChain: chain.Name,
		FilterName: name, FilterType: filterType, Position: position,
	}
	if index, found := extensionIndex[id]; found {
		result.Extensions[index].Attachments = appendUniqueExtensionAttachment(result.Extensions[index].Attachments, attachment)
		return
	}

	dependencies := []domain.EnvoyExtensionDependency{}
	if ecdsReferenced {
		evidence := "http_filters[].config_discovery"
		if ecdsResolved {
			evidence += " + EcdsConfigDump"
		}
		dependencies = append(dependencies, domain.EnvoyExtensionDependency{Kind: "ECDS", Name: name, Relation: "resolves through", Evidence: evidence, Resolved: ecdsResolved})
	}
	for _, clusterName := range nestedStringValues(config, "cluster_name") {
		dependencies = appendUniqueExtensionDependency(dependencies, domain.EnvoyExtensionDependency{
			Kind: "Cluster", Name: clusterName, Relation: dependencyRelation(kind, "Cluster"),
			Evidence: clusterDependencyEvidence(kind),
		})
	}
	if target := firstNestedString(config, "target_uri"); target != "" {
		dependencies = appendUniqueExtensionDependency(dependencies, domain.EnvoyExtensionDependency{
			Kind: "External gRPC", Name: safeLocation(target), Relation: dependencyRelation(kind, "External gRPC"),
			Evidence: "typed_config grpc_service/google_grpc.target_uri", Resolved: true,
		})
	}
	if kind == "Wasm" {
		if module := wasmModuleLocation(config); module != "" {
			dependencies = appendUniqueExtensionDependency(dependencies, domain.EnvoyExtensionDependency{
				Kind: "Wasm module", Name: safeLocation(module), Relation: "loads",
				Evidence: "typed_config.config.vm_config.code", Resolved: true,
			})
		}
	}
	item := domain.EnvoyExtension{
		ID: id, Name: name, Kind: kind, TypeURL: typeURL, Status: domain.StatusHealthy,
		ConfigSource: source, ConfigSummary: extensionSummary(kind, config),
		Attachments: []domain.EnvoyExtensionAttachment{attachment}, Dependencies: dependencies,
	}
	extensionIndex[id] = len(result.Extensions)
	result.Extensions = append(result.Extensions, item)
}

func extensionTypedConfig(filter map[string]any, extensionConfigs map[string]map[string]any) (config map[string]any, source, typeURL string, ecdsReferenced, ecdsResolved bool) {
	name := stringValue(filter, "name", "")
	if direct := object(filter, "typed_config"); direct != nil {
		config = unwrapTypedConfig(direct)
		source = "inline typed_config"
	}
	discovery := object(filter, "config_discovery")
	if discovery != nil {
		ecdsReferenced = true
		if ecds := extensionConfigs[name]; ecds != nil {
			config = unwrapTypedConfig(object(ecds, "typed_config"))
			source = "ECDS"
			ecdsResolved = config != nil
		} else if fallback := object(discovery, "default_config"); fallback != nil {
			config = unwrapTypedConfig(fallback)
			source = "ECDS default_config"
			ecdsResolved = true
		} else {
			source = "ECDS reference"
		}
		if typeURL == "" {
			typeURLs := stringValues(discovery, "type_urls")
			if len(typeURLs) > 0 {
				typeURL = typeURLs[0]
			}
		}
	}
	if typeURL == "" {
		typeURL = stringValue(config, "@type", "")
	}
	if source == "" {
		source = "filter name"
	}
	return config, source, typeURL, ecdsReferenced, ecdsResolved
}

func unwrapTypedConfig(config map[string]any) map[string]any {
	for config != nil && strings.Contains(stringValue(config, "@type", ""), "TypedExtensionConfig") {
		next := object(config, "typed_config")
		if next == nil {
			break
		}
		config = next
	}
	return config
}

func extensionKind(name, typeURL string, filter map[string]any) string {
	value := strings.ToLower(name + " " + typeURL + " " + strings.Join(stringValues(object(filter, "config_discovery"), "type_urls"), " "))
	switch {
	case strings.Contains(value, "ext_proc"), strings.Contains(value, "externalprocessor"):
		return "ext_proc"
	case strings.Contains(value, "wasm"):
		return "Wasm"
	default:
		return "Envoy Filter"
	}
}

func extensionSummary(kind string, config map[string]any) string {
	switch kind {
	case "ext_proc":
		parts := []string{}
		clusters := nestedStringValues(config, "cluster_name")
		if len(clusters) > 0 {
			parts = append(parts, "gRPC: "+strings.Join(clusters, ", "))
		} else if target := firstNestedString(config, "target_uri"); target != "" {
			parts = append(parts, "gRPC: "+safeLocation(target))
		}
		if timeout := stringValue(config, "message_timeout", ""); timeout != "" {
			parts = append(parts, "timeout "+timeout)
		}
		if value, found := boolValue(config, "failure_mode_allow"); found {
			if value {
				parts = append(parts, "failure open")
			} else {
				parts = append(parts, "failure closed")
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, " · ")
		}
	case "Wasm":
		wasm := object(config, "config")
		parts := []string{}
		if pluginName := stringValue(wasm, "name", ""); pluginName != "" {
			parts = append(parts, pluginName)
		}
		if rootID := stringValue(wasm, "root_id", ""); rootID != "" {
			parts = append(parts, "root "+rootID)
		}
		if runtime := stringValue(object(wasm, "vm_config"), "runtime", ""); runtime != "" {
			parts = append(parts, runtime)
		}
		if len(parts) > 0 {
			return strings.Join(parts, " · ")
		}
	}
	if typeURL := stringValue(config, "@type", ""); typeURL != "" {
		return "configured as " + shortTypeURL(typeURL)
	}
	return "configured"
}

func clusterDependencyEvidence(extensionKind string) string {
	if extensionKind == "ext_proc" {
		return "typed_config grpc_service/envoy_grpc.cluster_name"
	}
	return "typed_config field cluster_name"
}

func dependencyRelation(extensionKind, dependencyKind string) string {
	if extensionKind == "ext_proc" && (dependencyKind == "Cluster" || dependencyKind == "External gRPC") {
		return "calls"
	}
	return "references"
}

func resolveExtensionDependencies(result *domain.EnvoyConfig) {
	clusters := map[string]bool{}
	for _, cluster := range result.Clusters {
		clusters[cluster.Name] = true
	}
	for extensionIndex := range result.Extensions {
		status := domain.StatusHealthy
		for dependencyIndex := range result.Extensions[extensionIndex].Dependencies {
			dependency := &result.Extensions[extensionIndex].Dependencies[dependencyIndex]
			if dependency.Kind == "Cluster" {
				dependency.Resolved = clusters[dependency.Name]
			}
			if !dependency.Resolved {
				status = domain.StatusWarning
			}
		}
		result.Extensions[extensionIndex].Status = status
	}
}

func appendUniqueExtensionAttachment(values []domain.EnvoyExtensionAttachment, value domain.EnvoyExtensionAttachment) []domain.EnvoyExtensionAttachment {
	for _, existing := range values {
		if existing.ListenerID == value.ListenerID && existing.FilterChain == value.FilterChain && existing.FilterName == value.FilterName && existing.Position == value.Position {
			return values
		}
	}
	return append(values, value)
}

func appendUniqueExtensionDependency(values []domain.EnvoyExtensionDependency, value domain.EnvoyExtensionDependency) []domain.EnvoyExtensionDependency {
	for _, existing := range values {
		if existing.Kind == value.Kind && existing.Name == value.Name && existing.Relation == value.Relation {
			return values
		}
	}
	return append(values, value)
}

func nestedStringValues(value any, wantedKey string) []string {
	found := map[string]bool{}
	var visit func(any)
	visit = func(current any) {
		switch item := current.(type) {
		case map[string]any:
			for key, child := range item {
				if key == wantedKey {
					if text, ok := child.(string); ok && text != "" {
						found[text] = true
					}
				}
				visit(child)
			}
		case []any:
			for _, child := range item {
				visit(child)
			}
		}
	}
	visit(value)
	values := make([]string, 0, len(found))
	for item := range found {
		values = append(values, item)
	}
	sort.Strings(values)
	return values
}

func firstNestedString(value any, wantedKey string) string {
	values := nestedStringValues(value, wantedKey)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func wasmModuleLocation(config map[string]any) string {
	code := object(config, "config", "vm_config", "code")
	if filename := stringValue(object(code, "local"), "filename", ""); filename != "" {
		return filename
	}
	return stringValue(object(code, "remote", "http_uri"), "uri", "")
}

func safeLocation(value string) string {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" {
		return value
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func shortTypeURL(value string) string {
	if index := strings.LastIndex(value, "."); index >= 0 && index+1 < len(value) {
		return value[index+1:]
	}
	return value
}

func boolValue(item map[string]any, key string) (bool, bool) {
	if item == nil {
		return false, false
	}
	value, found := item[key].(bool)
	return value, found
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
