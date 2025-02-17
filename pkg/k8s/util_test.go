package k8s

import (
	"fmt"
	"testing"

	tassert "github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/version"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/kubernetes"
	fakeclient "k8s.io/client-go/kubernetes/fake"

	"github.com/openservicemesh/osm/pkg/service"
	"github.com/openservicemesh/osm/pkg/tests"
)

func TestGetHostnamesForService(t *testing.T) {
	testCases := []struct {
		name              string
		service           *corev1.Service
		locality          service.Locality
		expectedHostnames []string
	}{
		{
			name: "hostnames corresponding to a service in the same namespace",
			service: tests.NewServiceFixture(tests.BookbuyerServiceName, tests.Namespace, map[string]string{
				tests.SelectorKey: tests.SelectorValue,
			}),
			locality: service.LocalNS,
			expectedHostnames: []string{
				tests.BookbuyerServiceName,
				fmt.Sprintf("%s:%d", tests.BookbuyerServiceName, tests.ServicePort),
				fmt.Sprintf("%s.%s", tests.BookbuyerServiceName, tests.Namespace),
				fmt.Sprintf("%s.%s:%d", tests.BookbuyerServiceName, tests.Namespace, tests.ServicePort),
				fmt.Sprintf("%s.%s.svc", tests.BookbuyerServiceName, tests.Namespace),
				fmt.Sprintf("%s.%s.svc:%d", tests.BookbuyerServiceName, tests.Namespace, tests.ServicePort),
				fmt.Sprintf("%s.%s.svc.cluster", tests.BookbuyerServiceName, tests.Namespace),
				fmt.Sprintf("%s.%s.svc.cluster:%d", tests.BookbuyerServiceName, tests.Namespace, tests.ServicePort),
				fmt.Sprintf("%s.%s.svc.cluster.local", tests.BookbuyerServiceName, tests.Namespace),
				fmt.Sprintf("%s.%s.svc.cluster.local:%d", tests.BookbuyerServiceName, tests.Namespace, tests.ServicePort),
			},
		},
		{
			name: "hostnames corresponding to a service NOT in the same namespace",
			service: tests.NewServiceFixture(tests.BookbuyerServiceName, tests.Namespace, map[string]string{
				tests.SelectorKey: tests.SelectorValue,
			}),
			locality: service.LocalCluster,
			expectedHostnames: []string{
				fmt.Sprintf("%s.%s", tests.BookbuyerServiceName, tests.Namespace),
				fmt.Sprintf("%s.%s:%d", tests.BookbuyerServiceName, tests.Namespace, tests.ServicePort),
				fmt.Sprintf("%s.%s.svc", tests.BookbuyerServiceName, tests.Namespace),
				fmt.Sprintf("%s.%s.svc:%d", tests.BookbuyerServiceName, tests.Namespace, tests.ServicePort),
				fmt.Sprintf("%s.%s.svc.cluster", tests.BookbuyerServiceName, tests.Namespace),
				fmt.Sprintf("%s.%s.svc.cluster:%d", tests.BookbuyerServiceName, tests.Namespace, tests.ServicePort),
				fmt.Sprintf("%s.%s.svc.cluster.local", tests.BookbuyerServiceName, tests.Namespace),
				fmt.Sprintf("%s.%s.svc.cluster.local:%d", tests.BookbuyerServiceName, tests.Namespace, tests.ServicePort),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert := tassert.New(t)

			actual := GetHostnamesForService(tc.service, tc.locality)
			assert.ElementsMatch(actual, tc.expectedHostnames)
			assert.Len(actual, len(tc.expectedHostnames))
		})
	}
}

