package route

import (
	"fmt"
	"sort"

	set "github.com/deckarep/golang-set"
	xds_route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	xds_matcher "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	"github.com/golang/protobuf/ptypes/wrappers"

	"github.com/openservicemesh/osm/pkg/constants"
	"github.com/openservicemesh/osm/pkg/envoy"
	"github.com/openservicemesh/osm/pkg/service"
	"github.com/openservicemesh/osm/pkg/trafficpolicy"
)

// Direction is a type to signify the direction associated with a route
type Direction int

const (
	// OutboundRoute is the direction for an outbound route
	OutboundRoute Direction = iota

	// InboundRoute is the direction for an inbound route
	InboundRoute
)

const (
	//InboundRouteConfigName is the name of the route config that the envoy will identify
	InboundRouteConfigName = "RDS_Inbound"

	//OutboundRouteConfigName is the name of the route config that the envoy will identify
	OutboundRouteConfigName = "RDS_Outbound"

	// inboundVirtualHost is the name of the virtual host on the inbound route configuration
	inboundVirtualHost = "inbound_virtualHost"

	// outboundVirtualHost is the name of the virtual host on the outbound route configuration
	outboundVirtualHost = "outbound_virtualHost"

	// MethodHeaderKey is the key of the header for HTTP methods
	MethodHeaderKey = ":method"

	// httpHostHeader is the name of the HTTP host header
	httpHostHeader = "host"
)

func createVirtualHostStub(namePrefix string, host string, domains []string) *xds_route.VirtualHost {
	fmt.Printf("DOMAINS: %+v\n", domains)
	name := fmt.Sprintf("%s|%s", namePrefix, host)
	virtualHost := xds_route.VirtualHost{
		Name:    name,
		Domains: domains,
	}
	return &virtualHost
}

func createInboundRoutes(routingRules []*trafficpolicy.Rule) []*xds_route.Route {
	var routes []*xds_route.Route

	for _, rule := range routingRules {
		// For a given route path, sanitize the methods in case there
		// is wildcard or if there are duplicates
		route := rule.Route.HTTPRoute
		allowedMethods := sanitizeHTTPMethods(route.Methods)
		for _, method := range allowedMethods {
			route := buildRoute(route.PathRegex, method, route.Headers, rule.Route.WeightedClusters, 100, InboundRoute)
			routes = append(routes, route)
		}
	}
	return routes
}

func createOutboundRoutes(routesClusters []*trafficpolicy.RouteWeightedClusters) []*xds_route.Route {
	var routes []*xds_route.Route
	for _, routeClusters := range routesClusters {
		emptyHeaders := map[string]string{}
		// TODO: buildRoute should take in path, method, headers from trafficpolicy.HTTPRoute when implemting traffic split v1alpha3
		log.Debug().Msgf("routeClusters: %#v", routeClusters)
		routes = append(routes, buildRoute(constants.RegexMatchAll, constants.WildcardHTTPMethod, emptyHeaders, routeClusters.WeightedClusters, routeClusters.TotalClustersWeight(), OutboundRoute))
	}
	return routes
}

func buildRoute(pathRegex, method string, headersMap map[string]string, weightedClusters set.Set, totalWeight int, direction Direction) *xds_route.Route {

	route := xds_route.Route{
		Match: &xds_route.RouteMatch{
			PathSpecifier: &xds_route.RouteMatch_SafeRegex{
				SafeRegex: &xds_matcher.RegexMatcher{
					EngineType: &xds_matcher.RegexMatcher_GoogleRe2{GoogleRe2: &xds_matcher.RegexMatcher_GoogleRE2{}},
					Regex:      pathRegex,
				},
			},
			Headers: getHeadersForRoute(method, headersMap),
		},
		Action: &xds_route.Route_Route{
			Route: &xds_route.RouteAction{
				ClusterSpecifier: &xds_route.RouteAction_WeightedClusters{
					WeightedClusters: buildWeightedCluster(weightedClusters, totalWeight, direction),
				},
			},
		},
	}
	return &route

}

