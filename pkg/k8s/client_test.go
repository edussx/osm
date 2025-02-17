package k8s

import (
	"context"
	"fmt"
	"testing"
	"time"

	mapset "github.com/deckarep/golang-set"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	tassert "github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	testclient "k8s.io/client-go/kubernetes/fake"

	policyv1alpha1 "github.com/openservicemesh/osm/pkg/apis/policy/v1alpha1"
	fakePolicyClient "github.com/openservicemesh/osm/pkg/gen/client/policy/clientset/versioned/fake"

	"github.com/openservicemesh/osm/pkg/announcements"
	"github.com/openservicemesh/osm/pkg/constants"
	"github.com/openservicemesh/osm/pkg/identity"
	"github.com/openservicemesh/osm/pkg/k8s/events"
	"github.com/openservicemesh/osm/pkg/service"
	"github.com/openservicemesh/osm/pkg/tests"
)

var (
	testMeshName = "mesh"
)

const (
	nsInformerSyncTimeout           = 3 * time.Second
	assertEventuallyPollingInterval = 10 * time.Millisecond
)

var _ = Describe("Test Namespace KubeController Methods", func() {
	Context("Testing ListMonitoredNamespaces", func() {
		It("should return monitored namespaces", func() {
			// Create namespace controller
			kubeClient := testclient.NewSimpleClientset()
			stop := make(chan struct{})
			kubeController, err := NewKubernetesController(kubeClient, nil, testMeshName, stop)
			Expect(err).ToNot(HaveOccurred())
			Expect(kubeController).ToNot(BeNil())

			// Create a test namespace that is monitored
			testNamespaceName := fmt.Sprintf("%s-1", tests.Namespace)
			testNamespace := corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   testNamespaceName,
					Labels: map[string]string{constants.OSMKubeResourceMonitorAnnotation: testMeshName},
				},
			}
			_, err = kubeClient.CoreV1().Namespaces().Create(context.TODO(), &testNamespace, metav1.CreateOptions{})
			Expect(err).To(BeNil())

			// Eventually asserts that all return values apart from the first value are nil or zero-valued,
			// so asserting that an error is nil is implicit.
			Eventually(func() ([]string, error) {
				return kubeController.ListMonitoredNamespaces()
			}, nsInformerSyncTimeout).Should(Equal([]string{testNamespaceName}))
		})
	})

	Context("Testing GetNamespace", func() {
		It("should return existing namespace if it exists", func() {
			// Create namespace controller
			kubeClient := testclient.NewSimpleClientset()
			stop := make(chan struct{})
			kubeController, err := NewKubernetesController(kubeClient, nil, testMeshName, stop)
			Expect(err).ToNot(HaveOccurred())
			Expect(kubeController).ToNot(BeNil())

			// Create a test namespace that is monitored
			testNamespaceName := fmt.Sprintf("%s-1", tests.Namespace)
			testNamespace := corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   testNamespaceName,
					Labels: map[string]string{constants.OSMKubeResourceMonitorAnnotation: testMeshName},
				},
			}

			// Create it
			nsCreate, err := kubeClient.CoreV1().Namespaces().Create(context.TODO(), &testNamespace, metav1.CreateOptions{})
			Expect(err).To(BeNil())

			// Check it is present
			Eventually(func() *corev1.Namespace {
				return kubeController.GetNamespace(testNamespaceName)
			}, nsInformerSyncTimeout).Should(Equal(nsCreate))

			// Delete it
			err = kubeClient.CoreV1().Namespaces().Delete(context.TODO(), testNamespaceName, metav1.DeleteOptions{})
			Expect(err).To(BeNil())

			// Check it is gone
			Eventually(func() *corev1.Namespace {
				return kubeController.GetNamespace(testNamespaceName)
			}, nsInformerSyncTimeout).Should(BeNil())
		})
	})

	Context("Testing IsMonitoredNamespace", func() {
		It("should work as expected", func() {
			// Create namespace controller
			kubeClient := testclient.NewSimpleClientset()
			stop := make(chan struct{})
			kubeController, err := NewKubernetesController(kubeClient, nil, testMeshName, stop)
			Expect(err).ToNot(HaveOccurred())
			Expect(kubeController).ToNot(BeNil())

			// Create a test namespace that is monitored
			testNamespaceName := fmt.Sprintf("%s-1", tests.Namespace)
			testNamespace := corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   testNamespaceName,
					Labels: map[string]string{constants.OSMKubeResourceMonitorAnnotation: testMeshName},
				},
			}

			_, err = kubeClient.CoreV1().Namespaces().Create(context.TODO(), &testNamespace, metav1.CreateOptions{})
			Expect(err).To(BeNil())

			Eventually(func() bool {
				return kubeController.IsMonitoredNamespace(testNamespaceName)
			}, nsInformerSyncTimeout).Should(BeTrue())

			fakeNamespaceIsMonitored := kubeController.IsMonitoredNamespace("fake")
			Expect(fakeNamespaceIsMonitored).To(BeFalse())
		})
	})

	Context("service controller", func() {
		var kubeClient *testclient.Clientset
		var kubeController Controller
		var err error

		BeforeEach(func() {
			kubeClient = testclient.NewSimpleClientset()
			kubeController, err = NewKubernetesController(kubeClient, nil, testMeshName, make(chan struct{}))
			Expect(err).ToNot(HaveOccurred())
			Expect(kubeController).ToNot(BeNil())
		})

		It("should create and delete services, and be detected if NS is monitored", func() {
			meshSvc := tests.BookbuyerService
			serviceChannel := events.Subscribe(announcements.ServiceAdded,
				announcements.ServiceDeleted,
				announcements.ServiceUpdated)
			defer events.Unsub(serviceChannel)

			// Create monitored namespace for this service
			testNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   tests.BookbuyerService.Namespace,
					Labels: map[string]string{constants.OSMKubeResourceMonitorAnnotation: testMeshName},
				},
			}
			_, err := kubeClient.CoreV1().Namespaces().Create(context.TODO(), testNamespace, metav1.CreateOptions{})
			Expect(err).To(BeNil())
			// Wait on namespace to be ready so that resources in this namespace are marked as monitored as soon as possible
			Eventually(func() bool {
				return kubeController.IsMonitoredNamespace(testNamespace.Name)
			}, nsInformerSyncTimeout).Should(BeTrue())

			svc := tests.NewServiceFixture(meshSvc.Name, meshSvc.Namespace, nil)
			_, err = kubeClient.CoreV1().Services(meshSvc.Namespace).Create(context.TODO(), svc, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())
			<-serviceChannel

			svcIncache := kubeController.GetService(meshSvc)
			Expect(svcIncache).To(Equal(svc))

			err = kubeClient.CoreV1().Services(meshSvc.Namespace).Delete(context.TODO(), svc.Name, metav1.DeleteOptions{})
			Expect(err).ToNot(HaveOccurred())
			<-serviceChannel

			svcIncache = kubeController.GetService(meshSvc)
			Expect(svcIncache).To(BeNil())
		})

		It("should return nil when the given MeshService is not found", func() {
			meshSvc := tests.BookbuyerService

			svcIncache := kubeController.GetService(meshSvc)
			Expect(svcIncache).To(BeNil())
		})

		It("should return an empty list when no services are found", func() {
			services := kubeController.ListServices()
			Expect(len(services)).To(Equal(0))
		})

		It("should return a list of Services", func() {
			// Define services to test with
			serviceChannel := events.Subscribe(announcements.ServiceAdded,
				announcements.ServiceDeleted,
				announcements.ServiceUpdated)
			defer events.Unsub(serviceChannel)
			testSvcs := []service.MeshService{
				{Name: uuid.New().String(), Namespace: "ns-1"},
				{Name: uuid.New().String(), Namespace: "ns-2"},
			}

			// Test services could belong to the same namespace, so ensure we create a list of unique namespaces
			testNamespaces := mapset.NewSet()
			for _, svc := range testSvcs {
				testNamespaces.Add(svc.Namespace)
			}

			// Create a namespace resource for each namespace
			for ns := range testNamespaces.Iter() {
				namespace := ns.(string)

				testNamespace := corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:   namespace,
						Labels: map[string]string{constants.OSMKubeResourceMonitorAnnotation: testMeshName},
					},
				}
				_, err = kubeClient.CoreV1().Namespaces().Create(context.TODO(), &testNamespace, metav1.CreateOptions{})
				Expect(err).To(BeNil())
			}
			for ns := range testNamespaces.Iter() {
				namespace := ns.(string)
				// Wait on namespace to be ready so that resources in this namespace are marked as monitored as soon as possible
				Eventually(func() bool {
					return kubeController.IsMonitoredNamespace(namespace)
				}, nsInformerSyncTimeout).Should(BeTrue())
			}

			// Add services
			for _, svcAdd := range testSvcs {
				svcSpec := tests.NewServiceFixture(svcAdd.Name, svcAdd.Namespace, nil)
				_, err := kubeClient.CoreV1().Services(svcAdd.Namespace).Create(context.TODO(), svcSpec, metav1.CreateOptions{})
				Expect(err).ToNot(HaveOccurred())
			}

			// Wait for all the service related events: 1 for each service created
			for range testSvcs {
				<-serviceChannel
			}

			services := kubeController.ListServices()
			Expect(len(testSvcs)).To(Equal(len(services)))
		})
	})

	Context("service account controller", func() {
		var kubeClient *testclient.Clientset
		var kubeController Controller
		var err error

		BeforeEach(func() {
			kubeClient = testclient.NewSimpleClientset()
			kubeController, err = NewKubernetesController(kubeClient, nil, testMeshName, make(chan struct{}))
			Expect(err).ToNot(HaveOccurred())
			Expect(kubeController).ToNot(BeNil())
		})

		It("should create and delete service accounts, and be detected if NS is monitored", func() {
			k8sSvcAccount := tests.BookbuyerServiceAccount
			serviceChannel := events.Subscribe(announcements.ServiceAccountAdded,
				announcements.ServiceAccountDeleted,
				announcements.ServiceAccountUpdated)
			defer events.Unsub(serviceChannel)

			// Create monitored namespace for this service
			testNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   tests.BookbuyerService.Namespace,
					Labels: map[string]string{constants.OSMKubeResourceMonitorAnnotation: testMeshName},
				},
			}
			_, err := kubeClient.CoreV1().Namespaces().Create(context.TODO(), testNamespace, metav1.CreateOptions{})
			Expect(err).To(BeNil())
			// Wait on namespace to be ready so that resources in this namespace are marked as monitored as soon as possible
			Eventually(func() bool {
				return kubeController.IsMonitoredNamespace(testNamespace.Name)
			}, nsInformerSyncTimeout).Should(BeTrue())

			svcAccount := tests.NewServiceAccountFixture(k8sSvcAccount.Name, k8sSvcAccount.Namespace)
			_, err = kubeClient.CoreV1().ServiceAccounts(svcAccount.Namespace).Create(context.TODO(), svcAccount, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())
			<-serviceChannel

			err = kubeClient.CoreV1().ServiceAccounts(svcAccount.Namespace).Delete(context.TODO(), svcAccount.Name, metav1.DeleteOptions{})
			Expect(err).ToNot(HaveOccurred())
			<-serviceChannel
		})

		It("should return an empty list when no service accounts are found", func() {
			services := kubeController.ListServiceAccounts()
			Expect(len(services)).To(Equal(0))
		})

		It("should return a list of service accounts", func() {
			// Define services to test with
			serviceChannel := events.Subscribe(announcements.ServiceAccountAdded,
				announcements.ServiceAccountDeleted,
				announcements.ServiceAccountUpdated)
			defer events.Unsub(serviceChannel)
			testSvcAccounts := []identity.K8sServiceAccount{
				{Name: uuid.New().String(), Namespace: "ns-1"},
				{Name: uuid.New().String(), Namespace: "ns-2"},
			}

			// Test service accounts could belong to the same namespace, so ensure we create a list of unique namespaces
			testNamespaces := mapset.NewSet()
			for _, svc := range testSvcAccounts {
				testNamespaces.Add(svc.Namespace)
			}

			// Create a namespace resource for each namespace
			for ns := range testNamespaces.Iter() {
				namespace := ns.(string)

				testNamespace := corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:   namespace,
						Labels: map[string]string{constants.OSMKubeResourceMonitorAnnotation: testMeshName},
					},
				}
				_, err = kubeClient.CoreV1().Namespaces().Create(context.TODO(), &testNamespace, metav1.CreateOptions{})
				Expect(err).To(BeNil())
			}
			for ns := range testNamespaces.Iter() {
				namespace := ns.(string)
				// Wait on namespace to be ready so that resources in this namespace are marked as monitored as soon as possible
				Eventually(func() bool {
					return kubeController.IsMonitoredNamespace(namespace)
				}, nsInformerSyncTimeout).Should(BeTrue())
			}

			// Add service accounts
			for _, svcAccountAdd := range testSvcAccounts {
				svcSpec := tests.NewServiceAccountFixture(svcAccountAdd.Name, svcAccountAdd.Namespace)
				_, err := kubeClient.CoreV1().ServiceAccounts(svcAccountAdd.Namespace).Create(context.TODO(), svcSpec, metav1.CreateOptions{})
				Expect(err).ToNot(HaveOccurred())
			}

			// Wait for all the service account related events: 1 for each service acount created
			for range testSvcAccounts {
				<-serviceChannel
			}

			svcAccounts := kubeController.ListServiceAccounts()
			Expect(len(testSvcAccounts)).To(Equal(len(svcAccounts)))
		})
	})

	Context("Test ListServiceIdentitiesForService()", func() {
		var kubeClient *testclient.Clientset
		var kubeController Controller
		var err error
		testMeshName := "foo"

		BeforeEach(func() {
			kubeClient = testclient.NewSimpleClientset()
			kubeController, err = NewKubernetesController(kubeClient, nil, testMeshName, make(chan struct{}))
			Expect(err).ToNot(HaveOccurred())
			Expect(kubeController).ToNot(BeNil())
		})

		It("should correctly return the ServiceAccounts associated with a service", func() {
			testNamespaceName := "test-ns"
			testSvcAccountName1 := "test-service-account-1"
			testSvcAccountName2 := "test-service-account-2"

			serviceChannel := events.Subscribe(announcements.ServiceAdded,
				announcements.ServiceDeleted,
				announcements.ServiceUpdated)
			defer events.Unsub(serviceChannel)
			podsChannel := events.Subscribe(announcements.PodAdded,
				announcements.PodDeleted,
				announcements.PodUpdated)
			defer events.Unsub(podsChannel)

			// Create a namespace
			testNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   testNamespaceName,
					Labels: map[string]string{constants.OSMKubeResourceMonitorAnnotation: testMeshName},
				},
			}
			_, err = kubeClient.CoreV1().Namespaces().Create(context.TODO(), testNamespace, metav1.CreateOptions{})
			Expect(err).To(BeNil())
			// Wait on namespace to be ready so that resources in this namespace are marked as monitored as soon as possible
			Eventually(func() bool {
				return kubeController.IsMonitoredNamespace(testNamespace.Name)
			}, nsInformerSyncTimeout).Should(BeTrue())

			// Create pods with labels that match the service
			pod1 := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod1",
					Namespace: testNamespaceName,
					Labels: map[string]string{
						"some-label": "test",
						"version":    "v1",
					},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: testSvcAccountName1,
				},
			}
			_, err = kubeClient.CoreV1().Pods(testNamespaceName).Create(context.TODO(), pod1, metav1.CreateOptions{})
			Expect(err).To(BeNil())
			<-podsChannel

			pod2 := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod2",
					Namespace: testNamespaceName,
					Labels: map[string]string{
						"some-label": "test",
						"version":    "v2",
					},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: testSvcAccountName2,
				},
			}
			_, err = kubeClient.CoreV1().Pods(testNamespaceName).Create(context.TODO(), pod2, metav1.CreateOptions{})
			Expect(err).To(BeNil())
			<-podsChannel

			// Create a service with selector that matches the pods above
			svc := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-1",
					Namespace: testNamespaceName,
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{{
						Name:     "servicePort",
						Protocol: corev1.ProtocolTCP,
						Port:     tests.ServicePort,
					}},
					Selector: map[string]string{
						"some-label": "test",
					},
				},
			}

			_, err := kubeClient.CoreV1().Services(testNamespaceName).Create(context.TODO(), svc, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())
			<-serviceChannel

			meshSvc := service.MeshService{
				Name:      svc.Name,
				Namespace: svc.Namespace,
			}

			svcAccounts, err := kubeController.ListServiceIdentitiesForService(meshSvc)

			Expect(err).ToNot(HaveOccurred())

			expectedSvcAccounts := []identity.K8sServiceAccount{
				{Name: pod1.Spec.ServiceAccountName, Namespace: pod1.Namespace},
				{Name: pod2.Spec.ServiceAccountName, Namespace: pod2.Namespace},
			}
			Expect(svcAccounts).Should(HaveLen(len(expectedSvcAccounts)))
			Expect(svcAccounts).Should(ConsistOf(expectedSvcAccounts))
		})

		It("should not return any ServiceAccounts associated with a service without selectors", func() {
			testNamespaceName := "test-ns"
			testSvcAccountName1 := "test-service-account-1"
			testSvcAccountName2 := "test-service-account-2"

			serviceChannel := events.Subscribe(announcements.ServiceAdded,
				announcements.ServiceDeleted,
				announcements.ServiceUpdated)
			defer events.Unsub(serviceChannel)
			podsChannel := events.Subscribe(announcements.PodAdded,
				announcements.PodDeleted,
				announcements.PodUpdated)
			defer events.Unsub(podsChannel)

			// Create a namespace
			testNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   testNamespaceName,
					Labels: map[string]string{constants.OSMKubeResourceMonitorAnnotation: testMeshName},
				},
			}
			_, err = kubeClient.CoreV1().Namespaces().Create(context.TODO(), testNamespace, metav1.CreateOptions{})
			Expect(err).To(BeNil())
			// Wait on namespace to be ready so that resources in this namespace are marked as monitored as soon as possible
			Eventually(func() bool {
				return kubeController.IsMonitoredNamespace(testNamespace.Name)
			}, nsInformerSyncTimeout).Should(BeTrue())

			// Create pods with labels that match the service
			pod1 := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod1",
					Namespace: testNamespaceName,
					Labels: map[string]string{
						"some-label": "test",
						"version":    "v1",
					},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: testSvcAccountName1,
				},
			}
			_, err = kubeClient.CoreV1().Pods(testNamespaceName).Create(context.TODO(), pod1, metav1.CreateOptions{})
			Expect(err).To(BeNil())
			<-podsChannel

			pod2 := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod2",
					Namespace: testNamespaceName,
					Labels: map[string]string{
						"some-label": "test",
						"version":    "v2",
					},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: testSvcAccountName2,
				},
			}
			_, err = kubeClient.CoreV1().Pods(testNamespaceName).Create(context.TODO(), pod2, metav1.CreateOptions{})
			Expect(err).To(BeNil())
			<-podsChannel

			// Create a service with selector that matches the pods above
			svc := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-1",
					Namespace: testNamespaceName,
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{{
						Name:     "servicePort",
						Protocol: corev1.ProtocolTCP,
						Port:     tests.ServicePort,
					}},
				},
			}

			_, err := kubeClient.CoreV1().Services(testNamespaceName).Create(context.TODO(), svc, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())
			<-serviceChannel

			meshSvc := service.MeshService{
				Name:      svc.Name,
				Namespace: svc.Namespace,
			}

			svcAccounts, err := kubeController.ListServiceIdentitiesForService(meshSvc)

			Expect(err).ToNot(HaveOccurred())

			var expectedSvcAccounts []identity.K8sServiceAccount
			Expect(svcAccounts).Should(HaveLen(0))
			Expect(svcAccounts).Should(ConsistOf(expectedSvcAccounts))
		})

	})

})