func TestGetServiceFromHostname(t *testing.T) {
	testCases := []struct {
		name            string
		hostnames       []string
		expectedService string
	}{
		{
			name: "gets the service name from hostname",
			hostnames: []string{
				tests.BookbuyerServiceName,
				fmt.Sprintf("%s:%d", tests.BookbuyerServiceName, tests.ServicePort),
				fmt.Sprintf("%s.%s", tests.BookbuyerServiceName, tests.Namespace),
				fmt.Sprintf("%s.%s:%d", tests.BookbuyerServiceName, tests.Namespace, tests.ServicePort),
				fmt.Sprintf("%s.%s.svc", tests.BookbuyerServiceName, tests.Namespace),
				fmt.Sprintf("%s.%s.svc:%d", tests.BookbuyerServiceName, tests.Namespace, tests.ServicePort),
				fmt.Sprintf("%s.%s.svc.cluster", tests.BookbuyerServiceName, tests.Namespace),
				fmt.Sprintf("%s.%s.svc.cluster:%d", tests.BookbuyerServiceName, tests.Namespace, tests.ServicePort),
				fmt.Sprintf("%s.%s.svc.cluster.local", tests.BookbuyerServiceName, tests.Namespace),
				fmt.Sprintf("%s.%s.svc.cluster.local:%d", tests.BookbuyerServiceName, tests.Namespace, tests.ServicePort),
			},
			expectedService: tests.BookbuyerServiceName,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert := tassert.New(t)

			for _, hostname := range tc.hostnames {
				actual := GetServiceFromHostname(hostname)
				assert.Equal(actual, tc.expectedService)
			}
		})
	}
}

func TestGetAppProtocolFromPortName(t *testing.T) {
	testCases := []struct {
		name             string
		portName         string
		expectedProtocal string
	}{
		{
			name:             "tcp protocol",
			portName:         "tcp-port-test",
			expectedProtocal: "tcp",
		},
		{
			name:             "http protocol",
			portName:         "http-port-test",
			expectedProtocal: "http",
		},
		{
			name:             "grpc protocol",
			portName:         "grpc-port-test",
			expectedProtocal: "grpc",
		},
		{
			name:             "default protocol",
			portName:         "port-test",
			expectedProtocal: "http",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert := tassert.New(t)

			actual := GetAppProtocolFromPortName(tc.portName)
			assert.Equal(tc.expectedProtocal, actual)
		})
	}
}

func TestGetKubernetesServerVersionNumber(t *testing.T) {
	testCases := []struct {
		name            string
		kubeClient      kubernetes.Interface
		version         string
		expectedVersion []int // of the form [1, 19, 0] for v1.19.0
		expectError     bool
	}{
		{
			name:            "invalid kubeClient should error",
			kubeClient:      nil,
			expectedVersion: nil,
			expectError:     true,
		},
		{
			name:            "invalid server version should error",
			kubeClient:      fakeclient.NewSimpleClientset(),
			version:         "foo",
			expectedVersion: nil,
			expectError:     true,
		},
		{
			name:            "valid server version",
			kubeClient:      fakeclient.NewSimpleClientset(),
			version:         "v1.19.0",
			expectedVersion: []int{1, 19, 0},
			expectError:     false,
		},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("Running test case %d: %s", i, tc.name), func(t *testing.T) {
			assert := tassert.New(t)

			if tc.kubeClient != nil {
				tc.kubeClient.Discovery().(*fakediscovery.FakeDiscovery).FakedServerVersion = &version.Info{
					GitVersion: tc.version,
				}
			}

			version, err := GetKubernetesServerVersionNumber(tc.kubeClient)
			assert.Equal(tc.expectError, err != nil)
			assert.Equal(tc.expectedVersion, version)
		})
	}
}

func TestNamespacedNameFrom(t *testing.T) {
	testCases := []struct {
		name      string
		in        string
		out       types.NamespacedName
		expectErr bool
	}{
		{
			name:      "valid namespaced name",
			in:        "foo/bar",
			out:       types.NamespacedName{Namespace: "foo", Name: "bar"},
			expectErr: false,
		},
		{
			name:      "invalid namespaced name with no separator",
			in:        "foobar",
			out:       types.NamespacedName{},
			expectErr: true,
		},
		{
			name:      "invalid namespaced name with multiple separators",
			in:        "foo/bar/baz",
			out:       types.NamespacedName{},
			expectErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert := tassert.New(t)

			actual, err := NamespacedNameFrom(tc.in)
			assert.Equal(tc.out, actual)
			assert.Equal(tc.expectErr, err != nil)
		})
	}
}