func buildWeightedCluster(weightedClusters set.Set, totalWeight int, direction Direction) *xds_route.WeightedCluster {
	var wc xds_route.WeightedCluster
	var total int
	for clusterInterface := range weightedClusters.Iter() {
		cluster := clusterInterface.(service.WeightedCluster)
		clusterName := string(cluster.ClusterName)
		total += cluster.Weight
		if direction == InboundRoute {
			// An inbound route is associated with a local cluster. The inbound route is applied
			// on the destination cluster, and the destination clusters that accept inbound
			// traffic have the name of the form 'someClusterName-local`.
			clusterName += envoy.LocalClusterSuffix
		}
		wc.Clusters = append(wc.Clusters, &xds_route.WeightedCluster_ClusterWeight{
			Name:   clusterName,
			Weight: &wrappers.UInt32Value{Value: uint32(cluster.Weight)},
		})
	}
	if direction == OutboundRoute {
		total = totalWeight
	}
	wc.TotalWeight = &wrappers.UInt32Value{Value: uint32(total)}
	sort.Stable(clusterWeightByName(wc.Clusters))
	return &wc
}

func getHeadersForRoute(method string, headersMap map[string]string) []*xds_route.HeaderMatcher {
	var headers []*xds_route.HeaderMatcher

	// add methods header
	methodsHeader := xds_route.HeaderMatcher{
		Name: MethodHeaderKey,
		HeaderMatchSpecifier: &xds_route.HeaderMatcher_SafeRegexMatch{
			SafeRegexMatch: &xds_matcher.RegexMatcher{
				EngineType: &xds_matcher.RegexMatcher_GoogleRe2{GoogleRe2: &xds_matcher.RegexMatcher_GoogleRE2{}},
				Regex:      getRegexForMethod(method),
			},
		},
	}
	headers = append(headers, &methodsHeader)

	// add all other custom headers
	for headerKey, headerValue := range headersMap {
		// omit the host header as we have already configured this
		if headerKey == httpHostHeader {
			continue
		}
		header := xds_route.HeaderMatcher{
			Name: headerKey,
			HeaderMatchSpecifier: &xds_route.HeaderMatcher_SafeRegexMatch{
				SafeRegexMatch: &xds_matcher.RegexMatcher{
					EngineType: &xds_matcher.RegexMatcher_GoogleRe2{GoogleRe2: &xds_matcher.RegexMatcher_GoogleRE2{}},
					Regex:      headerValue,
				},
			},
		}
		headers = append(headers, &header)
	}
	return headers
}

func getWeightedCluster(weightedClusters set.Set, totalClustersWeight int, direction Direction) *xds_route.WeightedCluster {
	var wc xds_route.WeightedCluster
	var total int
	for clusterInterface := range weightedClusters.Iter() {
		cluster := clusterInterface.(service.WeightedCluster)
		clusterName := string(cluster.ClusterName)
		total += cluster.Weight
		if direction == InboundRoute {
			// An inbound route is associated with a local cluster. The inbound route is applied
			// on the destination cluster, and the destination clusters that accept inbound
			// traffic have the name of the form 'someClusterName-local`.
			clusterName += envoy.LocalClusterSuffix
		}
		wc.Clusters = append(wc.Clusters, &xds_route.WeightedCluster_ClusterWeight{
			Name:   clusterName,
			Weight: &wrappers.UInt32Value{Value: uint32(cluster.Weight)},
		})
	}
	if direction == OutboundRoute {
		// For an outbound route from the source, the pre-computed total weight based on the weights defined in
		// the traffic split policies are used.
		total = totalClustersWeight
	}
	wc.TotalWeight = &wrappers.UInt32Value{Value: uint32(total)}
	sort.Stable(clusterWeightByName(wc.Clusters))
	return &wc
}

// This method gets a list of all the distinct upstream clusters for a domain
// needed to configure source service's weighted routes
func getDistinctWeightedClusters(routePolicyWeightedClustersMap map[string]trafficpolicy.RouteWeightedClusters) set.Set {
	weightedClusters := set.NewSet()
	for _, perRouteWeightedClusters := range routePolicyWeightedClustersMap {
		if weightedClusters.Cardinality() == 0 {
			weightedClusters = perRouteWeightedClusters.WeightedClusters
		}
		weightedClusters.Union(perRouteWeightedClusters.WeightedClusters)
	}
	return weightedClusters
}