func TestGetEndpoint(t *testing.T) {
	assert := tassert.New(t)

	// Create kubernetes controller
	kubeClient := testclient.NewSimpleClientset()
	stop := make(chan struct{})
	kubeController, err := NewKubernetesController(kubeClient, nil, testMeshName, stop)
	assert.Nil(err)
	assert.NotNil(kubeController)

	endpointsChannel := events.Subscribe(announcements.EndpointAdded,
		announcements.EndpointDeleted,
		announcements.EndpointUpdated)
	defer events.Unsub(endpointsChannel)

	// Create a namespace
	testNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   tests.BookbuyerService.Namespace,
			Labels: map[string]string{constants.OSMKubeResourceMonitorAnnotation: testMeshName},
		},
	}
	_, err = kubeClient.CoreV1().Namespaces().Create(context.TODO(), testNamespace, metav1.CreateOptions{})
	assert.Nil(err)
	// Wait on namespace to be ready
	assert.Eventually(func() bool {
		return kubeController.IsMonitoredNamespace(tests.BookbuyerService.Namespace)
	}, nsInformerSyncTimeout, assertEventuallyPollingInterval)

	testEndpoint := corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tests.BookbuyerService.Name,
			Namespace: tests.BookbuyerService.Namespace,
		},
	}

	// Create endpoint
	expectedEndpoint, err := kubeClient.CoreV1().Endpoints(tests.BookbuyerService.Namespace).Create(context.TODO(), &testEndpoint, metav1.CreateOptions{})
	assert.Nil(err)
	<-endpointsChannel

	endpoint, err := kubeController.GetEndpoints(tests.BookbuyerService)
	assert.Nil(err)
	assert.NotNil(endpoint)
	assert.Equal(expectedEndpoint, endpoint)

	// Delete it
	err = kubeClient.CoreV1().Endpoints(tests.BookbuyerService.Namespace).Delete(context.TODO(), tests.BookbuyerService.Name, metav1.DeleteOptions{})
	assert.Nil(err)
	<-endpointsChannel

	// Check it is gone
	endpoint, err = kubeController.GetEndpoints(tests.BookbuyerService)
	assert.Nil(err)
	assert.Nil(endpoint)
}

