package lds

import (
	"fmt"
	"net"
	"testing"

	xds_core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	xds_listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	xds_tcp_proxy "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/tcp_proxy/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"github.com/golang/mock/gomock"
	"github.com/golang/protobuf/ptypes"
	tassert "github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/openservicemesh/osm/pkg/apis/config/v1alpha1"
	"github.com/openservicemesh/osm/pkg/auth"
	"github.com/openservicemesh/osm/pkg/catalog"
	"github.com/openservicemesh/osm/pkg/configurator"
	"github.com/openservicemesh/osm/pkg/endpoint"
	"github.com/openservicemesh/osm/pkg/envoy/rds/route"
	"github.com/openservicemesh/osm/pkg/identity"
	"github.com/openservicemesh/osm/pkg/service"
	"github.com/openservicemesh/osm/pkg/tests"
	"github.com/openservicemesh/osm/pkg/trafficpolicy"
)

func TestGetOutboundHTTPFilterChainForService(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockCatalog := catalog.NewMockMeshCataloger(mockCtrl)
	mockConfigurator := configurator.NewMockConfigurator(mockCtrl)

	// Mock calls used to build the HTTP connection manager
	mockConfigurator.EXPECT().IsTracingEnabled().Return(false).AnyTimes()
	mockConfigurator.EXPECT().GetTracingEndpoint().Return("test-api").AnyTimes()
	mockConfigurator.EXPECT().GetInboundExternalAuthConfig().Return(auth.ExtAuthConfig{
		Enable: false,
	}).AnyTimes()
	mockConfigurator.EXPECT().GetFeatureFlags().Return(v1alpha1.FeatureFlags{
		EnableWASMStats: false,
	}).AnyTimes()

	lb := &listenerBuilder{
		meshCatalog:     mockCatalog,
		cfg:             mockConfigurator,
		serviceIdentity: tests.BookbuyerServiceIdentity,
	}

	testCases := []struct {
		name                     string
		expectedEndpoints        []endpoint.Endpoint
		servicePort              uint32
		expectedFilterChainMatch *xds_listener.FilterChainMatch
		expectError              bool
	}{
		{
			name: "service with multiple endpoints",
			expectedEndpoints: []endpoint.Endpoint{
				{IP: net.ParseIP("1.1.1.1"), Port: 80},
				{IP: net.ParseIP("2.2.2.2"), Port: 80},
			},
			servicePort: 80, // this can be different from the target port in the endpoints
			expectedFilterChainMatch: &xds_listener.FilterChainMatch{
				DestinationPort: &wrapperspb.UInt32Value{Value: 80}, // same as 'servicePort'
				PrefixRanges: []*xds_core.CidrRange{
					// The order is guaranteed to be sorted
					{
						AddressPrefix: "1.1.1.1",
						PrefixLen: &wrapperspb.UInt32Value{
							Value: 32,
						},
					},
					{
						AddressPrefix: "2.2.2.2",
						PrefixLen: &wrapperspb.UInt32Value{
							Value: 32,
						},
					},
				},
			},
			expectError: false,
		},
		{
			name:                     "service with no endpoints",
			expectedEndpoints:        []endpoint.Endpoint{},
			servicePort:              80,
			expectedFilterChainMatch: nil,
			expectError:              true,
		},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("Testing test case %d: %s", i, tc.name), func(t *testing.T) {
			assert := tassert.New(t)

			mockCatalog.EXPECT().GetResolvableServiceEndpoints(tests.BookstoreApexService).Return(tc.expectedEndpoints, nil)
			httpFilterChain, err := lb.getOutboundHTTPFilterChainForService(tests.BookstoreApexService, tc.servicePort)

			assert.Equal(err != nil, tc.expectError)

			if err != nil {
				assert.Nil(httpFilterChain)
			} else {
				assert.NotNil(httpFilterChain)
				assert.Len(httpFilterChain.FilterChainMatch.PrefixRanges, len(tc.expectedEndpoints))

				for _, filter := range httpFilterChain.Filters {
					assert.Equal(wellknown.HTTPConnectionManager, filter.Name)
				}
			}
		})
	}
}