// This method gets a list of all the distinct domains for a host
// needed to configure virtual hosts
func getDistinctDomains(routePolicyWeightedClustersMap map[string]trafficpolicy.RouteWeightedClusters) set.Set {
	domains := set.NewSet()
	for _, perRouteWeightedClusters := range routePolicyWeightedClustersMap {
		if domains.Cardinality() == 0 {
			domains = perRouteWeightedClusters.Hostnames
		}
		domains.Union(perRouteWeightedClusters.Hostnames)
	}
	return domains
}

func getTotalWeightForClusters(weightedClusters set.Set) int {
	var totalWeight int
	for clusterInterface := range weightedClusters.Iter() {
		cluster := clusterInterface.(service.WeightedCluster)
		totalWeight += cluster.Weight
	}
	return totalWeight
}

type clusterWeightByName []*xds_route.WeightedCluster_ClusterWeight

func (c clusterWeightByName) Len() int      { return len(c) }
func (c clusterWeightByName) Swap(i, j int) { c[i], c[j] = c[j], c[i] }
func (c clusterWeightByName) Less(i, j int) bool {
	if c[i].Name == c[j].Name {
		return c[i].Weight.Value < c[j].Weight.Value
	}
	return c[i].Name < c[j].Name
}

// sanitizeHTTPMethods takes in a list of HTTP methods including a wildcard (*) and returns a wildcard if any of
// the methods is a wildcard or sanitizes the input list to avoid duplicates.
func sanitizeHTTPMethods(allowedMethods []string) []string {
	var newAllowedMethods []string
	keys := make(map[string]interface{})
	for _, method := range allowedMethods {
		if method != "" {
			if method == constants.WildcardHTTPMethod {
				newAllowedMethods = []string{constants.WildcardHTTPMethod}
				return newAllowedMethods
			}
			if _, value := keys[method]; !value {
				keys[method] = nil
				newAllowedMethods = append(newAllowedMethods, method)
			}
		}
	}
	return newAllowedMethods
}

//NewRouteConfigurationStub creates the route configuration placeholder
func NewRouteConfigurationStub(routeConfigName string) *xds_route.RouteConfiguration {
	routeConfiguration := xds_route.RouteConfiguration{
		Name:             routeConfigName,
		ValidateClusters: &wrappers.BoolValue{Value: true},
	}
	return &routeConfiguration
}

func getRegexForMethod(httpMethod string) string {
	methodRegex := httpMethod
	if httpMethod == constants.WildcardHTTPMethod {
		methodRegex = constants.RegexMatchAll
	}
	return methodRegex
}

// BuildRouteConfiguration constructs the Envoy constructs ([]*xds_route.RouteConfiguration) for implementing inbound and outbound routes
func BuildRouteConfiguration(inbound []*trafficpolicy.InboundTrafficPolicy, outbound []*trafficpolicy.OutboundTrafficPolicy) []*xds_route.RouteConfiguration {
	routeConfiguration := []*xds_route.RouteConfiguration{}

	if len(inbound) > 0 {
		inboundRouteConfig := NewRouteConfigurationStub(InboundRouteConfigName)

		for _, in := range inbound {
			virtualHost := createVirtualHostStub(inboundVirtualHost, in.Name, in.Hostnames)
			virtualHost.Routes = createInboundRoutes(in.Rules)
			inboundRouteConfig.VirtualHosts = append(inboundRouteConfig.VirtualHosts, virtualHost)
		}

		routeConfiguration = append(routeConfiguration, inboundRouteConfig)
	}

	if len(outbound) > 0 {
		outboundRouteConfig := NewRouteConfigurationStub(OutboundRouteConfigName)

		for _, out := range outbound {
			virtualHost := createVirtualHostStub(outboundVirtualHost, out.Name, out.Hostnames)
			log.Debug().Msgf("Create outbound routes for %v", out.Name)
			virtualHost.Routes = createOutboundRoutes(out.Routes)
			outboundRouteConfig.VirtualHosts = append(outboundRouteConfig.VirtualHosts, virtualHost)
		}
		routeConfiguration = append(routeConfiguration, outboundRouteConfig)
	}

	return routeConfiguration
}
