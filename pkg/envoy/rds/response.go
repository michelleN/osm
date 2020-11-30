package rds

import (
	set "github.com/deckarep/golang-set"
	xds_route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	xds_discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/golang/protobuf/ptypes"

	cat "github.com/openservicemesh/osm/pkg/catalog"
	"github.com/openservicemesh/osm/pkg/certificate"
	"github.com/openservicemesh/osm/pkg/configurator"
	"github.com/openservicemesh/osm/pkg/envoy"
	"github.com/openservicemesh/osm/pkg/envoy/route"
	"github.com/openservicemesh/osm/pkg/kubernetes"
	"github.com/openservicemesh/osm/pkg/service"
	"github.com/openservicemesh/osm/pkg/trafficpolicy"
)

// NewResponse creates a new Route Discovery Response.
func NewResponse(catalog cat.MeshCataloger, proxy *envoy.Proxy, _ *xds_discovery.DiscoveryRequest, _ configurator.Configurator, _ certificate.Manager) (*xds_discovery.DiscoveryResponse, error) {
	//svcList, err := catalog.GetServicesFromEnvoyCertificate(proxy.GetCommonName())
	proxyIdentity, err := cat.GetServiceAccountFromProxyCertificate(proxy.GetCommonName())
	if err != nil {
		log.Error().Err(err).Msgf("Error looking up Service Account for Envoy with CN=%q", proxy.GetCommonName())
		return nil, err
	}
	// Github Issue #1575
	log.Debug().Msgf("Proxy Service Account %#v", proxyIdentity)
	//proxyServiceName := svcList[0]

	inboundTrafficPolicies, outboundTrafficPolicies, _ := catalog.ListTrafficPoliciesForSA(proxyIdentity)
	log.Debug().Msgf("For %s: outbound policies: %+v", proxyIdentity, outboundTrafficPolicies)
	if proxyIdentity.Name == "bookbuyer" {
		for i, out := range outboundTrafficPolicies {
			log.Debug().Msgf("outbound[%v] %+v", i, out)
			for x, rwc := range out.Routes {
				log.Debug().Msgf("outbound[%v] routeweightedclusters[%v] routeweightedcluster %+v", i, x, rwc.WeightedClusters)
			}
		}
	}

	resp := &xds_discovery.DiscoveryResponse{
		TypeUrl: string(envoy.TypeRDS),
	}

	var routeConfiguration []*xds_route.RouteConfiguration

	// fetch ingress routes
	// merge ingress routes on top of existing
	/* TODO
	if err = updateRoutesForIngress(proxyServiceName, catalog, inboundAggregatedRoutesByHostnames); err != nil {
		return nil, err
	}
	*/

	routeConfiguration = route.BuildRouteConfiguration(inboundTrafficPolicies, outboundTrafficPolicies)
	log.Debug().Msgf("routeConfiguration(proxyServiceName %#v): %+v", proxyIdentity, routeConfiguration)

	for _, config := range routeConfiguration {
		marshalledRouteConfig, err := ptypes.MarshalAny(config)
		if err != nil {
			log.Error().Err(err).Msgf("Failed to marshal route config for proxy")
			return nil, err
		}
		resp.Resources = append(resp.Resources, marshalledRouteConfig)

	}
	log.Debug().Msgf("proxyServiceName %#v, LEN %v resp.Resources: %+v \n ", proxyIdentity, len(resp.Resources), resp.Resources)

	return resp, nil
}

func aggregateRoutesByHost(routesPerHost map[string]map[string]trafficpolicy.RouteWeightedClusters, routePolicy trafficpolicy.HTTPRoute, weightedCluster service.WeightedCluster, hostname string) {
	host := kubernetes.GetServiceFromHostname(hostname)
	_, exists := routesPerHost[host]
	if !exists {
		// no host found, create a new route map
		routesPerHost[host] = make(map[string]trafficpolicy.RouteWeightedClusters)
	}
	routePolicyWeightedCluster, routeFound := routesPerHost[host][routePolicy.PathRegex]
	if routeFound {
		// add the cluster to the existing route
		routePolicyWeightedCluster.WeightedClusters.Add(weightedCluster)
		routePolicyWeightedCluster.HTTPRoute.Methods = append(routePolicyWeightedCluster.HTTPRoute.Methods, routePolicy.Methods...)
		if routePolicyWeightedCluster.HTTPRoute.Headers == nil {
			routePolicyWeightedCluster.HTTPRoute.Headers = make(map[string]string)
		}
		for headerKey, headerValue := range routePolicy.Headers {
			routePolicyWeightedCluster.HTTPRoute.Headers[headerKey] = headerValue
		}
		routePolicyWeightedCluster.Hostnames.Add(hostname)
		routesPerHost[host][routePolicy.PathRegex] = routePolicyWeightedCluster
	} else {
		// no route found, create a new route and cluster mapping on host
		routesPerHost[host][routePolicy.PathRegex] = createRoutePolicyWeightedClusters(routePolicy, weightedCluster, hostname)
	}
}

func createRoutePolicyWeightedClusters(routePolicy trafficpolicy.HTTPRoute, weightedCluster service.WeightedCluster, hostname string) trafficpolicy.RouteWeightedClusters {
	return trafficpolicy.RouteWeightedClusters{
		HTTPRoute:        routePolicy,
		WeightedClusters: set.NewSet(weightedCluster),
		Hostnames:        set.NewSet(hostname),
	}
}