func TestGetOutboundTCPFilterChainForService(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockCatalog := catalog.NewMockMeshCataloger(mockCtrl)
	mockConfigurator := configurator.NewMockConfigurator(mockCtrl)

	lb := &listenerBuilder{
		meshCatalog:     mockCatalog,
		cfg:             mockConfigurator,
		serviceIdentity: tests.BookbuyerServiceIdentity,
	}

	testCases := []struct {
		name                     string
		expectedEndpoints        []endpoint.Endpoint
		servicePort              uint32
		expectedFilterChainMatch *xds_listener.FilterChainMatch
		expectError              bool
	}{
		{
			name: "service with multiple endpoints",
			expectedEndpoints: []endpoint.Endpoint{
				{IP: net.ParseIP("1.1.1.1"), Port: 80},
				{IP: net.ParseIP("2.2.2.2"), Port: 80},
			},
			servicePort: 80, // this can be different from the target port in the endpoints
			expectedFilterChainMatch: &xds_listener.FilterChainMatch{
				DestinationPort: &wrapperspb.UInt32Value{Value: 80}, // same as 'servicePort'
				PrefixRanges: []*xds_core.CidrRange{
					// The order is guaranteed to be sorted
					{
						AddressPrefix: "1.1.1.1",
						PrefixLen: &wrapperspb.UInt32Value{
							Value: 32,
						},
					},
					{
						AddressPrefix: "2.2.2.2",
						PrefixLen: &wrapperspb.UInt32Value{
							Value: 32,
						},
					},
				},
			},
			expectError: false,
		},
		{
			name:                     "service with no endpoints",
			expectedEndpoints:        []endpoint.Endpoint{},
			servicePort:              80,
			expectedFilterChainMatch: nil,
			expectError:              true,
		},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("Testing test case %d: %s", i, tc.name), func(t *testing.T) {
			assert := tassert.New(t)

			mockCatalog.EXPECT().GetResolvableServiceEndpoints(tests.BookstoreApexService).Return(tc.expectedEndpoints, nil)
			mockCatalog.EXPECT().GetWeightedClustersForUpstream(tests.BookstoreApexService).Times(1)

			tcpFilterChain, err := lb.getOutboundTCPFilterChainForService(tests.BookstoreApexService, tc.servicePort)

			assert.Equal(err != nil, tc.expectError)

			if err != nil {
				assert.Nil(tcpFilterChain)
			} else {
				assert.NotNil(tcpFilterChain)
				assert.Len(tcpFilterChain.FilterChainMatch.PrefixRanges, len(tc.expectedEndpoints))

				for _, filter := range tcpFilterChain.Filters {
					assert.Equal(wellknown.TCPProxy, filter.Name)
				}
			}
		})
	}
}

func TestGetInboundMeshHTTPFilterChain(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockCatalog := catalog.NewMockMeshCataloger(mockCtrl)
	mockConfigurator := configurator.NewMockConfigurator(mockCtrl)

	// Mock calls used to build the HTTP connection manager
	mockConfigurator.EXPECT().IsTracingEnabled().Return(false).AnyTimes()
	mockConfigurator.EXPECT().GetTracingEndpoint().Return("test-api").AnyTimes()
	mockConfigurator.EXPECT().GetInboundExternalAuthConfig().Return(auth.ExtAuthConfig{
		Enable: false,
	}).AnyTimes()
	mockConfigurator.EXPECT().GetFeatureFlags().Return(v1alpha1.FeatureFlags{
		EnableWASMStats:        false,
		EnableMulticlusterMode: true,
	}).AnyTimes()

	lb := &listenerBuilder{
		meshCatalog:     mockCatalog,
		cfg:             mockConfigurator,
		serviceIdentity: tests.BookbuyerServiceIdentity,
	}

	proxyService := tests.BookbuyerService

	testCases := []struct {
		name           string
		permissiveMode bool
		port           uint32

		expectedFilterChainMatch *xds_listener.FilterChainMatch
		expectedFilterNames      []string
		expectError              bool
	}{
		{
			name:           "inbound HTTP filter chain with permissive mode disabled",
			permissiveMode: false,
			port:           80,
			expectedFilterChainMatch: &xds_listener.FilterChainMatch{
				DestinationPort:      &wrapperspb.UInt32Value{Value: 80},
				ServerNames:          []string{proxyService.ServerName()},
				TransportProtocol:    "tls",
				ApplicationProtocols: []string{"osm"},
			},
			expectedFilterNames: []string{wellknown.RoleBasedAccessControl, wellknown.HTTPConnectionManager},
			expectError:         false,
		},
		{
			name:           "inbound HTTP filter chain with permissive mode enabled",
			permissiveMode: true,
			port:           90,
			expectedFilterChainMatch: &xds_listener.FilterChainMatch{
				DestinationPort:      &wrapperspb.UInt32Value{Value: 90},
				ServerNames:          []string{proxyService.ServerName()},
				TransportProtocol:    "tls",
				ApplicationProtocols: []string{"osm"},
			},
			expectedFilterNames: []string{wellknown.HTTPConnectionManager},
			expectError:         false,
		},
	}

	trafficTargets := []trafficpolicy.TrafficTargetWithRoutes{
		{
			Name:        "ns-1/test-1",
			Destination: identity.ServiceIdentity("sa-1.ns-1.cluster.local"),
			Sources: []identity.ServiceIdentity{
				identity.ServiceIdentity("sa-2.ns-2.cluster.local"),
				identity.ServiceIdentity("sa-3.ns-3.cluster.local"),
			},
			TCPRouteMatches: nil,
		},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("Testing test case %d: %s", i, tc.name), func(t *testing.T) {
			assert := tassert.New(t)

			mockConfigurator.EXPECT().IsPermissiveTrafficPolicyMode().Return(tc.permissiveMode).Times(1)
			if !tc.permissiveMode {
				// mock catalog calls used to build the RBAC filter
				mockCatalog.EXPECT().ListInboundTrafficTargetsWithRoutes(lb.serviceIdentity).Return(trafficTargets, nil).Times(1)
			}

			filterChain, err := lb.getInboundMeshHTTPFilterChain(proxyService, tc.port)

			assert.Equal(err != nil, tc.expectError)
			assert.Equal(filterChain.FilterChainMatch, tc.expectedFilterChainMatch)
			assert.Len(filterChain.Filters, len(tc.expectedFilterNames))
			for i, filter := range filterChain.Filters {
				assert.Equal(filter.Name, tc.expectedFilterNames[i])
			}
		})
	}
}

