package trafficpolicy

import (
	"reflect"
	"testing"

	set "github.com/deckarep/golang-set"
	"github.com/openservicemesh/osm/pkg/service"
)

func TestTotalClustersWeight(t *testing.T) {
	weightedCluster1 := service.WeightedCluster{ClusterName: "cluster1", Weight: 50}
	weightedClusters := set.NewSet(weightedCluster1)

	r := &RouteWeightedClusters{
		HTTPRoute: HTTPRoute{
			PathRegex: "/hello",
		},
		WeightedClusters: weightedClusters,
	}

	expected := 50
	actual := r.TotalClustersWeight()
	if expected != actual {
		t.Errorf("Expected TotalClusteresWeight to return %v but got %v ", expected, actual)
	}
}

/*

type OutboundTrafficPolicy struct {
	Name                     string                   `json:"name:omitempty"`
	Hostnames                []string                 `json:"hostnames"`
	RouteWeightedClustersList []*RouteWeightedClusters `json:"http_routes_clusters:omitempty"`
}
*/

// BookstoreBuyHTTPRoute is an HTTP route to buy books
var BookstoreBuyHTTPRoute = HTTPRoute{
	PathRegex: "/buy",
	Methods:   []string{"GET"},
	Headers: map[string]string{
		"user-agent": "something",
	},
}

var testWeightedCluster = service.WeightedCluster{
	ClusterName: "default/bookstore-v1",
	Weight:      100,
}

var testRoute2 = HTTPRoute{
	PathRegex: "/sell",
	Methods:   []string{"GET"},
	Headers: map[string]string{
		"user-agent": "another",
	},
}

var testWeightedCluster2 = service.WeightedCluster{
	ClusterName: "default/bookstore-v2",
	Weight:      100,
}

func TestAddRouteWeightedClusters(t *testing.T) {
	out := OutboundTrafficPolicy{}
	out.AddRoute(BookstoreBuyHTTPRoute, testWeightedCluster)

	// make sure out has new rwc

	if len(out.Routes) != 1 {
		t.Errorf("Expected Routes length to be 1 but got %v", len(out.Routes))
	}

	if !reflect.DeepEqual(out.Routes[0].WeightedClusters, set.NewSet(testWeightedCluster)) {
		t.Errorf("Expected weighted clusters to be %v, got %v", set.NewSet(testWeightedCluster), out.Routes[0].WeightedClusters)
	}

	out.AddRoute(testRoute2, testWeightedCluster2)
	if len(out.Routes) != 3 {
		t.Errorf("Expected Routes length to be 2 but got %v: %v", len(out.Routes), out.Routes[0])
	}

}
