package trafficpolicy

import (
	"reflect"

	set "github.com/deckarep/golang-set"
	"github.com/rs/zerolog/log"

	"github.com/openservicemesh/osm/pkg/service"
)

func (in *InboundTrafficPolicy) AddRule(httpRoute HTTPRoute, weightedCluster service.WeightedCluster, sa service.K8sServiceAccount) {

	route := RouteWeightedClusters{
		HTTPRoute:        httpRoute,
		WeightedClusters: set.NewSet(weightedCluster),
	}
	routeExists := false
	for _, rule := range in.Rules {
		if reflect.DeepEqual(rule.Route, route) {
			routeExists = true
			rule.ServiceAccounts.Add(sa) //TODO test
			break
		}
	}
	if !routeExists {
		in.Rules = append(in.Rules, &Rule{
			Route:           route,
			ServiceAccounts: set.NewSet(sa),
		})
	}
}

func (out *OutboundTrafficPolicy) AddRoute(route HTTPRoute, weightedCluster service.WeightedCluster) {
	// loop through httproutesclusters and see if route exists
	// if route exists, ignore
	// if route does not exist then add RouteWeightedClusters to HTTPRoutesClusters slice

	routeExists := false
	for _, r := range out.Routes {
		if reflect.DeepEqual(r.HTTPRoute, route) {
			routeExists = true
			log.Debug().Msgf("Ignoring as route %v already exists", route)
			return
		}
	}

	if !routeExists {
		out.Routes = append(out.Routes, &RouteWeightedClusters{
			HTTPRoute:        route,
			WeightedClusters: set.NewSet(weightedCluster),
		})
	}
}

// TotalClustersWeight returns total weight of the WeightedClusters in trafficpolicy.RouteWeightedClusters
func (rwc *RouteWeightedClusters) TotalClustersWeight() int {
	var totalWeight int
	if rwc.WeightedClusters.Cardinality() > 0 {
		for clusterInterface := range rwc.WeightedClusters.Iter() { // iterate
			cluster := clusterInterface.(service.WeightedCluster)
			totalWeight += cluster.Weight
		}
	}
	return totalWeight
}

func MergeInboundPolicies(inbound []*InboundTrafficPolicy, policies ...*InboundTrafficPolicy) []*InboundTrafficPolicy {
	for _, p := range policies {
		foundHostnames := false
		for _, in := range inbound {
			if reflect.DeepEqual(in.Hostnames, p.Hostnames) {
				foundHostnames = true
				rules := mergeRules(in.Rules, p.Rules)
				in.Rules = rules
			}
		}
		if !foundHostnames {
			inbound = append(inbound, p)
		}
	}
	return inbound
}

func MergeOutboundPolicies(outbound []*OutboundTrafficPolicy, policies ...*OutboundTrafficPolicy) []*OutboundTrafficPolicy {
	for _, p := range policies {
		foundHostnames := false
		for _, out := range outbound {
			if reflect.DeepEqual(out.Hostnames, p.Hostnames) {
				foundHostnames = true
				out.Routes = mergeRoutesWeightedClusters(out.Routes, p.Routes)
			}
		}
		if !foundHostnames {
			outbound = append(outbound, p)
		}
	}
	return outbound
}

func mergeRules(originalRules, latestRules []*Rule) []*Rule {
	for _, latest := range latestRules {
		foundRoute := false
		for _, original := range originalRules {
			if reflect.DeepEqual(latest.Route, original.Route) {
				foundRoute = true
				original.ServiceAccounts.Add(latest.ServiceAccounts)
			}
		}
		if !foundRoute {
			originalRules = append(originalRules, latest)
		}
	}
	return originalRules
}

func mergeRoutesWeightedClusters(originalRoutes, latestRoutes []*RouteWeightedClusters) []*RouteWeightedClusters {
	// find if latest route is in original
	for _, latest := range latestRoutes {
		foundRoute := false
		for _, original := range originalRoutes {
			if reflect.DeepEqual(original.HTTPRoute, latest.HTTPRoute) {
				foundRoute = true
				//TODO add debug line
				continue
			}
		}
		if !foundRoute {
			originalRoutes = append(originalRoutes, latest)
		}
	}
	return originalRoutes
}