func TestGetInboundMeshTCPFilterChain(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockCatalog := catalog.NewMockMeshCataloger(mockCtrl)
	mockConfigurator := configurator.NewMockConfigurator(mockCtrl)

	// Mock calls used to build the HTTP connection manager
	mockConfigurator.EXPECT().IsTracingEnabled().Return(false).AnyTimes()
	mockConfigurator.EXPECT().GetTracingEndpoint().Return("test-api").AnyTimes()
	mockConfigurator.EXPECT().GetInboundExternalAuthConfig().Return(auth.ExtAuthConfig{
		Enable: false,
	}).AnyTimes()
	mockConfigurator.EXPECT().GetFeatureFlags().Return(v1alpha1.FeatureFlags{
		EnableMulticlusterMode: true,
	}).AnyTimes()

	lb := &listenerBuilder{
		meshCatalog:     mockCatalog,
		cfg:             mockConfigurator,
		serviceIdentity: tests.BookbuyerServiceIdentity,
	}

	proxyService := tests.BookbuyerService

	testCases := []struct {
		name           string
		permissiveMode bool
		port           uint32

		expectedFilterChainMatch *xds_listener.FilterChainMatch
		expectedFilterNames      []string
		expectError              bool
	}{
		{
			name:           "inbound TCP filter chain with permissive mode disabled",
			permissiveMode: false,
			port:           80,
			expectedFilterChainMatch: &xds_listener.FilterChainMatch{
				DestinationPort:      &wrapperspb.UInt32Value{Value: 80},
				ServerNames:          []string{proxyService.ServerName()},
				TransportProtocol:    "tls",
				ApplicationProtocols: []string{"osm"},
			},
			expectedFilterNames: []string{wellknown.RoleBasedAccessControl, wellknown.TCPProxy},
			expectError:         false,
		},

		{
			name:           "inbound TCP filter chain with permissive mode enabled",
			permissiveMode: true,
			port:           90,
			expectedFilterChainMatch: &xds_listener.FilterChainMatch{
				DestinationPort:      &wrapperspb.UInt32Value{Value: 90},
				ServerNames:          []string{proxyService.ServerName()},
				TransportProtocol:    "tls",
				ApplicationProtocols: []string{"osm"},
			},
			expectedFilterNames: []string{wellknown.TCPProxy},
			expectError:         false,
		},
	}

	trafficTargets := []trafficpolicy.TrafficTargetWithRoutes{
		{
			Name:        "ns-1/test-1",
			Destination: identity.ServiceIdentity("sa-1.ns-1.cluster.local"),
			Sources: []identity.ServiceIdentity{
				identity.ServiceIdentity("sa-2.ns-2.cluster.local"),
				identity.ServiceIdentity("sa-3.ns-3.cluster.local"),
			},
			TCPRouteMatches: nil,
		},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("Testing test case %d: %s", i, tc.name), func(t *testing.T) {
			assert := tassert.New(t)

			mockConfigurator.EXPECT().IsPermissiveTrafficPolicyMode().Return(tc.permissiveMode).Times(1)
			if !tc.permissiveMode {
				// mock catalog calls used to build the RBAC filter
				mockCatalog.EXPECT().ListInboundTrafficTargetsWithRoutes(lb.serviceIdentity).Return(trafficTargets, nil).Times(1)
			}

			filterChain, err := lb.getInboundMeshTCPFilterChain(proxyService, tc.port)

			assert.Equal(err != nil, tc.expectError)
			assert.Equal(filterChain.FilterChainMatch, tc.expectedFilterChainMatch)
			assert.Len(filterChain.Filters, len(tc.expectedFilterNames))
			for i, filter := range filterChain.Filters {
				assert.Equal(filter.Name, tc.expectedFilterNames[i])
			}
		})
	}
}