func TestIsMetricsEnabled(t *testing.T) {
	testCases := []struct {
		name                    string
		addPrometheusAnnotation bool
		expectedMetricsScraping bool
		scrapingAnnotation      string
	}{
		{
			name:                    "pod without prometheus scraping annotation",
			scrapingAnnotation:      "false",
			addPrometheusAnnotation: false,
			expectedMetricsScraping: false,
		},
		{
			name:                    "pod with prometheus scraping annotation set to true",
			scrapingAnnotation:      "true",
			addPrometheusAnnotation: true,
			expectedMetricsScraping: true,
		},
		{
			name:                    "pod with prometheus scraping annotation set to false",
			scrapingAnnotation:      "false",
			addPrometheusAnnotation: true,
			expectedMetricsScraping: false,
		},
		{
			name:                    "pod with incorrect prometheus scraping annotation",
			scrapingAnnotation:      "no",
			addPrometheusAnnotation: true,
			expectedMetricsScraping: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert := tassert.New(t)

			kubeClient := testclient.NewSimpleClientset()
			stop := make(chan struct{})
			kubeController, err := NewKubernetesController(kubeClient, nil, testMeshName, stop)
			assert.Nil(err)
			assert.NotNil(kubeController)

			proxyUUID := uuid.New()
			namespace := uuid.New().String()
			podlabels := map[string]string{
				tests.SelectorKey:                tests.SelectorValue,
				constants.EnvoyUniqueIDLabelName: proxyUUID.String(),
			}

			// Ensure correct presetup
			pods, err := kubeClient.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{})
			assert.Nil(err)
			assert.Len(pods.Items, 0)

			newPod1 := tests.NewPodFixture(namespace, fmt.Sprintf("pod-1-%s", proxyUUID), tests.BookstoreServiceAccountName, podlabels)

			if tc.addPrometheusAnnotation {
				newPod1.Annotations = map[string]string{
					constants.PrometheusScrapeAnnotation: tc.scrapingAnnotation,
				}
			}
			_, err = kubeClient.CoreV1().Pods(namespace).Create(context.TODO(), &newPod1, metav1.CreateOptions{})
			assert.Nil(err)

			// Ensure correct setup
			pods, err = kubeClient.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{})
			assert.Nil(err)
			assert.Len(pods.Items, 1)

			actual := kubeController.IsMetricsEnabled(&newPod1)
			assert.Equal(actual, tc.expectedMetricsScraping)
		})
	}
}

