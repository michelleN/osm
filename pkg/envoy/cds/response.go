package cds

import (
	"fmt"

	xds_cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	xds_discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/golang/protobuf/ptypes"

	cat "github.com/openservicemesh/osm/pkg/catalog"
	"github.com/openservicemesh/osm/pkg/certificate"
	"github.com/openservicemesh/osm/pkg/configurator"
	"github.com/openservicemesh/osm/pkg/envoy"
	"github.com/openservicemesh/osm/pkg/featureflags"
	"github.com/openservicemesh/osm/pkg/service"
)

// NewResponse creates a new Cluster Discovery Response.
func NewResponse(catalog cat.MeshCataloger, proxy *envoy.Proxy, _ *xds_discovery.DiscoveryRequest, cfg configurator.Configurator, _ certificate.Manager) (*xds_discovery.DiscoveryResponse, error) {
	proxyIdentity, err := cat.GetServiceAccountFromProxyCertificate(proxy.GetCommonName())
	if err != nil {
		log.Error().Err(err).Msgf("Error looking up proxy identity for Envoy with CN=%q", proxy.GetCommonName())
		return nil, err
	}

	svcList, err := catalog.GetServicesForServiceAccount(proxyIdentity)
	if err != nil {
		log.Error().Err(err).Msgf("Error looking up Services for ServiceAccount %s in CDS", proxyIdentity)
		return nil, err
	}

	resp := &xds_discovery.DiscoveryResponse{
		TypeUrl: string(envoy.TypeCDS),
	}
	// The clusters have to be unique, so use a map to prevent duplicates. Keys correspond to the cluster name.
	clusterFactories := make(map[string]*xds_cluster.Cluster)

	outboundServices, err := catalog.ListAllowedOutboundServicesForIdentity(proxyIdentity)
	if err != nil {
		log.Error().Err(err).Msgf("Error listing outbound services for proxy %q", proxyIdentity)
		return nil, err
	}

	// Build remote clusters based on allowed outbound services
	for _, dstService := range outboundServices {
		// Github Issue #1575
		proxyServiceName := svcList[0]

		if _, found := clusterFactories[dstService.String()]; found {
			// Guard against duplicates
			continue
		}

		remoteCluster, err := getUpstreamServiceCluster(dstService, proxyServiceName, cfg)
		if err != nil {
			log.Error().Err(err).Msgf("Failed to construct service cluster for proxy %s", proxyServiceName)
			return nil, err
		}

		if featureflags.IsBackpressureEnabled() {
			enableBackpressure(catalog, remoteCluster, dstService)
		}

		clusterFactories[remoteCluster.Name] = remoteCluster
	}

	for _, svc := range svcList {
		// Create a local cluster for the service.
		// The local cluster will be used for incoming traffic.
		localClusterName := getLocalClusterName(svc)
		localCluster, err := getLocalServiceCluster(catalog, svc, localClusterName)
		if err != nil {
			log.Error().Err(err).Msgf("Failed to get local cluster config for proxy %s", svc)
			return nil, err
		}
		clusterFactories[localCluster.Name] = localCluster
	}

	if cfg.IsEgressEnabled() {
		// Add a pass-through cluster for egress
		passthroughCluster := getOutboundPassthroughCluster()
		clusterFactories[passthroughCluster.Name] = passthroughCluster
	}

	for _, cluster := range clusterFactories {
		log.Debug().Msgf("Proxy identity %s constructed ClusterConfiguration: %+v ", proxyIdentity, cluster)
		marshalledClusters, err := ptypes.MarshalAny(cluster)
		if err != nil {
			log.Error().Err(err).Msgf("Failed to marshal cluster for proxy %s", proxy.GetCommonName())
			return nil, err
		}
		resp.Resources = append(resp.Resources, marshalledClusters)
	}

	if cfg.IsPrometheusScrapingEnabled() {
		prometheusCluster := getPrometheusCluster()
		marshalledCluster, err := ptypes.MarshalAny(&prometheusCluster)
		if err != nil {
			log.Error().Err(err).Msgf("Error marshaling Prometheus cluster for proxy with CN=%s", proxy.GetCommonName())
			return nil, err
		}
		resp.Resources = append(resp.Resources, marshalledCluster)
	}

	if cfg.IsTracingEnabled() {
		tracingCluster := getTracingCluster(cfg)
		marshalledCluster, err := ptypes.MarshalAny(&tracingCluster)
		if err != nil {
			log.Error().Err(err).Msgf("Error marshaling tracing cluster for proxy with CN=%s", proxy.GetCommonName())
			return nil, err
		}
		resp.Resources = append(resp.Resources, marshalledCluster)
	}

	return resp, nil
}

func getLocalClusterName(proxyServiceName service.MeshService) string {
	return fmt.Sprintf("%s%s", proxyServiceName, envoy.LocalClusterSuffix)
}