// Tests getOutboundFilterChainMatchForService and ensures the filter chain match returned is as expected
func TestGetOutboundFilterChainMatchForService(t *testing.T) {
	mockCtrl := gomock.NewController(t)

	mockConfigurator := configurator.NewMockConfigurator(mockCtrl)
	mockCatalog := catalog.NewMockMeshCataloger(mockCtrl)

	lb := newListenerBuilder(mockCatalog, tests.BookbuyerServiceIdentity, mockConfigurator, nil)

	testCases := []struct {
		name        string
		endpoints   []endpoint.Endpoint
		servicePort uint32

		expectedFilterChainMatch *xds_listener.FilterChainMatch
		expectError              bool
	}{
		{
			// test case 1
			name: "outbound HTTP filter chain for service with endpoints",
			endpoints: []endpoint.Endpoint{
				{
					IP: net.IPv4(192, 168, 10, 1),
				},
				{
					IP: net.IPv4(192, 168, 20, 2),
				},
			},
			servicePort: 80,
			expectedFilterChainMatch: &xds_listener.FilterChainMatch{
				DestinationPort: &wrapperspb.UInt32Value{Value: 80}, // same as 'servicePort'
				PrefixRanges: []*xds_core.CidrRange{
					{
						AddressPrefix: "192.168.10.1",
						PrefixLen: &wrapperspb.UInt32Value{
							Value: 32,
						},
					},
					{
						AddressPrefix: "192.168.20.2",
						PrefixLen: &wrapperspb.UInt32Value{
							Value: 32,
						},
					},
				},
			},
			expectError: false,
		},

		{
			// test case 2
			name:                     "outbound HTTP filter chain for service without endpoints",
			endpoints:                []endpoint.Endpoint{},
			servicePort:              80,
			expectedFilterChainMatch: nil,
			expectError:              true,
		},

		{
			// test case 3
			name: "outbound TCP filter chain for service with endpoints",
			endpoints: []endpoint.Endpoint{
				{
					IP: net.IPv4(192, 168, 10, 1),
				},
				{
					IP: net.IPv4(192, 168, 20, 2),
				},
			},
			servicePort: 90,
			expectedFilterChainMatch: &xds_listener.FilterChainMatch{
				DestinationPort: &wrapperspb.UInt32Value{Value: 90}, // same as 'servicePort'
				PrefixRanges: []*xds_core.CidrRange{
					{
						AddressPrefix: "192.168.10.1",
						PrefixLen: &wrapperspb.UInt32Value{
							Value: 32,
						},
					},
					{
						AddressPrefix: "192.168.20.2",
						PrefixLen: &wrapperspb.UInt32Value{
							Value: 32,
						},
					},
				},
			},
			expectError: false,
		},

		{
			// test case 4
			name:                     "outbound TCP filter chain for service without endpoints",
			endpoints:                []endpoint.Endpoint{},
			servicePort:              80,
			expectedFilterChainMatch: nil,
			expectError:              true,
		},

		{
			// test case 5
			name: "outbound TCP filter chain for service with duplicated endpoints",
			endpoints: []endpoint.Endpoint{
				{
					IP:   net.IPv4(192, 168, 10, 1),
					Port: 8080,
				},
				{
					IP:   net.IPv4(192, 168, 10, 1),
					Port: 8090,
				},
			},
			servicePort: 80,
			expectedFilterChainMatch: &xds_listener.FilterChainMatch{
				DestinationPort: &wrapperspb.UInt32Value{Value: 80}, // same as 'servicePort'
				PrefixRanges: []*xds_core.CidrRange{
					{
						AddressPrefix: "192.168.10.1",
						PrefixLen: &wrapperspb.UInt32Value{
							Value: 32,
						},
					},
				},
			},
			expectError: false,
		},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("Testing test case %d: %s", i, tc.name), func(t *testing.T) {
			assert := tassert.New(t)

			// mock endpoints returned
			mockCatalog.EXPECT().GetResolvableServiceEndpoints(tests.BookstoreApexService).Return(tc.endpoints, nil)

			filterChainMatch, err := lb.getOutboundFilterChainMatchForService(tests.BookstoreApexService, tc.servicePort)
			assert.Equal(tc.expectError, err != nil)
			assert.Equal(tc.expectedFilterChainMatch, filterChainMatch)
		})
	}
}