func TestUpdateStatus(t *testing.T) {
	testCases := []struct {
		name             string
		existingResource interface{}
		updatedResource  interface{}
		expectErr        bool
	}{
		{
			name: "valid IngressBackend resource",
			existingResource: &policyv1alpha1.IngressBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ingress-backend-1",
					Namespace: "test",
				},
				Spec: policyv1alpha1.IngressBackendSpec{
					Backends: []policyv1alpha1.BackendSpec{
						{
							Name: "backend1",
							Port: policyv1alpha1.PortSpec{
								Number:   80,
								Protocol: "http",
							},
						},
					},
					Sources: []policyv1alpha1.IngressSourceSpec{
						{
							Kind:      "Service",
							Name:      "client",
							Namespace: "foo",
						},
					},
				},
			},
			updatedResource: &policyv1alpha1.IngressBackend{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ingress-backend-1",
					Namespace: "test",
				},
				Spec: policyv1alpha1.IngressBackendSpec{
					Backends: []policyv1alpha1.BackendSpec{
						{
							Name: "backend1",
							Port: policyv1alpha1.PortSpec{
								Number:   80,
								Protocol: "http",
							},
						},
					},
					Sources: []policyv1alpha1.IngressSourceSpec{
						{
							Kind:      "Service",
							Name:      "client",
							Namespace: "foo",
						},
					},
				},
				Status: policyv1alpha1.IngressBackendStatus{
					CurrentStatus: "valid",
					Reason:        "valid",
				},
			},
		}, {
			name:             "unsupported resource",
			existingResource: &policyv1alpha1.Egress{},
			updatedResource:  &policyv1alpha1.Egress{},
			expectErr:        true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			a := tassert.New(t)
			kubeClient := testclient.NewSimpleClientset()
			policyClient := fakePolicyClient.NewSimpleClientset(tc.existingResource.(runtime.Object))

			c, err := NewKubernetesController(kubeClient, policyClient, testMeshName, make(chan struct{}))
			a.Nil(err)

			_, err = c.UpdateStatus(tc.updatedResource)
			a.Equal(tc.expectErr, err != nil)
		})
	}
}