func TestGetOutboundTCPFilter(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	type testCase struct {
		name                   string
		upstream               service.MeshService
		clusterWeights         []service.WeightedCluster
		expectedTCPProxyConfig *xds_tcp_proxy.TcpProxy
		expectError            bool
	}

	testCases := []testCase{
		{
			name: "TCP filter for upstream without any traffic split policies",
			upstream: service.MeshService{
				Name:      "foo",
				Namespace: "bar",
			},
			clusterWeights: nil,
			expectedTCPProxyConfig: &xds_tcp_proxy.TcpProxy{
				StatPrefix:       "outbound-mesh-tcp-proxy.bar/foo",
				ClusterSpecifier: &xds_tcp_proxy.TcpProxy_Cluster{Cluster: "bar/foo"},
			},
			expectError: false,
		},
		{
			name: "TCP filter for upstream with matching traffic split policy",
			upstream: service.MeshService{
				Name:      "foo",
				Namespace: "bar",
			},
			clusterWeights: []service.WeightedCluster{
				{
					ClusterName: "bar/foo-v1",
					Weight:      10,
				},
				{
					ClusterName: "bar/foo-v2",
					Weight:      90,
				},
			},
			expectedTCPProxyConfig: &xds_tcp_proxy.TcpProxy{
				StatPrefix: "outbound-mesh-tcp-proxy.bar/foo",
				ClusterSpecifier: &xds_tcp_proxy.TcpProxy_WeightedClusters{
					WeightedClusters: &xds_tcp_proxy.TcpProxy_WeightedCluster{
						Clusters: []*xds_tcp_proxy.TcpProxy_WeightedCluster_ClusterWeight{
							{
								Name:   "bar/foo-v1",
								Weight: 10,
							},
							{
								Name:   "bar/foo-v2",
								Weight: 90,
							},
						},
					},
				},
			},
			expectError: false,
		},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("Testing test case %d: %s", i, tc.name), func(t *testing.T) {
			mockCatalog := catalog.NewMockMeshCataloger(mockCtrl)
			mockConfigurator := configurator.NewMockConfigurator(mockCtrl)

			mockCatalog.EXPECT().GetWeightedClustersForUpstream(tc.upstream).Return(tc.clusterWeights).Times(1)

			lb := newListenerBuilder(mockCatalog, tests.BookbuyerServiceIdentity, mockConfigurator, nil)
			filter, err := lb.getOutboundTCPFilter(tc.upstream)

			assert := tassert.New(t)
			assert.Equal(tc.expectError, err != nil)

			actualConfig := &xds_tcp_proxy.TcpProxy{}
			err = ptypes.UnmarshalAny(filter.GetTypedConfig(), actualConfig)
			assert.Nil(err)
			assert.Equal(wellknown.TCPProxy, filter.Name)

			assert.Equal(tc.expectedTCPProxyConfig.ClusterSpecifier, actualConfig.ClusterSpecifier)

			assert.Equal(tc.expectedTCPProxyConfig.StatPrefix, actualConfig.StatPrefix)
		})
	}
}

func TestGetOutboundHTTPFilter(t *testing.T) {
	assert := tassert.New(t)
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockConfigurator := configurator.NewMockConfigurator(mockCtrl)
	lb := &listenerBuilder{
		cfg: mockConfigurator,
	}

	mockConfigurator.EXPECT().IsTracingEnabled()
	mockConfigurator.EXPECT().GetTracingEndpoint()
	mockConfigurator.EXPECT().GetInboundExternalAuthConfig().Return(auth.ExtAuthConfig{
		Enable: false,
	}).AnyTimes()
	mockConfigurator.EXPECT().GetFeatureFlags().Return(v1alpha1.FeatureFlags{
		EnableWASMStats: false,
	}).AnyTimes()

	filter, err := lb.getOutboundHTTPFilter(route.OutboundRouteConfigName)
	assert.NoError(err)
	assert.Equal(filter.Name, wellknown.HTTPConnectionManager)
}
